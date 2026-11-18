package webapi

import (
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/chainreactors/cyber/agent"
)

const (
	// scheduleInterval 是调度器检查到期计划的周期。
	scheduleInterval = 30 * time.Second
	// scheduleMaxConcurrent 是计划任务研究会话 + 计划后续（验证/复测）会话合计的全局并发上限：
	// 达到上限时本轮不再发起，到期计划保持到期、后续项保持 pending，下轮再试（不记 run）。
	scheduleMaxConcurrent = 2
	maxCronLength         = 128
	scheduleUpcomingCount = 3
)

// scheduleTaskNamespace 是计划任务 task_id 的 UUIDv5 命名空间（固定随机值）。
var scheduleTaskNamespace = [16]byte{0x5c, 0x3e, 0x2a, 0x71, 0x0d, 0x4b, 0x4f, 0x8e, 0x9a, 0x61, 0x27, 0xc4, 0x13, 0xb8, 0x6e, 0x05}

// scheduleTaskID 由 asset_id + scheduled_for 确定性生成（UUIDv5），使重复检查/Hub 重启
// 不会重复发起（LaunchAssetTask 按 task_id 幂等）。
func scheduleTaskID(assetID, scheduledFor string) string {
	return scheduleUUID(assetID, scheduledFor)
}

// scheduleUUID 是计划相关确定性 ID：UUIDv5(固定命名空间, 各部分以换行连接)。
func scheduleUUID(parts ...string) string {
	hash := sha1.New()
	hash.Write(scheduleTaskNamespace[:])
	hash.Write([]byte(strings.Join(parts, "\n")))
	sum := hash.Sum(nil)
	sum[6] = (sum[6] & 0x0f) | 0x50
	sum[8] = (sum[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}

// parseScheduleCron 用上游 agent.ParseCron 校验 5 字段 cron，并要求未来有触发时间。
func parseScheduleCron(raw string, now time.Time) (*agent.CronExpr, error) {
	expr := strings.TrimSpace(raw)
	if expr == "" {
		return nil, errors.New("cron 不能为空")
	}
	if len(expr) > maxCronLength {
		return nil, fmt.Errorf("cron 不能超过 %d 字符", maxCronLength)
	}
	cron, err := agent.ParseCron(expr)
	if err != nil {
		return nil, fmt.Errorf("cron 非法：%v", err)
	}
	if cron.Next(now).IsZero() {
		return nil, errors.New("cron 在未来 4 年内没有触发时间")
	}
	return cron, nil
}

// scheduler 是随 Capability 生命周期运行的单 goroutine。
type scheduler struct {
	once   sync.Once
	cancel context.CancelFunc
	done   chan struct{}
}

func (c *Capability) scheduleClock() time.Time {
	if c.scheduleNow != nil {
		return c.scheduleNow()
	}
	return time.Now()
}

// scheduleLocation 是解释 cron 的时区：Hub 本地时区（测试可注入）。
func (c *Capability) scheduleLocation() *time.Location {
	if c.scheduleLoc != nil {
		return c.scheduleLoc
	}
	return time.Local
}

// startScheduler 只启动一次；首次检查在一个周期后（让本机模型节点先连上 Hub）。
func (c *Capability) startScheduler() {
	c.sched.once.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		c.sched.cancel, c.sched.done = cancel, make(chan struct{})
		go func() {
			defer close(c.sched.done)
			ticker := time.NewTicker(scheduleInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					_ = c.runDueSchedules(ctx)
				}
			}
		}()
	})
}

func (c *Capability) stopScheduler() {
	c.sched.once.Do(func() {}) // 未启动的不再启动
	if c.sched.cancel != nil {
		c.sched.cancel()
		<-c.sched.done
	}
}

// scheduledTaskRunning 判断计划任务的研究会话是否仍在运行（依据见 apiLauncher.SessionRunning）。
func (c *Capability) scheduledTaskRunning(ctx context.Context, taskID string) (bool, error) {
	if c.launcher == nil || taskID == "" {
		return false, nil
	}
	task, err := c.store.Get(ctx, taskID)
	if err != nil || task == nil {
		return false, err
	}
	return c.launcher.SessionRunning(ctx, task.InitialSessionID)
}

// runDueSchedules 处理一次检查：对每个到期计划补跑最近一次应运行时间（停机错过多次只补一次），
// 然后 next_run_at 按 cron 推进到 now 之后。返回首个存储错误（单个计划的发起失败记入 run）。
// 之后处理计划后续（候选验证/已知漏洞复测）。研究优先：先发到期计划，剩余名额再给后续；
// 全局并发 = 运行中的计划研究 + 运行中的后续会话。
func (c *Capability) runDueSchedules(ctx context.Context) error {
	now := c.scheduleClock().In(c.scheduleLocation())
	due, err := c.store.dueSchedules(ctx, now)
	if err != nil {
		return err
	}
	states, running, err := c.loadFollowupStates(ctx)
	if err != nil {
		return err
	}
	latest, err := c.store.latestLaunchedRuns(ctx)
	if err != nil {
		return err
	}
	for _, run := range latest {
		active, err := c.scheduledTaskRunning(ctx, run.TaskID)
		if err != nil || active {
			running++ // 无法核对时按运行中计，宁可少发
		}
	}
	var firstErr error
	keep := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	for _, item := range due {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		launched, err := c.processDueSchedule(ctx, item, now, running)
		keep(err)
		if launched {
			running++
		}
	}
	keep(c.processFollowups(ctx, states, running))
	return firstErr
}

// processDueSchedule 处理单个到期计划；返回是否本次发起成功（计入并发）。
func (c *Capability) processDueSchedule(ctx context.Context, item assetScheduleModel, now time.Time, running int) (bool, error) {
	newRun := func(scheduledFor, taskID, status, message string) scheduleRunModel {
		return scheduleRunModel{ID: "wsr_" + newSessionID() + newSessionID(), AssetID: item.AssetID, ScheduledFor: scheduledFor,
			TaskID: taskID, Status: status, Error: message, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	}
	next, err := parseScheduleTime(item.NextRunAt)
	if err != nil {
		// 不可解析的 next_run_at 不应出现（只由本代码写）；按现在重算，避免卡死。
		return false, c.store.recordScheduleRun(ctx, newRun(formatScheduleTime(now.Truncate(time.Minute)), "", ScheduleRunFailed, "next_run_at 损坏，已按当前时间重算"), item.NextRunAt, c.nextAfter(item.Cron, now))
	}
	cron, err := agent.ParseCron(item.Cron)
	if err != nil {
		return false, c.store.recordScheduleRun(ctx, newRun(item.NextRunAt, "", ScheduleRunFailed, "cron 非法："+err.Error()), item.NextRunAt, "")
	}
	// 停机期间错过多次：只补最近一次应运行时间。
	scheduledAt := next.In(now.Location())
	for {
		candidate := cron.Next(scheduledAt)
		if candidate.IsZero() || candidate.After(now) {
			break
		}
		scheduledAt = candidate
	}
	scheduledFor := formatScheduleTime(scheduledAt)
	newNext := formatScheduleTime(cron.Next(now))
	record := func(taskID, status, message string) error {
		return c.store.recordScheduleRun(ctx, newRun(scheduledFor, taskID, status, message), item.NextRunAt, newNext)
	}

	// 发起前重读：期间被人停用/改动则按最新状态处理。
	current, err := c.store.schedule(ctx, item.AssetID)
	if err != nil {
		return false, err
	}
	if current == nil || current.NextRunAt != item.NextRunAt && current.Enabled {
		return false, nil // 已删除或已重新保存（新 next_run_at），交下轮按新计划处理
	}
	if !current.Enabled {
		return false, record("", ScheduleRunSkipped, "计划已停用")
	}
	asset, err := c.store.assetModel(ctx, item.AssetID)
	if err != nil {
		return false, err
	}
	if asset == nil {
		return false, record("", ScheduleRunSkipped, "资产不存在")
	}
	if asset.Archived {
		return false, record("", ScheduleRunSkipped, "资产已归档")
	}
	previous, err := c.store.latestLaunchedRun(ctx, item.AssetID, scheduledFor)
	if err != nil {
		return false, err
	}
	if previous != nil {
		active, err := c.scheduledTaskRunning(ctx, previous.TaskID)
		if err != nil {
			return false, nil // 暂不能核对：保持到期，下轮再试
		}
		if active {
			return false, record("", ScheduleRunSkipped, "上一次计划任务仍在运行（任务 "+previous.TaskID+"）")
		}
	}
	if running >= scheduleMaxConcurrent {
		return false, nil // 并发已满：保持到期，下轮再试，不记 run
	}
	taskID := scheduleTaskID(item.AssetID, scheduledFor)
	_, err = c.LaunchAssetTask(ctx, item.AssetID, taskID, AssetLaunchOptions{})
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err() // Hub 关闭中断：不记失败，重启后同 task_id 续完
		}
		message, recordedTask := err.Error(), ""
		var launchErr *AssetLaunchError
		if errors.As(err, &launchErr) {
			message = launchErr.Stage + "：" + launchErr.Msg
			if launchErr.Result != nil && launchErr.Result.Progress.TaskCreated {
				recordedTask = taskID // 任务已建（部分完成），便于查看
			}
		}
		return false, record(recordedTask, ScheduleRunFailed, message)
	}
	return true, record(taskID, ScheduleRunLaunched, "")
}

// nextAfter 计算 cron 在 t 之后的下一次触发（UTC 存储格式）；非法或无触发返回空串。
func (c *Capability) nextAfter(expr string, t time.Time) string {
	cron, err := agent.ParseCron(expr)
	if err != nil {
		return ""
	}
	return formatScheduleTime(cron.Next(t.In(c.scheduleLocation())))
}

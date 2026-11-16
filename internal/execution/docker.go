package execution

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"wscyber/internal/domain"
)

const executionLabel = "io.wscyber.execution"

// Docker CLI 不继承服务端凭据、代理或 Docker context；首期只支持本机标准 Unix socket。
func runDocker(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	args = append([]string{"--host=unix:///var/run/docker.sock"}, args...)
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Env = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/nonexistent", "DOCKER_CONFIG=/nonexistent", "LC_ALL=C",
	}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = 2 * time.Second
	return cmd.Run()
}

func (e *Executor) createArgs(name, token, command string) []string {
	return []string{
		"create", "--name", name, "--label", executionLabel + "=" + token,
		"--runtime", e.config.Runtime, "--pull", "never", "--init",
		"--network", e.config.Network, "--user", "65534:65534", "--read-only",
		"--cap-drop", "ALL", "--security-opt", "no-new-privileges:true",
		"--pids-limit", "64", "--memory", "256m", "--memory-swap", "256m", "--cpus", "1",
		"--ulimit", "nofile=1024:1024", "--ipc", "none", "--log-driver", "none", "--no-healthcheck",
		"--tmpfs", "/tmp:rw,nosuid,nodev,size=512m,mode=1777",
		"--workdir", "/tmp", "--entrypoint", "/usr/bin/env",
		e.config.Image,
		"-i", "HOME=/tmp", "TMPDIR=/tmp", "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		// 容器内也限时：控制面崩溃后仍会到期退出，不依赖宿主 Go context 存活。
		"LANG=C.UTF-8", "/usr/bin/timeout", "--signal=TERM", "--kill-after=2s", fmt.Sprintf("%.3fs", e.config.Timeout.Seconds()), "/bin/sh", "-c", command,
	}
}

func (e *Executor) executeCommand(ctx context.Context, command string) (result domain.ActionResult, err error) {
	result.ExitCode = -1
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return result, fmt.Errorf("生成沙箱唯一标识失败：%w", err)
	}
	token := hex.EncodeToString(random)
	name := "wscyber-exec-" + token
	containerID := ""
	creationConfirmed := false
	// 不使用 --rm：超时杀掉 CLI 后仍必须主动清理并确认本次容器已不存在。
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cancel()
		cleanupErr := e.cleanupContainer(cleanupCtx, name, token, containerID, creationConfirmed)
		if cleanupErr != nil {
			result.EffectUnknown = true
			err = errors.Join(err, cleanupErr)
		}
	}()
	out, stderr, createErr := e.dockerMetadata(ctx, e.createArgs(name, token, command))
	if createErr != nil {
		return result, fmt.Errorf("创建命令沙箱失败：%w；%s", createErr, strings.TrimSpace(stderr))
	}
	createdID := strings.TrimSpace(out)
	if !validContainerID(createdID) {
		return result, fmt.Errorf("Docker 未返回可确认的容器 ID")
	}
	ownerID, _, ownerErr := e.ownedContainer(ctx, name, token)
	if ownerErr != nil || ownerID != createdID {
		return result, fmt.Errorf("无法确认新建沙箱的唯一归属：%v", ownerErr)
	}
	containerID = ownerID
	creationConfirmed = true
	budget := &outputBudget{remaining: e.config.MaxOutputBytes}
	stdout := &boundedWriter{budget: budget}
	stderrOutput := &boundedWriter{budget: budget}
	runErr := e.docker(ctx, []string{"start", "--attach", containerID}, stdout, stderrOutput)
	result.Stdout, result.Stderr = stdout.bytes(), stderrOutput.bytes()
	result.Truncated = budget.wasTruncated()
	// 独立检查容器状态；CLI 返回码不能替代容器进程的退出状态。
	stateCtx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	stateOut, stateErrText, stateErr := e.dockerMetadata(stateCtx, []string{"inspect", "--type", "container", "--format", "{{json .State}}", containerID})
	var state struct {
		Status    string `json:"Status"`
		Running   bool   `json:"Running"`
		ExitCode  int    `json:"ExitCode"`
		Error     string `json:"Error"`
		OOMKilled bool   `json:"OOMKilled"`
	}
	if stateErr == nil {
		stateErr = json.Unmarshal([]byte(stateOut), &state)
	}
	if ctx.Err() != nil {
		result.Truncated = true
		result.EffectUnknown = true
		return result, fmt.Errorf("本地命令超时或取消，不能据此认为命令未执行：%w", ctx.Err())
	}
	if stateErr != nil || state.Running || state.Status != "exited" {
		result.EffectUnknown = true
		result.Truncated = true
		return result, fmt.Errorf("无法确认本地命令已正常结束：%v；%s", errors.Join(runErr, stateErr), strings.TrimSpace(stateErrText))
	}
	result.ExitCode = state.ExitCode
	if state.ExitCode != 0 || state.OOMKilled {
		return result, fmt.Errorf("本地命令退出码 %d（内存限制终止：%t）", state.ExitCode, state.OOMKilled)
	}
	if runErr != nil || state.Error != "" {
		result.EffectUnknown = true
		result.Truncated = true
		return result, fmt.Errorf("沙箱执行回执异常：%v；%s", runErr, state.Error)
	}
	return result, nil
}

func (e *Executor) dockerMetadata(ctx context.Context, args []string) (string, string, error) {
	budget := &outputBudget{remaining: 16 << 10}
	stdout, stderr := &boundedWriter{budget: budget}, &boundedWriter{budget: budget}
	err := e.docker(ctx, args, stdout, stderr)
	if budget.wasTruncated() {
		err = errors.Join(err, fmt.Errorf("Docker 控制回执超过上限"))
	}
	return string(stdout.bytes()), string(stderr.bytes()), err
}

func (e *Executor) ownedContainer(ctx context.Context, name, token string) (id string, absent bool, err error) {
	out, stderr, err := e.dockerMetadata(ctx, []string{
		"inspect", "--type", "container", "--format", "{{.Id}} {{index .Config.Labels \"" + executionLabel + "\"}}", name,
	})
	if err != nil {
		if missingContainer(stderr, name) {
			return "", true, nil
		}
		return "", false, fmt.Errorf("无法核验沙箱归属：%w；%s", err, strings.TrimSpace(stderr))
	}
	fields := strings.Fields(out)
	if len(fields) != 2 || !validContainerID(fields[0]) || fields[1] != token {
		return "", false, fmt.Errorf("拒绝操作标签不匹配的容器")
	}
	return fields[0], false, nil
}

func (e *Executor) cleanupContainer(ctx context.Context, name, token, id string, creationConfirmed bool) error {
	if id == "" {
		foundID, absent, err := e.ownedContainer(ctx, name, token)
		if err != nil {
			return fmt.Errorf("沙箱清理未确认（%s）：%w", name, err)
		}
		if absent {
			if !creationConfirmed {
				// 创建 RPC 可能在 CLI 退出后继续执行；一次查无结果不等于绝无迟到容器。
				return fmt.Errorf("沙箱创建回执未知；当前未发现 %s，但不能确认没有迟到容器", name)
			}
			return nil
		}
		id = foundID
	}
	_, removeText, removeErr := e.dockerMetadata(ctx, []string{"rm", "--force", "--volumes", id})
	_, inspectText, inspectErr := e.dockerMetadata(ctx, []string{"inspect", "--type", "container", "--format", "{{.Id}}", id})
	if inspectErr != nil && missingContainer(inspectText, id) {
		return nil
	}
	return fmt.Errorf("未确认沙箱 %s 已删除：%v；%s；%s", name, errors.Join(removeErr, inspectErr), strings.TrimSpace(removeText), strings.TrimSpace(inspectText))
}

func validContainerID(id string) bool {
	if len(id) != 64 {
		return false
	}
	for _, c := range id {
		if c < '0' || c > '9' {
			if c < 'a' || c > 'f' {
				return false
			}
		}
	}
	return true
}

func missingContainer(stderr, id string) bool {
	text := strings.TrimSpace(stderr)
	return text == "Error: No such object: "+id ||
		text == "Error: No such container: "+id ||
		text == "Error response from daemon: No such container: "+id
}

// stdout/stderr 共用预算，超限后继续消费以免堵塞子进程，但不再扩大内存。
type outputBudget struct {
	mu        sync.Mutex
	remaining int64
	truncated bool
}

type boundedWriter struct {
	budget *outputBudget
	buf    bytes.Buffer
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	w.budget.mu.Lock()
	defer w.budget.mu.Unlock()
	n := len(p)
	keep := min(int64(n), w.budget.remaining)
	w.buf.Write(p[:keep])
	w.budget.remaining -= keep
	if keep < int64(n) {
		w.budget.truncated = true
	}
	return n, nil
}

func (w *boundedWriter) bytes() []byte {
	w.budget.mu.Lock()
	defer w.budget.mu.Unlock()
	return bytes.Clone(w.buf.Bytes())
}

func (b *outputBudget) wasTruncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}

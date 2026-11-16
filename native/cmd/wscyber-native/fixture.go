package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/chainreactors/cyber/agent/provider"
	agentsession "github.com/chainreactors/cyber/agent/session"
	"github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/eventbus"
	"github.com/chainreactors/cyber/core/extension"
	coreproc "github.com/chainreactors/cyber/core/proc"
	coretool "github.com/chainreactors/cyber/core/tool"
	terminalext "github.com/chainreactors/cyber/exts/terminal"
	"github.com/chainreactors/cyber/pkg/harness"
	"github.com/chainreactors/utils/proc"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const (
	fixtureMarker  = "WSCYBER_H1_NATIVE_TOOL"
	fixtureCommand = "/usr/bin/printf 'WSCYBER_H1_NATIVE_TOOL\\n'"
	fixtureCallID  = "fixture-bash-1"
)

// fixtureProvider 只提供模型边界的确定性响应，不执行工具或控制运行循环。
// StandardLoop 决定调用、工具调度、结果回流及终止；第二次请求必须携带真实工具结果。
type fixtureProvider struct {
	cancel   context.CancelFunc
	scenario string
	calls    int
	sawTool  bool
}

func (*fixtureProvider) Name() string { return "wscyber-h1-fixture" }

func (p *fixtureProvider) ChatCompletion(ctx context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.calls++
	switch p.calls {
	case 1:
		found := false
		for _, tool := range req.Tools {
			if tool.Name == "bash" {
				found = true
			}
		}
		if !found {
			return nil, errors.New("原生 bash 工具未发布")
		}
		args, err := aop.JSONValue(map[string]any{"command": fixtureCommand, "timeout": 5})
		if err != nil {
			return nil, err
		}
		return &provider.ChatCompletionResponse{Choices: []provider.Choice{{
			Message:      &aop.Message{Role: "assistant", Content: []*aop.Content{{Value: &aop.Content_ToolCall{ToolCall: &aop.ToolCall{Id: fixtureCallID, Name: "bash", Arguments: args}}}}},
			FinishReason: "tool_calls",
		}}}, nil
	case 2:
		for _, message := range req.Messages {
			result := provider.MessageToolResult(message)
			if result != nil && result.CallId == fixtureCallID && !result.IsError && strings.Contains(coretool.ResultText(result), fixtureMarker) {
				p.sawTool = true
			}
		}
		if !p.sawTool {
			return nil, errors.New("模型边界未收到真实 bash 结果")
		}
		if p.scenario == "cancel-after-tool" {
			p.cancel()
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return &provider.ChatCompletionResponse{Choices: []provider.Choice{{Message: provider.TextMessage("assistant", "fixture：已收到原生 bash 结果"), FinishReason: "stop"}}}, nil
	default:
		return nil, errors.New("fixture 不允许额外模型请求")
	}
}

// 这是验收摘要，不是第二套运行状态或业务事件；原生事件原样另存 JSONL。
type fixtureReport struct {
	Fixture           bool        `json:"fixture"`
	Scenario          string      `json:"scenario"`
	Workspace         string      `json:"workspace"`
	SessionID         string      `json:"session_id"`
	TurnID            string      `json:"turn_id"`
	ToolCallID        string      `json:"tool_call_id"`
	ToolOutput        string      `json:"tool_output"`
	Output            string      `json:"output"`
	StopReason        string      `json:"stop_reason"`
	ProviderCalls     int         `json:"provider_calls"`
	ProviderSawTool   bool        `json:"provider_saw_tool"`
	NativeEvents      int         `json:"native_events"`
	Canceled          bool        `json:"canceled"`
	SessionClosed     bool        `json:"session_closed"`
	ObserverClosed    bool        `json:"observer_closed"`
	HarnessClosed     bool        `json:"harness_closed"`
	WorkspaceRemoved  bool        `json:"workspace_removed"`
	Processes         []proc.Info `json:"native_processes"`
	NoActiveProcesses bool        `json:"no_active_processes"`
	Error             string      `json:"error,omitempty"`
}

func runFixture(ctx context.Context, root, scenario string, eventOutput io.Writer) (report fixtureReport, resultErr error) {
	report.Fixture, report.Scenario = true, scenario
	workspace, err := os.MkdirTemp(root, "wscyber-native-h1-")
	if err != nil {
		return report, err
	}
	report.Workspace = workspace
	defer func() {
		err := os.RemoveAll(workspace) // 仅删除本函数刚创建的唯一工作区。
		report.WorkspaceRemoved = err == nil
		resultErr = errors.Join(resultErr, err)
	}()
	requestCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	p := &fixtureProvider{scenario: scenario, cancel: cancel}
	h, err := harness.New(harness.Config{
		Base: harness.BaseConfig{
			Directory:    workspace,
			SkillExclude: []string{"cyber"},
			Provider:     provider.StartupConfig{Mode: provider.StartupDisabled},
			Egress:       harness.NoEgress(), // 只是不发布路由，不代表宿主网络隔离。
			Terminal:     terminalext.Config{Timeout: 5, MaximumTimeout: 5 * time.Second},
		},
		Session: &agentsession.Config{PrimarySessionID: "h1-fixture", SkipBaseSkills: true}, // Loop=nil：原生 StandardLoop。
		Extensions: []extension.Extension{extension.Func{LoadFunc: func(scope *extension.Scope) error {
			state, err := extension.Use[*provider.State](scope)
			if err != nil {
				return err
			}
			state.Set(p, provider.ProviderConfig{Model: "h1-fixture-no-network"})
			return nil
		}}},
	})
	if err != nil {
		return report, err
	}
	var runtime *agentsession.Runtime
	var processes *coreproc.Manager
	var subscription *eventbus.Subscription[*aop.Event]
	var mu sync.Mutex
	var observed []*aop.Event
	var writeErr error
	defer func() {
		// 不能继承取消的请求；保持 Observe 直至 SessionEnded 和 Harness 关闭。
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if runtime != nil && report.SessionID != "" {
			reason := agentsession.SessionCloseCompleted
			if report.Canceled {
				reason = agentsession.SessionCloseCanceled
			} else if resultErr != nil {
				reason = agentsession.SessionCloseError
			}
			err := runtime.CloseSession(cleanup, report.SessionID, reason)
			report.SessionClosed = err == nil
			resultErr = errors.Join(resultErr, err)
		}
		err := h.Close(cleanup)
		report.HarnessClosed = err == nil && !h.Active()
		resultErr = errors.Join(resultErr, err)
		if processes != nil {
			report.Processes = processes.List()
			report.NoActiveProcesses = true
			for _, process := range report.Processes {
				if process.State.Active() {
					report.NoActiveProcesses = false
					resultErr = errors.Join(resultErr, fmt.Errorf("原生工具资源仍活跃: %s", process.ID))
				}
			}
		}
		if subscription != nil {
			err := subscription.Close(cleanup)
			report.ObserverClosed = err == nil
			resultErr = errors.Join(resultErr, err)
		}
		mu.Lock()
		defer mu.Unlock()
		report.NativeEvents = len(observed)
		resultErr = errors.Join(resultErr, writeErr)
		if resultErr == nil {
			resultErr = validateEvents(observed, &report)
		}
	}()
	if err := h.Load(ctx); err != nil {
		return report, err
	}
	runtime, err = h.Runtime()
	if err != nil {
		return report, err
	}
	processes, err = h.Processes()
	if err != nil {
		return report, err
	}
	subscription = runtime.Observe(func(event *aop.Event) {
		mu.Lock()
		defer mu.Unlock()
		observed = append(observed, proto.Clone(event).(*aop.Event))
		data, err := protojson.Marshal(event)
		if err == nil {
			_, err = eventOutput.Write(append(data, '\n'))
		}
		writeErr = errors.Join(writeErr, err)
	})
	session, err := runtime.OpenSession(ctx, agentsession.SessionOptions{ID: "h1-fixture", SingleTask: true})
	if err != nil {
		return report, err
	}
	report.SessionID = session.ID()
	turn, err := session.Run(requestCtx, agentsession.RunInput{Content: []*aop.Content{aop.Text("显式本机 fixture：执行固定 printf，然后结束；不访问网络或其他文件。")}, MaxTurns: 3})
	if err != nil {
		return report, err
	}
	report.TurnID = turn.TurnID()
	result, runErr := turn.Wait()
	report.ProviderCalls, report.ProviderSawTool = p.calls, p.sawTool
	report.Canceled = errors.Is(runErr, context.Canceled)
	if result != nil {
		report.Output, report.StopReason = result.Output, string(result.Stop)
	}
	if scenario == "cancel-after-tool" {
		if !report.Canceled || !p.sawTool {
			return report, fmt.Errorf("预期工具返回后取消，实际 err=%v sawTool=%t", runErr, p.sawTool)
		}
	} else if runErr != nil {
		return report, runErr
	}
	if p.calls != 2 || !p.sawTool {
		return report, errors.New("原生工具结果回流断言失败")
	}
	return report, nil
}

func validateEvents(events []*aop.Event, report *fixtureReport) error {
	var started, turnStarted, turnEnded, sessionEnded, call, result bool
	var lastSeq uint64
	for _, event := range events {
		if event.SessionId != report.SessionID {
			continue
		}
		if event.Id == "" || event.EmittedAt == nil || event.Seq <= lastSeq {
			return errors.New("原生事件身份/顺序缺失")
		}
		lastSeq = event.Seq
		if event.GetSessionStarted() != nil {
			started = true
		}
		if ended := event.GetSessionEnded(); ended != nil {
			want := "completed"
			if report.Canceled {
				want = "canceled"
			}
			if ended.Reason != want {
				return fmt.Errorf("SessionEnded=%q，预期 %q", ended.Reason, want)
			}
			sessionEnded = true
		}
		if event.TurnId != report.TurnID {
			continue
		}
		if event.GetTurnStarted() != nil {
			turnStarted = true
		}
		if ended := event.GetTurnEnded(); ended != nil {
			if ended.StopReason != report.StopReason {
				return errors.New("TurnEnded 与结果停止原因不一致")
			}
			turnEnded = true
		}
		if tool := event.GetToolCall(); tool != nil && tool.Id == fixtureCallID && tool.Name == "bash" {
			args, err := aop.DecodeJSON[map[string]any](tool.Arguments)
			if err != nil || args["command"] != fixtureCommand {
				return errors.New("原生工具调用参数不匹配")
			}
			call, report.ToolCallID = true, tool.Id
		}
		if tool := event.GetToolResult(); tool != nil && tool.CallId == fixtureCallID {
			report.ToolOutput = coretool.ResultText(tool)
			result = !tool.IsError && strings.Contains(report.ToolOutput, fixtureMarker)
		}
	}
	if !started || !turnStarted || !turnEnded || !sessionEnded || !call || !result {
		return fmt.Errorf("原生事件链不完整：session=%t/%t turn=%t/%t tool=%t/%t", started, sessionEnded, turnStarted, turnEnded, call, result)
	}
	return nil
}

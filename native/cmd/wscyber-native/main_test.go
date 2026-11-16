package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chainreactors/cyber/aop"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestFixtureSuccess(t *testing.T) {
	checkFixture(t, "success", "completed")
}

func TestFixtureCancelAfterTool(t *testing.T) {
	checkFixture(t, "cancel-after-tool", "canceled")
}

func checkFixture(t *testing.T, scenario, stopReason string) {
	t.Helper()
	root := t.TempDir()
	evidence := filepath.Join(root, "evidence")
	var stdout, stderr bytes.Buffer
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if err := run(ctx, []string{"--fixture", "--scenario", scenario, "--work-root", root, "--evidence", evidence}, &stdout, &stderr); err != nil {
		t.Fatalf("fixture: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	var report fixtureReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.Fixture || report.Scenario != scenario || report.StopReason != stopReason || report.Canceled != (scenario == "cancel-after-tool") {
		t.Fatalf("错误的运行结果: %+v", report)
	}
	if !report.SessionClosed || !report.ObserverClosed || !report.HarnessClosed || !report.WorkspaceRemoved || !report.NoActiveProcesses {
		t.Fatalf("生命周期未完整收尾: %+v", report)
	}
	if _, err := os.Stat(report.Workspace); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("工作区未删除: %v", err)
	}
	if report.ProviderCalls != 2 || !report.ProviderSawTool || report.ToolCallID != fixtureCallID || !strings.Contains(report.ToolOutput, fixtureMarker) {
		t.Fatalf("缺少原生工具结果回流: %+v", report)
	}
	if len(report.Processes) != 1 {
		t.Fatalf("原生进程登记数=%d", len(report.Processes))
	}
	process := report.Processes[0]
	if process.Command != fixtureCommand || process.State != "completed" || process.ExitStatus() != 0 || process.OutputBytes == 0 {
		t.Fatalf("工具进程未真实完成: %+v", process)
	}
	data, err := os.ReadFile(filepath.Join(evidence, "native-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var events []*aop.Event
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte{'\n'}) {
		event := &aop.Event{}
		if err := protojson.Unmarshal(line, event); err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	if len(events) != report.NativeEvents {
		t.Fatal("事件文件与摘要数量不一致")
	}
	if err := validateEvents(events, &report); err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(filepath.Join(evidence, "fixture-summary.json"))
	if err != nil || !bytes.Equal(stored, stdout.Bytes()) {
		t.Fatalf("摘要落盘不一致: %v", err)
	}
}

func TestRejectUnconfiguredProduction(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(t.Context(), nil, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "拒绝启动") || stdout.Len() != 0 {
		t.Fatalf("未显式选择fixture却启动: err=%v output=%s", err, stdout.String())
	}
}

package webapi

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// 固定前端输出由 testdata/first-message-fixtures.cjs 从实际 TS 函数生成。
// 修改首条消息时重新生成，并与 Go 全文比对（含账号占位符块）。
func TestScopeFirstMessagesMatchFrontendFixtures(t *testing.T) {
	var fixtures []struct {
		Scope, Kind, Message string
	}
	raw, err := os.ReadFile("testdata/first-message-fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) != 6 {
		t.Fatalf("fixtures = %d, want 6", len(fixtures))
	}
	source := &FindingDetail{Number: "WSC-000042", Content: findingContent{
		Title: "合成接口候选", Description: "中性测试描述。",
		Steps: []findingStepInput{{Text: "读取合成接口"}}, Remediation: "核对访问控制。", Limitations: "仅合成测试。",
	}, Evidence: []FindingEvidenceSnapshot{{Method: "GET", URL: "https://api.example.test/items/1"}, {URL: "https://api.example.test/items/2"}}}
	origins := []string{"http://example.test", "https://api.example.test"}
	credentials := "\n\n测试账号（资产登记，仅用于上述授权范围内的测试）：\n- 名称：测试账号；用户名：user-a；凭据 ID：account-a；口令：用占位符 {{cred:account-a.secret}}（平台发送时替换，你看不到明文）"
	for _, fixture := range fixtures {
		t.Run(fixture.Scope+"/"+fixture.Kind, func(t *testing.T) {
			var got string
			switch fixture.Kind {
			case "research":
				got = strings.Join(webAPIFirstMessage(&Task{KnownInfo: "合成目标", Objective: "核对合成接口", Origins: origins, Policy: &TaskPolicy{Scope: fixture.Scope}}), "\n")
			case "verification":
				got = pinnedFirstMessage(verificationMessageVariant, origins, source, fixture.Scope)
			case "retest":
				got = pinnedFirstMessage(retestMessageVariant, origins, source, fixture.Scope)
			default:
				t.Fatalf("unknown kind %q", fixture.Kind)
			}
			if got+credentials != fixture.Message {
				t.Fatalf("Go/frontend message mismatch:\n%s", got+credentials)
			}
			if fixture.Scope == ScopeUnrestricted && (strings.Contains(got, "仅限以下 origin") || strings.Contains(got, "对授权 origin 发起请求")) {
				t.Fatalf("unrestricted message contains origins-only restriction: %s", got)
			}
		})
	}
}

// 计划后续与漏洞卡复测共用的启动路径：即使调用方 Task 带旧策略，也按父任务现值构造。
func TestPinnedFirstMessageUsesCurrentParentScope(t *testing.T) {
	f := newScheduleFixture(t, "2026-11-23T09:30:00Z")
	f.createAsset("a1")
	if status, _, raw := f.do("PUT", "/api/webapi/assets/a1/tasks/scope-task", ""); status != 201 {
		t.Fatalf("launch = %d %s", status, raw)
	}
	findingID, revisionID := f.addFinding("scope-task", "合成候选", simpleContent(), nil)
	task, err := f.capability.store.Get(t.Context(), "scope-task")
	if err != nil {
		t.Fatal(err)
	}
	source, err := f.capability.store.findingRevision(t.Context(), task.ID, findingID, revisionID)
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{ScopeUnrestricted, ScopeOrigins} {
		if _, err := f.capability.store.setPolicy(t.Context(), task.ID, scope, ApprovalHighRisk); err != nil {
			t.Fatal(err)
		}
		stale := ScopeUnrestricted
		if scope == ScopeUnrestricted {
			stale = ScopeOrigins
		}
		task.Policy = &TaskPolicy{Scope: stale}
		for _, variant := range []pinnedMessageVariant{verificationMessageVariant, retestMessageVariant} {
			sessionID := newSessionID()
			if err := f.capability.startPinnedSession(t.Context(), task, findingID, revisionID, sessionID, "测试", variant); err != nil {
				t.Fatal(err)
			}
			runs := f.runsTo(sessionID)
			if len(runs) != 1 || runText(runs[0]) != pinnedFirstMessage(variant, task.Origins, source, scope) {
				t.Fatalf("first message did not use current parent policy %s: %+v", scope, runs)
			}
		}
	}
}

func TestScopeMessageMatchesFrontend(t *testing.T) {
	raw, err := os.ReadFile("../ui/source-overlay/src/lib/scope-message.ts")
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{ScopeOrigins, ScopeUnrestricted} {
		for _, pinned := range []bool{false, true} {
			if !strings.Contains(string(raw), "'"+scopeMessage(scope, pinned)+"'") {
				t.Fatalf("frontend lacks scope line: %s", scopeMessage(scope, pinned))
			}
		}
	}
	for _, pinned := range []bool{false, true} {
		if scopeMessage("", pinned) != scopeMessage(ScopeOrigins, pinned) {
			t.Fatal("missing policy must preserve origins wording")
		}
	}
	defaultTask := &Task{KnownInfo: "合成目标", Objective: "合成目的", Origins: []string{"https://example.test"}}
	withoutPolicy := strings.Join(webAPIFirstMessage(defaultTask), "\n")
	defaultTask.Policy = &TaskPolicy{Scope: ScopeOrigins}
	if strings.Join(webAPIFirstMessage(defaultTask), "\n") != withoutPolicy {
		t.Fatal("missing research policy must preserve origins message")
	}
}

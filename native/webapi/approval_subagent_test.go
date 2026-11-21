package webapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	aop "github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/operation"
	coretool "github.com/chainreactors/cyber/core/tool"
)

// M5-0 用例改为 M5a 语义：即使任务关闭审批，匿名/非研究会话的 subagent 仍被平台准入拒绝，普通工具不受影响。
func TestApprovalSubagentDeniedForAllSessions(t *testing.T) {
	capability, _, task, inv, _, _ := newHTTPToolFixture(t, http.NotFoundHandler())
	if _, err := capability.store.setPolicy(t.Context(), task.ID, ScopeUnrestricted, ApprovalOff); err != nil {
		t.Fatal(err)
	}
	h := newApprovalHarness(t, capability)
	cases := map[string]string{
		inv.SessionID:   "只开放命名子 Agent oracle_explorer",
		"ordinary-chat": "只开放命名子 Agent oracle_explorer",
		"":              "只开放命名子 Agent oracle_explorer",
	}
	for sessionID, want := range cases {
		t.Run("session="+sessionID, func(t *testing.T) {
			inv := operation.Invocation{SessionID: sessionID, TurnID: "subagent-turn", CallID: "subagent-call"}
			called := false
			run := func(context.Context, string) (*aop.ToolResult, error) {
				called = true
				return &aop.ToolResult{}, nil
			}
			result, err := h.execute(t.Context(), inv, "subagent", `{"prompt":"not executed"}`, run)
			if called || !strings.Contains(coretool.ResultText(result)+fmt.Sprint(err), "已拒绝 subagent："+want) {
				t.Fatalf("subagent denial: called=%v result=%v err=%v", called, result, err)
			}
			// 命名 oracle_explorer 但调用方不是研究会话（或 prompt 无 Oracle）同样拒绝。
			result, err = h.execute(t.Context(), inv, "subagent", `{"name":"oracle_explorer","prompt":"O-001"}`, run)
			if called || !strings.Contains(coretool.ResultText(result)+fmt.Sprint(err), "已拒绝 subagent") {
				t.Fatalf("named subagent: called=%v result=%v err=%v", called, result, err)
			}
			inv.CallID = "read-call"
			if _, err := h.execute(t.Context(), inv, "read", `{}`, run); err != nil || !called {
				t.Fatalf("ordinary tool affected: called=%v err=%v", called, err)
			}
		})
	}
	if states := h.states(); len(states) != 0 {
		t.Fatalf("subagent admission should not request approval: %v", states)
	}
}

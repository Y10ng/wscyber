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

func TestApprovalSubagentDeniedForAllSessions(t *testing.T) {
	capability, _, task, inv, _, _ := newHTTPToolFixture(t, http.NotFoundHandler())
	// 关闭任务审批也不能放行尚无任务归属的子 Agent。
	if _, err := capability.store.setPolicy(t.Context(), task.ID, ScopeUnrestricted, ApprovalOff); err != nil {
		t.Fatal(err)
	}
	h := newApprovalHarness(t, capability)
	for _, sessionID := range []string{inv.SessionID, "ordinary-chat", ""} {
		t.Run("session="+sessionID, func(t *testing.T) {
			inv := operation.Invocation{SessionID: sessionID, TurnID: "subagent-turn", CallID: "subagent-call"}
			called := false
			run := func(context.Context, string) (*aop.ToolResult, error) {
				called = true
				return &aop.ToolResult{}, nil
			}
			result, err := h.execute(t.Context(), inv, "subagent", `{"prompt":"not executed"}`, run)
			if called || !strings.Contains(coretool.ResultText(result)+fmt.Sprint(err), "平台尚未接入子 Agent 的任务归属、出口与预算，已拒绝 subagent") {
				t.Fatalf("subagent denial: called=%v result=%v err=%v", called, result, err)
			}
			inv.CallID = "read-call"
			if _, err := h.execute(t.Context(), inv, "read", `{}`, run); err != nil || !called {
				t.Fatalf("ordinary tool affected: called=%v err=%v", called, err)
			}
		})
	}
	if states := h.states(); len(states) != 0 {
		t.Fatalf("unconditional denial should not request approval: %v", states)
	}
}

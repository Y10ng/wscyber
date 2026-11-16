package policy

import (
	"testing"

	"wscyber/internal/domain"
)

func TestDefaultAllowAndRequestSignals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		input  domain.ActionInput
		status string
	}{
		{"ordinary GET", domain.ActionInput{Kind: "http", URL: "https://target.test/users"}, "approved"},
		{"ordinary POST", domain.ActionInput{Kind: "http", Method: "POST", URL: "https://target.test/search", Body: `{"query":"test"}`}, "approved"},
		{"ordinary PATCH", domain.ActionInput{Kind: "http", Method: "PATCH", URL: "https://target.test/user", Body: `{"name":"test"}`}, "approved"},
		{"reason is not request", domain.ActionInput{Kind: "http", URL: "https://target.test/status", Reason: "do not delete"}, "approved"},
		{"non destructive word", domain.ActionInput{Kind: "http", URL: "https://target.test/dropdown"}, "approved"},
		{"tool no per-command gate", domain.ActionInput{Kind: "local_command", Command: "tool --help"}, "approved"},
		{"DELETE method", domain.ActionInput{Kind: "http", Method: "DELETE", URL: "https://target.test/users/1"}, "pending"},
		{"GET delete", domain.ActionInput{Kind: "http", URL: "https://target.test/delete"}, "pending"},
		{"encoded path", domain.ActionInput{Kind: "http", URL: "https://target.test/%64elete"}, "pending"},
		{"double encoded path", domain.ActionInput{Kind: "http", URL: "https://target.test/%2564elete"}, "pending"},
		{"RPC query", domain.ActionInput{Kind: "http", URL: "https://target.test/rpc?action=deleteUser"}, "pending"},
		{"camel path", domain.ActionInput{Kind: "http", URL: "https://target.test/batchDelete"}, "pending"},
		{"POST JSON", domain.ActionInput{Kind: "http", URL: "https://target.test/rpc", Body: `{"action":"delete"}`}, "pending"},
		{"escaped JSON", domain.ActionInput{Kind: "http", URL: "https://target.test/rpc", Body: `{"action":"` + string(rune(92)) + `u0064elete"}`}, "pending"},
		{"nested JSON", domain.ActionInput{Kind: "http", URL: "https://target.test/rpc", Body: `[{"payload":{"delete":true}}]`}, "pending"},
		{"GraphQL", domain.ActionInput{Kind: "http", URL: "https://target.test/graphql", Body: `{"query":"mutation { deleteUser(id: 1) { id } }"}`}, "pending"},
		{"form", domain.ActionInput{Kind: "http", URL: "https://target.test/rpc", Body: "action=%64elete&id=1"}, "pending"},
		{"override header", domain.ActionInput{Kind: "http", URL: "https://target.test/users/1", Headers: map[string]string{"X-HTTP-Method-Override": "DELETE"}}, "pending"},
		{"Chinese action", domain.ActionInput{Kind: "http", URL: "https://target.test/rpc", Body: `{"action":"删除"}`}, "pending"},
		{"invalid", domain.ActionInput{Kind: "http", URL: "%%%"}, "pending"},
		{"unknown kind", domain.ActionInput{Kind: "invented"}, "pending"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Evaluate(tc.input)
			if got.Status != tc.status || got.Reason == "" {
				t.Fatalf("decision=%+v, want %s", got, tc.status)
			}
		})
	}
}

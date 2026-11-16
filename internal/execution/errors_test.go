package execution

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"wscyber/internal/domain"
)

func TestSafeDiagnostic(t *testing.T) {
	input := domain.ActionInput{URL: "https://example.test/?token=private-value", Command: "printf secret-command", Body: "private-body", Headers: map[string]string{"Authorization": "Bearer private-key"}}
	raw := fmt.Errorf("HTTP error %s %s %s %s; wscyber-exec-unique-id", input.URL, input.Command, input.Body, input.Headers["Authorization"])
	err := diagnostic(raw, input)
	text := SafeErrorMessage(err)
	for _, secret := range []string{"private-value", "secret-command", "private-body", "private-key"} {
		if strings.Contains(text, secret) {
			t.Fatal("diagnostic exposed input")
		}
	}
	if !strings.Contains(text, "wscyber-exec-unique-id") || !errors.Is(err, raw) {
		t.Fatal("lost diagnostic identity")
	}
	if strings.Contains(SafeErrorMessage(errors.New("private-key")), "private-key") {
		t.Fatal("untrusted adapter error exposed")
	}
}

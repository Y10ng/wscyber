package modelconfig

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"wscyber/internal/domain"
)

func editInput() ModelConfigEdit {
	key, header := "private-key", "header-secret"
	return ModelConfigEdit{ModelConnection: input().ModelConnection, Enabled: true, APIKey: &key, Headers: []HeaderEntry{{Name: "x-private", Value: &header}}, Parameters: []ParameterEntry{{Name: "metadata", Value: json.RawMessage(`{"nested":null}`)}}}
}
func TestEditSecretEntriesAndReceiver(t *testing.T) {
	ctx := context.Background()
	repo := &memoryRepo{}
	m := New(repo, testVault(t))
	in := editInput()
	if _, err := m.Edit(ctx, testID, in); err != nil {
		t.Fatal(err)
	}
	in.ExpectedRevision = 1
	in.Name = "普通编辑"
	in.APIKey = nil
	in.Headers[0].Value = nil
	in.Parameters[0].Value = nil
	v, err := m.Edit(ctx, testID, in)
	if err != nil || v.Revision != 2 || !v.APIKeySet {
		t.Fatal(v, err)
	}
	doc, err := m.document(repo.record)
	if err != nil || doc.Secrets.APIKey != "private-key" || doc.Secrets.Headers["X-Private"] != "header-secret" || string(doc.Secrets.Parameters["metadata"]) != `{"nested":null}` {
		t.Fatal("保留值失效", err)
	}
	in.ExpectedRevision = 2
	for _, mutate := range []func(*ModelConfigEdit){
		func(i *ModelConfigEdit) { i.BaseURL = "http://127.0.0.1:54321/v1" },
		func(i *ModelConfigEdit) { i.Headers = []HeaderEntry{{Name: "X-New"}} },
		func(i *ModelConfigEdit) { i.Parameters = []ParameterEntry{{Name: "temperature"}} },
		func(i *ModelConfigEdit) { i.Headers = append(i.Headers, HeaderEntry{Name: "X-PRIVATE"}) },
		func(i *ModelConfigEdit) { i.Parameters = append(i.Parameters, ParameterEntry{Name: "metadata"}) },
	} {
		copy := in
		mutate(&copy)
		if _, err := m.Edit(ctx, testID, copy); !errors.Is(err, domain.ErrInvalid) {
			t.Fatal("秘密歧义未拒绝", err)
		}
	}
	in.Parameters = []ParameterEntry{{Name: "metadata", Value: json.RawMessage(`null`)}}
	in.Headers = []HeaderEntry{}
	if _, err := m.Edit(ctx, testID, in); err != nil {
		t.Fatal(err)
	}
	doc, _ = m.document(repo.record)
	if len(doc.Secrets.Headers) != 0 || string(doc.Secrets.Parameters["metadata"]) != "null" {
		t.Fatal("删除/null失效")
	}
	in.ExpectedRevision = 3
	in.Auth = "none"
	in.Parameters = []ParameterEntry{}
	v, err = m.Edit(ctx, testID, in)
	if err != nil || v.APIKeySet {
		t.Fatal("none必须清除认证秘密", v, err)
	}
	in.ExpectedRevision = 4
	in.Auth = "bearer"
	in.Enabled = false
	if _, err := m.Edit(ctx, testID, in); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("禁用不能绕过key必填", err)
	}
	key := " "
	in.APIKey = &key
	if _, err := m.Edit(ctx, testID, in); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("空白key未拒绝", err)
	}
}
func TestEditStrictRawJSON(t *testing.T) {
	raw, _ := json.Marshal(editInput())
	if _, err := DecodeEdit(raw); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		strings.Replace(string(raw), `"enabled":true`, `"enabled":true,"enabled":false`, 1),
		strings.Replace(string(raw), `"enabled":true`, `"Enabled":true`, 1),
		strings.Replace(string(raw), `"api_key":"private-key"`, `"api_key":null`, 1),
		strings.Replace(string(raw), `"name":"x-private"`, `"name":"x-private","extra":1`, 1),
		strings.Replace(string(raw), `"value":"header-secret"`, `"value":null`, 1),
		strings.Replace(string(raw), `{"nested":null}`, `{"nested":1,"nested":2}`, 1),
		strings.Replace(string(raw), `"headers":[{"name":"x-private","value":"header-secret"}]`, `"headers":null`, 1),
		`null`, `[]`, string(raw) + ` {}`,
	} {
		if _, err := DecodeEdit([]byte(bad)); !errors.Is(err, domain.ErrInvalid) {
			t.Fatal("歧义JSON被接受", bad, err)
		}
	}
	for _, mutate := range []func(*ModelConfigEdit){
		func(i *ModelConfigEdit) { i.Headers = []HeaderEntry{{Name: "Authorization", Value: i.APIKey}} },
		func(i *ModelConfigEdit) { v := "a\nb"; i.Headers[0].Value = &v },
		func(i *ModelConfigEdit) {
			i.Parameters = []ParameterEntry{{Name: "model", Value: json.RawMessage(`"override"`)}}
		},
		func(i *ModelConfigEdit) {
			i.Parameters = []ParameterEntry{{Name: "metadata", Value: json.RawMessage(strings.Repeat("[", 18) + "1" + strings.Repeat("]", 18))}}
		},
		func(i *ModelConfigEdit) {
			i.Parameters = []ParameterEntry{{Name: "metadata", Value: json.RawMessage(`"` + strings.Repeat("a", 33<<10) + `"`)}}
		},
	} {
		in := editInput()
		mutate(&in)
		if _, err := New(&memoryRepo{}, testVault(t)).Edit(context.Background(), testID, in); !errors.Is(err, domain.ErrInvalid) {
			t.Fatal(err)
		}
	}
}

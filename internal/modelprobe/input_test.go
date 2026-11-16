package modelprobe

import (
	"strings"
	"testing"
)

func TestDecodeConfirmation(t *testing.T) {
	for _, raw := range []string{
		`{"config_revision":1,"confirm_billable":true}`,
		` {"confirm_billable":true,"config_revision":9007199254740991} `,
	} {
		if _, err := DecodeConfirmation([]byte(raw)); err != nil {
			t.Fatal(raw, err)
		}
	}
	for _, raw := range []string{
		``, `null`, `[]`, `{}`, `true`, `{"config_revision":1}`, `{"confirm_billable":true}`,
		`{"config_revision":null,"confirm_billable":true}`, `{"config_revision":1,"confirm_billable":null}`,
		`{"config_revision":1,"confirm_billable":false}`, `{"config_revision":0,"confirm_billable":true}`,
		`{"config_revision":-1,"confirm_billable":true}`, `{"config_revision":9007199254740992,"confirm_billable":true}`,
		`{"config_revision":1.0,"confirm_billable":true}`, `{"config_revision":1e0,"confirm_billable":true}`,
		`{"config_revision":"1","confirm_billable":true}`, `{"config_revision":1,"confirm_billable":"true"}`,
		`{"Config_revision":1,"confirm_billable":true}`, `{"config_revision":1,"Confirm_billable":true}`,
		`{"config_revision":1,"confirm_billable":true,"prompt":"private"}`,
		`{"config_revision":1,"config_revision":1,"confirm_billable":true}`,
		`{"config_revision":1,"confirm_billable":true,"confirm_billable":true}`,
		`{"config_revision":1,"confirm_billable":true,"confirm_billable":true}`,
		`{"config_revision":1,"confirm_billable":true} {}`, `{"config_revision":1,"confirm_billable":true} null`,
		`{"config_revision":1,"confirm_billable":true`, `{"config_revision":1,"confirm_billable":true,}`,
		strings.Repeat(" ", 1025), string([]byte{0xff}),
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := DecodeConfirmation([]byte(raw)); err == nil {
				t.Fatal("应拒绝歧义确认")
			}
		})
	}
}

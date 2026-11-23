package pgschema_test

import (
	"context"
	"errors"
	"testing"

	"wscyber/native/internal/testdb"
	"wscyber/native/pgschema"
)

func TestCheckRoleAttributes(t *testing.T) {
	if err := pgschema.CheckRoleAttributes(pgschema.RoleAttributes{Name: "app"}); err != nil {
		t.Fatal(err)
	}
	for _, attrs := range []pgschema.RoleAttributes{
		{Name: "postgres", Superuser: true},
		{Name: "bypass", BypassRLS: true},
		{Name: "both", Superuser: true, BypassRLS: true},
	} {
		if err := pgschema.CheckRoleAttributes(attrs); !errors.Is(err, pgschema.ErrUnsafeRole) {
			t.Fatalf("%+v 应被拒绝，得到 %v", attrs, err)
		}
	}
}

func TestCheckRoleOnTestRole(t *testing.T) {
	db := testdb.New(t).Open(t, "role-test")
	attrs, err := pgschema.QueryRoleAttributes(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if attrs.Name == "" || attrs.Superuser || attrs.BypassRLS {
		t.Fatalf("测试角色应为非超级用户、无 BYPASSRLS: %+v", attrs)
	}
	if err := pgschema.CheckRole(context.Background(), db); err != nil {
		t.Fatal(err)
	}
}

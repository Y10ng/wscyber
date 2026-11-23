// wscyber overlay：上游 pkg/web/service 中不存在的新增文件（manifest.json 中
// sha256 为 null，脚本会确认上游没有同名文件）。
//
// 存储身份（native/POSTGRES.md §4.4）：身份随 ctx 传递，SQLiteStore 的每个读写
// 方法在自己的事务开头执行 set_config('app.role'/'app.tenant_id'/'app.user_id',
// …, true)。is_local=true 使设置只在本事务内有效，连接归还连接池后不残留。
//
// ctx 没有身份时视为 system + 默认租户 "default"：上游后台路径（事件持久化、
// 扫描状态等）一律用 context.Background()，而 chat_sessions/aop_request_ledger/
// raw_artifacts 是根表，没有 app.tenant_id 时插入会报 WT001。P5 再收紧。

package service

import (
	"context"
	"fmt"
	"strings"
)

// Identity 是一次存储访问的身份。Role 取 "admin"、"user" 或 "system"；
// TenantID 为空表示不限定租户（admin/system 的跨租户视图，此时不能新建根表行）。
type Identity struct {
	TenantID string
	UserID   string
	Role     string
}

const (
	identityRoleAdmin  = "admin"
	identityRoleUser   = "user"
	identityRoleSystem = "system"
	// identityDefaultTenant 是基线种子里的默认租户 ID。
	identityDefaultTenant = "default"
)

type identityContextKey struct{}

// WithIdentity 返回携带身份的 ctx。
func WithIdentity(ctx context.Context, identity Identity) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, identityContextKey{}, identity)
}

// IdentityFrom 取 ctx 上显式设置的身份；没有时 ok=false（存储层随后按
// system + 默认租户处理）。
func IdentityFrom(ctx context.Context) (Identity, bool) {
	if ctx == nil {
		return Identity{}, false
	}
	identity, ok := ctx.Value(identityContextKey{}).(Identity)
	return identity, ok
}

// WithSystem 返回后台任务用的 system 身份；tenantID 为空表示跨租户。
func WithSystem(ctx context.Context, tenantID string) context.Context {
	return WithIdentity(ctx, Identity{TenantID: tenantID, Role: identityRoleSystem})
}

// storeIdentity 解析本次存储访问实际使用的身份并校验取值。
func storeIdentity(ctx context.Context) (Identity, error) {
	identity, ok := IdentityFrom(ctx)
	if !ok {
		return Identity{TenantID: identityDefaultTenant, Role: identityRoleSystem}, nil
	}
	identity.TenantID = strings.TrimSpace(identity.TenantID)
	identity.UserID = strings.TrimSpace(identity.UserID)
	switch identity.Role {
	case identityRoleAdmin, identityRoleSystem:
	case identityRoleUser:
		if identity.TenantID == "" {
			return Identity{}, fmt.Errorf("store identity: role %q requires a tenant", identity.Role)
		}
	default:
		return Identity{}, fmt.Errorf("store identity: unknown role %q", identity.Role)
	}
	return identity, nil
}

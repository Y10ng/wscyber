package pgschema

import (
	"context"

	"github.com/uptrace/bun"
)

// MigrateUpTo 只应用版本 ≤ upTo 的迁移（测试构造旧版本库用）。
func MigrateUpTo(ctx context.Context, db *bun.DB, upTo string) (MigrateResult, error) {
	lockConn, err := db.DB.Conn(ctx)
	if err != nil {
		return MigrateResult{}, err
	}
	defer discardConn(lockConn)
	return migrateLocked(ctx, db, lockConn, upTo)
}

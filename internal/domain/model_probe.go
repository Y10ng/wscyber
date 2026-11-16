package domain

import (
	"context"
	"time"
)

// ModelTest 只保存脱敏事实；Attempted=nil 表示没有确定的发送阶段信息。
// HTTPStatus 与 Attempted 均不能证明未计费或研究能力。
type ModelTest struct {
	ID             string     `json:"id"`
	ConfigID       string     `json:"config_id"`
	ConfigRevision int64      `json:"config_revision"`
	Status         string     `json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	Outcome        string     `json:"outcome,omitempty"`
	HTTPStatus     *int       `json:"http_status,omitempty"`
	Attempted      *bool      `json:"attempted,omitempty"`
}

const ModelTestCapacity = 8 // 含正在执行的一个测试；不是研究执行槽。

// ModelTestRepository 的受理与配置保存共享事务锁序；所有写入需单实例锁。
// Get 按全局ID读取，HTTP归属核对由编排层完成。
type ModelTestRepository interface {
	GetModelTest(context.Context, string) (ModelTest, error)
	AcceptModelTest(context.Context, string, string, int64, func(SealedModelConfig) error) (ModelTest, bool, error)
	ClaimModelTest(context.Context) (ModelTest, error)
	CompleteModelTest(context.Context, string, string, *int, *bool) error
	InterruptModelTests(context.Context) error
}

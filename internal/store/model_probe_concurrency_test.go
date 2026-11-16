package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"wscyber/internal/domain"
)

// 固定停在“第一次GET确实未找到”之后，允许另一个受理与禁用先提交。
// 不靠sleep碰撞；数据库受理仍是实际Store事务，worker只在本测试中被挡住。
type delayedProbeLookup struct {
	*Store
	once    sync.Once
	read    chan struct{}
	release chan struct{}
}

func (s *delayedProbeLookup) GetModelTest(ctx context.Context, id string) (domain.ModelTest, error) {
	v, err := s.Store.GetModelTest(ctx, id)
	first := false
	s.once.Do(func() { first = true })
	if first {
		close(s.read)
		select {
		case <-s.release:
		case <-ctx.Done():
			return domain.ModelTest{}, ctx.Err()
		}
	}
	return v, err
}
func (s *delayedProbeLookup) ClaimModelTest(ctx context.Context) (domain.ModelTest, error) {
	<-ctx.Done()
	return domain.ModelTest{}, ctx.Err()
}

func TestPostgresModelProbeReplayWinsConcurrentDisable(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	f := newProbeFixture(t, false)
	m := modelManager(t, s)
	in := saveProbeConfig(t, ctx, m, modelID, f.server.URL, "openai_chat", 32)
	delayed := &delayedProbeLookup{Store: s, read: make(chan struct{}), release: make(chan struct{})}
	p := startProbe(t, ctx, delayed, m)
	type result struct {
		receipt domain.ModelTest
		created bool
		err     error
	}
	got := make(chan result, 1)
	go func() { r, c, e := p.Submit(ctx, modelID, probeID, 1); got <- result{r, c, e} }()
	select {
	case <-delayed.read:
	case <-time.After(3 * time.Second):
		t.Fatal("未到达受控首次读取")
	}
	if _, err := s.GetModelTest(ctx, probeID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("首次读取必须确实不存在", err)
	}
	if _, created, err := s.AcceptModelTest(ctx, modelID, probeID, 1, m.ValidateRecordForUse); err != nil || !created {
		t.Fatal(created, err)
	}
	in.ExpectedRevision = 1
	in.Enabled = false
	if _, err := m.Save(ctx, modelID, in); err != nil {
		t.Fatal(err)
	}
	close(delayed.release)
	select {
	case r := <-got:
		if r.err != nil || r.created || r.receipt.ID != probeID || r.receipt.ConfigRevision != 1 || r.receipt.Status != "pending" {
			t.Fatalf("并发已提交回执必须优先于新版禁用: %+v", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("回放未完成")
	}
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM model_tests`).Scan(&n); err != nil || n != 1 || f.hits.Load() != 0 {
		t.Fatal(n, err, f.hits.Load())
	}
}

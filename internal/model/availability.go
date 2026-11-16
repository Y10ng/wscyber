package model

import (
	"context"
	"wscyber/internal/domain"
)

// ValidateMissionModel 仅校验本地绑定/解密；控制请求不调用供应商。
// 这是启用前检查，不能替代实际决策前再次核对当前 enabled。
func (m *Routed) ValidateMissionModel(ctx context.Context, id string) error {
	b, err := m.repo.GetMissionModelBinding(ctx, id)
	if err != nil {
		return err
	}
	if b == nil || b.Source == "environment" {
		if m.environment == nil || (b != nil && b.EnvironmentFingerprint != "" && b.EnvironmentFingerprint != m.fingerprint) {
			return domain.ErrUnavailable
		}
		return nil
	}
	_, err = m.models.ResolveVersion(ctx, b.ConfigID, b.ConfigRevision)
	return err
}

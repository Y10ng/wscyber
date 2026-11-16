package modelconfig

import (
	"context"
	"wscyber/internal/domain"
)

// Default 也在缺少部署密钥时读取默认选择，禁止误把不可解密默认当作未选择并退回环境。
func (m *Manager) Default(ctx context.Context) (id, name string, usable bool, err error) {
	if m == nil || m.repo == nil {
		return "", "", false, nil
	}
	settings, err := m.repo.GetModelSettings(ctx)
	if err != nil {
		return "", "", false, err
	}
	id = settings.DefaultConfigID
	if id == "" || !m.Available() {
		return id, "", false, nil
	}
	record, err := m.repo.GetModelConfig(ctx, id, 0)
	if err != nil {
		return id, "", false, err
	}
	doc, err := m.recordForUse(record)
	if err != nil {
		return id, "", false, nil
	}
	return id, doc.Model, true, nil
}

// VersionView 展示历史绑定元数据，但 enabled 仍取当前配置状态；永不回传秘密。
func (m *Manager) VersionView(ctx context.Context, id string, revision int64) (domain.ModelConfigView, error) {
	if err := m.check(); err != nil {
		return domain.ModelConfigView{}, err
	}
	record, err := m.repo.GetModelConfig(ctx, id, revision)
	if err != nil {
		return domain.ModelConfigView{}, err
	}
	doc, err := m.document(record)
	if err != nil {
		return domain.ModelConfigView{}, domain.ErrUnavailable
	}
	return view(record, doc), nil
}

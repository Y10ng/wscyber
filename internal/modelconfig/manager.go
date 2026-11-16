package modelconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"wscyber/internal/domain"
)

type Manager struct {
	repo  domain.ModelConfigRepository
	vault *Vault
}

func New(repo domain.ModelConfigRepository, vault *Vault) *Manager {
	return &Manager{repo: repo, vault: vault}
}
func (m *Manager) Available() bool { return m != nil && m.repo != nil && m.vault != nil }
func (m *Manager) check() error {
	if !m.Available() {
		return fmt.Errorf("%w：模型配置加密密钥未配置", domain.ErrUnavailable)
	}
	return nil
}

func (m *Manager) document(record domain.SealedModelConfig) (domain.ModelDocument, error) {
	var doc domain.ModelDocument
	plain, err := m.vault.open(record.ID, record.Revision, record.Ciphertext)
	if err != nil {
		return doc, err
	}
	defer clear(plain)
	if err := json.Unmarshal(plain, &doc); err != nil {
		return doc, fmt.Errorf("模型配置密文内容无效")
	}
	if err := validateConnection(doc.ModelConnection); err != nil {
		return doc, fmt.Errorf("已保存模型配置不符合连接契约")
	}
	return doc, nil
}

func view(record domain.SealedModelConfig, doc domain.ModelDocument) domain.ModelConfigView {
	headers := slices.Sorted(maps.Keys(doc.Secrets.Headers))
	if headers == nil {
		headers = []string{}
	}
	keys := slices.Sorted(maps.Keys(doc.Secrets.Parameters))
	if keys == nil {
		keys = []string{}
	}
	return domain.ModelConfigView{ModelConnection: doc.ModelConnection, ID: record.ID, Revision: record.Revision, Enabled: record.Enabled, APIKeySet: doc.Secrets.APIKey != "", HeaderNames: headers, ParameterKeys: keys, CreatedAt: record.CreatedAt}
}

func (m *Manager) List(ctx context.Context) ([]domain.ModelConfigView, error) {
	if err := m.check(); err != nil {
		return nil, err
	}
	records, err := m.repo.ListModelConfigs(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.ModelConfigView, 0, len(records))
	for _, r := range records {
		doc, err := m.document(r)
		if err != nil {
			return nil, err
		}
		out = append(out, view(r, doc))
	}
	return out, nil
}
func (m *Manager) Get(ctx context.Context, id string) (domain.ModelConfigView, error) {
	var zero domain.ModelConfigView
	if err := m.check(); err != nil {
		return zero, err
	}
	if !ValidID(id) {
		return zero, invalid("配置ID必须为32位小写十六进制")
	}
	r, err := m.repo.GetModelConfig(ctx, id, 0)
	if err != nil {
		return zero, err
	}
	doc, err := m.document(r)
	if err != nil {
		return zero, err
	}
	return view(r, doc), nil
}

// ResolveVersion 只解密指定的已保存版本，但启用状态始终来自配置当前行。
// 调用者仍须在持久受理事务中校验当前revision，不能用此读取替代并发检查。
func (m *Manager) ResolveVersion(ctx context.Context, id string, revision int64) (domain.ModelDocument, error) {
	var zero domain.ModelDocument
	if err := m.check(); err != nil {
		return zero, err
	}
	if !ValidID(id) || revision < 1 || revision > 1<<53-1 {
		return zero, invalid("配置ID或版本无效")
	}
	r, err := m.repo.GetModelConfig(ctx, id, revision)
	if err != nil {
		return zero, err
	}
	return m.recordForUse(r)
}

// ValidateRecordForUse 在受理事务持锁期间只做本地解密/校验，不查库、不外发。
// 记录必须由仓储提供；不是接收客户端密文的接口。
func (m *Manager) ValidateRecordForUse(r domain.SealedModelConfig) error {
	_, err := m.recordForUse(r)
	return err
}

func (m *Manager) recordForUse(r domain.SealedModelConfig) (domain.ModelDocument, error) {
	var zero domain.ModelDocument
	if err := m.check(); err != nil {
		return zero, err
	}
	if !r.Enabled {
		return zero, fmt.Errorf("%w：模型配置已禁用", domain.ErrConflict)
	}
	doc, err := m.document(r)
	if err != nil {
		return zero, domain.ErrUnavailable
	}
	doc, err = ValidateForUse(doc)
	if err != nil {
		return zero, domain.ErrUnavailable
	}
	return doc, nil
}

func patch[T any](change domain.SecretChange[T], old T, hasValue bool) (T, error) {
	var zero T
	switch change.Mode {
	case "keep":
		if hasValue {
			return zero, invalid("keep 不得携带秘密值")
		}
		return old, nil
	case "clear":
		if hasValue {
			return zero, invalid("clear 不得携带秘密值")
		}
		return zero, nil
	case "replace":
		return change.Value, nil
	default:
		return zero, invalid("秘密修改必须选择 keep、replace 或 clear")
	}
}

func (m *Manager) Save(ctx context.Context, id string, in domain.ModelConfigWrite) (domain.ModelConfigView, error) {
	var zero domain.ModelConfigView
	if err := m.check(); err != nil {
		return zero, err
	}
	if !ValidID(id) || in.ExpectedRevision < 0 || in.ExpectedRevision >= 1<<53-1 {
		return zero, invalid("配置ID或预期版本无效")
	}
	if err := validateConnection(in.ModelConnection); err != nil {
		return zero, err
	}
	var previous domain.ModelDocument
	if in.ExpectedRevision > 0 {
		r, err := m.repo.GetModelConfig(ctx, id, 0)
		if err != nil {
			return zero, err
		}
		if r.Revision != in.ExpectedRevision {
			return zero, fmt.Errorf("%w：配置版本已变化，请重新读取", domain.ErrConflict)
		}
		previous, err = m.document(r)
		if err != nil {
			return zero, err
		}
		// 防止一次改地址操作顺带把旧凭据/敏感扩展发送到新接收方。
		changed := previous.BaseURL != in.BaseURL || previous.Protocol != in.Protocol || previous.Auth != in.Auth
		if changed && ((previous.Secrets.APIKey != "" && in.APIKey.Mode == "keep") || (len(previous.Secrets.Headers) > 0 && in.Headers.Mode == "keep") || (len(previous.Secrets.Parameters) > 0 && in.Parameters.Mode == "keep")) {
			return zero, invalid("变更协议、地址或认证方式时，已有秘密必须明确替换或清除")
		}
	}
	key, err := patch(in.APIKey, previous.Secrets.APIKey, in.APIKey.Value != "")
	if err != nil {
		return zero, err
	}
	headers, err := patch(in.Headers, previous.Secrets.Headers, in.Headers.Value != nil)
	if err != nil {
		return zero, err
	}
	parameters, err := patch(in.Parameters, previous.Secrets.Parameters, in.Parameters.Value != nil)
	if err != nil {
		return zero, err
	}
	if key != "" && (!validText(key, 8192) || in.Auth == "none") {
		return zero, invalid("凭据格式无效，none 认证不得保留 API key")
	}
	if in.Enabled && in.Auth != "none" && key == "" {
		return zero, invalid("启用该认证配置前必须提供 API key")
	}
	headers, err = normalizeHeaders(headers)
	if err != nil {
		return zero, err
	}
	if err := validateParameters(parameters); err != nil {
		return zero, err
	}
	if parameters == nil {
		parameters = map[string]json.RawMessage{}
	}
	doc := domain.ModelDocument{ModelConnection: in.ModelConnection, Secrets: domain.ModelSecrets{APIKey: key, Headers: headers, Parameters: parameters}}
	plain, err := json.Marshal(doc)
	if err != nil {
		return zero, invalid("配置无法编码")
	}
	defer clear(plain)
	revision := in.ExpectedRevision + 1
	sealed, err := m.vault.seal(id, revision, plain)
	if err != nil {
		return zero, err
	}
	r, err := m.repo.SaveModelConfig(ctx, domain.SealedModelConfig{ID: id, Revision: revision, Enabled: in.Enabled, Ciphertext: sealed}, in.ExpectedRevision)
	if err != nil {
		return zero, err
	}
	return view(r, doc), nil
}

func (m *Manager) Settings(ctx context.Context) (domain.ModelSettings, error) {
	if err := m.check(); err != nil {
		return domain.ModelSettings{}, err
	}
	return m.repo.GetModelSettings(ctx)
}
func (m *Manager) SetDefault(ctx context.Context, id string, expected int64) (domain.ModelSettings, error) {
	var zero domain.ModelSettings
	if err := m.check(); err != nil {
		return zero, err
	}
	if (id != "" && !ValidID(id)) || expected < 0 || expected >= 1<<53-1 {
		return zero, invalid("默认模型ID或版本无效")
	}
	if id != "" {
		v, err := m.Get(ctx, id)
		if err != nil {
			return zero, err
		}
		if !v.Enabled {
			return zero, fmt.Errorf("%w：不能选择已禁用配置", domain.ErrConflict)
		}
	}
	return m.repo.SetModelDefault(ctx, id, expected)
}

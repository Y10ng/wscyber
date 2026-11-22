package webapi

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	aop "github.com/chainreactors/cyber/aop"
)

// 账号占位符只在平台发送前解析；credential id 允许点号，扩展名取最后一个 .username/.secret。
// 其它形如 {{...}} 的文本不匹配，因而保持原样，不猜测语义。
var credentialPlaceholderPattern = regexp.MustCompile(`\{\{cred:([^{}]+)\.(username|secret)\}\}`)
var credentialSecretPlaceholderPattern = regexp.MustCompile(`\{\{cred:[^{}]+\.secret\}\}`)

type resolvedCredential struct {
	model     assetCredentialModel
	username  string
	secret    string
	hasSecret bool
}

type credentialRedaction struct {
	secret      string
	placeholder string
}

// credentialResolver 同时持有本任务资产的账号（用于替换）和全部 secret（用于工具结果回替）。
// 明文只在单次调用的内存中使用；webapi_requests 仍保存实际发送与接收的字节。
type credentialResolver struct {
	assetID    string
	items      []resolvedCredential
	byID       map[string]resolvedCredential
	redactions []credentialRedaction
}

func loadCredentialResolver(ctx context.Context, store *taskStore, secrets *secretBox, taskID string) (*credentialResolver, error) {
	assetID, err := store.taskAssetID(ctx, taskID)
	if err != nil {
		return nil, errors.New("读取任务关联资产失败")
	}
	return loadCredentialResolverForAsset(ctx, store, secrets, assetID)
}

func loadCredentialResolverForAsset(ctx context.Context, store *taskStore, secrets *secretBox, assetID string) (*credentialResolver, error) {
	resolver := &credentialResolver{assetID: assetID, byID: map[string]resolvedCredential{}}
	if assetID == "" {
		return resolver, nil
	}
	credentials, err := store.assetCredentials(ctx, assetID)
	if err != nil {
		return nil, errors.New("读取资产测试账号失败")
	}
	for _, model := range credentials[assetID] {
		item := resolvedCredential{model: model, username: model.Username, hasSecret: model.SecretCipher != ""}
		if item.hasSecret {
			if secrets == nil {
				return nil, errSecretKeyInvalid
			}
			aead, err := secrets.load(false)
			if err != nil {
				return nil, err
			}
			// 首条消息虽然只给占位符，仍先解密一次，保留密钥缺失/损坏时明确失败的既有行为。
			item.secret, err = openSecret(aead, assetID, model.ID, model.SecretCipher)
			if err != nil {
				return nil, err
			}
			if item.secret != "" {
				resolver.redactions = append(resolver.redactions, credentialRedaction{secret: item.secret, placeholder: credentialPlaceholder(model.ID, "secret")})
			}
		}
		resolver.items = append(resolver.items, item)
		resolver.byID[model.ID] = item
	}
	// 长 secret 先替换，避免一个 secret 是另一个的前缀时只回替了后半段。
	sort.SliceStable(resolver.redactions, func(i, j int) bool {
		if len(resolver.redactions[i].secret) != len(resolver.redactions[j].secret) {
			return len(resolver.redactions[i].secret) > len(resolver.redactions[j].secret)
		}
		return resolver.redactions[i].placeholder < resolver.redactions[j].placeholder
	})
	return resolver, nil
}

func credentialPlaceholder(id, kind string) string {
	return "{{cred:" + id + "." + kind + "}}"
}

// replace 解析 text 中所有受支持占位符；任何失败都返回第一个错误，调用方必须保证 0 发送。
func (r *credentialResolver) replace(text string) (string, error) {
	var firstErr error
	resolved := credentialPlaceholderPattern.ReplaceAllStringFunc(text, func(token string) string {
		if firstErr != nil {
			return token
		}
		parts := credentialPlaceholderPattern.FindStringSubmatch(token)
		id, kind := parts[1], parts[2]
		if r.assetID == "" {
			firstErr = fmt.Errorf("占位符 %s 无法解析：任务未关联资产", token)
			return token
		}
		item, ok := r.byID[id]
		if !ok {
			firstErr = fmt.Errorf("占位符 %s 的凭据 ID 不属于本任务关联资产", token)
			return token
		}
		if kind == "username" {
			return item.username
		}
		if !item.hasSecret {
			firstErr = fmt.Errorf("占位符 %s 的凭据未设置 secret", token)
			return token
		}
		return item.secret
	})
	return resolved, firstErr
}

func (r *credentialResolver) resolveHTTPArguments(args httpToolArguments) (httpToolArguments, error) {
	var err error
	if args.URL, err = r.replace(args.URL); err != nil {
		return args, err
	}
	headers := make(map[string]string, len(args.Headers))
	for _, name := range sortedHeaderNames(args.Headers) {
		value, err := r.replace(args.Headers[name])
		if err != nil {
			return args, fmt.Errorf("header %s：%w", name, err)
		}
		headers[name] = value
	}
	args.Headers = headers
	if args.Body, err = r.replace(args.Body); err != nil {
		return args, err
	}
	return args, nil
}

func sortedHeaderNames(headers map[string]string) []string {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// redact 只处理占位符之外的文本，避免短 secret 与 credential id 相同时破坏已有占位符。
func (r *credentialResolver) redact(text string) string {
	if r == nil || len(r.redactions) == 0 || text == "" {
		return text
	}
	var out strings.Builder
	last := 0
	for _, match := range credentialSecretPlaceholderPattern.FindAllStringIndex(text, -1) {
		out.WriteString(r.redactPlain(text[last:match[0]]))
		out.WriteString(text[match[0]:match[1]])
		last = match[1]
	}
	out.WriteString(r.redactPlain(text[last:]))
	return out.String()
}

func (r *credentialResolver) redactPlain(text string) string {
	for _, item := range r.redactions {
		text = strings.ReplaceAll(text, item.secret, item.placeholder)
	}
	return text
}

// redactToolResult 保留工具结果的调用关联、错误与终止标记，只回替文本块中的 secret。
func redactToolResult(result *aop.ToolResult, resolver *credentialResolver) *aop.ToolResult {
	if result == nil || resolver == nil {
		return result
	}
	for _, block := range result.Output {
		if text := block.GetText(); text != nil {
			text.Text = resolver.redact(text.Text)
		}
	}
	return result
}

// credentialMessageLines 是研究/计划验证/计划复测共用的“测试账号”块，不含 secret 明文。
func (r *credentialResolver) credentialMessageLines() []string {
	if r == nil || len(r.items) == 0 {
		return nil
	}
	lines := []string{"测试账号（资产登记，仅用于上述授权范围内的测试）："}
	for _, item := range r.items {
		fields := []string{}
		if item.model.Label != "" {
			fields = append(fields, "名称："+item.model.Label)
		}
		if item.username != "" {
			fields = append(fields, "用户名："+item.username)
		}
		fields = append(fields, "凭据 ID："+item.model.ID)
		if item.hasSecret {
			fields = append(fields, "口令：用占位符 "+credentialPlaceholder(item.model.ID, "secret")+"（平台发送时替换，你看不到明文）")
		}
		lines = append(lines, "- "+strings.Join(fields, "；"))
	}
	return lines
}

// followupFirstMessage 在计划验证/复测的固定首条消息后追加同一账号块；手动路径由前端片处理。
func (c *Capability) followupFirstMessage(ctx context.Context, taskID, base string) (string, error) {
	resolver, err := loadCredentialResolver(ctx, c.store, c.secrets, taskID)
	if err != nil {
		return "", err
	}
	lines := resolver.credentialMessageLines()
	if len(lines) == 0 {
		return base, nil
	}
	return base + "\n\n" + strings.Join(lines, "\n"), nil
}

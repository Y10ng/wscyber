package main

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"

	"connectrpc.com/connect"
	"github.com/chainreactors/cyber/core/extension"
	"github.com/chainreactors/cyber/core/types"
	cfg "github.com/chainreactors/cyber/pkg/config"
	"github.com/chainreactors/cyber/pkg/rpc"
	webpkg "github.com/chainreactors/cyber/pkg/web"
	webapi "github.com/chainreactors/cyber/pkg/web/api"
	"github.com/chainreactors/cyber/pkg/web/host"
	webservice "github.com/chainreactors/cyber/pkg/web/service"
	"google.golang.org/protobuf/proto"
)

// 只限制配置范围和错误输出；文件算法、秘密保留、运行时快照归原生存储。
// 本模式不创建 Provider/Profile/节点，不验证模型端点，也不发送模型请求。
type modelConfigStore struct {
	native *host.FileConfigStore
}

var _ webservice.ConfigStore = (*modelConfigStore)(nil)

func newModelConfigStore(stateDir string) *modelConfigStore {
	directory := filepath.Join(stateDir, "model-config")
	codec := host.SharedConfigCodec()
	codec.Validate = validateModelConfig
	return &modelConfigStore{native: &host.FileConfigStore{
		Explicit: filepath.Join(directory, "cyber.yaml"),
		Codec:    codec,
		Runtime: &cfg.Option{
			Sections: codec.Sections,
			Context: &cfg.Context{
				Directory: directory, Home: directory,
				Executable: filepath.Join(directory, "wscyber-hub"),
				LookupEnv:  func(string) (string, bool) { return "", false },
			},
		},
	}}
}

func validateModelConfig(value *types.DistributeConfig) error {
	if value == nil {
		return nil
	}
	// 空映射/空消息没有实际设置，兼容原生 ConfigPanel 的提交形状。
	// optional timeout 即使显式为0也有字段存在，属于Agent修改，仍拒绝。
	if proto.Size(value.Agent) != 0 || proto.Size(value.Traffic) != 0 || proto.Size(value.Node) != 0 ||
		len(value.Extensions) != 0 || len(value.ProtoReflect().GetUnknown()) != 0 {
		return webapi.Errorf(webapi.CodeInvalidArgument, "此模式仅支持模型配置，不接受 Agent、流量、节点或扩展设置")
	}
	if err := webapi.ValidateLLMConfig(value.Llm); err != nil {
		// 原生校验可能引用用户填写的名称；不将该错误及配置正文写入诊断。
		return webapi.Errorf(webapi.CodeInvalidArgument, "模型配置无效：请检查供应商、模型、配置标识和数值范围")
	}
	return nil
}

func (s *modelConfigStore) GetDistributeConfig(ctx context.Context) (string, bool, *types.DistributeConfig, error) {
	path, loaded, value, err := s.native.GetDistributeConfig(ctx)
	if err != nil {
		return "", false, nil, modelStoreError(ctx, "无法读取私有模型配置")
	}
	if err := validateModelConfig(value); err != nil {
		return "", false, nil, err
	}
	return path, loaded, value, nil
}

func (s *modelConfigStore) PrepareDistributeConfig(ctx context.Context, value *types.DistributeConfig) (*webservice.PreparedConfig, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateModelConfig(value); err != nil {
		return nil, err
	}
	value = proto.CloneOf(value)
	if value != nil {
		// 不把空消息落成非LLM配置章节；未传入字段由原生存储保持。
		value.Agent, value.Traffic, value.Node, value.Extensions = nil, nil, nil, nil
	}
	prepared, err := s.native.PrepareDistributeConfig(ctx, value)
	if err != nil {
		return nil, modelStoreError(ctx, "无法准备私有模型配置；未提交更改")
	}
	return prepared, nil
}

func (s *modelConfigStore) CommitDistributeConfig(ctx context.Context, prepared *webservice.PreparedConfig) error {
	if err := s.native.CommitDistributeConfig(ctx, prepared); err != nil {
		return modelStoreError(ctx, "无法提交私有模型配置")
	}
	return nil
}

func (s *modelConfigStore) DiscardDistributeConfig(prepared *webservice.PreparedConfig) {
	s.native.DiscardDistributeConfig(prepared)
}

func modelStoreError(ctx context.Context, message string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return webapi.Errorf(webapi.CodeFailedPrecondition, "%s", message)
}

// 原生Config API的独立probe可自带base_url/proxy并借用已存key，不能仅约束存储。
// 精确方法路由覆盖原生ConfigService前缀；其他Config RPC保持原实现。
func modelProbeDenialExtension() extension.Extension {
	return extension.Func{LoadFunc: func(scope *extension.Scope) error {
		auth, err := extension.Use[webpkg.Auth](scope)
		if err != nil {
			return err
		}
		return extension.Add(scope,
			webpkg.Route{Pattern: rpc.ConfigServiceTestLLMProcedure, Handler: deniedModelProbe[types.LLMProbeRequest, types.LLMProbeResult](rpc.ConfigServiceTestLLMProcedure, auth)},
			webpkg.Route{Pattern: rpc.ConfigServiceListModelsProcedure, Handler: deniedModelProbe[types.LLMProbeRequest, types.ListModelsResult](rpc.ConfigServiceListModelsProcedure, auth)},
			webpkg.Route{Pattern: rpc.ConfigServiceTestConnectionProcedure, Handler: deniedModelProbe[types.TestConnectionRequest, types.TestConnectionResponse](rpc.ConfigServiceTestConnectionProcedure, auth)},
		)
	}}
}

func deniedModelProbe[Request, Response any](procedure string, auth webpkg.Auth) *connect.Handler {
	return connect.NewUnaryHandler(procedure, func(_ context.Context, request *connect.Request[Request]) (*connect.Response[Response], error) {
		// 原生Auth的HTTP Middleware仅保护/api/；RPC必须使用同一Auth做认证。
		// 只借用其Bearer/Cookie策略，不实现另一份令牌/会话校验。
		if auth == nil || !auth.Authenticate(&http.Request{Header: request.Header()}) {
			return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid or missing access key"))
		}
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("仅配置管理模式不提供模型探测、模型列表或连接测试；未发送请求"))
	}, connect.WithReadMaxBytes(64<<10))
}

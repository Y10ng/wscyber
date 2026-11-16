// wscyber-hub 提供显式本机 Hub；模型配置管理与固定工具 fixture 分别选择。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"unicode"

	"github.com/chainreactors/cyber/core/telemetry"
	webext "github.com/chainreactors/cyber/exts/web"
	"github.com/chainreactors/cyber/pkg/web/host"

	"wscyber/native/webapi"
)

type options struct {
	localHub      bool
	listen        string
	keyFile       string
	stateDir      string
	uiDir         string
	fixtureNode   bool
	modelConfig   bool
	modelNode     bool
	modelEndpoint string
	modelReport   *modelNodeReport   // 仅收尾诊断，不是节点应用回执。
	fixtureReport *fixtureNodeReport // 仅收尾验收摘要，不是业务状态。
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "wscyber-hub:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, output io.Writer) error {
	var opts options
	flags := flag.NewFlagSet("wscyber-hub", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.BoolVar(&opts.localHub, "local-hub", false, "显式启用本机 Hub；配合 --model-node 启动正式模型节点")
	flags.BoolVar(&opts.fixtureNode, "fixture-node", false, "仅本机固定工具 fixture；仍须 --local-hub，不接真实模型")
	flags.BoolVar(&opts.modelConfig, "model-config", false, "仅管理原生模型配置；不接执行节点/Provider，与 --fixture-node 互斥")
	flags.BoolVar(&opts.modelNode, "model-node", false, "启动原生正式模型节点；显式 --model-endpoint 时保留本机受控验收分支")
	flags.StringVar(&opts.modelEndpoint, "model-endpoint", "", "模型节点独立允许的 http://127.0.0.1:非零端口/v1；不由保存配置授予")
	flags.StringVar(&opts.listen, "listen", "127.0.0.1:0", "仅允许 loopback IP:port；0 为随机端口")
	flags.StringVar(&opts.keyFile, "access-key-file", "", "0600 普通文件，内含本机专用非空测试 key")
	flags.StringVar(&opts.stateDir, "state-dir", "", "必须是尚不存在的新目录；父目录须已存在")
	flags.StringVar(&opts.uiDir, "ui-dir", "", "可选固定原生UI构建产物目录；不配置仍无前端，key/state必须在其外")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			flags.SetOutput(output)
			flags.PrintDefaults()
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("不接受位置参数")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	encoder := json.NewEncoder(output)
	var readyErr error
	if opts.fixtureNode && !opts.modelConfig && !opts.modelNode {
		opts.fixtureReport = &fixtureNodeReport{}
	}
	if opts.modelNode && !opts.fixtureNode && !opts.modelConfig {
		opts.modelReport = &modelNodeReport{}
	}
	err := serveLocalHub(ctx, opts, func(addr string) {
		// 不输出 key、Cookie 或上游配置；这不是业务事件/状态协议。
		readyErr = encoder.Encode(map[string]any{
			"event": "ready", "address": addr, "mode": "local-hub",
			"execution_node": false, "fixture_node_configured": opts.fixtureNode, "frontend": opts.uiDir != "",
			"model_config_management": opts.modelConfig || opts.modelNode, "model_node_configured": opts.modelNode,
			// ready 仅指 Hub 监听；节点就绪必须通过原生握手和实际 API 核验。
		})
		if readyErr != nil {
			cancel()
		}
	})
	if opts.fixtureReport != nil {
		err = errors.Join(err, encoder.Encode(map[string]any{"event": "fixture-closed", "fixture": opts.fixtureReport}))
	}
	if opts.modelReport != nil {
		err = errors.Join(err, encoder.Encode(map[string]any{"event": "model-node-closed", "node": opts.modelReport}))
	}
	if err = errors.Join(err, readyErr); err != nil {
		return err
	}
	return encoder.Encode(map[string]string{"event": "stopped"})
}

func serveLocalHub(ctx context.Context, opts options, ready func(string)) error {
	if !opts.localHub {
		return fmt.Errorf("必须显式指定 --local-hub")
	}
	if opts.modelConfig && opts.fixtureNode {
		return fmt.Errorf("--model-config 与 --fixture-node 不可同时启用")
	}
	if err := validateModelNodeOptions(opts); err != nil {
		return err
	}
	addr, err := netip.ParseAddrPort(opts.listen)
	if err != nil || !addr.Addr().IsLoopback() || addr.Addr().Zone() != "" {
		return fmt.Errorf("--listen 必须为明确的 loopback IP:port")
	}
	if strings.TrimSpace(opts.stateDir) == "" || strings.ContainsAny(opts.stateDir, "?#") {
		return fmt.Errorf("--state-dir 必须指定，且不得含 SQLite URI 分隔符")
	}
	stateDir, err := filepath.Abs(opts.stateDir)
	if err != nil {
		return err
	}
	key, err := readPrivateKey(opts.keyFile)
	if err != nil {
		return err
	}
	var assets *uiAssets
	if opts.uiDir != "" {
		assets, err = openUIAssets(opts.uiDir, opts.keyFile, stateDir)
		if err != nil {
			return fmt.Errorf("加载原生UI资产: %w", err)
		}
		defer assets.root.Close()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// 验收模式仍独占新目录；正式模式只重开本入口自己的已有状态，不清空/迁移。
	if opts.modelNode && opts.modelEndpoint == "" {
		if err := os.MkdirAll(stateDir, 0700); err != nil {
			return fmt.Errorf("创建或重开正式 state-dir: %w", err)
		}
		if err := os.Chmod(stateDir, 0700); err != nil {
			return fmt.Errorf("保护正式 state-dir: %w", err)
		}
	} else if err := os.Mkdir(stateDir, 0700); err != nil {
		return fmt.Errorf("创建专属新 state-dir: %w", err)
	}
	config := host.Config{
		Addr: addr.String(),
		// 上游 host.Serve 会向 Logger.Infof 传入 AccessKey。明确使用
		// 原生 io.Discard 日志器，禁止改接控制台/全局 logger。
		Logger: telemetry.NopLogger(),
		Management: webext.Config{
			Product:  "wscyber-local-hub",
			Database: filepath.Join(stateDir, "hub.sqlite"), AccessKey: key,
			// 默认不构造 Profile、ConfigStore、RuntimeLLM 或 Scans，
			// 不发现/读取宿主配置；UI仅通过原生host.Config.Static挂载。
		},
		Ready: ready,
	}
	// 原生Web业务扩展：保存“两输入+明确授权”任务记录、HTTP Flow证据、候选验证及报告。
	if err := webapi.PrepareWebAPISchema(filepath.Join(stateDir, "hub.sqlite")); err != nil {
		return fmt.Errorf("准备 webapi 验证与报告表: %w", err)
	}
	webapiCapability, err := webapi.NewCapability(filepath.Join(stateDir, "hub.sqlite"))
	if err != nil {
		return fmt.Errorf("初始化 webapi 能力: %w", err)
	}
	// 覆盖初始化后、host 接管前的失败路径；Capability.Close 可重复调用。
	defer func() { _ = webapiCapability.Close(context.Background()) }()
	config.Management.Capabilities = append(config.Management.Capabilities, webapiCapability)
	if assets != nil {
		config.Static = assets
		config.Extensions = append(config.Extensions, uiFallbackExtension(assets))
	}
	if opts.modelConfig {
		store := newModelConfigStore(stateDir)
		config.Management.ConfigStore = store
		config.Management.ConfigAPI.Sections = store.native.Codec.Sections
		config.Management.RuntimeLLM = store.native.RuntimeLLM
		config.Extensions = append(config.Extensions, modelProbeDenialExtension())
	}
	if opts.modelNode {
		if opts.modelEndpoint == "" {
			return serveProductModelNode(ctx, config, stateDir, key, opts.modelReport, webapiCapability)
		}
		return serveModelNode(ctx, config, stateDir, key, opts.modelEndpoint, opts.modelReport, webapiCapability)
	}
	if !opts.fixtureNode {
		return host.Serve(ctx, config)
	}
	return serveFixtureNode(ctx, config, stateDir, key, opts.fixtureReport)
}

func readPrivateKey(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("必须指定 --access-key-file；不允许关闭鉴权")
	}
	// 本入口面向 Linux；不跟随符号链接，不因 FIFO 等特殊文件阻塞。
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", fmt.Errorf("无法打开私有测试 key 文件")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return "", fmt.Errorf("测试 key 必须来自权限为 0600 的普通文件")
	}
	raw, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(raw) > 4096 {
		return "", fmt.Errorf("测试 key 文件不可读或超过 4096 字节")
	}
	key := strings.TrimSpace(string(raw))
	if key == "" || strings.ContainsFunc(key, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return "", fmt.Errorf("测试 key 必须非空且不含内部空白/控制字符")
	}
	return key, nil
}

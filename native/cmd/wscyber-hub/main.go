// wscyber-hub 提供显式本机 Hub；模型配置管理与固定工具 fixture 分别选择。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
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

	"wscyber/native/pgschema"
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
	// serve 子命令专用：允许非 loopback 监听、密钥文件 0600/0400、无 --ui-dir 时使用嵌入前端。
	serve   bool
	embedUI fs.FS
	// 启动期间由 serveLocalHub 填写，供 ready 事件读取；可为 nil。
	status *hubStatus
}

// hubStatus 只记录启动时实际选用的前端来源与清单哈希，不含 key 等私有信息。
type hubStatus struct {
	frontend       string // "ui-dir" | "embed" | ""
	manifestSHA256 string
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
	// 首参不以 "-" 开头即子命令；否则走原有旗标解析（演示栈、测试、验收分支不变）。
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "serve":
			return runServe(ctx, args[1:], output)
		case "version":
			return runVersion(args[1:], output)
		case "healthcheck":
			return runHealthcheck(ctx, args[1:], output)
		default:
			return fmt.Errorf("未知子命令 %q；可用：serve、version、healthcheck（或旧旗标形式 --local-hub …）", args[0])
		}
	}
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
	return runHub(ctx, opts, output, nil)
}

// runHub 启动 Hub 并输出 JSON 行事件 ready → (*-closed) → stopped；extra 为 ready 的附加字段。
func runHub(ctx context.Context, opts options, output io.Writer, extra map[string]any) error {
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
	if opts.status == nil {
		opts.status = &hubStatus{}
	}
	browser := browserPath()
	err := serveLocalHub(ctx, opts, func(addr string) {
		mode := "local-hub"
		if opts.serve {
			mode = "serve"
		}
		// 不输出 key、Cookie 或上游配置；这不是业务事件/状态协议。
		event := map[string]any{
			"event": "ready", "address": addr, "mode": mode,
			"execution_node": false, "fixture_node_configured": opts.fixtureNode, "frontend": opts.status.frontend != "",
			"model_config_management": opts.modelConfig || opts.modelNode, "model_node_configured": opts.modelNode,
			"version": currentBuildInfo(opts.status.manifestSHA256).Version, "browser": browser,
			// ready 仅指 Hub 监听；节点就绪必须通过原生握手和实际 API 核验。
		}
		for name, value := range extra {
			event[name] = value
		}
		readyErr = encoder.Encode(event)
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
	if opts.serve {
		// serve 允许内网/通配地址（反向代理终止 TLS）；仍只接受 IP 字面量，不解析主机名。
		if err != nil || addr.Addr().Zone() != "" {
			return fmt.Errorf("--listen 必须为 IP:port（如 127.0.0.1:8080、0.0.0.0:8080）")
		}
	} else if err != nil || !addr.Addr().IsLoopback() || addr.Addr().Zone() != "" {
		return fmt.Errorf("--listen 必须为明确的 loopback IP:port")
	}
	if strings.TrimSpace(opts.stateDir) == "" || strings.ContainsAny(opts.stateDir, "?#") {
		return fmt.Errorf("--state-dir 必须指定，且不得含 SQLite URI 分隔符")
	}
	stateDir, err := filepath.Abs(opts.stateDir)
	if err != nil {
		return err
	}
	key, err := readPrivateKey(opts.keyFile, opts.serve)
	if err != nil {
		return err
	}
	// 前端选择：--ui-dir（开发预览）> 嵌入前端（仅 serve）> 无前端。
	var assets *uiAssets
	source := ""
	if opts.uiDir != "" {
		assets, err = openUIAssets(opts.uiDir, opts.keyFile, stateDir)
		if err != nil {
			return fmt.Errorf("加载原生UI资产: %w", err)
		}
		defer assets.Close()
		source = "ui-dir"
	} else if opts.embedUI != nil {
		if assets, err = openEmbeddedUI(opts.embedUI); err != nil {
			return err
		}
		if assets != nil {
			source = "embed"
		}
	}
	manifestSHA := ""
	if assets != nil {
		manifestSHA = assets.manifestSHA256
	}
	if opts.status != nil {
		opts.status.frontend, opts.status.manifestSHA256 = source, manifestSHA
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
	// P3a 占位：webapi 已只接 PostgreSQL（NewCapability 要连接串），Hub 的连接串与迁移接线在 P3b；
	// 在此之前传空连接串，启动在这里明确失败（“连接串不能为空”）。
	webapiCapability, err := webapi.NewCapability(pgschema.DatabaseURL{}, stateDir)
	if err != nil {
		return fmt.Errorf("初始化 webapi 能力: %w", err)
	}
	// 覆盖初始化后、host 接管前的失败路径；Capability.Close 可重复调用。
	defer func() { _ = webapiCapability.Close(context.Background()) }()
	config.Management.Capabilities = append(config.Management.Capabilities, webapiCapability)
	config.Extensions = append(config.Extensions, versionExtension(currentBuildInfo(manifestSHA)))
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
		// katana（上游 goflags）在用户配置目录生成默认配置；未设 XDG_CONFIG_HOME 时指到 state-dir 下（见 native/BROWSER.md）。
		if err := webapi.EnsureToolConfigHome(stateDir); err != nil {
			return err
		}
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

// readPrivateKey 读取访问密钥。旧旗标形式要求恰为 0600；serve（relaxed）另接受 0400
// （systemd LoadCredential）。两者都拒绝符号链接与特殊文件。
func readPrivateKey(path string, relaxed bool) (string, error) {
	if path == "" {
		return "", fmt.Errorf("必须指定 --access-key-file；不允许关闭鉴权")
	}
	raw, err := readPrivateFile(path, relaxed)
	if err != nil {
		if relaxed {
			return "", fmt.Errorf("访问密钥文件%w", err)
		}
		// 旧旗标形式保留原错误文本。
		var fileErr *privateFileError
		errors.As(err, &fileErr)
		switch fileErr.kind {
		case privateFileOpen:
			return "", fmt.Errorf("无法打开私有测试 key 文件")
		case privateFileMode:
			return "", fmt.Errorf("测试 key 必须来自权限为 0600 的普通文件")
		default:
			return "", fmt.Errorf("测试 key 文件不可读或超过 4096 字节")
		}
	}
	key := strings.TrimSpace(string(raw))
	if key == "" || strings.ContainsFunc(key, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return "", fmt.Errorf("测试 key 必须非空且不含内部空白/控制字符")
	}
	return key, nil
}

type privateFileErrorKind int

const (
	privateFileOpen privateFileErrorKind = iota
	privateFileMode
	privateFileSize
)

// privateFileError 只描述失败类别，不含文件内容。
type privateFileError struct {
	kind privateFileErrorKind
	msg  string
}

func (e *privateFileError) Error() string { return e.msg }

// readPrivateFile 读取 ≤4096 字节的私有普通文件：不跟随符号链接、不因 FIFO 等阻塞；
// 权限须恰为 0600，relaxed 时另接受 0400（属主只读，group/other 无任何权限）。
func readPrivateFile(path string, relaxed bool) ([]byte, error) {
	// 本入口面向 Linux。
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, &privateFileError{privateFileOpen, "无法打开（须存在且不是符号链接）"}
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, &privateFileError{privateFileMode, "须为普通文件"}
	}
	if perm := info.Mode().Perm(); perm != 0600 && !(relaxed && perm == 0400) {
		allowed := "0600"
		if relaxed {
			allowed = "0600 或 0400"
		}
		return nil, &privateFileError{privateFileMode, fmt.Sprintf("权限须为 %s（当前 %04o）", allowed, perm)}
	}
	raw, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(raw) > 4096 {
		return nil, &privateFileError{privateFileSize, "不可读或超过 4096 字节"}
	}
	return raw, nil
}

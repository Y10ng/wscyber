package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"wscyber/internal/config"
	"wscyber/internal/domain"
	"wscyber/internal/execution"
	"wscyber/internal/httpapi"
	"wscyber/internal/model"
	"wscyber/internal/modelconfig"
	"wscyber/internal/modelledger"
	"wscyber/internal/modelprobe"
	"wscyber/internal/research"
	"wscyber/internal/store"
)

func main() {
	if err := run(); err != nil {
		slog.Error("wscyber 启动/运行失败", "reason", err.Error())
		os.Exit(1)
	}
}
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	var vault *modelconfig.Vault
	if cfg.ModelEncryptionKey != "" {
		vault, err = modelconfig.NewVault(cfg.ModelEncryptionKey)
		if err != nil {
			return err
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, 20*time.Second)
	repo, err := store.Open(startup, cfg.DatabaseURL)
	if err != nil {
		cancel()
		return fmt.Errorf("数据库连接、schema 初始化或单实例锁失败，请检查本项目 PostgreSQL 配置（不输出含凭据的连接错误）")
	}
	defer repo.Close()
	models := modelconfig.New(repo, vault)
	ledger := modelledger.New(repo, vault, models)
	if models.Available() {
		if _, err = models.List(startup); err != nil {
			cancel()
			return fmt.Errorf("模型配置解密校验失败，未启动调度；请核对部署密钥和数据完整性")
		}
	}
	if err = repo.Recover(startup); err != nil {
		cancel()
		return fmt.Errorf("数据库恢复失败，未启动调度")
	}
	cancel()
	var mdl domain.Model
	environmentConfig := model.Config{BaseURL: cfg.ModelBaseURL, APIKey: cfg.ModelAPIKey, Model: cfg.ModelName, Timeout: cfg.ModelTimeout}
	fingerprint := ""
	if cfg.ModelConfigured() {
		fingerprint = model.EnvironmentFingerprint(environmentConfig)
		mdl, err = model.New(environmentConfig, ledger)
		if err != nil {
			return fmt.Errorf("模型配置无效，未启动调度")
		}
	}
	routed := model.NewRouted(repo, models, mdl, fingerprint, ledger)
	// 路由始终可用：启动后新配置立即可用；已有绑定缺失密钥会明确阻塞而不是静默换模型。
	mdl = routed
	probes := modelprobe.New(ctx, repo, models, ledger)
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = probes.Close(cleanup)
	}()
	executor := execution.New(execution.Config{Image: cfg.SandboxImage, Runtime: cfg.SandboxRuntime, Network: cfg.SandboxNetwork, Timeout: cfg.ActionTimeout, MaxOutputBytes: cfg.MaxOutputBytes, AllowLoopback: cfg.AllowLoopback})
	info := httpapi.SystemInfo{ModelContentRecordingAvailable: ledger.Available(), Name: "wscyber", Version: "0.1.0-dev", ModelConfigured: cfg.ModelConfigured(), Model: cfg.ModelName, SandboxImage: cfg.SandboxImage, SandboxRuntime: cfg.SandboxRuntime, NetworkMode: cfg.SandboxNetwork + "；HTTP 动作风险分流，工具直连未拦截", MaxParallel: cfg.MaxParallel, MaxSteps: cfg.MaxSteps}
	server := &http.Server{Addr: cfg.Listen, Handler: httpapi.New(repo, executor, httpapi.Config{Token: cfg.AuthToken, WebDir: cfg.WebDir, System: info, Models: models, Probes: probes, Ledger: ledger, MissionModels: routed, EnvironmentFingerprint: fingerprint}), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	engine := research.New(repo, mdl, executor, research.Config{MaxParallel: cfg.MaxParallel, MaxSteps: cfg.MaxSteps, PollInterval: 500 * time.Millisecond, ModelTimeout: cfg.ModelTimeout, ActionTimeout: cfg.ActionTimeout})
	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("无法监听配置地址；未启动研究与动作调度")
	}
	defer listener.Close()
	engineDone := make(chan struct{})
	go func() { defer close(engineDone); engine.Run(ctx) }()
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	slog.Info("wscyber 已启动", "listen", cfg.Listen, "model_configured", cfg.ModelConfigured(), "runtime", cfg.SandboxRuntime)
	if cfg.AllowLoopback {
		slog.Warn("已启用本地 fixture loopback 访问；仅用于受控开发验证")
	}
	select {
	case <-ctx.Done():
	case <-ledger.Failed():
		err = fmt.Errorf("模型调用账本持久化未确认，停止全部模型派发；请重启恢复，不得自动重发")
	case <-probes.Done():
		if probes.Err() != nil {
			err = fmt.Errorf("连通测试持久执行已停止，未重试外发；请重新启动恢复回执：%w", probes.Err())
		}
	case err = <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
	}
	stop()
	shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdown)
	if probeErr := probes.Close(shutdown); probeErr != nil && !errors.Is(err, probeErr) {
		// Close也会返回此前worker的失败；保留首个原因，且不重复输出同一失败。
		err = errors.Join(err, fmt.Errorf("连通测试执行或关闭存在未解决错误，请勿假定未发送；下次启动将保守恢复：%w", probeErr))
	}
	// 等待执行器清理与回执落库后再释放单实例锁。异常退出由下次 Recover 保守标未知。
	select {
	case <-engineDone:
	case <-shutdown.Done():
		return errors.Join(err, fmt.Errorf("关闭等待超时；请勿假定在途动作未执行，下次启动将保守恢复"))
	}
	return err
}

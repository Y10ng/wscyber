// wscyber-native 是 Cyber 原生装配入口；H1 仅开放显式本机 fixture 验收。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) (resultErr error) {
	flags := flag.NewFlagSet("wscyber-native", flag.ContinueOnError)
	flags.SetOutput(stderr)
	fixture := flags.Bool("fixture", false, "显式使用无模型网络的确定性 fixture（非生产研究）")
	scenario := flags.String("scenario", "success", "fixture 场景：success 或 cancel-after-tool")
	workRoot := flags.String("work-root", "", "已存在的专属临时目录父路径（必填）")
	evidence := flags.String("evidence", "", "新建的验收证据目录，不覆盖已有目录（必填）")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if !*fixture {
		return errors.New("生产 provider/任务授权尚未接线；拒绝启动，验收须显式指定 --fixture")
	}
	if flags.NArg() != 0 || (*scenario != "success" && *scenario != "cancel-after-tool") || *workRoot == "" || *evidence == "" {
		return errors.New("需要 --fixture --scenario success|cancel-after-tool --work-root DIR --evidence NEW_DIR，且不接受位置参数")
	}
	root, err := filepath.Abs(*workRoot)
	if err != nil {
		return err
	}
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("work-root 必须是已存在的目录")
	}
	if err := os.Mkdir(*evidence, 0700); err != nil {
		return fmt.Errorf("新建证据目录: %w", err)
	}
	events, err := os.OpenFile(filepath.Join(*evidence, "native-events.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, events.Close()) }()
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	report, runErr := runFixture(ctx, root, *scenario, events)
	if runErr != nil {
		report.Error = runErr.Error()
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return errors.Join(runErr, err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(*evidence, "fixture-summary.json"), data, 0600); err != nil {
		return errors.Join(runErr, err)
	}
	_, err = stdout.Write(data)
	return errors.Join(runErr, err)
}

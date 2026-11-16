package execution

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"wscyber/internal/domain"
)

// 此替身只验证 CLI 参数和控制流程，绝不调用 Docker 或启动容器。
type dockerFixture struct {
	t            *testing.T
	calls        [][]string
	name         string
	token        string
	id           string
	removed      bool
	wrongOwner   bool
	failCreate   bool
	failCleanup  bool
	timeoutStart bool
	exitCode     int
	stdout       string
	stderr       string
}

func newDockerFixture(t *testing.T, config Config) (*Executor, *dockerFixture) {
	t.Helper()
	e := New(config)
	fixture := &dockerFixture{t: t, id: strings.Repeat("a", 64)}
	e.docker = fixture.run
	return e, fixture
}

func option(args []string, flag string) string {
	for i := range args {
		if args[i] == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func contains(args []string, value string) bool {
	for _, arg := range args {
		if arg == value {
			return true
		}
	}
	return false
}

func (f *dockerFixture) run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	f.calls = append(f.calls, append([]string(nil), args...))
	switch args[0] {
	case "create":
		f.name = option(args, "--name")
		f.token = strings.TrimPrefix(option(args, "--label"), executionLabel+"=")
		if f.failCreate {
			fmt.Fprint(stderr, "unknown runtime")
			return errors.New("create failed")
		}
		fmt.Fprintln(stdout, f.id)
		return nil
	case "start":
		if !reflect.DeepEqual(args, []string{"start", "--attach", f.id}) {
			f.t.Errorf("unexpected start args: %q", args)
		}
		io.WriteString(stdout, f.stdout)
		io.WriteString(stderr, f.stderr)
		if f.timeoutStart {
			<-ctx.Done()
			return ctx.Err()
		}
		if f.exitCode != 0 {
			return errors.New("container command nonzero")
		}
		return nil
	case "rm":
		if ctx.Err() != nil {
			f.t.Error("cleanup inherited the canceled execution context")
		}
		if !reflect.DeepEqual(args, []string{"rm", "--force", "--volumes", f.id}) {
			f.t.Errorf("cleanup is not limited to this exact container: %q", args)
		}
		if f.failCleanup {
			return errors.New("daemon unavailable")
		}
		f.removed = true
		fmt.Fprintln(stdout, f.id)
		return nil
	case "inspect":
		target := args[len(args)-1]
		format := option(args, "--format")
		if f.failCreate || f.removed {
			fmt.Fprint(stderr, "Error: No such object: "+target)
			return errors.New("not found")
		}
		if strings.Contains(format, ".Config.Labels") {
			token := f.token
			if f.wrongOwner {
				token = "not-our-token"
			}
			fmt.Fprintf(stdout, "%s %s\n", f.id, token)
			return nil
		}
		if format == "{{json .State}}" {
			if f.timeoutStart {
				fmt.Fprint(stdout, `{"Status":"running","Running":true,"ExitCode":0}`)
			} else {
				fmt.Fprintf(stdout, `{"Status":"exited","Running":false,"ExitCode":%d}`, f.exitCode)
			}
			return nil
		}
		if f.failCleanup {
			return errors.New("daemon unavailable")
		}
		fmt.Fprintln(stdout, f.id)
		return nil
	default:
		f.t.Errorf("unexpected Docker command: %q", args)
		return errors.New("unexpected command")
	}
}

func TestDockerHardeningArgumentsAndUniqueIdentity(t *testing.T) {
	e, fixture := newDockerFixture(t, Config{})
	command := "printf '%s' '$(not-a-host-command)'"
	result, err := e.Execute(context.Background(), domain.ActionInput{Kind: "local_command", Command: command}, nil)
	if err != nil || result.ExitCode != 0 || result.EffectUnknown || !fixture.removed {
		t.Fatalf("execution/cleanup failed: %#v, %v", result, err)
	}
	args := fixture.calls[0]
	for flag, want := range map[string]string{
		"--network": "bridge", "--user": "65534:65534", "--cap-drop": "ALL",
		"--security-opt": "no-new-privileges:true", "--pids-limit": "64", "--memory": "256m",
		"--memory-swap": "256m", "--cpus": "1", "--entrypoint": "/usr/bin/env", "--workdir": "/tmp",
		"--runtime": "runc", "--pull": "never", "--ipc": "none", "--log-driver": "none",
	} {
		if got := option(args, flag); got != want {
			t.Errorf("%s = %q; want %q", flag, got, want)
		}
	}
	if !contains(args, "--init") || !contains(args, "--read-only") || !contains(args, "--no-healthcheck") || !strings.Contains(option(args, "--tmpfs"), "nosuid,nodev") {
		t.Fatalf("missing filesystem/healthcheck hardening: %q", args)
	}
	if !contains(args, defaultImage) || args[len(args)-1] != command || !contains(args, "-i") || !contains(args, "/usr/bin/timeout") || !contains(args, "--kill-after=2s") {
		t.Fatalf("image/entrypoint/command not explicit: %q", args)
	}
	for _, forbidden := range []string{"--privileged", "--volume", "--mount", "--env-file", "--env", "--pid", "--rm"} {
		if contains(args, forbidden) {
			t.Errorf("unexpected sandbox option %s", forbidden)
		}
	}
	if !strings.HasPrefix(fixture.name, "wscyber-exec-") || len(fixture.token) != 32 || fixture.name != "wscyber-exec-"+fixture.token {
		t.Fatalf("invalid identity name=%q token=%q", fixture.name, fixture.token)
	}
	firstName := fixture.name
	fixture.removed = false
	_, err = e.Execute(context.Background(), domain.ActionInput{Kind: "local_command", Command: "true"}, nil)
	if err != nil || fixture.name == firstName {
		t.Fatalf("container name reused or next execution failed: %q, %v", fixture.name, err)
	}
}

func TestDockerNetworkSelection(t *testing.T) {
	e, fixture := newDockerFixture(t, Config{Network: "none"})
	if _, err := e.Execute(context.Background(), domain.ActionInput{Kind: "local_command", Command: "true"}, nil); err != nil {
		t.Fatal(err)
	}
	if option(fixture.calls[0], "--network") != "none" || strings.Contains(option(fixture.calls[0], "--tmpfs"), "noexec") {
		t.Fatal("离线开关或临时可执行工作区无效", fixture.calls[0])
	}
	e, fixture = newDockerFixture(t, Config{Network: "host"})
	if _, err := e.Execute(context.Background(), domain.ActionInput{Kind: "local_command", Command: "true"}, nil); err == nil || len(fixture.calls) != 0 {
		t.Fatal("不支持的网络模式应在创建容器前失败")
	}
}

func TestDockerSharedOutputLimitAndExitStatus(t *testing.T) {
	e, fixture := newDockerFixture(t, Config{MaxOutputBytes: 5})
	fixture.stdout, fixture.stderr, fixture.exitCode = "1234", "5678", 7
	result, err := e.Execute(context.Background(), domain.ActionInput{Kind: "local_command", Command: "exit 7"}, nil)
	if err == nil || result.ExitCode != 7 || result.EffectUnknown || !result.Truncated || len(result.Stdout)+len(result.Stderr) != 5 || !fixture.removed {
		t.Fatalf("incorrect output/exit capture: %#v, %v", result, err)
	}
}

func TestDockerTimeoutStillConfirmsCleanup(t *testing.T) {
	e, fixture := newDockerFixture(t, Config{Timeout: 10 * time.Millisecond})
	fixture.timeoutStart = true
	result, err := e.Execute(context.Background(), domain.ActionInput{Kind: "local_command", Command: "never finishes"}, nil)
	if err == nil || !fixture.removed || !result.EffectUnknown || !result.Truncated {
		t.Fatalf("timeout did not retain uncertainty and clean: %#v, %v", result, err)
	}
	last := fixture.calls[len(fixture.calls)-1]
	if last[0] != "inspect" || last[len(last)-1] != fixture.id {
		t.Fatalf("cleanup absence was not checked: %q", last)
	}
}

func TestDockerCleanupFailureIsUnknown(t *testing.T) {
	e, fixture := newDockerFixture(t, Config{})
	fixture.failCleanup = true
	result, err := e.Execute(context.Background(), domain.ActionInput{Kind: "local_command", Command: "true"}, nil)
	if err == nil || !result.EffectUnknown || !strings.Contains(err.Error(), "未确认沙箱") {
		t.Fatalf("cleanup failure was hidden: %#v, %v", result, err)
	}
}

func TestDockerWrongLabelNeverDeletesContainer(t *testing.T) {
	e, fixture := newDockerFixture(t, Config{})
	fixture.wrongOwner = true
	result, err := e.Execute(context.Background(), domain.ActionInput{Kind: "local_command", Command: "true"}, nil)
	if err == nil || !result.EffectUnknown {
		t.Fatalf("ownership failure was hidden: %#v, %v", result, err)
	}
	for _, args := range fixture.calls {
		if args[0] == "start" || args[0] == "rm" {
			t.Fatalf("touched mismatched container: %q", args)
		}
	}
}

func TestDockerRuntimeFailureDoesNotFallback(t *testing.T) {
	e, fixture := newDockerFixture(t, Config{Runtime: "runsc"})
	fixture.failCreate = true
	result, err := e.Execute(context.Background(), domain.ActionInput{Kind: "local_command", Command: "true"}, nil)
	if err == nil || !result.EffectUnknown {
		t.Fatalf("unconfirmed creation should fail conservatively: %#v, %v", result, err)
	}
	creates := 0
	for _, args := range fixture.calls {
		if args[0] == "create" {
			creates++
			if option(args, "--runtime") != "runsc" {
				t.Fatalf("explicit runtime changed: %q", args)
			}
		}
		if args[0] == "start" || args[0] == "rm" {
			t.Fatalf("operated on a container whose creation failed: %q", args)
		}
	}
	if creates != 1 {
		t.Fatalf("runtime failure retried %d times", creates)
	}
}

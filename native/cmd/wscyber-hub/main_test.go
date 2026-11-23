package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	aop "github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/extension"
	"github.com/chainreactors/cyber/core/types"
	webext "github.com/chainreactors/cyber/exts/web"
	"github.com/chainreactors/cyber/pkg/rpc"
	webpkg "github.com/chainreactors/cyber/pkg/web"
	webapi "github.com/chainreactors/cyber/pkg/web/api"
	webservice "github.com/chainreactors/cyber/pkg/web/service"

	"wscyber/native/internal/testdb"
	"wscyber/native/pgschema"
	webapitasks "wscyber/native/webapi"
)

func testOptions(t *testing.T) (options, string) {
	t.Helper()
	root := t.TempDir()
	key := rand.Text()
	keyFile := filepath.Join(root, "access.key")
	if err := os.WriteFile(keyFile, []byte(key+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// 每个测试一个从已迁移模板克隆的独立 PG 库 + 专属 state-dir（native/POSTGRES.md §7.1）。
	return options{localHub: true, listen: "127.0.0.1:0", keyFile: keyFile, stateDir: filepath.Join(root, "state"), database: testdb.New(t).URL}, key
}

func TestRejectUnsafeConfig(t *testing.T) {
	cases := map[string]func(*options){
		"production":      func(o *options) { o.localHub = false },
		"wildcard":        func(o *options) { o.listen = "0.0.0.0:0" },
		"ipv6-wildcard":   func(o *options) { o.listen = "[::]:0" },
		"dns":             func(o *options) { o.listen = "localhost:0" },
		"remote":          func(o *options) { o.listen = "192.0.2.1:80" },
		"bad-port":        func(o *options) { o.listen = "127.0.0.1:65536" },
		"missing-key":     func(o *options) { o.keyFile = "" },
		"empty-key":       func(o *options) { mustWrite(t, o.keyFile, []byte(" \n")) },
		"multi-token-key": func(o *options) { mustWrite(t, o.keyFile, []byte("test token")) },
		"public-key-file": func(o *options) {
			if err := os.Chmod(o.keyFile, 0644); err != nil {
				t.Fatal(err)
			}
		},
		"symlink-key": func(o *options) {
			link := o.keyFile + ".link"
			if err := os.Symlink(o.keyFile, link); err != nil {
				t.Fatal(err)
			}
			o.keyFile = link
		},
		"missing-state": func(o *options) { o.stateDir = "" },
		"missing-database": func(o *options) {
			o.database = pgschema.DatabaseURL{}
			t.Setenv(envDatabaseURLFile, "")
			t.Setenv(envDatabaseURL, "")
		},
		"public-database-file": func(o *options) {
			o.databaseURLFile = writeDatabaseURLFile(t, o.database)
			o.database = pgschema.DatabaseURL{}
			if err := os.Chmod(o.databaseURLFile, 0644); err != nil {
				t.Fatal(err)
			}
		},
		"existing-state": func(o *options) {
			if err := os.Mkdir(o.stateDir, 0700); err != nil {
				t.Fatal(err)
			}
			mustWrite(t, filepath.Join(o.stateDir, "keep"), []byte("unchanged"))
		},
		"invalid-existing-state": func(o *options) {
			o.modelNode = true
			mustWrite(t, o.stateDir, []byte("not a directory"))
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			opts, key := testOptions(t)
			mutate(&opts)
			called := false
			err := serveLocalHub(t.Context(), opts, func(string) { called = true })
			if err == nil || called {
				t.Fatal("unsafe configuration was not rejected before listening")
			}
			if strings.Contains(err.Error(), key) {
				t.Fatal("error exposed private test key")
			}
			if !opts.database.IsZero() && strings.Contains(err.Error(), opts.database.DSN()) {
				t.Fatal("error exposed database URL")
			}
			if name == "existing-state" {
				data, err := os.ReadFile(filepath.Join(opts.stateDir, "keep"))
				if err != nil || string(data) != "unchanged" {
					t.Fatal("existing state was changed")
				}
			}
		})
	}
}

func TestLocalHubNativeAPIAndShutdown(t *testing.T) {
	opts, key := testOptions(t)
	// 本包不并行测试；截获真实进程输出而非替换原生日志器，防止 key 泄漏回归。
	capture, err := os.CreateTemp(t.TempDir(), "host-output-")
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = capture, capture
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr; _ = capture.Close() }()
	ctx, cancel := context.WithCancel(t.Context())
	ready, done := make(chan string, 1), make(chan error, 1)
	go func() { done <- serveLocalHub(ctx, opts, func(addr string) { ready <- addr }) }()
	joined := false
	t.Cleanup(func() {
		cancel()
		if !joined {
			select {
			case err := <-done:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(12 * time.Second):
				t.Error("host did not close")
			}
		}
	})
	var addr string
	select {
	case addr = <-ready:
	case err := <-done:
		joined = true
		t.Fatalf("host startup failed: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("host not ready")
	}
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	base := "http://" + addr
	get := func(path, token string, want int) []byte {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, base+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != want {
			t.Fatalf("%s status=%d want=%d", path, res.StatusCode, want)
		}
		if strings.Contains(string(body), key) {
			t.Fatal("read-only response exposed key")
		}
		return body
	}
	get("/api/manifest", "", 401)
	get("/api/manifest", "invalid", 401)
	var manifest webpkg.Manifest
	if err := json.Unmarshal(get("/api/manifest", key, 200), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Product != "wscyber-local-hub" || len(manifest.Capabilities) != 2 || manifest.Capabilities[0].ID != "core" || manifest.Capabilities[1].ID != "webapi" || len(manifest.Profiles) != 0 {
		t.Fatal("manifest does not describe the native core+webapi empty Hub")
	}
	get("/", "", 404)
	get("/health", "", 200)
	get(rpc.ScanServiceListScansProcedure, key, 404)

	sessions := rpc.NewSessionServiceClient(client, base)
	_, err = sessions.ListSessions(t.Context(), connect.NewRequest(&types.ListSessionsRequest{}))
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatal("native Connect route did not reject missing authentication")
	}
	request := connect.NewRequest(&types.ListSessionsRequest{})
	request.Header().Set("Authorization", "Bearer "+key)
	listed, err := sessions.ListSessions(t.Context(), request)
	if err != nil || len(listed.Msg.GetSessions()) != 0 {
		t.Fatal("native empty session listing failed")
	}

	statusRequest := connect.NewRequest(&types.GetStatusRequest{})
	statusRequest.Header().Set("Authorization", "Bearer "+key)
	status, err := rpc.NewSystemServiceClient(client, base).GetStatus(t.Context(), statusRequest)
	if err != nil {
		t.Fatal(err)
	}
	if s := status.Msg.GetStatus(); s.GetLlmAvailable() || s.GetLlmApiKeyConfigured() || s.GetConfigLoaded() || s.GetLlmProvider() != "" || s.GetLlmModel() != "" || s.GetConfigPath() != "" {
		t.Fatal("empty Hub unexpectedly loaded a model/config")
	}
	agentRequest := connect.NewRequest(&types.ListAgentsRequest{})
	agentRequest.Header().Set("Authorization", "Bearer "+key)
	agents, err := rpc.NewAgentServiceClient(client, base).ListAgents(t.Context(), agentRequest)
	if err != nil || len(agents.Msg.GetAgents()) != 0 {
		t.Fatal("empty Hub unexpectedly has an execution node")
	}

	cancel()
	select {
	case err := <-done:
		joined = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(12 * time.Second):
		t.Fatal("native ServeHTTP shutdown timed out")
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal("listening port was not released")
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	assertNoStateFD(t, opts.stateDir)
	assertNoHubConnections(t, opts.database)
	store, err := webservice.NewSQLiteStore(opts.database.DSN(), webapitasks.SchemaModule())
	if err != nil {
		t.Fatal(err)
	}
	rows, listErr := store.ListSessions(t.Context(), 1)
	closeErr := store.Close()
	if listErr != nil || closeErr != nil || len(rows) != 0 {
		t.Fatal("native empty store reopen/list/close failed")
	}
	assertNoStateFD(t, opts.stateDir)
	output, err := os.ReadFile(capture.Name())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(output), key) {
		t.Fatal("host logger exposed private test key")
	}
}

func TestListenFailureClosesStore(t *testing.T) {
	opts, key := testOptions(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	opts.listen = listener.Addr().String()
	err = serveLocalHub(t.Context(), opts, func(string) { t.Error("occupied listener was reported ready") })
	if err == nil {
		t.Fatal("occupied listener was accepted")
	}
	if strings.Contains(err.Error(), key) {
		t.Fatal("startup failure exposed key")
	}
	assertNoStateFD(t, opts.stateDir)
	assertNoHubConnections(t, opts.database)
}

func TestEmptyHubUnavailable(t *testing.T) {
	// 不请求实际目标/模型；直接验证所复用的原生 Service 对缺失 node 的语义。
	web := webext.New(webext.Config{Product: "wscyber-local-hub", Database: testdb.New(t).DSN(), AccessKey: rand.Text()})
	set, err := extension.New(web)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := set.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	if err := set.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	service := web.Service().(*webservice.Service)
	err = service.OpenAgentSession(t.Context(), "h2a-no-node", &aop.OpenSessionRequest{NodeId: "not-connected", SessionId: "not-created"})
	var nativeErr *webapi.Error
	if !errors.As(err, &nativeErr) || nativeErr.Code != webapi.CodeUnavailable {
		t.Fatal("empty Hub did not return native unavailable")
	}
}

func assertNoStateFD(t *testing.T, stateDir string) {
	t.Helper()
	files, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		target, err := os.Readlink(filepath.Join("/proc/self/fd", f.Name()))
		if err == nil && strings.HasPrefix(target, stateDir+string(os.PathSeparator)) {
			t.Fatal("state descriptor remains open")
		}
	}
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

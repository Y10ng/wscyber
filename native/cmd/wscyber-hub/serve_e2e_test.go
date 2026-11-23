package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chainreactors/cyber/core/types"
	"github.com/chainreactors/cyber/pkg/rpc"

	"wscyber/native/internal/testdb"
)

type runningHub struct {
	events <-chan map[string]any
	done   <-chan error
	cancel context.CancelFunc
}

// startRun 在进程内运行 run(args)，逐行解码 stdout 事件。
func startRun(t *testing.T, args ...string) runningHub {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	reader, writer := io.Pipe()
	events, done := make(chan map[string]any, 8), make(chan error, 1)
	go func() {
		err := run(ctx, args, writer)
		writer.Close()
		done <- err
	}()
	go func() {
		decoder := json.NewDecoder(reader)
		for {
			var event map[string]any
			if decoder.Decode(&event) != nil {
				close(events)
				return
			}
			events <- event
		}
	}()
	t.Cleanup(cancel)
	return runningHub{events: events, done: done, cancel: cancel}
}

func (h runningHub) next(t *testing.T) map[string]any {
	t.Helper()
	select {
	case event, ok := <-h.events:
		if !ok {
			t.Fatalf("hub exited early: %v", <-h.done)
		}
		return event
	case <-time.After(15 * time.Second):
		t.Fatal("hub event timeout")
	}
	return nil
}

// stop 取消并核对收尾事件序列以 stopped 结束、run 无错误。
func (h runningHub) stop(t *testing.T) []string {
	t.Helper()
	h.cancel()
	var names []string
	for {
		select {
		case event, ok := <-h.events:
			if !ok {
				if err := <-h.done; err != nil {
					t.Fatal(err)
				}
				if len(names) == 0 || names[len(names)-1] != "stopped" {
					t.Fatalf("shutdown events: %v", names)
				}
				return names
			}
			names = append(names, event["event"].(string))
		case <-time.After(15 * time.Second):
			t.Fatal("hub did not stop")
		}
	}
}

func httpGet(t *testing.T, url, key string) (int, string) {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(body)
}

// waitModelNode 核验进程内正式节点成功回连登记，不是停止前的时序规避。
func waitModelNode(t *testing.T, base, key string) {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	agents := rpc.NewAgentServiceClient(client, base)
	deadline := time.Now().Add(10 * time.Second)
	for {
		listed, err := agents.ListAgents(t.Context(), modelAuthRequest(&types.ListAgentsRequest{}, key))
		if err == nil && len(listed.Msg.GetAgents()) == 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("model node did not register: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestServeEndToEnd(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{"XDG_DATA_HOME", "XDG_CONFIG_HOME", envListen, envStateDir, envAccessKeyFile, envDatabaseURLFile} {
		t.Setenv(name, "")
	}
	t.Setenv("HOME", home)
	database := testdb.New(t).URL
	dsn, password := database.DSN(), databasePassword(t, database)
	// 第一次经环境变量取连接串（读后删除），第二次经 --database-url-file。
	t.Setenv(envDatabaseURL, dsn)
	oldUI, oldNotices := embeddedUI, notices
	t.Cleanup(func() { embeddedUI, notices = oldUI, oldNotices })
	embedded := syntheticEmbeddedUI(t)
	embeddedUI = func() fs.FS { return embedded }
	var noticeText bytes.Buffer
	notices = &noticeText

	hub := startRun(t, "serve", "--listen", "0.0.0.0:0")
	ready := hub.next(t)
	state := filepath.Join(home, ".local", "share", "wscyber")
	keyFile := filepath.Join(state, "access.key")
	if ready["event"] != "ready" || ready["mode"] != "serve" || ready["frontend"] != true || ready["version"] != "dev" ||
		ready["access_key_file"] != keyFile || ready["state_dir"] != state || ready["model_node_configured"] != true {
		t.Fatalf("ready event: %v", ready)
	}
	if _, ok := ready["browser"].(string); !ok {
		t.Fatal("ready lacks browser")
	}
	if db, _ := ready["database"].(map[string]any); db["database"] != database.Database() || db["user"] != database.User() || ready["schema"] != float64(1) {
		t.Fatalf("ready database/schema: %v %v", ready["database"], ready["schema"])
	}
	if _, set := os.LookupEnv(envDatabaseURL); set {
		t.Fatal("WSCYBER_DATABASE_URL still in environment")
	}
	key, err := readPrivateKey(keyFile, true)
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + nodeDialAddress(ready["address"].(string))
	if status, body := httpGet(t, base+"/", ""); status != 200 || !strings.Contains(body, "synthetic-ui-boundary-only") {
		t.Fatalf("embedded index: %d", status)
	}
	if status, _ := httpGet(t, base+"/health", ""); status != 200 {
		t.Fatal("/health")
	}
	if status, _ := httpGet(t, base+"/api/wscyber/version", ""); status != 401 {
		t.Fatal("version API without key not rejected")
	}
	status, body := httpGet(t, base+"/api/wscyber/version", key)
	var info map[string]any
	if status != 200 || json.Unmarshal([]byte(body), &info) != nil || info["version"] != "dev" || len(info["ui_manifest_sha256"].(string)) != 64 {
		t.Fatalf("version API: %d %s", status, body)
	}
	// 通配监听下进程内节点经 127.0.0.1 回连并登记。
	waitModelNode(t, base, key)
	var out bytes.Buffer
	if err := run(t.Context(), []string{"healthcheck", "--url", base}, &out); err != nil || out.String() != "ok /health\n" {
		t.Fatalf("healthcheck: %q %v", out.String(), err)
	}
	if names := hub.stop(t); strings.Join(names, ",") != "model-node-closed,stopped" {
		t.Fatalf("shutdown events: %v", names)
	}
	if !strings.Contains(noticeText.String(), "已生成访问密钥文件") || !strings.Contains(noticeText.String(), "来源 "+envDatabaseURL) {
		t.Fatalf("notices: %s", noticeText.String())
	}
	firstNotices := noticeText.String()
	assertNoHubConnections(t, database)

	// 第二次启动：同一 state 复用密钥；--ui-dir 优先于嵌入前端。
	noticeText.Reset()
	uiDir := syntheticUI(t)
	hub = startRun(t, "serve", "--listen", "127.0.0.1:0", "--ui-dir", uiDir, "--database-url-file", writeDatabaseURLFile(t, database))
	ready = hub.next(t)
	if ready["frontend"] != true || ready["access_key_file"] != keyFile {
		t.Fatalf("second ready: %v", ready)
	}
	if again, _ := readPrivateKey(keyFile, true); again != key {
		t.Fatal("access key changed on restart")
	}
	if status, _ := httpGet(t, "http://"+ready["address"].(string)+"/api/wscyber/version", key); status != 200 {
		t.Fatal("reused key rejected")
	}
	hub.stop(t)
	if strings.Contains(noticeText.String(), "已生成") || !strings.Contains(noticeText.String(), "来源 --database-url-file") {
		t.Fatalf("second notices: %s", noticeText.String())
	}
	assertNoHubConnections(t, database)
	for _, text := range []string{firstNotices, noticeText.String(), body} {
		if strings.Contains(text, key) || strings.Contains(text, dsn) || strings.Contains(text, password) {
			t.Fatal("secret exposed")
		}
	}
}

func TestServeRejectsBadInputBeforeSideEffects(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv(envStateDir, "")
	t.Setenv(envDatabaseURL, "")
	t.Setenv(envDatabaseURLFile, "")
	dbFile := filepath.Join(home, "db.url")
	mustWrite(t, dbFile, []byte("postgres://u@db.example.test/d"))
	if err := os.Chmod(dbFile, 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"serve", "--listen", "localhost:8080"},
		{"serve", "--listen", "[fe80::1%eth0]:8080"},
		{"serve", "--database-url-file", dbFile},
		{"serve"}, // 没有任何连接串
	} {
		if err := run(t.Context(), args, io.Discard); err == nil {
			t.Fatalf("%v accepted", args)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".local")); !os.IsNotExist(err) {
		t.Fatal("rejected serve created state")
	}
}

// 旧旗标形式：ready 只多 version/browser/database/schema；即便二进制有嵌入前端也不启用；版本 API 可用。
func TestLegacyFlagsUnchanged(t *testing.T) {
	oldUI := embeddedUI
	t.Cleanup(func() { embeddedUI = oldUI })
	embedded := syntheticEmbeddedUI(t)
	embeddedUI = func() fs.FS { return embedded }
	opts, key := testOptions(t)
	hub := startRun(t, "--local-hub", "--listen", "127.0.0.1:0", "--access-key-file", opts.keyFile, "--state-dir", opts.stateDir,
		"--database-url-file", writeDatabaseURLFile(t, opts.database))
	ready := hub.next(t)
	want := []string{"address", "browser", "database", "event", "execution_node", "fixture_node_configured", "frontend", "mode", "model_config_management", "model_node_configured", "schema", "version"}
	if len(ready) != len(want) || ready["mode"] != "local-hub" || ready["frontend"] != false {
		t.Fatalf("legacy ready: %v", ready)
	}
	for _, name := range want {
		if _, ok := ready[name]; !ok {
			t.Fatalf("legacy ready lacks %s", name)
		}
	}
	base := "http://" + ready["address"].(string)
	if status, _ := httpGet(t, base+"/", ""); status != 404 {
		t.Fatal("legacy mode served embedded frontend")
	}
	if status, _ := httpGet(t, base+"/api/wscyber/version", key); status != 200 {
		t.Fatal("legacy version API")
	}
	hub.stop(t)
}

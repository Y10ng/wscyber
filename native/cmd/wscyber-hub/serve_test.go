package main

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"testing/fstest"

	"wscyber/native/internal/buildinfo"
	"wscyber/native/internal/uiembed"
)

// 合成嵌入前端：与 syntheticUI 同内容，外加占位文件；不是原生 UI 构建产物。
func syntheticEmbeddedUI(t *testing.T) fstest.MapFS {
	t.Helper()
	dir := syntheticUI(t)
	files := fstest.MapFS{uiembed.Placeholder: {Data: []byte("placeholder\n"), Mode: 0444}}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		files[filepath.ToSlash(rel)] = &fstest.MapFile{Data: data, Mode: 0444}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestSubcommandDispatch(t *testing.T) {
	var out bytes.Buffer
	if err := run(t.Context(), []string{"bogus"}, &out); err == nil || !strings.Contains(err.Error(), "未知子命令") {
		t.Fatalf("unknown subcommand accepted: %v", err)
	}
	if err := run(t.Context(), []string{"version", "extra"}, &out); err == nil {
		t.Fatal("version accepted positional argument")
	}
	if err := run(t.Context(), []string{"serve", "--no-such-flag"}, &out); err == nil {
		t.Fatal("serve accepted unknown flag")
	}
	out.Reset()
	if err := run(t.Context(), []string{"serve", "--help"}, &out); err != nil || !strings.Contains(out.String(), "database-url-file") || !strings.Contains(out.String(), "access-key-file") {
		t.Fatalf("serve help: %v %q", err, out.String())
	}
	// 旧旗标形式：位置参数仍被拒绝，--help 仍列旧旗标。
	if err := run(t.Context(), []string{"--local-hub", "extra"}, &out); err == nil || !strings.Contains(err.Error(), "不接受位置参数") {
		t.Fatalf("legacy positional: %v", err)
	}
	out.Reset()
	if err := run(t.Context(), []string{"--help"}, &out); err != nil || !strings.Contains(out.String(), "model-endpoint") || strings.Contains(out.String(), "database-url-file") {
		t.Fatal("legacy help changed")
	}
	if err := run(t.Context(), nil, &out); err == nil || !strings.Contains(err.Error(), "--local-hub") {
		t.Fatalf("no-args legacy behaviour changed: %v", err)
	}
}

func TestVersionCommand(t *testing.T) {
	old := embeddedUI
	t.Cleanup(func() { embeddedUI = old })
	embeddedUI = func() fs.FS { return fstest.MapFS{uiembed.Placeholder: {Data: []byte("x")}} }
	var out bytes.Buffer
	if err := run(t.Context(), []string{"version"}, &out); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.HasPrefix(text, "wscyber-hub dev (") || !strings.Contains(text, "cyber f4b1fb2f") || !strings.Contains(text, "ui 147423bd (未嵌入)") || !strings.Contains(text, "schema 0") {
		t.Fatalf("version text: %q", text)
	}
	embedded := syntheticEmbeddedUI(t)
	embeddedUI = func() fs.FS { return embedded }
	out.Reset()
	if err := run(t.Context(), []string{"version", "--json"}, &out); err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(out.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"version", "commit", "build_time", "cyber_commit", "ui_commit", "ui_manifest_sha256", "schema", "go"} {
		if _, ok := fields[name]; !ok {
			t.Fatalf("version JSON missing %s: %s", name, out.String())
		}
	}
	if fields["version"] != "dev" || fields["cyber_commit"] != buildinfo.CyberCommit || fields["ui_commit"] != uiCommit || len(fields["ui_manifest_sha256"].(string)) != 64 {
		t.Fatalf("version JSON values: %s", out.String())
	}
}

func TestResolveServeConfig(t *testing.T) {
	env := func(values map[string]string) func(string) (string, bool) {
		return func(name string) (string, bool) { v, ok := values[name]; return v, ok }
	}
	got, err := resolveServeConfig(serveConfig{}, env(map[string]string{"HOME": "/home/tester"}))
	if err != nil || got.listen != "127.0.0.1:8080" || got.stateDir != "/home/tester/.local/share/wscyber" || got.accessKeyFile != "" || got.databaseURLFile != "" {
		t.Fatalf("defaults: %+v %v", got, err)
	}
	got, _ = resolveServeConfig(serveConfig{}, env(map[string]string{"HOME": "/home/tester", "XDG_DATA_HOME": "/data"}))
	if got.stateDir != "/data/wscyber" {
		t.Fatalf("XDG_DATA_HOME ignored: %s", got.stateDir)
	}
	got, _ = resolveServeConfig(serveConfig{}, env(map[string]string{"HOME": "/home/tester", "XDG_DATA_HOME": "relative"}))
	if got.stateDir != "/home/tester/.local/share/wscyber" {
		t.Fatalf("relative XDG_DATA_HOME used: %s", got.stateDir)
	}
	all := env(map[string]string{envListen: "0.0.0.0:9000", envStateDir: "/srv/state", envAccessKeyFile: "/run/key", envDatabaseURLFile: "/run/db.url"})
	got, err = resolveServeConfig(serveConfig{}, all)
	if err != nil || got.listen != "0.0.0.0:9000" || got.stateDir != "/srv/state" || got.accessKeyFile != "/run/key" || got.databaseURLFile != "/run/db.url" {
		t.Fatalf("env: %+v %v", got, err)
	}
	got, _ = resolveServeConfig(serveConfig{listen: "127.0.0.1:1", stateDir: "/flag", accessKeyFile: "/fk", databaseURLFile: "/fd", uiDir: "/ui"}, all)
	if got.listen != "127.0.0.1:1" || got.stateDir != "/flag" || got.accessKeyFile != "/fk" || got.databaseURLFile != "/fd" || got.uiDir != "/ui" {
		t.Fatalf("flags must win over env: %+v", got)
	}
	if _, err := resolveServeConfig(serveConfig{}, env(nil)); err == nil {
		t.Fatal("missing HOME without state dir accepted")
	}
}

func TestPrivateFilePermissions(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, mode os.FileMode, data string) string {
		path := filepath.Join(dir, name)
		mustWrite(t, path, []byte(data))
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		return path
	}
	cases := []struct {
		path            string
		strict, relaxed bool
	}{
		{write("k600", 0600, "synthetic-key"), true, true},
		{write("k400", 0400, "synthetic-key"), false, true},
		{write("k640", 0640, "synthetic-key"), false, false},
		{write("k644", 0644, "synthetic-key"), false, false},
		{write("k700", 0700, "synthetic-key"), false, false},
		{write("k200", 0200, "synthetic-key"), false, false},
		{write("big", 0600, strings.Repeat("a", 4097)), false, false},
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(cases[0].path, link); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	cases = append(cases, struct {
		path            string
		strict, relaxed bool
	}{link, false, false}, struct {
		path            string
		strict, relaxed bool
	}{fifo, false, false}, struct {
		path            string
		strict, relaxed bool
	}{dir, false, false})
	for _, c := range cases {
		_, strictErr := readPrivateKey(c.path, false)
		_, relaxedErr := readPrivateKey(c.path, true)
		if (strictErr == nil) != c.strict || (relaxedErr == nil) != c.relaxed {
			t.Fatalf("%s: strict=%v relaxed=%v", filepath.Base(c.path), strictErr, relaxedErr)
		}
		for _, err := range []error{strictErr, relaxedErr} {
			if err != nil && strings.Contains(err.Error(), "synthetic-key") {
				t.Fatal("error exposed file content")
			}
		}
	}
	// 旧旗标形式错误文本保持不变。
	if _, err := readPrivateKey(cases[2].path, false); err == nil || err.Error() != "测试 key 必须来自权限为 0600 的普通文件" {
		t.Fatalf("legacy mode message changed: %v", err)
	}
	if _, err := readPrivateKey(link, false); err == nil || err.Error() != "无法打开私有测试 key 文件" {
		t.Fatalf("legacy open message changed: %v", err)
	}
	if _, err := readPrivateKey(filepath.Join(dir, "big"), false); err == nil || err.Error() != "测试 key 文件不可读或超过 4096 字节" {
		t.Fatalf("legacy size message changed: %v", err)
	}
}

func TestLoadDatabaseURL(t *testing.T) {
	dir := t.TempDir()
	const dsn = "postgres://wscyber:synthetic-pass@db.example.test:5432/wscyber?sslmode=disable"
	file := filepath.Join(dir, "db.url")
	mustWrite(t, file, []byte(dsn+"\n"))
	t.Setenv(envDatabaseURL, "postgres://env:synthetic-env@db.example.test/other")
	got, err := loadDatabaseURL(file)
	if err != nil || got.dsn != dsn || got.source != "file" {
		t.Fatalf("file: %+v %v", got, err)
	}
	if _, set := os.LookupEnv(envDatabaseURL); set {
		t.Fatal("WSCYBER_DATABASE_URL not unset when file wins")
	}
	if err := os.Chmod(file, 0400); err != nil {
		t.Fatal(err)
	}
	if got, err := loadDatabaseURL(file); err != nil || got.dsn != dsn {
		t.Fatalf("0400 file rejected: %v", err)
	}
	if err := os.Chmod(file, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadDatabaseURL(file); err == nil || strings.Contains(err.Error(), "synthetic-pass") {
		t.Fatalf("0644 file accepted or leaked: %v", err)
	}
	t.Setenv(envDatabaseURL, "host=db.example.test user=wscyber password=synthetic-kv dbname=wscyber")
	got, err = loadDatabaseURL("")
	if err != nil || got.source != "env" || !strings.HasPrefix(got.dsn, "host=") {
		t.Fatalf("env: %+v %v", got, err)
	}
	if _, set := os.LookupEnv(envDatabaseURL); set {
		t.Fatal("WSCYBER_DATABASE_URL not unset after read")
	}
	if got, err := loadDatabaseURL(""); err != nil || got.source != "" {
		t.Fatalf("absent: %+v %v", got, err)
	}
	for _, bad := range []string{"", "  \n", "postgres://a@b/c\npostgres://d", "mysql://u:synthetic-bad@h/d"} {
		mustWrite(t, file, []byte(bad))
		if err := os.Chmod(file, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadDatabaseURL(file); err == nil || strings.Contains(err.Error(), "synthetic-bad") {
			t.Fatalf("invalid DSN %q accepted or leaked: %v", bad, err)
		}
	}
	if _, err := loadDatabaseURL(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing file accepted")
	}
}

func TestEnsureAccessKey(t *testing.T) {
	state := t.TempDir()
	path, created, err := ensureAccessKey(state)
	if err != nil || !created || path != filepath.Join(state, "access.key") {
		t.Fatalf("generate: %s %v %v", path, created, err)
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode().Perm() != 0600 || !info.Mode().IsRegular() {
		t.Fatalf("generated key mode: %v %v", info, err)
	}
	first, err := readPrivateKey(path, true)
	if err != nil || len(first) != 64 {
		t.Fatalf("generated key unreadable: %v len=%d", err, len(first))
	}
	again, created, err := ensureAccessKey(state)
	if err != nil || created || again != path {
		t.Fatalf("reuse: %v %v", created, err)
	}
	second, _ := readPrivateKey(path, true)
	if second != first {
		t.Fatal("existing key was replaced")
	}
	entries, _ := os.ReadDir(state)
	if len(entries) != 1 {
		t.Fatalf("temporary key file left behind: %v", entries)
	}
	// 已存在但权限不对的文件：不覆盖，由读取校验拒绝。
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, created, err := ensureAccessKey(state); err != nil || created {
		t.Fatal("existing key overwritten")
	}
	if _, err := readPrivateKey(path, true); err == nil {
		t.Fatal("0644 key accepted")
	}
}

func TestNodeDialAddress(t *testing.T) {
	for in, want := range map[string]string{
		"0.0.0.0:8080":   "127.0.0.1:8080",
		"[::]:9":         "127.0.0.1:9",
		"127.0.0.1:1234": "127.0.0.1:1234",
		"[::1]:5":        "[::1]:5",
		"192.0.2.1:80":   "192.0.2.1:80",
		"not-an-address": "not-an-address",
	} {
		if got := nodeDialAddress(in); got != want {
			t.Fatalf("%s -> %s, want %s", in, got, want)
		}
	}
}

func TestEmbeddedUI(t *testing.T) {
	for _, empty := range []fstest.MapFS{{}, {uiembed.Placeholder: {Data: []byte("x")}}} {
		assets, err := openEmbeddedUI(empty)
		if err != nil || assets != nil || embeddedUIPresent(empty) {
			t.Fatal("placeholder-only embed must mean no frontend")
		}
	}
	valid := syntheticEmbeddedUI(t)
	assets, err := openEmbeddedUI(valid)
	if err != nil || assets == nil || !embeddedUIPresent(valid) {
		t.Fatalf("valid embed rejected: %v", err)
	}
	if len(assets.manifestSHA256) != 64 || assets.manifestSHA256 != embeddedManifestSHA(valid) {
		t.Fatal("manifest hash mismatch")
	}
	for _, name := range []string{uiManifest, uiembed.Placeholder, "late.key"} {
		if f, err := assets.Open(name); err == nil {
			f.Close()
			t.Fatalf("%s exposed", name)
		}
	}
	if err := assets.Close(); err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(fstest.MapFS){
		"changed-asset":    func(m fstest.MapFS) { m["assets/fixture.js"] = &fstest.MapFile{Data: []byte("changed")} },
		"missing-asset":    func(m fstest.MapFS) { delete(m, "assets/fixture.js") },
		"unlisted-file":    func(m fstest.MapFS) { m["private.sqlite"] = &fstest.MapFile{Data: []byte("x")} },
		"missing-manifest": func(m fstest.MapFS) { delete(m, uiManifest) },
		"extra-dir":        func(m fstest.MapFS) { m["state/x.js"] = &fstest.MapFile{Data: []byte("x")} },
		"wrong-commit": func(m fstest.MapFS) {
			var manifest uiBuildManifest
			if err := json.Unmarshal(m[uiManifest].Data, &manifest); err != nil {
				t.Fatal(err)
			}
			manifest.UICommit = "main"
			m[uiManifest].Data, _ = json.Marshal(manifest)
		},
		"symlink": func(m fstest.MapFS) {
			m["assets/link.js"] = &fstest.MapFile{Data: []byte("assets/fixture.js"), Mode: fs.ModeSymlink}
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			m := syntheticEmbeddedUI(t)
			mutate(m)
			if assets, err := openEmbeddedUI(m); err == nil || assets != nil {
				t.Fatal("corrupt embed accepted")
			}
		})
	}
}

func TestHealthcheckCommand(t *testing.T) {
	var readyz, health int
	readyzBody := `{"status":"ready"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/readyz":
			w.WriteHeader(readyz)
			_, _ = w.Write([]byte(readyzBody))
		case "/health":
			w.WriteHeader(health)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	check := func(args ...string) (string, error) {
		var out bytes.Buffer
		err := run(t.Context(), append([]string{"healthcheck"}, args...), &out)
		return out.String(), err
	}
	readyz, health = 200, 200
	if out, err := check("--url", server.URL); err != nil || out != "ok /readyz\n" {
		t.Fatalf("readyz: %q %v", out, err)
	}
	readyz = 404
	if out, err := check("--url", server.URL+"/"); err != nil || out != "ok /health\n" {
		t.Fatalf("health fallback: %q %v", out, err)
	}
	health = 503
	if _, err := check("--url", server.URL); err == nil {
		t.Fatal("503 reported healthy")
	}
	readyz = 503
	if _, err := check("--url", server.URL); err == nil {
		t.Fatal("readyz 503 reported healthy")
	}
	// 200 但不是就绪 JSON（如前端 index）不算健康。
	readyz, readyzBody = 200, "<!doctype html>"
	if _, err := check("--url", server.URL); err == nil {
		t.Fatal("non-JSON 200 reported healthy")
	}
	readyzBody = `{"status":"ready"}`
	// 默认地址取 WSCYBER_LISTEN，通配地址改连 127.0.0.1。
	readyz, health = 404, 200
	_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	t.Setenv(envListen, "0.0.0.0:"+port)
	if out, err := check(); err != nil || out != "ok /health\n" {
		t.Fatalf("default via WSCYBER_LISTEN: %q %v", out, err)
	}
	for _, bad := range []string{"ftp://127.0.0.1:1", "http://u:p@127.0.0.1:1", "http://127.0.0.1:1/health", "127.0.0.1:1"} {
		if _, err := check("--url", bad); err == nil {
			t.Fatalf("bad url %s accepted", bad)
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := listener.Addr().String()
	listener.Close()
	if _, err := check("--url", "http://"+closed); err == nil {
		t.Fatal("closed port reported healthy")
	}
}

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 仅用于参数/原生HTTP边界的合成资产，不是Cyber构建产物或UI验收。
func syntheticUI(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "public")
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	content := map[string]string{
		"index.html":        `<!doctype html><html><script type="module" src="/assets/fixture.js"></script>synthetic-ui-boundary-only</html>`,
		"assets/fixture.js": "/* synthetic asset, NOT native UI */",
	}
	manifest := uiBuildManifest{Schema: 2, RootCommit: uiRootCommit, UICommit: uiCommit,
		SourcePackageLockHash: uiSourceLockHash, BuildPackageLockHash: uiBuildLockHash,
		DependencyOverlayHash: uiOverlayHash, Files: map[string]string{}}
	for name, text := range content {
		mustWrite(t, filepath.Join(dir, name), []byte(text))
		hash := sha256.Sum256([]byte(text))
		manifest.Files[name] = hex.EncodeToString(hash[:])
	}
	writeSyntheticManifest(t, dir, manifest)
	return dir
}

func writeSyntheticManifest(t *testing.T, dir string, m uiBuildManifest) {
	t.Helper()
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, uiManifest), data)
}

func TestUIAssetsBoundary(t *testing.T) {
	cases := []string{"valid", "missing-manifest", "wrong-commit", "legacy-manifest", "wrong-source-lock", "source-as-build-lock", "wrong-build-lock", "missing-build-lock", "wrong-overlay", "missing-overlay", "changed-asset", "missing-asset", "no-real-js", "key-in-public", "state-in-public", "state-parent-alias", "symlink", "hardlink", "unlisted-file", "project-root"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			opts, _ := testOptions(t)
			dir := syntheticUI(t)
			switch name {
			case "missing-manifest":
				if err := os.Remove(filepath.Join(dir, uiManifest)); err != nil {
					t.Fatal(err)
				}
			case "wrong-commit", "no-real-js", "legacy-manifest", "wrong-source-lock", "source-as-build-lock", "wrong-build-lock", "missing-build-lock", "wrong-overlay", "missing-overlay":
				raw, err := os.ReadFile(filepath.Join(dir, uiManifest))
				if err != nil {
					t.Fatal(err)
				}
				var m uiBuildManifest
				if err := json.Unmarshal(raw, &m); err != nil {
					t.Fatal(err)
				}
				switch name {
				case "wrong-commit":
					m.UICommit = "main"
				case "legacy-manifest":
					// 重现旧schema1；原始锁字段不能冒充覆盖后的构建锁。
					legacy, err := json.Marshal(struct {
						Schema          int               `json:"schema"`
						RootCommit      string            `json:"root_commit"`
						UICommit        string            `json:"ui_commit"`
						PackageLockHash string            `json:"package_lock_sha256"`
						Files           map[string]string `json:"files"`
					}{1, uiRootCommit, uiCommit, uiSourceLockHash, m.Files})
					if err != nil {
						t.Fatal(err)
					}
					mustWrite(t, filepath.Join(dir, uiManifest), legacy)
				case "wrong-source-lock":
					m.SourcePackageLockHash = uiBuildLockHash
				case "source-as-build-lock":
					m.BuildPackageLockHash = uiSourceLockHash
				case "wrong-build-lock":
					m.BuildPackageLockHash = strings.Repeat("0", 64)
				case "missing-build-lock":
					m.BuildPackageLockHash = ""
				case "wrong-overlay":
					m.DependencyOverlayHash = strings.Repeat("0", 64)
				case "missing-overlay":
					m.DependencyOverlayHash = ""
				case "no-real-js":
					mustWrite(t, filepath.Join(dir, "index.html"), []byte("placeholder"))
					h := sha256.Sum256([]byte("placeholder"))
					m.Files["index.html"] = hex.EncodeToString(h[:])
				}
				if name != "legacy-manifest" {
					writeSyntheticManifest(t, dir, m)
				}
			case "changed-asset":
				mustWrite(t, filepath.Join(dir, "assets/fixture.js"), []byte("changed"))
			case "missing-asset":
				if err := os.Remove(filepath.Join(dir, "assets/fixture.js")); err != nil {
					t.Fatal(err)
				}
			case "key-in-public":
				opts.keyFile = filepath.Join(dir, "access.key")
				mustWrite(t, opts.keyFile, []byte("synthetic-key"))
			case "state-in-public":
				opts.stateDir = filepath.Join(dir, "state")
			case "state-parent-alias":
				link := filepath.Join(t.TempDir(), "alias")
				if err := os.Symlink(dir, link); err != nil {
					t.Fatal(err)
				}
				opts.stateDir = filepath.Join(link, "state")
			case "symlink":
				if err := os.Symlink(opts.keyFile, filepath.Join(dir, "assets/key.js")); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(opts.keyFile, filepath.Join(dir, "assets/key.js")); err != nil {
					t.Fatal(err)
				}
			case "unlisted-file":
				mustWrite(t, filepath.Join(dir, "private.sqlite"), []byte("not public"))
			case "project-root":
				dir = filepath.Dir(dir)
			}
			assets, err := openUIAssets(dir, opts.keyFile, opts.stateDir)
			if name != "valid" {
				if assets != nil {
					assets.Close()
				}
				if err == nil {
					t.Fatal("unsafe UI directory accepted")
				}
				if _, err := os.Stat(opts.stateDir); !os.IsNotExist(err) {
					t.Fatal("asset validation touched state")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer assets.Close()
			// 启动后增加的文件也不成为公开资产；清单本身不可读。
			mustWrite(t, filepath.Join(dir, "late.key"), []byte("not public"))
			for _, path := range []string{uiManifest, "late.key", "../access.key", "assets"} {
				if f, err := assets.Open(path); err == nil {
					f.Close()
					t.Fatal("private/unlisted path exposed")
				}
			}
		})
	}
}

func TestUIStaticNativeRoutes(t *testing.T) {
	opts, key := testOptions(t)
	opts.uiDir = syntheticUI(t)
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
			case <-time.After(10 * time.Second):
				t.Error("test Hub did not close")
			}
		}
	})
	var addr string
	select {
	case addr = <-ready:
	case err := <-done:
		joined = true
		t.Fatalf("native UI route registration failed: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("test Hub not ready")
	}
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	type requestCase struct {
		method, path, token, contentType string
		status                           int
	}
	requests := []requestCase{
		{"GET", "/", "", "text/html", 200},
		{"GET", "/sessions/test", "", "text/html", 200},
		{"GET", "/assets/fixture.js", "", "javascript", 200},
		{"GET", "/api/manifest", "", "application/json", 401},
		{"GET", "/api/manifest", "wrong", "application/json", 401},
		{"GET", "/api/manifest", key, "application/json", 200},
		{"POST", "/cyber.rpc.chat.SessionService/ListSessions", "", "application/json", 401},
		{"POST", "/cyber.rpc.chat.SessionService/ListSessions", "wrong", "application/json", 401},
		{"POST", "/cyber.rpc.chat.SessionService/ListSessions", key, "application/json", 200},
		{"POST", "/cyber.rpc.scan.ScanService/ListScans", key, "text/plain", 404},
		{"POST", "/cyber.rpc.unknown.Service/Unknown", key, "text/plain", 404},
		{"GET", "/cyber.rpc.unknown.Service", "", "text/plain", 404},
		{"GET", "/ioa/data", key, "text/plain", 404},
		{"GET", "/ioa", "", "text/plain", 404},
		{"GET", "/readyz", "", "text/plain", 404},
		{"GET", "/assets/missing.js", "", "text/plain", 404},
		{"GET", "/assets/missing.css", "", "text/plain", 404},
		{"GET", "/" + uiManifest, "", "text/plain", 404},
		{"GET", "/access.key", "", "text/plain", 404},
		{"GET", "/state/hub.sqlite", "", "text/plain", 404},
		{"GET", "/api/missing", key, "text/plain", 404},
		{"GET", "/api/auth/session", "", "application/json", 200},
		{"GET", "/health", "", "application/json", 200},
	}
	for _, test := range requests {
		req, err := http.NewRequestWithContext(t.Context(), test.method, "http://"+addr+test.path, strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Connect-Protocol-Version", "1")
		if test.token != "" {
			req.Header.Set("Authorization", "Bearer "+test.token)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(res.Body)
		res.Body.Close()
		if readErr != nil || res.StatusCode != test.status || !strings.Contains(res.Header.Get("Content-Type"), test.contentType) {
			t.Fatalf("%s %s got status=%d type=%s want %d/%s", test.method, test.path, res.StatusCode, res.Header.Get("Content-Type"), test.status, test.contentType)
		}
		if test.status != 200 && strings.Contains(string(body), "synthetic-ui-boundary-only") {
			t.Fatal("unimplemented API/asset returned index")
		}
		if strings.Contains(string(body), key) {
			t.Fatal("private key exposed")
		}
		if test.path == "/assets/fixture.js" && (string(body) != "/* synthetic asset, NOT native UI */" || !strings.Contains(res.Header.Get("Cache-Control"), "immutable")) {
			t.Fatal("native asset serving/cache changed")
		}
		if test.path == "/" && res.Header.Get("Cache-Control") != "no-cache" {
			t.Fatal("native entry caching changed")
		}
	}
	cancel()
	select {
	case err := <-done:
		joined = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("native host did not stop")
	}
	assertNoStateFD(t, opts.stateDir)
}

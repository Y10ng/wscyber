package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/chainreactors/cyber/core/extension"
	webpkg "github.com/chainreactors/cyber/pkg/web"
	"github.com/chainreactors/cyber/pkg/web/host"
)

const (
	uiRootCommit = "f4b1fb2f33158a1a3c382de9c1cb63807a6b55cf"
	uiCommit     = "147423bd42b665aab361409c57258d9370f75bb0"
	// 原始来源锁不等于实际构建闭包；H3-A2显式覆盖仅补固定子包缺项。
	uiSourceLockHash = "23da6a44f3f816986cf2768c454ea9071b0078654a688274797825c10bf48ea0"
	uiBuildLockHash  = "996a5ab5c64b6ac622f2d7eb170d7422646d25f7bbbdd5bf9b48535214890416"
	uiOverlayHash    = "b4f3a409e4b98d81b6cfff7a63414080f4e9b8eb09352c07b4f6be2c592c2528"
	uiManifest       = ".wscyber-ui.json"
	uiMaxBytes       = 128 << 20
)

type uiBuildManifest struct {
	Schema                int               `json:"schema"`
	RootCommit            string            `json:"root_commit"`
	UICommit              string            `json:"ui_commit"`
	SourcePackageLockHash string            `json:"source_package_lock_sha256"`
	BuildPackageLockHash  string            `json:"build_package_lock_sha256"`
	DependencyOverlayHash string            `json:"dependency_overlay_sha256"`
	Files                 map[string]string `json:"files"`
}

// 只公开构建清单中的普通资源；不公开清单、目录、临时文件或后来添加的key。
// os.Root 同时限制后续路径解析不能通过符号链接逃出静态根。
// 运行时不得原地修改构建目录；此清单是完整性校验，不是签名/恶意本地管理员隔离。
type uiAssets struct {
	root  *os.Root
	files map[string]string
}

func (u *uiAssets) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) || u.files[name] == "" {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	f, err := u.root.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
	}
	return f, nil
}

func pathWithin(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func openUIAssets(dir, keyFile, stateDir string) (_ *uiAssets, resultErr error) {
	uiDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, fmt.Errorf("--ui-dir 必须为已构建资源目录: %w", err)
	}
	uiDir, err = filepath.Abs(uiDir)
	if err != nil {
		return nil, err
	}
	keyPath, err := filepath.EvalSymlinks(keyFile)
	if err != nil {
		return nil, err
	}
	keyPath, err = filepath.Abs(keyPath)
	if err != nil {
		return nil, err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(stateDir))
	if err != nil {
		return nil, err
	}
	parent, err = filepath.Abs(parent)
	if err != nil {
		return nil, err
	}
	statePath := filepath.Join(parent, filepath.Base(stateDir))
	if pathWithin(uiDir, keyPath) || pathWithin(uiDir, statePath) || pathWithin(statePath, uiDir) {
		return nil, fmt.Errorf("key/state 必须在公开静态根之外，且state不可包含UI")
	}
	root, err := os.OpenRoot(uiDir)
	if err != nil {
		return nil, err
	}
	defer func() {
		if resultErr != nil {
			root.Close()
		}
	}()
	manifestInfo, err := root.Lstat(uiManifest)
	if err != nil || !manifestInfo.Mode().IsRegular() || manifestInfo.Size() > 1<<20 {
		return nil, fmt.Errorf("缺少普通构建清单或清单过大")
	}
	manifestFile, err := root.Open(uiManifest)
	if err != nil {
		return nil, fmt.Errorf("缺少原生UI构建清单；须先运行固定来源构建脚本")
	}
	raw, readErr := io.ReadAll(io.LimitReader(manifestFile, (1<<20)+1))
	manifestFile.Close()
	if readErr != nil || len(raw) > 1<<20 {
		return nil, fmt.Errorf("UI清单不可读或过大")
	}
	var manifest uiBuildManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, fmt.Errorf("UI清单JSON无效")
	}
	if manifest.Schema != 2 || manifest.RootCommit != uiRootCommit || manifest.UICommit != uiCommit ||
		manifest.SourcePackageLockHash != uiSourceLockHash || manifest.BuildPackageLockHash != uiBuildLockHash ||
		manifest.DependencyOverlayHash != uiOverlayHash {
		return nil, fmt.Errorf("UI清单不是本入口固定来源/构建锁/依赖覆盖版本")
	}
	if len(manifest.Files) < 2 || len(manifest.Files) > 4096 || manifest.Files["index.html"] == "" {
		return nil, fmt.Errorf("UI清单缺少index/资源或条目过多")
	}
	keyInfo, err := os.Stat(keyPath)
	if err != nil {
		return nil, err
	}
	// 整棵目录拒绝符号链接、硬链接和未列出的文件，避免把私有工作区误当public。
	var total int64
	seen := 0
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("UI目录不接受符号链接")
		}
		if entry.IsDir() {
			if name != "." && name != "assets" && !strings.HasPrefix(name, "assets/") {
				return fmt.Errorf("UI目录含非资源目录")
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || os.SameFile(keyInfo, info) {
			return fmt.Errorf("UI目录含特殊/私有文件")
		}
		if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Nlink != 1 {
			return fmt.Errorf("UI目录不接受硬链接")
		}
		if name == uiManifest {
			return nil
		}
		want := manifest.Files[name]
		if !validUIFile(name) || len(want) != 64 {
			return fmt.Errorf("UI目录含未登记/非资源文件")
		}
		total += info.Size()
		if info.Size() <= 0 || total > uiMaxBytes {
			return fmt.Errorf("UI资源为空或超过128MiB边界")
		}
		f, err := root.Open(name)
		if err != nil {
			return err
		}
		h := sha256.New()
		_, err = io.Copy(h, io.LimitReader(f, uiMaxBytes+1))
		f.Close()
		if err != nil || hex.EncodeToString(h.Sum(nil)) != want {
			return fmt.Errorf("UI资源哈希不符: %s", name)
		}
		seen++
		return nil
	})
	if err != nil {
		return nil, err
	}
	if seen != len(manifest.Files) {
		return nil, fmt.Errorf("UI清单引用了缺失资源")
	}
	assets := &uiAssets{root: root, files: manifest.Files}
	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		return nil, err
	}
	refs := regexp.MustCompile(`(?:src|href)=["'](/assets/[^"']+)["']`).FindAllSubmatch(index, -1)
	hasJS := false
	for _, ref := range refs {
		name := strings.TrimPrefix(string(ref[1]), "/")
		if manifest.Files[name] == "" {
			return nil, fmt.Errorf("index引用未登记资源")
		}
		hasJS = hasJS || strings.HasSuffix(name, ".js")
	}
	if !hasJS || !strings.Contains(string(index), "<script") {
		return nil, fmt.Errorf("index缺少实际构建JS入口")
	}
	return assets, nil
}

func validUIFile(name string) bool {
	if !fs.ValidPath(name) || strings.ContainsAny(name, "\\?#") {
		return false
	}
	if name == "index.html" {
		return true
	}
	if !strings.HasPrefix(name, "assets/") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") {
			return false
		}
	}
	switch path.Ext(name) {
	case ".js", ".css", ".wasm", ".svg", ".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico", ".woff", ".woff2", ".ttf":
		return true
	}
	return false
}

// 上游SPA遇到缺失文件会回index。通过原生Route扩展最小限制其fallback；
// 更具体的原生API/Connect注册仍优先，无新mux、无认证/业务协议副本。
func uiFallbackExtension(assets *uiAssets) extension.Extension {
	spa := host.SPAFileServer(assets)
	fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if strings.HasPrefix(name, "cyber.rpc.") || name == "ioa" || strings.HasPrefix(name, "ioa/") || name == "api" ||
			strings.HasPrefix(name, ".") || strings.Contains(name, "/.") ||
			((strings.HasPrefix(name, "assets/") || path.Ext(name) != "") && assets.files[name] == "") ||
			(r.Method != http.MethodGet && r.Method != http.MethodHead) {
			http.NotFound(w, r)
			return
		}
		spa.ServeHTTP(w, r)
	})
	return extension.Func{LoadFunc: func(scope *extension.Scope) error {
		return extension.Add(scope,
			webpkg.Route{Pattern: "/{service}", Handler: fallback},
			webpkg.Route{Pattern: "/{service}/{rest...}", Handler: fallback},
		)
	}}
}

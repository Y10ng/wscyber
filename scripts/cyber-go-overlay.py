#!/usr/bin/env python3
"""按 native/patches/cyber/manifest.json 对固定 Cyber 模块做整文件 Go overlay，并透传 go 子命令参数。

用法: cyber-go-overlay.py [--manifest PATH] -- <build|test|vet|list> [go 参数...]
"""

import fcntl
import hashlib
import json
from pathlib import Path, PurePosixPath
import shutil
import subprocess
import sys

ROOT = Path(__file__).resolve().parent.parent
DEFAULT_MANIFEST = ROOT / "native/patches/cyber/manifest.json"
OUTPUT = ROOT / "var/cyber-overlay"
COMMANDS = {"build", "test", "vet", "list"}


class OverlayError(RuntimeError):
    pass


def sha256_file(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def safe_relative(value: object, what: str) -> PurePosixPath:
    if not isinstance(value, str) or not value:
        raise OverlayError(f"清单 {what} 必须是非空字符串")
    rel = PurePosixPath(value)
    if rel.is_absolute() or ".." in rel.parts or rel.as_posix() != value:
        raise OverlayError(f"清单 {what} 不是规范的相对路径: {value!r}")
    if rel.suffix != ".go":
        raise OverlayError(f"清单 {what} 只允许 .go 文件: {value!r}")
    return rel


def load_manifest(path: Path) -> dict:
    """读取并校验清单结构；返回 {version, source, entries:[(upstream, sha256|None, replacement_path)]}。"""
    try:
        doc = json.loads(path.read_text())
    except (OSError, ValueError) as exc:
        raise OverlayError(f"无法读取 overlay 清单 {path}: {exc}") from exc
    for key in ("module", "source", "version", "files"):
        if key not in doc:
            raise OverlayError(f"清单缺少字段 {key}")
    if not isinstance(doc["files"], list) or not doc["files"]:
        raise OverlayError("清单 files 为空")
    base = path.resolve().parent
    entries, seen = [], set()
    for index, item in enumerate(doc["files"]):
        if not isinstance(item, dict) or set(item) != {"upstream", "sha256", "replacement"}:
            raise OverlayError(f"清单 files[{index}] 必须恰有 upstream/sha256/replacement 三个字段")
        upstream = safe_relative(item["upstream"], f"files[{index}].upstream")
        if upstream in seen:
            raise OverlayError(f"清单重复路径 {upstream}")
        seen.add(upstream)
        digest = item["sha256"]
        if digest is not None and (not isinstance(digest, str) or len(digest) != 64 or digest.strip("0123456789abcdef")):
            raise OverlayError(f"清单 files[{index}].sha256 必须是 64 位小写十六进制或 null")
        replacement = base / safe_relative(item["replacement"], f"files[{index}].replacement")
        if not replacement.is_file() or replacement.is_symlink():
            raise OverlayError(f"替换文件不存在或不是普通文件: {replacement}")
        entries.append((upstream, digest, replacement))
    return {"module": doc["module"], "source": doc["source"], "version": doc["version"], "entries": entries}


def verify_sources(module_root: Path, manifest: dict, label: str) -> None:
    """上游源与清单哈希逐个比对；新增文件（sha256=null）要求上游不存在。"""
    if not (module_root / "go.mod").is_file():
        raise OverlayError(f"{label} 缺少 go.mod: {module_root}")
    for upstream, digest, _ in manifest["entries"]:
        source = module_root / upstream
        if digest is None:
            if source.exists():
                raise OverlayError(f"{label} 已存在清单标为新增的文件 {upstream}；上游版本变化，需重新移植并更新清单")
            continue
        if not source.is_file():
            raise OverlayError(f"{label} 缺少 {upstream}；上游版本变化，未应用 overlay")
        actual = sha256_file(source)
        if actual != digest:
            raise OverlayError(f"{label} 的 {upstream} sha256 {actual} 与清单 {digest} 不符；上游源已变化，需三方 diff 重新移植后更新清单，未应用 overlay")


def module_cache_root(manifest: dict) -> Path:
    gomodcache = subprocess.run(["go", "env", "GOMODCACHE"], check=True, capture_output=True, text=True).stdout.strip()
    if not gomodcache:
        raise OverlayError("go env GOMODCACHE 为空")
    return Path(gomodcache) / (manifest["source"] + "@" + manifest["version"])


def prepare(manifest_path: Path) -> tuple[Path, Path]:
    """在 heavy.lock 内调用：核对上游源、维护只读副本、写 overlay.json 与派生 modfile。"""
    manifest = load_manifest(manifest_path)
    module_root = module_cache_root(manifest)
    verify_sources(module_root, manifest, "固定 Cyber 模块缓存")
    OUTPUT.mkdir(parents=True, exist_ok=True)
    # Go 禁止 overlay 替换 GOMODCACHE 下的文件：复用一份核对过的根模块副本，
    # 派生 modfile 的 replace 指向它。stamp 记录副本来源版本。
    module_copy = OUTPUT / "module-current"
    stamp = OUTPUT / "module-current.source"
    expected_stamp = manifest["source"] + "@" + manifest["version"] + "\n"
    if module_copy.exists() and (not stamp.is_file() or stamp.read_text() != expected_stamp):
        raise OverlayError(f"派生副本 {module_copy} 来源与清单版本不符；请人工删除该目录后重试")
    if not module_copy.exists():
        shutil.copytree(module_root, module_copy, copy_function=shutil.copy2)
        stamp.write_text(expected_stamp)
    if (module_copy / "go.mod").read_bytes() != (module_root / "go.mod").read_bytes():
        raise OverlayError("派生副本 go.mod 与固定版本不一致；未应用 overlay")
    verify_sources(module_copy, manifest, "派生副本")
    overlay = OUTPUT / "overlay.json"
    replace = {str(module_copy / upstream): str(replacement) for upstream, _, replacement in manifest["entries"]}
    overlay.write_text(json.dumps({"Replace": replace}, indent=2) + "\n")
    main_mod = OUTPUT / "native.mod"
    shutil.copyfile(ROOT / "native/go.mod", main_mod)
    shutil.copyfile(ROOT / "native/go.sum", OUTPUT / "native.sum")
    subprocess.run(["go", "mod", "edit", "-modfile=" + str(main_mod), "-replace=" + manifest["module"] + "=" + str(module_copy)],
                   check=True, cwd=ROOT / "native")
    return overlay, main_mod


def parse_args(argv: list[str]) -> tuple[Path, list[str]]:
    manifest = DEFAULT_MANIFEST
    args = list(argv)
    if len(args) >= 2 and args[0] == "--manifest":
        manifest = Path(args[1]).resolve()
        args = args[2:]
    if args and args[0] == "--":
        args = args[1:]
    return manifest, args


def main() -> int:
    manifest_path, args = parse_args(sys.argv[1:])
    if not args or args[0] not in COMMANDS:
        print("用法: cyber-go-overlay.py [--manifest PATH] -- <build|test|vet|list> [go 参数...]", file=sys.stderr)
        return 2
    lock = ROOT / "var/heavy.lock"
    lock.parent.mkdir(parents=True, exist_ok=True)
    with lock.open("a") as lock_file:
        # 锁覆盖副本、overlay、派生 modfile 的读写与 go 执行。
        fcntl.flock(lock_file, fcntl.LOCK_EX)
        try:
            overlay, main_mod = prepare(manifest_path)
        except OverlayError as exc:
            print("cyber-go-overlay: " + str(exc), file=sys.stderr)
            return 1
        return subprocess.run(["go", "-C", str(ROOT / "native"), args[0], "-mod=readonly", "-modfile=" + str(main_mod),
                               "-overlay=" + str(overlay), *args[1:]], check=False).returncode


if __name__ == "__main__":
    raise SystemExit(main())

#!/usr/bin/env python3
"""在派生的固定 Cyber 模块上应用单文件 Go overlay，并透传 build/test 参数。"""

import json
from pathlib import Path
import shutil
import subprocess
import sys

VERSION = "v0.0.0-20261109041610-f4b1fb2f3315"
RELATIVE = "pkg/web/service/store_sqlite.go"
OLD = 'db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000&_pragma=foreign_keys(1)")'
NEW = 'db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000&_txlock=immediate&_pragma=foreign_keys(1)")'


def main() -> int:
    if len(sys.argv) < 2 or sys.argv[1] == "--":
        args = sys.argv[2:]
    else:
        args = sys.argv[1:]
    if not args or args[0] not in {"build", "test"}:
        print("用法: cyber-go-overlay.py -- <build|test> [go 参数...]", file=sys.stderr)
        return 2
    root = Path(__file__).resolve().parent.parent
    source = Path.home() / ("go/pkg/mod/github.com/chainreactors/cyber-harness@" + VERSION) / RELATIVE
    text = source.read_text()
    module_root = source.parents[3]
    if not (module_root / "go.mod").is_file():
        raise RuntimeError("固定 Cyber 根模块缺少 go.mod")
    if text.count(OLD) != 1:
        raise RuntimeError("固定 Cyber SQLite 源文件与预期不匹配；未应用 overlay")
    output = root / "var/cyber-sqlite-overlay"
    output.mkdir(parents=True, exist_ok=True)
    lock = root / "var/full-system-models.Pg2PL1Id/heavy.lock"
    with lock.open("a") as lock_file:
        import fcntl
        fcntl.flock(lock_file, fcntl.LOCK_EX)
        # Go 禁止直接替换 GOMODCACHE 下的文件。复用已核对的固定根模块副本，
        # 测试和构建共用稳定路径；锁覆盖副本、overlay 与 modfile 的全部读写。
        module_copy = output / "module-current"
        if not module_copy.exists():
            shutil.copytree(module_root, module_copy, copy_function=shutil.copy2)
        copied_source = module_copy / RELATIVE
        if not copied_source.is_file() or not (module_copy / "go.mod").is_file() or copied_source.read_text() != text:
            raise RuntimeError("派生 Cyber 源文件与固定版本不一致；未应用 overlay")
        replacement = output / "store_sqlite.go"
        replacement.write_text(text.replace(OLD, NEW))
        overlay = output / "overlay.json"
        overlay.write_text(json.dumps({"Replace": {str(copied_source): str(replacement)}}, indent=2) + "\n")
        main_mod = output / "native.mod"
        shutil.copyfile(root / "native/go.mod", main_mod)
        shutil.copyfile(root / "native/go.sum", output / "native.sum")
        subprocess.run(["go", "mod", "edit", "-modfile=" + str(main_mod), "-replace=github.com/chainreactors/cyber=" + str(module_copy)], check=True)
        return subprocess.run(["go", "-C", str(root / "native"), args[0], "-mod=readonly", "-modfile=" + str(main_mod), "-overlay=" + str(overlay), *args[1:]], check=False).returncode


if __name__ == "__main__":
    raise SystemExit(main())

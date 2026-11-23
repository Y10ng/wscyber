#!/usr/bin/env python3
"""核对 overlay Hub 的 Linux/amd64 可达模块与分片许可记录（只读，不作法律结论）。"""

import hashlib
import json
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parent.parent


def json_stream(text):
    decoder = json.JSONDecoder()
    while text.strip():
        text = text.lstrip()
        item, end = decoder.raw_decode(text)
        yield item
        text = text[end:]


def main():
    platform = subprocess.check_output(["go", "env", "GOOS", "GOARCH"], text=True).split()
    if platform != ["linux", "amd64"]:
        raise ValueError("许可清单核验范围仅为 Linux/amd64")
    output = subprocess.check_output(
        [sys.executable, "-I", str(ROOT / "scripts/cyber-go-overlay.py"),
         "--", "list", "-deps", "-json", "./cmd/wscyber-hub"], text=True)
    packages = list(json_stream(output))
    actual = {p["Module"]["Path"]: p["Module"]["Version"] for p in packages
              if p.get("Module") and not p["Module"].get("Main")}
    records = {}
    docs = {p.name: json.loads(p.read_text()) for p in (ROOT / "native").glob("*licenses.json")}
    errors = []
    for name, doc in docs.items():
        own = set()
        for record in doc["modules"]:
            module = record["module"]
            if module in records:
                errors.append(f"重复许可记录: {module}")
            records[module] = record
            own.add(module)
        baseline = doc.get("baseline", {})
        refs = baseline.get("files", [baseline] if "file" in baseline else [])
        available = set()
        for ref in refs:
            filename = ref["file"]
            if filename not in docs:
                errors.append(f"{name}: 未知基线 {filename}")
                continue
            digest = hashlib.sha256((ROOT / "native" / filename).read_bytes()).hexdigest()
            if digest != ref["sha256"]:
                errors.append(f"{name}: 基线哈希不符 {filename}")
            available.update(m["module"] for m in docs[filename]["modules"])
        inherited = set(baseline.get("same_version_modules", []))
        if inherited - available or inherited & own:
            errors.append(f"{name}: 基线模块缺少记录或与本片重复")
        if "new_module_count" in doc and doc["new_module_count"] != len(own):
            errors.append(f"{name}: new_module_count 不符")
        if "reachable_module_count" in doc and doc["reachable_module_count"] != len(own | inherited):
            errors.append(f"{name}: reachable_module_count 不符")
    missing = sorted(actual.keys() - records.keys())
    extra = sorted(records.keys() - actual.keys())
    if missing or extra:
        errors.append(f"缺失={missing}; 多余={extra}")
    for module in actual.keys() & records.keys():
        if actual[module] != records[module]["version"]:
            errors.append(f"版本不符: {module}")
    forbidden = ("modernc.org/sqlite", "github.com/uptrace/bun/dialect/sqlitedialect", "modernc.org/libc")
    linked = [p["ImportPath"] for p in packages
              if any(p["ImportPath"] == x or p["ImportPath"].startswith(x + "/") for x in forbidden)]
    if linked:
        errors.append(f"已移除驱动仍可达: {linked}")
    print(f"Hub packages={len(packages)}, external modules={len(actual)}, license records={len(records)}")
    print(f"missing={len(missing)}, extra={len(extra)}, forbidden packages={len(linked)}")
    for error in errors:
        print(error, file=sys.stderr)
    if errors:
        return 1
    print("PASS: 模块集合完全相等、版本一致、分片计数与基线哈希有效；非法律审查。")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, ValueError, KeyError, subprocess.CalledProcessError) as exc:
        print(f"cyber-dependency-check: {exc}", file=sys.stderr)
        raise SystemExit(1)

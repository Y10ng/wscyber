#!/usr/bin/env python3
"""清理测试崩溃残留的 PostgreSQL 库（native/internal/testdb 的 wscyber_tpl_* / wscyber_t_*）。

连接信息取自环境变量 WSCYBER_TEST_DATABASE_URL（与 go test 相同；可先
`source ~/.config/wscyber/pg-test.env`）。连接串拆成 PG* 环境变量交给 psql，
不出现在命令行参数里，也不打印。

默认只删除没有活动连接的残留库（正在跑的测试库会被跳过）；--force 连同有连接的一起删，
--dry-run 只列出。
"""

import argparse
import os
import re
import subprocess
import sys
from urllib.parse import parse_qs, unquote, urlsplit

ENV = "WSCYBER_TEST_DATABASE_URL"
NAME_RE = re.compile(r"^wscyber_(tpl_[0-9]+_|t_)[0-9a-f]{16}$")


def pg_env(dsn: str) -> dict:
    env = dict(os.environ)
    for key in ("PGHOST", "PGPORT", "PGUSER", "PGPASSWORD", "PGDATABASE", "PGSSLMODE", "PGSERVICE", "PGOPTIONS"):
        env.pop(key, None)
    if dsn.startswith(("postgres://", "postgresql://")):
        parts = urlsplit(dsn)
        if parts.hostname:
            env["PGHOST"] = parts.hostname
        if parts.port:
            env["PGPORT"] = str(parts.port)
        if parts.username:
            env["PGUSER"] = unquote(parts.username)
        if parts.password is not None:
            env["PGPASSWORD"] = unquote(parts.password)
        database = unquote(parts.path.lstrip("/"))
        if database:
            env["PGDATABASE"] = database
        query = parse_qs(parts.query)
        if "sslmode" in query:
            env["PGSSLMODE"] = query["sslmode"][-1]
        return env
    # key=value 形式：交给 libpq 的 PGSERVICE 不现实，这里只支持常见字段。
    mapping = {"host": "PGHOST", "port": "PGPORT", "user": "PGUSER", "password": "PGPASSWORD", "dbname": "PGDATABASE", "sslmode": "PGSSLMODE"}
    for key, value in re.findall(r"(\w+)\s*=\s*('(?:[^'\\]|\\.)*'|\S+)", dsn):
        if key in mapping:
            if value.startswith("'"):
                value = re.sub(r"\\(.)", r"\1", value[1:-1])
            env[mapping[key]] = value
    return env


def psql(env: dict, sql: str) -> str:
    result = subprocess.run(
        ["psql", "-X", "-q", "-A", "-t", "-v", "ON_ERROR_STOP=1", "-c", sql],
        env=env, capture_output=True, text=True, check=False,
    )
    if result.returncode != 0:
        raise RuntimeError("psql 失败: " + result.stderr.strip())
    return result.stdout


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--dry-run", action="store_true", help="只列出，不删除")
    parser.add_argument("--force", action="store_true", help="有活动连接的库也删除（会打断正在运行的测试）")
    args = parser.parse_args()

    dsn = os.environ.get(ENV, "").strip()
    if not dsn:
        print(f"未设置 {ENV}；先 source ~/.config/wscyber/pg-test.env", file=sys.stderr)
        return 2
    env = pg_env(dsn)
    rows = psql(env, """
SELECT d.datname, (SELECT count(*) FROM pg_stat_activity a WHERE a.datname = d.datname)
FROM pg_database d
WHERE d.datname LIKE 'wscyber\\_tpl\\_%' OR d.datname LIKE 'wscyber\\_t\\_%'
ORDER BY 1""")
    dropped = skipped = 0
    for line in rows.splitlines():
        if not line.strip():
            continue
        name, connections = line.rsplit("|", 1)
        if not NAME_RE.match(name):
            print(f"跳过（名称不符合 testdb 格式）: {name}")
            skipped += 1
            continue
        busy = int(connections) > 0
        if busy and not args.force:
            print(f"跳过（{connections} 个活动连接，可能正在测试）: {name}")
            skipped += 1
            continue
        if args.dry_run:
            print(f"将删除: {name}")
            continue
        psql(env, f'DROP DATABASE IF EXISTS "{name}" WITH (FORCE)')
        print(f"已删除: {name}")
        dropped += 1
    print(f"完成：删除 {dropped}，跳过 {skipped}" + ("（dry-run）" if args.dry_run else ""))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

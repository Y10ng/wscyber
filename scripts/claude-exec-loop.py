#!/usr/bin/env python3
"""执行会话自动续跑：headless 运行 claude，中断（网关错误/提前结束）后自动 resume，
直到任务目录出现 result.md 或达到轮数上限。

用法：scripts/claude-exec-loop.py <model> <短名> <任务目录> [--rounds N]
例：  scripts/claude-exec-loop.py wangsu-o-latest r3-backend var/r3-report
"""
import argparse
import json
import os
import subprocess
import sys
import time
import uuid

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

FIRST = ("按 {task} 执行本片。headless 运行，没人能实时回答：有歧义就写进 {result} 的“问题”段并停止。"
         "工作区可能已有本片之前中断留下的改动（看 git status/diff），在其基础上继续。"
         "先用3-5行复述理解，再开工；完成后写 {result} 并结束。")
AGAIN = ("上一轮被中断了。先看 git status/diff 和 {task} 确认进度，接着做，不要从头重来；"
         "全部完成后写 {result} 并结束。")


def show(line: str, log) -> None:
    log.write(line)
    log.flush()
    try:
        event = json.loads(line)
    except ValueError:
        print(line, end="", flush=True)
        return
    kind = event.get("type")
    if kind == "assistant":
        for block in event.get("message", {}).get("content", []):
            if block.get("type") == "text" and block.get("text", "").strip():
                print(block["text"].strip(), flush=True)
            elif block.get("type") == "tool_use":
                arg = json.dumps(block.get("input", {}), ensure_ascii=False)
                print(f"  → {block.get('name')} {arg[:160]}", flush=True)
    elif kind == "result":
        print(f"[轮结束] subtype={event.get('subtype')} is_error={event.get('is_error')}", flush=True)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("model")
    parser.add_argument("name")
    parser.add_argument("taskdir")
    parser.add_argument("--rounds", type=int, default=30)
    args = parser.parse_args()

    os.chdir(ROOT)
    task = os.path.join(args.taskdir, "task.md")
    result = os.path.join(args.taskdir, "result.md")
    if not os.path.isfile(task):
        print(f"缺少 {task}", file=sys.stderr)
        return 2
    session = str(uuid.uuid4())
    log = open(os.path.join(args.taskdir, "exec.log"), "a", encoding="utf-8")
    print(f"会话 wscyber-{args.name} id={session}（手动接管：claude --resume {session}）", flush=True)

    base = ["claude", "-p", "--verbose", "--output-format", "stream-json",
            "--dangerously-skip-permissions", "--model", args.model,
            "--strict-mcp-config", "--mcp-config", '{"mcpServers":{}}',
            "--disallowedTools", "Agent", "Workflow"]
    for round_no in range(1, args.rounds + 1):
        if round_no == 1:
            cmd = base + ["--session-id", session, "--name", f"wscyber-{args.name}", "--",
                          FIRST.format(task=task, result=result)]
        else:
            cmd = base + ["--resume", session, "--", AGAIN.format(task=task, result=result)]
        print(f"\n===== 第 {round_no} 轮 {time.strftime('%H:%M:%S')} =====", flush=True)
        proc = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        for line in proc.stdout:
            show(line, log)
        code = proc.wait()
        if os.path.isfile(result) and os.path.getsize(result) > 0:
            print(f"已交回 {result}，结束。", flush=True)
            return 0
        wait = min(10 * round_no, 60)
        print(f"未完成（退出码 {code}），{wait}s 后续跑。", flush=True)
        time.sleep(wait)
    print(f"达到 {args.rounds} 轮上限仍未交回，停止。", flush=True)
    return 1


if __name__ == "__main__":
    sys.exit(main())

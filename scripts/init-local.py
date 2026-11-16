#!/usr/bin/env python3
"""只初始化新项目本地配置；不覆盖既有配置、不打印凭据。"""
from pathlib import Path
import os
import secrets

root = Path(__file__).resolve().parent.parent
password = secrets.token_hex(24)
token = secrets.token_hex(32)
text = (root / ".env.example").read_text()
text = text.replace("replace_with_random_hex", password)
text = text.replace("replace_with_at_least_32_random_characters", token)
try:
    fd = os.open(root / ".env", os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
except FileExistsError:
    raise SystemExit(".env 已存在，未修改。")
with os.fdopen(fd, "w") as f:
    f.write(text)
print("已创建本项目 .env（0600），未配置模型；访问令牌仅保存在该文件。")

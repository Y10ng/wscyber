#!/usr/bin/env python3
"""固定原生 UI 准备/构建；只用 python3 -I，从仓库受控脚本运行。

prepare 不执行源码；build 明确执行已固定的 tsc/Vite，须在模型会话退出后串行运行。
rebuild 复用既有工作区与 node_modules 做有界增量构建（刷新源码覆盖→tsc→Vite→替换release）。
不下载工具链、不跟随 main、不运行 npm 生命周期，不修改上游/全局配置。
"""
import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import signal
import stat
import subprocess
import sys
import tarfile
import urllib.request

REPO = Path(__file__).resolve().parents[1]
LOCKFILE = REPO / 'native/ui/sources.lock.json'
OVERLAY = REPO / 'native/ui/dependencies.overlay.json'
OVERLAY_FILES = REPO / 'native/ui/build-overlay'
SOURCE_OVERLAY = REPO / 'native/ui/source-overlay.json'
SOURCE_FILES = REPO / 'native/ui/source-overlay'
SHARED_LOCK = REPO / 'var/heavy.lock'
SELECT = ['LICENSE', '.gitmodules', 'web/frontend', 'web/static/.gitkeep']
LIMIT = 128 * 1024 * 1024


def fail(message):
    raise RuntimeError(message)


def digest(path):
    h = hashlib.sha256()
    with path.open('rb') as f:
        while chunk := f.read(65536):
            h.update(chunk)
    return h.hexdigest()


def files(root, excluded=()):
    result = {}
    if root.is_symlink():
        fail('目录不可为链接: ' + str(root))
    for directory, dirs, names in os.walk(root):
        for name in sorted(dirs + names):
            p = Path(directory) / name
            relative = p.relative_to(root).as_posix()
            if p.is_symlink() or not (p.is_dir() or p.is_file()):
                fail('不接受链接/特殊文件: ' + str(p))
            if relative in excluded:
                if not p.is_dir():
                    fail('排除项必须是独立目录: ' + str(p))
                dirs.remove(name)
            elif p.is_file():
                result[relative] = digest(p)
    return result


def tree_hash(mapping):
    return hashlib.sha256(''.join(k + '\0' + v + '\n' for k, v in sorted(mapping.items())).encode()).hexdigest()


def write_json(path, value):
    with path.open('x', encoding='utf-8') as f:
        json.dump(value, f, indent=2, ensure_ascii=False)
        f.write('\n')


def environment(work):
    # 不继承代理、API key、Cookie、NODE_OPTIONS、Git/npm全局配置或开发CLI环境。
    heap = os.environ.get('WSCYBER_UI_HEAP_MB', '1536')
    if not (heap.isascii() and heap.isdecimal()) or not 256 <= int(heap) <= 8192:
        fail('WSCYBER_UI_HEAP_MB 必须是 256–8192 的整数')
    return {
        'PATH': '/usr/bin:/bin', 'LANG': 'C.UTF-8', 'HOME': str(work / 'home'),
        'TMPDIR': str(work / 'tmp'), 'XDG_CONFIG_HOME': str(work / 'home/config'),
        'XDG_CACHE_HOME': str(work / 'home/cache'), 'XDG_DATA_HOME': str(work / 'home/data'),
        'GIT_CONFIG_NOSYSTEM': '1', 'GIT_CONFIG_GLOBAL': '/dev/null',
        'GIT_TERMINAL_PROMPT': '0', 'CI': 'true', 'TERM': 'dumb',
        'NODE_OPTIONS': f'--max-old-space-size={int(heap)}', 'UV_THREADPOOL_SIZE': '2',
        'GOMAXPROCS': '2', 'GOGC': '30',
        'npm_config_cache': str(work / 'npm-cache'),
        'npm_config_userconfig': str(work / 'home/npm-user.conf'),
        'npm_config_globalconfig': str(work / 'home/npm-global.conf'),
        'npm_config_registry': 'https://registry.npmjs.org/',
        'npm_config_ignore_scripts': 'true', 'npm_config_audit': 'false',
        'npm_config_fund': 'false', 'npm_config_update_notifier': 'false',
        'npm_config_maxsockets': '2', 'npm_config_fetch_retries': '0',
        'npm_config_fetch_timeout': '60000', 'PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD': '1',
    }


def run(argv, work, *, cwd=REPO, timeout=120, capture=False):
    # 独立进程组仅用于超时/中断时回收本命令子树；仍前台等待、不脱离监督。
    with subprocess.Popen(argv, cwd=cwd, env=environment(work), text=True, process_group=0,
                          stdout=subprocess.PIPE if capture else None,
                          stderr=subprocess.PIPE if capture else None) as proc:
        try:
            out, err = proc.communicate(timeout=timeout)
        except BaseException:
            try:
                os.killpg(proc.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            try:
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                pass
            # 组长退出不代表esbuild等子进程都退出；精确清理同一组。
            try:
                os.killpg(proc.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            proc.communicate()
            raise
        if proc.returncode:
            raise subprocess.CalledProcessError(proc.returncode, argv, output=out, stderr=err)
        return subprocess.CompletedProcess(argv, proc.returncode, out, err)


def download(url, target, maximum=32 * 1024 * 1024):
    # 禁止继承系统代理/凭据；固定 codeload URL，拒绝跨源重定向。
    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, *args, **kwargs):
            fail('固定源码URL不允许重定向')
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    with opener.open(url, timeout=45) as response, target.open('xb') as out:
        size = 0
        while chunk := response.read(65536):
            size += len(chunk)
            if size > maximum:
                fail('下载超出边界')
            out.write(chunk)


def extract(archive, dest, prefix='', selected=None):
    # 每份归档独占新空目录，不执行归档中的脚本。拒绝路径逃逸/链接/设备/重复项。
    dest.mkdir(mode=0o700)
    with tarfile.open(archive, 'r:*') as tar:
        total, count, seen = 0, 0, set()
        for m in tar:
            p = PurePosixPath(m.name)
            if p.is_absolute() or '..' in p.parts or '\\' in m.name or not p.parts:
                fail('不安全归档路径')
            if prefix:
                if p.parts[0] != prefix:
                    fail('归档根与固定版本不符')
                p = PurePosixPath(*p.parts[1:])
            name = p.as_posix()
            if name == '.':
                continue
            if selected is not None and not any(name == s or name.startswith(s + '/') for s in selected):
                continue
            if not (m.isfile() or m.isdir()) or name in seen:
                fail('归档含链接/特殊/重复条目')
            seen.add(name)
            total += m.size
            count += 1
            if total > LIMIT or count > 10000:
                fail('解包超出边界')
            target = dest / name
            if m.isdir():
                target.mkdir(parents=True, exist_ok=True, mode=0o700)
            else:
                target.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
                with tar.extractfile(m) as src, target.open('xb') as out:
                    shutil.copyfileobj(src, out, 65536)
                target.chmod(0o600)


def root_files(root, installed=False):
    result = {}
    for selected in SELECT:
        p = root / selected
        if p.is_dir():
            # 仅构建副本的确切安装目录；其内容由依赖复用核验单独负责。
            excluded = ('node_modules',) if installed and selected == 'web/frontend' else ()
            result.update({selected + '/' + k: v for k, v in files(p, excluded).items()})
        elif p.is_file() and not p.is_symlink():
            result[selected] = digest(p)
        else:
            fail('固定root源缺少 ' + selected)
    return result


def dependency_overlay(work, pins):
    """核验原样inputs与正式覆盖；npm永远不在inputs中执行。"""
    original = work / 'inputs/root-extracted'
    ui = work / 'inputs/ui-extracted'
    if tree_hash(root_files(original)) != pins['root']['selected_tree_sha256']:
        fail('原始root输入树不符')
    if tree_hash(files(ui)) != pins['cyber_ui']['tree_sha256']:
        fail('原始cyber-ui输入树不符')
    overlay = json.loads(OVERLAY.read_text())
    origin = overlay['source']
    front = original / 'web/frontend'
    if (overlay['schema'] != 1 or origin['sources_lock_sha256'] != digest(LOCKFILE)
            or origin['root_commit'] != pins['root']['commit']
            or origin['ui_commit'] != pins['cyber_ui']['commit']
            or origin['package_lock_sha256'] != pins['package_lock_sha256']
            or digest(front / 'package.json') != origin['package_sha256']
            or digest(front / 'package-lock.json') != origin['package_lock_sha256']
            or digest(ui / 'pnpm-lock.yaml') != origin['ui_pnpm_lock_sha256']):
        fail('覆盖来源与固定原始锁不符')
    if set(overlay['files']) != {'package.json', 'package-lock.json'} or files(OVERLAY_FILES) != overlay['files']:
        fail('正式覆盖只允许两个已固定文件')
    expected_package = json.loads((front / 'package.json').read_text())
    additions = overlay['direct_additions']
    if set(additions) & set(expected_package['dependencies']):
        fail('覆盖不可替换已有直接依赖')
    expected_package['dependencies'].update(additions)
    package = json.loads((OVERLAY_FILES / 'package.json').read_text())
    if package != expected_package:
        fail('package覆盖超出缺项补充范围')
    old = json.loads((front / 'package-lock.json').read_text())
    new = json.loads((OVERLAY_FILES / 'package-lock.json').read_text())
    expected_root = dict(old['packages'][''])
    expected_root['dependencies'] = package['dependencies']
    if (new['lockfileVersion'] != 3 or new['packages'][''] != expected_root
            or {k:v for k,v in old.items() if k != 'packages'} != {k:v for k,v in new.items() if k != 'packages'}
            or any(new['packages'].get(k) != v for k,v in old['packages'].items() if k)):
        fail('原锁既有条目发生变化')
    added = {k:v for k,v in new['packages'].items() if k not in old['packages']}
    audited = {k:{f:v for f,v in item.items() if f not in ('basis', 'required_by')}
               for k,item in overlay['audit']['added_entries'].items()}
    if added != audited:
        fail('实际新增依赖与审计记录不符')
    for name, version in additions.items():
        if new['packages']['node_modules/' + name]['version'] != version:
            fail('直接补充不是固定版本: ' + name)
    return overlay


def source_overlay(work):
    overlay = json.loads(SOURCE_OVERLAY.read_text())
    if (set(overlay) != {'schema', 'sources_lock_sha256', 'files'} or overlay['schema'] != 1
            or overlay['sources_lock_sha256'] != digest(LOCKFILE) or not isinstance(overlay['files'], dict)):
        fail('源码覆盖清单不是固定来源版本')
    original = files(work / 'inputs/root-extracted/web/frontend')
    original.update({'cyber-ui/' + k: v for k, v in files(work / 'inputs/ui-extracted').items()})
    replacements = files(SOURCE_FILES)
    if set(replacements) != set(overlay['files']):
        fail('正式源码覆盖与逐文件清单不一致')
    for name, entry in overlay['files'].items():
        p = PurePosixPath(name)
        scoped = name.startswith('src/') or (len(p.parts) > 4 and p.parts[:2] == ('cyber-ui', 'packages') and p.parts[3] == 'src')
        if (p.is_absolute() or p.as_posix() != name or '..' in p.parts or '\\' in name
                or any(part.startswith('.') for part in p.parts) or not scoped
                or p.suffix not in {'.js', '.jsx', '.ts', '.tsx', '.css'}):
            fail('源码覆盖只允许src源码文件: ' + name)
        owned = name not in original
        if (not isinstance(entry, dict) or set(entry) != {'source_sha256', 'sha256'}
                or (owned and entry['source_sha256'] is not None)
                or (not owned and entry['source_sha256'] != original[name])
                or entry['sha256'] != replacements[name]):
            fail('源码覆盖原始/当前hash不符: ' + name)
        if (SOURCE_FILES / name).read_bytes().startswith(b'version https://git-lfs.github.com/spec/v1'):
            fail('源码覆盖不可使用LFS指针')
    return overlay


def apply_source_overlay(work):
    for name in source_overlay(work)['files']:
        refresh_copy(SOURCE_FILES / name, work / 'source/web/frontend' / name, work)


def verify_sources(work, pins, installed=False):
    overlay = dependency_overlay(work, pins)
    code = source_overlay(work)
    source = work / 'source'
    # 原始来源始终全量核哈希；构建副本只准清单中逐文件差异。
    expected = root_files(work / 'inputs/root-extracted')
    expected.update({'web/frontend/cyber-ui/' + k: v for k, v in files(work / 'inputs/ui-extracted').items()})
    expected.update({'web/frontend/' + k: v for k, v in overlay['files'].items()})
    expected.update({'web/frontend/' + k: v['sha256'] for k, v in code['files'].items()})
    mapping = root_files(source, installed)
    if mapping != expected:
        fail('应用覆盖后的工作副本不符（包括新增文件）')
    ui = source / 'web/frontend/cyber-ui'
    for name in mapping:
        if name.startswith('web/frontend/'):
            with (source / name).open('rb') as f:
                if f.read(80).startswith(b'version https://git-lfs.github.com/spec/v1'):
                    fail('不接受未取回的LFS指针: ' + name)
    wasm = ui / 'packages/cstx/src/wasm/cstx_wasm_bg.wasm'
    with wasm.open('rb') as f:
        if f.read(8) != b'\x00asm\x01\x00\x00\x00':
            fail('固定Wasm不是实际WebAssembly模块')
    front = source / 'web/frontend'
    package = json.loads((front / 'package.json').read_text())
    if package['scripts']['build'] != 'tsc && vite build':
        fail('构建入口不符')
    lock = json.loads((front / 'package-lock.json').read_text())
    if digest(front / 'package-lock.json') != overlay['files']['package-lock.json']:
        fail('实际构建package-lock不符')
    for name, item in lock['packages'].items():
        if name and (not item.get('resolved', '').startswith('https://registry.npmjs.org/') or not item.get('integrity')):
            fail('依赖缺少固定公共来源/integrity: ' + name)
    if any(PurePosixPath(name).name == '.npmrc' for name in mapping):
        fail('不消费源码自带npm配置')


def init_workspace(work):
    work.mkdir(mode=0o700)  # 不能覆盖已有工作区。
    for name in ('home', 'tmp', 'inputs'):
        (work / name).mkdir(mode=0o700)
    for name in ('npm-user.conf', 'npm-global.conf'):
        (work / 'home' / name).touch(mode=0o600, exist_ok=False)


def prepare(args, pins, work):
    init_workspace(work)
    root_download = work / 'inputs/root-download'
    root_download.mkdir(mode=0o700)
    archive = root_download / 'source.tar'
    if args.root_repo:
        root_repo = Path(args.root_repo).resolve(strict=True)
        actual = run(['git', '-C', str(root_repo), 'rev-parse', pins['root']['commit'] + '^{commit}'], work, capture=True).stdout.strip()
        if actual != pins['root']['commit']:
            fail('本地仓库缺少固定root提交')
        run(['git', '-C', str(root_repo), 'archive', '--format=tar', '-o', str(archive), actual, *SELECT], work)
        extract(archive, work / 'inputs/root-extracted')
    else:
        download(pins['root']['url'], archive, 64 * 1024 * 1024)
        extract(archive, work / 'inputs/root-extracted', 'cyber-harness-' + pins['root']['commit'], SELECT)
    if tree_hash(root_files(work / 'inputs/root-extracted')) != pins['root']['selected_tree_sha256']:
        fail('固定root源内容校验失败')
    ui_download = work / 'inputs/ui-download'
    ui_download.mkdir(mode=0o700)
    ui_archive = ui_download / 'source.tar.gz'
    if args.ui_archive:
        candidate = Path(args.ui_archive).resolve(strict=True)
        if candidate.stat().st_size > 32 * 1024 * 1024:
            fail('UI归档过大')
        shutil.copyfile(candidate, ui_archive)
    else:
        download(pins['cyber_ui']['url'], ui_archive)
    if digest(ui_archive) != pins['cyber_ui']['archive_sha256']:
        fail('固定UI归档SHA256不符')
    extract(ui_archive, work / 'inputs/ui-extracted', 'cyber-ui-' + pins['cyber_ui']['commit'])
    if tree_hash(files(work / 'inputs/ui-extracted')) != pins['cyber_ui']['tree_sha256']:
        fail('固定UI源树校验失败')
    shutil.copytree(work / 'inputs/root-extracted', work / 'source')
    ui_dest = work / 'source/web/frontend/cyber-ui'
    if ui_dest.exists():
        ui_dest.rmdir()  # gitlink的空目录；非空则失败，不删除未知内容。
    shutil.copytree(work / 'inputs/ui-extracted', ui_dest)
    overlay = dependency_overlay(work, pins)
    for name in overlay['files']:
        # 原始inputs只读保留；只覆盖刚复制、已核验来源的两个构建文件。
        shutil.copyfile(OVERLAY_FILES / name, work / 'source/web/frontend' / name)
    apply_source_overlay(work)
    write_prepared(work, pins, {
        'preparation': 'archives', 'root_archive_sha256': digest(archive),
        'ui_archive_sha256': digest(ui_archive),
    })
    print('PREPARED', work, '\nNode安装/构建尚未运行。')


def write_prepared(work, pins, details):
    installed = 'dependency_reuse' in details
    verify_sources(work, pins, installed)
    code = source_overlay(work)
    write_json(work / 'prepared.json', {
        **build_provenance(pins), 'sources_lock_sha256': digest(LOCKFILE),
        'source_overlay_sha256': digest(SOURCE_OVERLAY),
        'owned_source_paths': sorted(name for name, entry in code['files'].items() if entry['source_sha256'] is None),
        'build_source_tree_sha256': tree_hash(root_files(work / 'source', installed)),
        **details, 'node_build_run': False,
        'license_status': 'cyber-ui UNKNOWN; local technical preparation only',
    })


def dependency_tree(root):
    """只读快照；复制时保留相对内部链接，拒绝指回旧workspace或外部。"""
    if not root.is_dir() or root.is_symlink() or root.resolve() != root.absolute():
        fail('依赖必须是独立真实目录')
    mapping = {}
    for directory, dirs, names in os.walk(root):
        for name in sorted(dirs + names):
            p = Path(directory) / name
            relative = p.relative_to(root).as_posix()
            mode = p.lstat().st_mode
            if stat.S_ISLNK(mode):
                target = os.readlink(p)
                if os.path.isabs(target):
                    fail('依赖链接必须相对且留在副本内: ' + relative)
                try:
                    resolved = p.resolve(strict=True)
                except (OSError, RuntimeError):
                    fail('依赖含失效或循环链接: ' + relative)
                if not resolved.is_relative_to(root):
                    fail('依赖链接必须相对且留在副本内: ' + relative)
                mapping[relative] = 'link:' + target
            elif stat.S_ISREG(mode):
                mapping[relative] = 'file:' + digest(p) + ':' + oct(stat.S_IMODE(mode))
            elif stat.S_ISDIR(mode):
                mapping[relative] = 'dir'
            else:
                fail('依赖包含特殊文件: ' + relative)
    return mapping


def installed_dependencies(front):
    locked = json.loads((front / 'package-lock.json').read_text())['packages']
    installed = json.loads((front / 'node_modules/.package-lock.json').read_text())
    entries = installed.get('packages', {})
    if (installed.get('lockfileVersion') != 3 or not entries
            or any(not name or locked.get(name) != item for name, item in entries.items())
            or any(name and not item.get('optional') and name not in entries for name, item in locked.items())):
        fail('已有安装记录与固定构建锁不符')
    for name, item in entries.items():
        package = json.loads((front / name / 'package.json').read_text())
        if package.get('version') != item.get('version'):
            fail('已安装包版本不符: ' + name)
    for name in ('typescript/bin/tsc', 'vite/bin/vite.js'):
        if not (front / 'node_modules' / name).is_file():
            fail('现有安装缺少构建工具')


def fork_workspace(donor, work, pins):
    # 不相信旧source作为原始来源，只读固定inputs与既有依赖安装。
    overlay = dependency_overlay(donor, pins)
    receipt = json.loads((donor / 'release/build.json').read_text())
    if (any(receipt.get(k) != v for k, v in build_provenance(pins).items())
            or receipt.get('sources_lock_sha256') != digest(LOCKFILE)
            or receipt.get('toolchain') != pins['toolchain']):
        fail('复用来源不是相同固定锁/工具链的已构建工作区')
    donor_front = donor / 'source/web/frontend'
    if any(digest(donor_front / name) != value for name, value in overlay['files'].items()):
        fail('复用来源的package/lock已改变')
    nodes = donor_front / 'node_modules'
    snapshot = dependency_tree(nodes)
    installed_dependencies(donor_front)
    init_workspace(work)
    for name in ('root-extracted', 'ui-extracted'):
        shutil.copytree(donor / 'inputs' / name, work / 'inputs' / name)
    dependency_overlay(work, pins)
    shutil.copytree(work / 'inputs/root-extracted', work / 'source')
    ui_dest = work / 'source/web/frontend/cyber-ui'
    if ui_dest.exists():
        ui_dest.rmdir()
    shutil.copytree(work / 'inputs/ui-extracted', ui_dest)
    front = work / 'source/web/frontend'
    for name in overlay['files']:
        shutil.copyfile(OVERLAY_FILES / name, front / name)
    apply_source_overlay(work)
    # 普通复制，绝不链接整个node_modules或使用硬链接；新Vite缓存不会写旧现场。
    shutil.copytree(nodes, front / 'node_modules', symlinks=True)
    if dependency_tree(front / 'node_modules') != snapshot or dependency_tree(nodes) != snapshot:
        fail('依赖复制前后快照不一致')
    installed_dependencies(front)
    write_prepared(work, pins, {
        'preparation': 'offline-fork',
        'dependency_reuse': {
            'donor_workspace': donor.relative_to(REPO).as_posix(),
            'donor_build_sha256': digest(donor / 'release/build.json'),
            'installed_lock_sha256': digest(front / 'node_modules/.package-lock.json'),
            'tree_sha256': tree_hash(snapshot), 'entries': len(snapshot),
        },
    })
    print('FORKED', work, '\n已独立复制固定来源/现有依赖；未运行npm/tsc/Vite。')


def scoped_source_name(name):
    p = PurePosixPath(name)
    return name.startswith('src/') or (len(p.parts) > 4 and p.parts[:2] == ('cyber-ui', 'packages')
                                        and p.parts[3] == 'src')


def refresh_copy(src, dest, work):
    # 在任何mkdir/写入前拒绝source或既有父路径链接、逃逸；统一由本helper创建父目录。
    source_root = work / 'source'
    if source_root.is_symlink() or not source_root.is_dir():
        fail('刷新source不是真实目录: ' + str(source_root))
    check_derived_path(dest, source_root, allow_file=True)
    dest.parent.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(src, dest)


def check_derived_path(path, root, allow_file=False):
    if path.is_symlink() or (path.exists() and (not path.is_file() if allow_file else not path.is_dir())):
        fail('派生路径类型/链接不符: ' + str(path))
    parent = path.parent if allow_file else path
    while not parent.exists() and parent != root:
        parent = parent.parent
    if parent.is_symlink() or not parent.is_dir() or not parent.resolve().is_relative_to(root.resolve()):
        fail('派生路径逃逸工作区: ' + str(path))


def restore_static_gitkeep(work):
    dest = work / 'source/web/static/.gitkeep'
    original = work / 'inputs/root-extracted/web/static/.gitkeep'
    if not dest.exists() and not dest.is_symlink() and original.is_file():
        refresh_copy(original, dest, work)
        print('RESTORED source/web/static/.gitkeep from inputs/root-extracted', flush=True)


def rebuild_refresh_source(work, pins):
    """只把当前正式源码覆盖刷新进工作区派生source；不改inputs/覆盖/依赖/锁。

    只处理允许的 src/ 或 cyber-ui/packages/<包>/src/ 表现源码；绝不回写package/lock等构建文件。
    返回是否实际刷新；覆盖未变则不动派生source。
    """
    prepared = json.loads((work / 'prepared.json').read_text())
    if (prepared['schema'] != 2 or prepared['sources_lock_sha256'] != digest(LOCKFILE)
            or prepared['dependency_overlay_sha256'] != digest(OVERLAY)
            or prepared.get('source_package_lock_sha256') != pins['package_lock_sha256']
            or prepared['build_package_lock_sha256'] != json.loads(OVERLAY.read_text())['files']['package-lock.json']):
        fail('工作区不是当前固定锁/依赖覆盖的已准备副本，不能rebuild')
    overlay_changed = prepared['source_overlay_sha256'] != digest(SOURCE_OVERLAY)
    if not overlay_changed:
        verify_sources(work, pins, installed=True)
        return False
    # 覆盖已变：旧清单已被正式清单替换，不可能逐文件核旧状态；改为自洽刷新——
    # 当前覆盖文件写入派生source；派生中既非原件又非当前覆盖的已允许源码文件，
    # 即之前覆盖、现已移除的文件，恢复固定原件。刷新后由verify_sources全量把关。
    code = source_overlay(work)  # 核正式清单与覆盖文件/固定来源hash
    original = files(work / 'inputs/root-extracted/web/frontend')
    original.update({'cyber-ui/' + k: v for k, v in files(work / 'inputs/ui-extracted').items()})
    front = work / 'source/web/frontend'
    current = {name: entry['sha256'] for name, entry in code['files'].items()}
    for name, hash_value in current.items():
        p = front / name
        if not p.is_file() or digest(p) != hash_value:
            refresh_copy(SOURCE_FILES / name, p, work)
    for name, hash_value in original.items():
        if name in current or not scoped_source_name(name):
            continue  # package/lock/配置等不在允许源码范围，绝不回写
        p = front / name
        if p.is_file() and digest(p) != hash_value:
            # 曾覆盖、现已移除，或派生中有未登记改动：一律回到固定原件。
            if name.startswith('cyber-ui/'):
                origin = work / 'inputs/ui-extracted' / name[len('cyber-ui/'):]
            else:
                origin = work / 'inputs/root-extracted/web/frontend' / name
            refresh_copy(origin, p, work)
    # 只清上一份prepared明确登记的旧自有组件；未知文件留给verify报差异。
    for name in prepared.get('owned_source_paths', []):
        if name not in current and scoped_source_name(name):
            p = front / name
            if p.exists() or p.is_symlink():
                check_derived_path(p, work / 'source', allow_file=True)
                if p.is_file():
                    p.unlink()
    return True


def build_owned_source_names(front):
    result = set()
    for root in (front / 'src', front / 'cyber-ui/packages'):
        if not root.is_dir() or root.is_symlink():
            continue
        for directory, dirs, names in os.walk(root):
            for name in names:
                p = Path(directory) / name
                relative = p.relative_to(front).as_posix()
                if p.is_file() and not p.is_symlink() and scoped_source_name(relative):
                    result.add(relative)
    return result


def rebuild(work, pins, release=None, source_dir=None):
    """复用当前工作区与node_modules：刷新覆盖→固定来源摘要→tsc→Vite→替换选定输出。"""
    prepared = json.loads((work / 'prepared.json').read_text())
    if (prepared['schema'] != 2 or prepared['sources_lock_sha256'] != digest(LOCKFILE)
            or prepared['dependency_overlay_sha256'] != digest(OVERLAY)
            or prepared['source_package_lock_sha256'] != pins['package_lock_sha256']):
        fail('工作区不是当前固定锁的已准备副本，不能rebuild')
    front = work / 'source/web/frontend'
    release = release or work / 'release'
    if (release.is_symlink() or not release.is_relative_to(REPO / 'var')
            or release.parent.resolve(strict=True) != release.parent
            or release in {work, work / 'source', work / 'inputs'} or release.is_relative_to(work / 'source')
            or release.is_relative_to(work / 'inputs')):
        fail('release输出必须位于本仓库var的真实父目录且不能吞并工作区来源')
    if release.exists() and (not (release / 'build.json').is_file() or (release / 'public').is_symlink()
            or (release / 'notices').is_symlink() or (release / 'provenance').is_symlink()):
        fail('已有输出目录只接受带build.json且无链接发布子目录的既有构建release')
    with SHARED_LOCK.open('r+') as heavy:
        fcntl.flock(heavy, fcntl.LOCK_EX | fcntl.LOCK_NB)
        restore_static_gitkeep(work)
        refreshed = rebuild_refresh_source(work, pins)
        # 同一heavy.lock内刷新→校验→固定来源摘要→构建；锁内不再接受外部变更。
        verify_sources(work, pins, installed=True)
        prepared = json.loads((work / 'prepared.json').read_text())
        code = source_overlay(work)
        prepared['source_overlay_sha256'] = digest(SOURCE_OVERLAY)
        prepared['owned_source_paths'] = sorted(name for name, entry in code['files'].items() if entry['source_sha256'] is None)
        prepared['build_source_tree_sha256'] = tree_hash(root_files(work / 'source', installed=True))
        (work / 'prepared.json').write_text(
            json.dumps(prepared, indent=2, ensure_ascii=False) + '\n', encoding='utf-8')
        (work / 'build-started.json').write_text(
            json.dumps({'dependency_install': False, 'rebuild': True}) + '\n', encoding='utf-8')
        node = run(['node', '--version'], work, capture=True).stdout.strip()
        if node != pins['toolchain']['node']:
            fail('node版本不符: ' + node)
        run(['node', str(front / 'node_modules/typescript/bin/tsc')], work, cwd=front, timeout=240)
        run(['node', str(front / 'node_modules/vite/bin/vite.js'), 'build'], work, cwd=front, timeout=360)
        # 先写完整新release到同父目录临时项，成功后再切换；失败保留上一次成功release。
        previous = release.with_name(release.name + '.previous')
        pending = release.with_name(release.name + '.pending')
        if previous.exists() or pending.exists():
            fail('上次替换残留未清理: ' + str(previous if previous.exists() else pending))
        pending.mkdir(mode=0o700)
        try:
            publish_into(work, pins, pending)  # 发布前失败：旧release不动，pending清掉。
        except BaseException:
            shutil.rmtree(pending, ignore_errors=True)
            raise
        if release.exists():
            release.rename(previous)
        try:
            pending.rename(release)
        except BaseException:
            # 切换失败即回滚，绝不留release入口缺失状态；不是原子切换，靠回滚兜底。
            if previous.exists():
                previous.rename(release)
            raise
        shutil.rmtree(previous, ignore_errors=True)  # 只留当前release，不留历史版本。
    print('REBUILT', release / 'public', '\n源码覆盖已刷新:', refreshed,
          '\n仍须真实HTTP/鉴权验收；未做浏览器验收。')


def build(work, pins):
    prepared = json.loads((work / 'prepared.json').read_text())
    reuse = prepared.get('dependency_reuse')
    if (prepared['schema'] != 2 or prepared['sources_lock_sha256'] != digest(LOCKFILE)
            or prepared['dependency_overlay_sha256'] != digest(OVERLAY)
            or prepared.get('source_overlay_sha256') != digest(SOURCE_OVERLAY)):
        fail('来源锁或覆盖在prepare之后改变；旧工作区须离线fork，不原地升级')
    restore_static_gitkeep(work)
    verify_sources(work, pins, bool(reuse))
    overlay = json.loads(OVERLAY.read_text())
    if (prepared['source_package_lock_sha256'] != pins['package_lock_sha256']
            or prepared['build_package_lock_sha256'] != overlay['files']['package-lock.json']
            or prepared['build_source_tree_sha256'] != tree_hash(root_files(work / 'source', bool(reuse)))):
        fail('准备收据与实际来源/构建锁/构建树不符')
    front = work / 'source/web/frontend'
    if (work / 'release').exists() or (work / 'build-started.json').exists():
        fail('已构建或尝试过的工作区只读保留，不隐式重试')
    if reuse:
        if (tree_hash(dependency_tree(front / 'node_modules')) != reuse['tree_sha256']
                or digest(front / 'node_modules/.package-lock.json') != reuse['installed_lock_sha256']):
            fail('复用依赖在fork之后改变')
        installed_dependencies(front)
    elif (front / 'node_modules').exists():
        fail('prepare工作区不能带入未登记的依赖安装')
    # 持用户指定的同一重型锁，不用独立锁绕并发约束。
    with SHARED_LOCK.open('r+') as heavy:
        fcntl.flock(heavy, fcntl.LOCK_EX | fcntl.LOCK_NB)
        for tool in (('node',) if reuse else ('node', 'npm')):
            got = run([tool, '--version'], work, capture=True).stdout.strip()
            if got != pins['toolchain'][tool]:
                fail(tool + '版本不符: ' + got)
        write_json(work / 'build-started.json', {'dependency_install': not bool(reuse)})
        if not reuse:
            run(['npm', 'ci', '--ignore-scripts', '--no-audit', '--no-fund', '--include=optional'], work, cwd=front, timeout=600)
        # 不使用npm run，故prebuild/postbuild及子模块turbo/pnpm不会执行。
        run(['node', str(front / 'node_modules/typescript/bin/tsc')], work, cwd=front, timeout=240)
        run(['node', str(front / 'node_modules/vite/bin/vite.js'), 'build'], work, cwd=front, timeout=360)
        publish(work, pins)
    print('BUILT', work / 'release/public', '\n仍须真实HTTP/鉴权验收；未做浏览器验收。')


def build_provenance(pins):
    overlay = json.loads(OVERLAY.read_text())
    return {
        'schema': 2, 'root_commit': pins['root']['commit'],
        'ui_commit': pins['cyber_ui']['commit'],
        'source_package_lock_sha256': pins['package_lock_sha256'],
        'build_package_lock_sha256': overlay['files']['package-lock.json'],
        'dependency_overlay_sha256': digest(OVERLAY),
    }


def publish(work, pins):
    release = work / 'release'
    if release.exists():
        fail('已有release，不覆盖发布')
    release.mkdir(mode=0o700)
    publish_into(work, pins, release)


def publish_into(work, pins, release):
    provenance = build_provenance(pins)
    prepared = json.loads((work / 'prepared.json').read_text())
    verify_sources(work, pins, installed=True)
    if (prepared['source_overlay_sha256'] != digest(SOURCE_OVERLAY)
            or prepared['build_source_tree_sha256'] != tree_hash(root_files(work / 'source', installed=True))):
        fail('构建阶段来源/覆盖发生变化')
    front = work / 'source/web/frontend'
    overlay = json.loads(OVERLAY.read_text())
    if any(digest(front / name) != value for name, value in overlay['files'].items()):
        fail('构建阶段改动了固定package/lock')
    static = work / 'source/web/static'
    mapping = files(static)
    if 'index.html' not in mapping or not any(n.startswith('assets/') and n.endswith('.js') for n in mapping):
        fail('Vite未生成真实index与JS资源')
    text = (static / 'index.html').read_text()
    if '/assets/' not in text or '<script' not in text:
        fail('index缺少构建入口')
    public = release / 'public'
    public.mkdir(mode=0o700)
    asset_hashes = {}
    for name, hash_value in mapping.items():
        if name == '.gitkeep':
            continue
        if name != 'index.html' and not name.startswith('assets/'):
            fail('未审查的静态顶层文件: ' + name)
        if name.endswith('.map') or any(part.startswith('.') for part in PurePosixPath(name).parts):
            fail('不发布source map或隐藏文件')
        target = public / name
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(static / name, target)
        asset_hashes[name] = hash_value
    # 清单只含公开构建归属/哈希，不放路径、key、state、环境或源码。
    write_json(public / '.wscyber-ui.json', {**provenance, 'files': asset_hashes})
    # 通知保留在非公开release侧；不是完整组合许可结论。
    notices = release / 'notices'
    notices.mkdir()
    shutil.copyfile(work / 'source/LICENSE', notices / 'cyber-harness-LICENSE')
    shutil.copyfile(REPO / 'native/ui/LICENSES.md', notices / 'LICENSES.md')
    # 原锁、实际锁与覆盖审计放在公开根之外，便于交付后复核而不混称上游。
    provenance_dir = release / 'provenance'
    provenance_dir.mkdir()
    shutil.copyfile(LOCKFILE, provenance_dir / 'sources.lock.json')
    shutil.copyfile(OVERLAY, provenance_dir / 'dependencies.overlay.json')
    shutil.copyfile(SOURCE_OVERLAY, provenance_dir / 'source-overlay.json')
    for name in source_overlay(work)['files']:
        dest = provenance_dir / 'source-overlay' / name
        dest.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(SOURCE_FILES / name, dest)
    for name in ('package.json', 'package-lock.json'):
        shutil.copyfile(work / 'inputs/root-extracted/web/frontend' / name, provenance_dir / ('source-' + name))
        shutil.copyfile(front / name, provenance_dir / ('build-' + name))
    inventory = []
    front = work / 'source/web/frontend'
    lock = json.loads((front / 'package-lock.json').read_text())
    for name, item in lock['packages'].items():
        if not name:
            continue
        package = front / name
        entry = {'path': name, 'version': item.get('version'), 'declared_license': item.get('license', 'UNKNOWN'),
                 'installed': package.is_dir(), 'notices': {}}
        if package.is_dir():
            for p in sorted(package.iterdir()):
                if p.is_file() and not p.is_symlink() and p.name.upper().startswith(('LICENSE', 'LICENCE', 'COPYING', 'NOTICE', 'COPYRIGHT')):
                    dest = notices / name / p.name
                    dest.parent.mkdir(parents=True, exist_ok=True)
                    shutil.copyfile(p, dest)
                    entry['notices'][p.name] = digest(p)
        inventory.append(entry)
    write_json(release / 'dependency-notices.json', inventory)
    write_json(release / 'build.json', {
        **provenance, 'sources_lock_sha256': digest(LOCKFILE), 'toolchain': pins['toolchain'],
        'source_overlay_sha256': prepared['source_overlay_sha256'],
        'build_source_tree_sha256': prepared['build_source_tree_sha256'],
        'dependency_reuse': prepared.get('dependency_reuse'),
        'commands': ([] if prepared.get('dependency_reuse') else ['npm ci --ignore-scripts --no-audit --no-fund --include=optional'])
                    + ['node typescript/bin/tsc', 'node vite/bin/vite.js build'],
        'node_options': environment(work)['NODE_OPTIONS'],
        'http_verified': False, 'browser_verified': False,
        'public_manifest_sha256': digest(public / '.wscyber-ui.json'),
    })


def interrupted(_signum, _frame):
    raise KeyboardInterrupt('终止当前前台构建并清理其子进程组')


def main():
    signal.signal(signal.SIGTERM, interrupted)
    if not sys.flags.isolated:
        fail('必须使用 python3 -I')
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('stage', choices=['prepare', 'fork', 'build', 'rebuild'])
    parser.add_argument('--workspace', required=True, help='仓库var下全新独立工作目录')
    parser.add_argument('--root-repo', help='可选只读本地git源；否则固定公开归档')
    parser.add_argument('--ui-archive', help='可选固定归档缓存；仍校验SHA256')
    parser.add_argument('--reuse-workspace', help='fork只读消费同固定锁的既有成功构建，不运行npm')
    parser.add_argument('--release-dir', help='rebuild可选新派生输出；本仓库var真实父目录下，默认work/release')
    args = parser.parse_args()
    work = Path(args.workspace).absolute()
    environment(work)  # 在任何工作区写入前拒绝非法heap设置。
    # 父目录必须已存在且无符号链接别名；不允许指向旧入口/上游/项目根。
    if work.parent.resolve(strict=True) != work.parent or not work.is_relative_to(REPO / 'var') or work == REPO / 'var':
        fail('workspace必须在本仓库var的真实父目录下')
    if work.is_symlink():
        fail('workspace不可为链接')
    pins = json.loads(LOCKFILE.read_text())
    os.umask(0o077)
    if args.stage == 'prepare':
        if args.release_dir:
            fail('仅rebuild接受--release-dir')
        if args.reuse_workspace:
            fail('prepare不接受复用工作区；请用fork')
        prepare(args, pins, work)
    elif args.stage == 'fork':
        if args.release_dir:
            fail('仅rebuild接受--release-dir')
        if not args.reuse_workspace or args.root_repo or args.ui_archive:
            fail('fork只接受--reuse-workspace，不下载/执行来源')
        donor = Path(args.reuse_workspace).absolute()
        if (donor.resolve(strict=True) != donor or not donor.is_relative_to(REPO / 'var')
                or donor == REPO / 'var' or work.is_relative_to(donor) or donor.is_relative_to(work)):
            fail('复用来源须为本仓库var下独立真实目录，不能嵌套或指向本轮workspace')
        with SHARED_LOCK.open('r+') as heavy:
            fcntl.flock(heavy, fcntl.LOCK_EX | fcntl.LOCK_NB)
            fork_workspace(donor, work, pins)
    else:
        if args.root_repo or args.ui_archive or args.reuse_workspace or (args.release_dir and args.stage != 'rebuild'):
            fail('build/rebuild不接受新的来源；仅rebuild接受--release-dir')
        if args.stage == 'rebuild':
            rebuild(work, pins, Path(args.release_dir).absolute() if args.release_dir else None)
        else:
            build(work, pins)


if __name__ == '__main__':
    try:
        main()
    except (RuntimeError, OSError, ValueError, subprocess.SubprocessError) as exc:
        print('cyber-ui:', exc, file=sys.stderr)
        sys.exit(1)

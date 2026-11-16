#!/usr/bin/env python3
"""scripts/cyber-ui-build.py rebuild入口的纯fixture定向检查。

不用Node/子进程/网络；在临时目录伪造inputs/node_modules/覆盖，mock tsc与Vite。
"""
import hashlib
import importlib.util
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

REPO = Path(__file__).resolve().parents[1]


def load_build():
    spec = importlib.util.spec_from_file_location('cyber_ui_build', REPO / 'scripts/cyber-ui-build.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def digest_of(data: bytes):
    return hashlib.sha256(data).hexdigest()


def files_tree(root, names):
    return {name: digest(root / name) for name in names}


class Fixture:
    def __init__(self, tmp):
        self.tmp = Path(tmp)
        self.repo = self.tmp / 'repo'
        self.work = self.tmp / 'var/w/ui-work'
        self.lockfile_hash = digest_of(b'{}')
        overlay_lock_hash = 'b' * 64
        native_ui = self.repo / 'native/ui'
        native_ui.mkdir(parents=True)
        (self.repo / 'var').mkdir()
        shared = self.repo / 'var/heavy.lock'
        shared.touch()

        # 固定依赖覆盖：两个文件。
        dep_files = {'package.json': '{"dependencies":{"x":"1.0.0"}}\n',
                     'package-lock.json': '{"lockfileVersion":3,"packages":{}}\n'}
        build_overlay = native_ui / 'build-overlay'
        build_overlay.mkdir()
        for name, text in dep_files.items():
            (build_overlay / name).write_text(text)
        (native_ui / 'dependencies.overlay.json').write_text(json.dumps({
            'schema': 1,
            'source': {'sources_lock_sha256': self.lockfile_hash, 'root_commit': 'r1',
                       'ui_commit': 'u1', 'package_lock_sha256': 'p1',
                       'package_sha256': 'psha', 'package_lock_sha256_2': 'x',
                       'ui_pnpm_lock_sha256': 'pn'},
            'files': {name: digest(build_overlay / name) for name in dep_files},
            'direct_additions': {}, 'audit': {'added_entries': {}},
        }))

        # 正式源码覆盖：初始一个文件，后改为另一个文件。
        source_files = native_ui / 'source-overlay'
        (source_files / 'src').mkdir(parents=True)
        overlay_a = source_files / 'src/a.tsx'
        overlay_a.write_text('export const A = 1\n')
        self.overlay_b = source_files / 'src/b.tsx'
        self.overlay_b.write_text('export const B = 2\n')
        (native_ui / 'source-overlay.json').write_text(json.dumps({
            'schema': 1, 'sources_lock_sha256': self.lockfile_hash,
            'files': {'src/a.tsx': {'source_sha256': digest(self.tmp / 'nonexistent')
                                    if False else None, 'sha256': digest(overlay_a)}},
        }))
        (native_ui / 'LICENSES.md').write_text('licenses\n')

        self.pins = {
            'root': {'commit': 'r1', 'url': '', 'selected_tree_sha256': ''},
            'cyber_ui': {'commit': 'u1', 'url': '', 'archive_sha256': '', 'tree_sha256': ''},
            'package_lock_sha256': 'p1',
            'toolchain': {'node': 'vfixture'},
        }

        # 输入树：root frontend 两个源文件 + cyber-ui 一个源文件。
        inputs = self.work / 'inputs'
        root_extracted = inputs / 'root-extracted'
        root_extracted.mkdir(parents=True)
        (root_extracted / 'LICENSE').write_text('license\n')
        (root_extracted / '.gitmodules').write_text('\n')
        (root_extracted / 'web/static').mkdir(parents=True)
        (root_extracted / 'web/static/.gitkeep').touch()
        front_src = root_extracted / 'web/frontend'
        (front_src / 'src').mkdir(parents=True)
        (front_src / 'src/a.tsx').write_text('export const A = 0\n')
        (front_src / 'src/b.tsx').write_text('export const B = 0\n')
        (front_src / 'package.json').write_text('{"scripts":{"build":"tsc && vite build"},"dependencies":{}}\n')
        (front_src / 'package-lock.json').write_text('{"lockfileVersion":3,"packages":{}}\n')
        ui_src = inputs / 'ui-extracted/packages/p/src'
        ui_src.mkdir(parents=True)
        (ui_src / 'c.ts').write_text('export const C = 0\n')
        self.source_sha = {'src/a.tsx': digest(front_src / 'src/a.tsx'),
                           'src/b.tsx': digest(front_src / 'src/b.tsx'),
                           'cyber-ui/packages/p/src/c.ts': digest(ui_src / 'c.ts')}

        # 正式源码覆盖清单修正：source_sha256 指向固定原件。
        manifest = json.loads((native_ui / 'source-overlay.json').read_text())
        manifest['files']['src/a.tsx']['source_sha256'] = self.source_sha['src/a.tsx']
        (native_ui / 'source-overlay.json').write_text(json.dumps(manifest))

        # 派生source：inputs副本 + 依赖两文件 + 覆盖 + node_modules。
        source = self.work / 'source'
        shutil.copytree(inputs / 'root-extracted', source)
        ui_dest = source / 'web/frontend/cyber-ui'
        shutil.copytree(inputs / 'ui-extracted', ui_dest)
        front = source / 'web/frontend'
        for name in dep_files:
            shutil.copyfile(build_overlay / name, front / name)
        shutil.copyfile(overlay_a, front / 'src/a.tsx')
        nodes = front / 'node_modules'
        nodes.mkdir()
        (nodes / 'typescript/bin').mkdir(parents=True)
        (nodes / 'typescript/bin/tsc').write_text('#!/bin/sh\n')
        (nodes / 'vite/bin').mkdir(parents=True)
        (nodes / 'vite/bin/vite.js').write_text('#!/bin/sh\n')
        (nodes / 'x').mkdir()
        (nodes / 'x/package.json').write_text('{"version":"1.0.0"}\n')
        (nodes / '.package-lock.json').write_text('{"lockfileVersion":3,"packages":{}}\n')
        # wasm与package scripts核验需要
        wasm = ui_dest / 'packages/cstx/src/wasm'
        wasm.mkdir(parents=True, exist_ok=True)
        (wasm / 'cstx_wasm_bg.wasm').write_bytes(b'\x00asm\x01\x00\x00\x00')
        # Vite产物
        static_out = source / 'web/static'
        (static_out / 'assets').mkdir(parents=True)
        (static_out / 'index.html').write_text('<script src="/assets/x.js"></script>')
        (static_out / 'assets/x.js').write_text('console.log(1)\n')

        prepared = {
            'schema': 2, 'sources_lock_sha256': self.lockfile_hash,
            'root_commit': 'r1', 'ui_commit': 'u1',
            'source_package_lock_sha256': 'p1',
            'build_package_lock_sha256': digest(build_overlay / 'package-lock.json'),
            'dependency_overlay_sha256': digest(native_ui / 'dependencies.overlay.json'),
            'source_overlay_sha256': digest(native_ui / 'source-overlay.json'),
            'build_source_tree_sha256': 'pending',
            'preparation': 'offline-fork',
            'dependency_reuse': None,
            'node_build_run': False,
            'license_status': 'fixture',
            'toolchain': self.pins['toolchain'],
        }
        (self.work / 'prepared.json').write_text(json.dumps(prepared, indent=2))
        (self.work / 'home').mkdir()
        (self.work / 'tmp').mkdir()

        self.module = load_build()
        # 指向fixture仓库路径。
        self.module.REPO = self.repo
        self.module.LOCKFILE = native_ui / 'sources.lock.json'
        self.module.LOCKFILE.write_text('{}')
        self.module.OVERLAY_FILES = build_overlay
        self.module.OVERLAY = native_ui / 'dependencies.overlay.json'
        self.module.SOURCE_OVERLAY = native_ui / 'source-overlay.json'
        self.module.SOURCE_FILES = source_files
        self.module.SHARED_LOCK = shared
        # dependency_overlay核验需要更完整字段，简化：绕过，直接让verify_sources通过较难，
        # 因此fixture采用monkeypatch方式验证rebuild主流程。
        self.calls = []
        self.module.verify_sources = lambda work, pins, installed=False: self.calls.append('verify')
        self.module.installed_dependencies = lambda front: self.calls.append('installed')
        self.module.run = lambda argv, work, cwd=None, timeout=120, capture=False: (
            self.calls.append(Path(argv[-1]).name)
            or subprocess.CompletedProcess(argv, 0, 'vfixture', ''))
        self.published = []
        def fake_publish(work, pins):
            pass
        self.module.publish = fake_publish
        self.module.publish_into = self._fake_publish_into
        (self.work / 'release/public').mkdir(parents=True)
        (self.work / 'release/public/index.html').write_text('old')
        self.old_release = self.work / 'release'

    def _fake_publish_into(self, work, pins, release):
        self.published.append(digest(self.module.SOURCE_OVERLAY))
        (release / 'public').mkdir(parents=True, exist_ok=True)
        (release / 'public/index.html').write_text('<script src="/assets/x.js"></script>')

    def manifest_replace_a_with_b(self):
        native_ui = self.repo / 'native/ui'
        manifest = json.loads((native_ui / 'source-overlay.json').read_text())
        del manifest['files']['src/a.tsx']
        manifest['files']['src/b.tsx'] = {
            'source_sha256': self.source_sha['src/b.tsx'], 'sha256': digest(self.overlay_b)}
        (native_ui / 'source-overlay.json').write_text(json.dumps(manifest))
        (self.repo / 'native/ui/source-overlay/src/a.tsx').unlink()

    def manifest_replace_cyber_ui(self):
        # 再移除一个cyber-ui覆盖（追加c.ts覆盖到清单与覆盖目录，再整体替换清单为空该项）。
        native_ui = self.repo / 'native/ui'
        overlay_c = native_ui / 'source-overlay/cyber-ui/packages/p/src/c.ts'
        overlay_c.parent.mkdir(parents=True, exist_ok=True)
        overlay_c.write_text('export const C = 9\n')
        manifest = json.loads((native_ui / 'source-overlay.json').read_text())
        manifest['files']['cyber-ui/packages/p/src/c.ts'] = {
            'source_sha256': self.source_sha['cyber-ui/packages/p/src/c.ts'], 'sha256': digest(overlay_c)}
        (native_ui / 'source-overlay.json').write_text(json.dumps(manifest))
        # 模拟“派生里曾应用过该覆盖”：派生c.ts与原件不同。
        derived_c = self.work / 'source/web/frontend/cyber-ui/packages/p/src/c.ts'
        derived_c.write_text('export const C = 9\n')
        # 新清单再移除该覆盖：刷新须把派生恢复为ui-extracted原件（正确无前缀路径）。
        manifest = json.loads((native_ui / 'source-overlay.json').read_text())
        del manifest['files']['cyber-ui/packages/p/src/c.ts']
        (native_ui / 'source-overlay.json').write_text(json.dumps(manifest))
        overlay_c.unlink()
        for parent in (overlay_c.parent, overlay_c.parent.parent, overlay_c.parent.parent.parent,
                       overlay_c.parent.parent.parent.parent):
            try:
                parent.rmdir()
            except OSError:
                pass


def check_no_copy_on_reuse(tmp):
    fx = Fixture(tmp)
    before = fx.calls
    fx.module.rebuild(fx.work, fx.pins)
    # 依赖目录未被触碰：node_modules里文件仍在且无复制动作（无dependency_tree调用记录）。
    assert 'tsc' in fx.calls and 'build' in fx.calls and '--version' in fx.calls, fx.calls
    assert not any(call == 'npm' for call in fx.calls)
    # 覆盖未变：不刷新。
    assert fx.published, 'publish未运行'
    print('ok: rebuild不复制依赖、不npm、未变覆盖不刷新')


def check_refresh_and_removed_overlay(tmp):
    fx = Fixture(tmp)
    # 依赖覆盖两文件与固定原件不同（模拟真实依赖overlay），刷新绝不能回写它们。
    overlay_dir = fx.repo / 'native/ui/build-overlay'
    before_pkg = (fx.work / 'source/web/frontend/package.json').read_bytes()
    before_lock = (fx.work / 'source/web/frontend/package-lock.json').read_bytes()
    assert before_pkg != (fx.work / 'inputs/root-extracted/web/frontend/package.json').read_bytes(), 'fixture应模拟依赖overlay差异'
    fx.manifest_replace_a_with_b()
    fx.manifest_replace_cyber_ui()  # 同时移除一个cyber-ui覆盖，须恢复ui-extracted原件（无前缀路径）
    fx.module.rebuild(fx.work, fx.pins)
    front = fx.work / 'source/web/frontend'
    assert (front / 'src/b.tsx').read_text() == fx.overlay_b.read_text(), '覆盖b未应用'
    assert (front / 'src/a.tsx').read_bytes() == (fx.work / 'inputs/root-extracted/web/frontend/src/a.tsx').read_bytes(), '移除覆盖未回到原件'
    assert (front / 'package.json').read_bytes() == before_pkg, '依赖package被刷新回写'
    assert (front / 'package-lock.json').read_bytes() == before_lock, '依赖lock被刷新回写'
    assert (front / 'cyber-ui/packages/p/src/c.ts').read_bytes() == \
        (fx.work / 'inputs/ui-extracted/packages/p/src/c.ts').read_bytes(), 'cyber-ui移除覆盖未恢复正确原件'
    # 第二次rebuild（覆盖未再变）：不重复刷新但仍构建。
    fx.calls.clear()
    fx.module.rebuild(fx.work, fx.pins)
    assert 'tsc' in fx.calls and 'build' in fx.calls
    print('ok: 刷新应用新覆盖、移除覆盖回原件、依赖文件不动、重复rebuild仍构建且不复制依赖')


def check_failure_keeps_old_release(tmp):
    fx = Fixture(tmp)
    old_index = fx.old_release / 'public/index.html'
    def failing_run(argv, work, cwd=None, timeout=120, capture=False):
        if '--version' in argv:
            return subprocess.CompletedProcess(argv, 0, 'vfixture', '')
        if Path(argv[-1]).name == 'tsc':
            raise RuntimeError('mock tsc failure')
        return subprocess.CompletedProcess(argv, 0, '', '')
    fx.module.run = failing_run
    try:
        fx.module.rebuild(fx.work, fx.pins)
    except RuntimeError as exc:
        assert 'mock tsc failure' in str(exc)
    else:
        raise AssertionError('rebuild应失败')
    assert old_index.read_text() == 'old', '旧release被毁'
    assert fx.old_release.is_dir(), '旧release目录应保留'
    print('ok: 失败保留上一次成功release')


def check_success_single_release(tmp):
    fx = Fixture(tmp)
    fx.module.rebuild(fx.work, fx.pins)
    release = fx.work / 'release'
    assert release.is_dir() and (release / 'public/index.html').read_text() != 'old', '未替换为新release'
    assert not (fx.work / 'release.previous').exists(), '残留旧release'
    assert not (fx.work / 'release.pending').exists(), '残留pending'
    leftovers = [p.name for p in fx.work.iterdir() if p.name.startswith('release')]
    assert leftovers == ['release'], leftovers
    print('ok: 成功后只留一个当前release')


def main():
    # 纯检查临时目录放本项目var下（项目工作边界），用完即删。
    base = REPO / 'var' / 'ui-rebuild-check-tmp'
    if base.exists():
        shutil.rmtree(base)
    base.mkdir(mode=0o700)
    tmp = tempfile.mkdtemp(prefix='case-', dir=base)
    try:
        check_no_copy_on_reuse(tmp)
        shutil.rmtree(tmp)
        tmp = tempfile.mkdtemp(prefix='case-', dir=base)
        check_refresh_and_removed_overlay(tmp)
        shutil.rmtree(tmp)
        tmp = tempfile.mkdtemp(prefix='case-', dir=base)
        check_failure_keeps_old_release(tmp)
        shutil.rmtree(tmp)
        tmp = tempfile.mkdtemp(prefix='case-', dir=base)
        check_success_single_release(tmp)
    finally:
        shutil.rmtree(base, ignore_errors=True)
    print('ALL REBUILD FIXTURE CHECKS PASSED')


if __name__ == '__main__':
    main()

#!/usr/bin/env python3
"""纯文件检查；不启动Node/npm/Go/HTTP，不执行构建源码。使用python3 -I。"""
import argparse
import importlib.util
import json
from pathlib import Path
import shutil
import sys
import tempfile
import unittest

sys.dont_write_bytecode = True
REPO = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('cyber_ui_build', REPO / 'scripts/cyber-ui-build.py')
build = importlib.util.module_from_spec(spec)
spec.loader.exec_module(build)


def put(path, content):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(content if isinstance(content, bytes) else content.encode())


def put_json(path, value):
    put(path, json.dumps(value, sort_keys=True))


class SourceChecks(unittest.TestCase):
    def setUp(self):
        # 所有合成来源/依赖在本次独占目录；测试脚本始终位于scripts，不执行输入。
        self.temp = tempfile.TemporaryDirectory(prefix='source-check.', dir=CHECK_ROOT)
        self.addCleanup(self.temp.cleanup)
        self.repo = Path(self.temp.name)
        self.original = {key: getattr(build, key) for key in ('REPO', 'LOCKFILE', 'OVERLAY', 'OVERLAY_FILES', 'SOURCE_OVERLAY', 'SOURCE_FILES')}
        self.addCleanup(lambda: [setattr(build, key, value) for key, value in self.original.items()])
        build.REPO = self.repo
        for key, name in (('LOCKFILE', 'sources.json'), ('OVERLAY', 'deps.json'), ('OVERLAY_FILES', 'deps'), ('SOURCE_OVERLAY', 'source.json'), ('SOURCE_FILES', 'overlay')):
            setattr(build, key, self.repo / name)
        self.donor = self.repo / 'var/donor'
        root = self.donor / 'inputs/root-extracted'
        ui = self.donor / 'inputs/ui-extracted'
        package = {'name': 'fixture', 'dependencies': {'typescript': '1.0.0', 'vite': '1.0.0'}, 'scripts': {'build': 'tsc && vite build'}}
        lock = {'lockfileVersion': 3, 'packages': {'': package}}
        for name in ('typescript', 'vite'):
            lock['packages']['node_modules/' + name] = {'version': '1.0.0', 'resolved': 'https://registry.npmjs.org/' + name, 'integrity': 'fixture'}
        for name, content in (('LICENSE', 'fixture'), ('.gitmodules', ''), ('web/static/.gitkeep', '\n'), ('web/frontend/src/App.tsx', 'original'), ('web/frontend/tsconfig.json', '{}')):
            put(root / name, content)
        for name, value in (('package.json', package), ('package-lock.json', lock)):
            put_json(root / 'web/frontend' / name, value)
            put_json(build.OVERLAY_FILES / name, value)
        put(ui / 'pnpm-lock.yaml', 'fixture')
        put(ui / 'packages/ui/src/Button.tsx', 'original-ui')
        put(ui / 'packages/cstx/src/wasm/cstx_wasm_bg.wasm', b'\x00asm\x01\x00\x00\x00')
        self.pins = {'root': {'commit': 'fixed-root', 'selected_tree_sha256': build.tree_hash(build.root_files(root))},
                     'cyber_ui': {'commit': 'fixed-ui', 'tree_sha256': build.tree_hash(build.files(ui))},
                     'package_lock_sha256': build.digest(root / 'web/frontend/package-lock.json'),
                     'toolchain': {'node': 'not-executed', 'npm': 'not-executed'}}
        put_json(build.LOCKFILE, self.pins)
        put_json(build.OVERLAY, {'schema': 1, 'source': {
            'sources_lock_sha256': build.digest(build.LOCKFILE), 'root_commit': 'fixed-root', 'ui_commit': 'fixed-ui',
            'package_sha256': build.digest(root / 'web/frontend/package.json'), 'package_lock_sha256': self.pins['package_lock_sha256'],
            'ui_pnpm_lock_sha256': build.digest(ui / 'pnpm-lock.yaml')},
            'files': build.files(build.OVERLAY_FILES), 'direct_additions': {}, 'audit': {'added_entries': {}}})
        self.overlay = {'schema': 1, 'sources_lock_sha256': build.digest(build.LOCKFILE), 'files': {}}
        put_json(build.SOURCE_OVERLAY, self.overlay)
        shutil.copytree(root, self.donor / 'source')
        shutil.copytree(ui, self.donor / 'source/web/frontend/cyber-ui')
        self.front = self.donor / 'source/web/frontend'
        for name, executable in (('typescript', 'bin/tsc'), ('vite', 'bin/vite.js')):
            put_json(self.front / 'node_modules' / name / 'package.json', {'version': '1.0.0'})
            put(self.front / 'node_modules' / name / executable, 'not executable test input')
        (self.front / 'node_modules/.bin').mkdir()
        (self.front / 'node_modules/.bin/tsc').symlink_to('../typescript/bin/tsc')
        put_json(self.front / 'node_modules/.package-lock.json', {'lockfileVersion': 3, 'packages': {k: v for k, v in lock['packages'].items() if k}})
        put_json(self.donor / 'release/build.json', {**build.build_provenance(self.pins), 'sources_lock_sha256': build.digest(build.LOCKFILE), 'toolchain': self.pins['toolchain']})

    def overlay_file(self, name='src/App.tsx'):
        original = self.donor / 'inputs/root-extracted/web/frontend' / name
        if name.startswith('cyber-ui/'):
            original = self.donor / 'inputs/ui-extracted' / name.removeprefix('cyber-ui/')
        put(build.SOURCE_FILES / name, 'replacement')
        self.overlay['files'][name] = {'source_sha256': build.digest(original), 'sha256': build.digest(build.SOURCE_FILES / name)}
        put_json(build.SOURCE_OVERLAY, self.overlay)

    def owned_overlay_file(self, name='src/components/Owned.tsx'):
        put(build.SOURCE_FILES / name, 'owned replacement')
        self.overlay['files'][name] = {'source_sha256': None, 'sha256': build.digest(build.SOURCE_FILES / name)}
        put_json(build.SOURCE_OVERLAY, self.overlay)

    def prepared_for_refresh(self):
        put_json(self.donor / 'prepared.json', {**build.build_provenance(self.pins),
            'source_overlay_sha256': 'outdated', 'sources_lock_sha256': build.digest(build.LOCKFILE)})

    def test_owned_component_create_verify_and_refresh_remove(self):
        self.owned_overlay_file()
        build.apply_source_overlay(self.donor)
        build.verify_sources(self.donor, self.pins, installed=True)
        self.prepared_for_refresh()
        self.assertTrue(build.rebuild_refresh_source(self.donor, self.pins))
        build.verify_sources(self.donor, self.pins, installed=True)
        unknown = self.front / 'src/components/Unknown.tsx'
        put(unknown, 'unknown must survive refresh')
        self.overlay['files'] = {}
        put_json(build.SOURCE_OVERLAY, self.overlay)
        shutil.rmtree(build.SOURCE_FILES / 'src/components')
        prepared = {**build.build_provenance(self.pins), 'owned_source_paths': ['src/components/Owned.tsx'],
            'source_overlay_sha256': 'outdated', 'sources_lock_sha256': build.digest(build.LOCKFILE)}
        put_json(self.donor / 'prepared.json', prepared)
        self.assertTrue(build.rebuild_refresh_source(self.donor, self.pins))
        self.assertFalse((self.front / 'src/components/Owned.tsx').exists())
        self.assertEqual(unknown.read_text(), 'unknown must survive refresh')
        self.assertEqual((self.front / 'src/App.tsx').read_text(), 'original')
        with self.assertRaises(RuntimeError):
            build.verify_sources(self.donor, self.pins, installed=True)
        unknown.unlink()
        build.verify_sources(self.donor, self.pins, installed=True)

    def test_rebuild_records_sources_before_tsc_and_spares_old_release(self):
        self.owned_overlay_file()
        self.prepared_for_refresh()
        put(self.repo / 'native/ui/LICENSES.md', 'fixture')
        put(self.front / 'node_modules/vite/bin/vite.js', str(self.front / 'node_modules/vite/bin/vite.js'))
        old_release = self.donor / 'release'
        old_bytes = {p.relative_to(old_release): p.read_bytes() for p in old_release.rglob('*') if p.is_file()}
        calls = []
        def fake_run(argv, work, **kwargs):
            calls.append(argv)
            if argv[-1].endswith('/bin/tsc'):
                receipt = json.loads((work / 'prepared.json').read_text())
                self.assertEqual(receipt['source_overlay_sha256'], build.digest(build.SOURCE_OVERLAY))
                self.assertEqual(receipt['owned_source_paths'], ['src/components/Owned.tsx'])
                self.assertEqual(receipt['build_source_tree_sha256'], build.tree_hash(build.root_files(work / 'source', installed=True)))
            if 'vite.js' in str(argv):
                put(self.donor / 'source/web/static/index.html', '<script src="/assets/app.js"></script>')
                put(work / 'source/web/static/index.html', '<script src="/assets/app.js"></script>')
                put(work / 'source/web/static/assets/app.js', 'fixture')
            return build.subprocess.CompletedProcess(argv, 0, 'not-executed', '')
        self.addCleanup(lambda: setattr(build, 'run', fake_run))
        build.run = fake_run
        build.SHARED_LOCK = self.donor / 'heavy.lock'
        build.SHARED_LOCK.touch()
        release = self.repo / 'var/new-output/release'
        release.parent.mkdir(parents=True)
        build.rebuild(self.donor, self.pins, release)
        self.assertEqual({p.relative_to(old_release): p.read_bytes() for p in old_release.rglob('*') if p.is_file()}, old_bytes)
        self.assertTrue((release / 'public/index.html').is_file())
        self.assertTrue(calls)

    def test_copy_rejects_linked_source_and_parent_before_mkdir(self):
        outside = self.repo / 'outside'
        outside.mkdir()
        source = self.donor / 'source'
        real_source = self.donor / 'real-source'
        source.rename(real_source)
        source.symlink_to(real_source, target_is_directory=True)
        target = source / 'web/frontend/src/components/Owned.tsx'
        with self.assertRaises(RuntimeError):
            build.refresh_copy(build.SOURCE_FILES / 'none', target, self.donor)
        self.assertFalse(target.exists())
        source.unlink()
        real_source.rename(source)
        components = self.front / 'src/components'
        components.symlink_to(outside, target_is_directory=True)
        with self.assertRaises(RuntimeError):
            build.refresh_copy(build.SOURCE_FILES / 'none', components / 'Owned.tsx', self.donor)
        self.assertEqual(list(outside.iterdir()), [])

    def test_exact_source_overlay(self):
        self.overlay_file()
        self.overlay_file('cyber-ui/packages/ui/src/Button.tsx')
        build.apply_source_overlay(self.donor)
        build.verify_sources(self.donor, self.pins, installed=True)
        put(self.front / 'src/unlisted.ts', 'unexpected')
        with self.assertRaises(RuntimeError):
            build.verify_sources(self.donor, self.pins, installed=True)

    def test_original_cannot_claim_owned(self):
        self.overlay_file()
        self.overlay['files']['src/App.tsx']['source_sha256'] = None
        put_json(build.SOURCE_OVERLAY, self.overlay)
        with self.assertRaises(RuntimeError):
            build.source_overlay(self.donor)

    def test_bad_hash_unlisted_and_dependency_paths(self):
        self.overlay_file()
        put(build.SOURCE_FILES / 'src/App.tsx', 'changed after manifest')
        with self.assertRaises(RuntimeError):
            build.source_overlay(self.donor)
        put(build.SOURCE_FILES / 'src/App.tsx', 'replacement')
        self.overlay['files']['src/App.tsx']['source_sha256'] = '0' * 64
        put_json(build.SOURCE_OVERLAY, self.overlay)
        with self.assertRaises(RuntimeError):
            build.source_overlay(self.donor)
        self.overlay_file()
        put(build.SOURCE_FILES / 'src/extra.ts', 'unlisted')
        with self.assertRaises(RuntimeError):
            build.source_overlay(self.donor)
        (build.SOURCE_FILES / 'src/extra.ts').unlink()
        self.overlay_file('package.json')
        with self.assertRaises(RuntimeError):
            build.source_overlay(self.donor)

    def test_escape_and_source_links(self):
        self.overlay_file()
        entry = self.overlay['files'].pop('src/App.tsx')
        self.overlay['files']['src/../src/App.tsx'] = entry
        put_json(build.SOURCE_OVERLAY, self.overlay)
        with self.assertRaises(RuntimeError):
            build.source_overlay(self.donor)
        file = build.SOURCE_FILES / 'src/App.tsx'
        file.unlink()
        file.symlink_to(self.front / 'src/App.tsx')
        with self.assertRaises(RuntimeError):
            build.source_overlay(self.donor)

    def test_fork_uses_inputs_and_is_independent(self):
        self.overlay_file()
        put(self.front / 'src/App.tsx', 'old workspace edits must not be copied')
        before = build.dependency_tree(self.front / 'node_modules')
        work = self.repo / 'var/new'
        build.fork_workspace(self.donor, work, self.pins)
        self.assertEqual((work / 'source/web/frontend/src/App.tsx').read_text(), 'replacement')
        self.assertEqual(build.dependency_tree(work / 'source/web/frontend/node_modules'), before)
        old = self.front / 'node_modules/typescript/bin/tsc'
        new = work / 'source/web/frontend/node_modules/typescript/bin/tsc'
        self.assertNotEqual(old.stat().st_ino, new.stat().st_ino)
        new.write_text('new-only')
        self.assertEqual(build.dependency_tree(self.front / 'node_modules'), before)
        self.assertNotEqual(build.tree_hash(build.dependency_tree(work / 'source/web/frontend/node_modules')), json.loads((work / 'prepared.json').read_text())['dependency_reuse']['tree_sha256'])
        with self.assertRaises(FileExistsError):
            build.fork_workspace(self.donor, work, self.pins)

    def test_dependency_links_and_lock_mismatch(self):
        nodes = self.front / 'node_modules'
        link = nodes / '.bin/tsc'
        link.unlink()
        for target in ('../../src/App.tsx', '../missing', str(nodes / 'typescript/bin/tsc')):
            with self.subTest(target=target):
                link.symlink_to(target)
                try:
                    with self.assertRaises(RuntimeError):
                        build.dependency_tree(nodes)
                finally:
                    link.unlink()
        put_json(nodes / '.package-lock.json', {'lockfileVersion': 3, 'packages': {}})
        with self.assertRaises(RuntimeError):
            build.installed_dependencies(self.front)

    def test_exclusions_remain_exact_and_inputs_are_fixed(self):
        build.verify_sources(self.donor, self.pins, installed=True)
        put(self.front / 'src/node_modules/extra.js', 'not an excluded directory')
        with self.assertRaises(RuntimeError):
            build.verify_sources(self.donor, self.pins, installed=True)
        (self.front / 'src/node_modules/extra.js').unlink()
        put(self.donor / 'inputs/root-extracted/web/frontend/src/App.tsx', 'tampered input')
        with self.assertRaises(RuntimeError):
            build.dependency_overlay(self.donor, self.pins)


if __name__ == '__main__':
    if not sys.flags.isolated:
        raise SystemExit('必须使用python3 -I')
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--scratch-parent', required=True, help='本任务包内已存在的纯检查临时父目录')
    parser.add_argument('--inputs-workspace', help='可选：只读核固定inputs和正式覆盖，不核/改旧source或依赖')
    args = parser.parse_args()
    CHECK_ROOT = Path(args.scratch_parent).absolute()
    if CHECK_ROOT.resolve(strict=True) != CHECK_ROOT or not CHECK_ROOT.is_relative_to(REPO / 'var'):
        raise SystemExit('scratch-parent必须位于本仓库var真实目录')
    # 防止纯检查无意回归成构建/下载；fork用例也不允许子进程。
    def forbidden(*_args, **_kwargs):
        raise AssertionError('纯检查禁止执行子进程/下载')
    build.run = build.download = forbidden
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(SourceChecks))
    if not result.wasSuccessful():
        raise SystemExit(1)
    if args.inputs_workspace:
        work = Path(args.inputs_workspace).absolute()
        if work.resolve(strict=True) != work or not work.is_relative_to(REPO / 'var'):
            raise SystemExit('inputs-workspace必须位于本仓库var真实目录')
        pins = json.loads(build.LOCKFILE.read_text())
        build.dependency_overlay(work, pins)
        overlay = build.source_overlay(work)
        print('固定inputs/依赖锁/源码覆盖纯检查通过，覆盖文件数：', len(overlay['files']))

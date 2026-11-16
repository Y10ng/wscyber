#!/usr/bin/env python3
"""主在执行模型退出、真实UI构建完成后运行；仅loopback HTTP，无浏览器/模型/任务执行。

启动一次编译Hub，读取真实资产和原生只读API，SIGTERM收尾后仅删本脚本新建的私有runtime。
检查日志/结果与二进制/公开资产保留；不碰用户预览、旧状态和共享缓存。
"""
import argparse
import hashlib
import json
import ipaddress
import os
from pathlib import Path
import secrets
import selectors
import shutil
import signal
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
import fcntl

REPO = Path(__file__).resolve().parents[1]


def sha(path):
    h = hashlib.sha256()
    with path.open('rb') as f:
        while chunk := f.read(65536):
            h.update(chunk)
    return h.hexdigest()


def verify_provenance(manifest):
    pins = json.loads((REPO/'native/ui/sources.lock.json').read_text())
    overlay_path = REPO/'native/ui/dependencies.overlay.json'
    overlay = json.loads(overlay_path.read_text())
    expected = {
        'schema':2, 'root_commit':pins['root']['commit'], 'ui_commit':pins['cyber_ui']['commit'],
        'source_package_lock_sha256':pins['package_lock_sha256'],
        'build_package_lock_sha256':overlay['files']['package-lock.json'],
        'dependency_overlay_sha256':sha(overlay_path),
    }
    if any(manifest.get(k) != v for k,v in expected.items()):
        raise RuntimeError('资产清单不是当前原始来源锁+实际构建锁+覆盖；拒绝旧/错清单')
    return expected


def interrupted(_signum, _frame):
    raise KeyboardInterrupt('停止本轮HTTP验收并清理专属Hub')


def main():
    signal.signal(signal.SIGTERM, interrupted)
    if not sys.flags.isolated:
        raise RuntimeError('必须使用 python3 -I')
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', required=True)
    parser.add_argument('--ui-dir', required=True)
    parser.add_argument('--evidence', required=True, help='仓库var下尚不存在的证据目录')
    args = parser.parse_args()
    binary = Path(args.binary).resolve(strict=True)
    public = Path(args.ui_dir).resolve(strict=True)
    evidence = Path(args.evidence).absolute()
    if not evidence.is_relative_to(REPO / 'var') or evidence.parent.resolve(strict=True) != evidence.parent or evidence.is_relative_to(public) or public.is_relative_to(evidence):
        raise RuntimeError('证据目录必须在本仓库var且与公开根分离')
    manifest = json.loads((public / '.wscyber-ui.json').read_text())
    provenance = verify_provenance(manifest)
    # 先拿同一重型锁再创建key/runtime；锁忙时不留下私有资源。
    heavy = (REPO/'var/heavy.lock').open('r+')
    fcntl.flock(heavy, fcntl.LOCK_EX | fcntl.LOCK_NB)
    os.umask(0o077)
    evidence.mkdir(mode=0o700)
    runtime = Path(tempfile.mkdtemp(prefix='private-runtime-', dir=evidence))
    for name in ('home', 'tmp'):
        (runtime / name).mkdir(mode=0o700)
    key = secrets.token_urlsafe(32)
    (runtime / 'test.key').write_text(key + '\n')
    (runtime / 'test.key').chmod(0o600)
    state = runtime / 'state'
    env = {'PATH':'/usr/bin:/bin', 'HOME':str(runtime/'home'), 'TMPDIR':str(runtime/'tmp'),
           'LANG':'C.UTF-8', 'TERM':'dumb', 'GOMAXPROCS':'2', 'GOGC':'30', 'GOMEMLIMIT':'384MiB'}
    checks, proc = [], None
    result = {'success':False, 'browser_verified':False, 'fixture_node_enabled':False, 'provenance':provenance,
              'binary_sha256':sha(binary), 'public_manifest_sha256':sha(public/'.wscyber-ui.json'), 'checks':checks}
    with heavy, (evidence/'hub.stdout.log').open('xb') as stdout, (evidence/'hub.stderr.log').open('xb') as stderr:
        try:
            proc = subprocess.Popen([str(binary), '--local-hub', '--listen', '127.0.0.1:0',
                                     '--access-key-file', str(runtime/'test.key'), '--state-dir', str(state),
                                     '--ui-dir', str(public)], cwd=REPO, env=env, stdout=subprocess.PIPE, stderr=stderr)
            result['pid'] = proc.pid
            result['pid_start_ticks'] = Path('/proc', str(proc.pid), 'stat').read_text().rsplit(')',1)[1].split()[19]
            with selectors.DefaultSelector() as selector:
                selector.register(proc.stdout, selectors.EVENT_READ)
                if not selector.select(timeout=15):
                    raise RuntimeError('Hub启动未及时返回ready')
                line = proc.stdout.readline()
            stdout.write(line)
            ready = json.loads(line)
            if ready.get('event') != 'ready' or ready.get('frontend') is not True or ready.get('fixture_node_configured') is not False:
                raise RuntimeError('Hub未明确挂载真实静态UI或意外配置执行节点')
            address = ready['address']
            host, port = address.rsplit(':',1)
            if not ipaddress.ip_address(host).is_loopback or not 0 < int(port) < 65536:
                raise RuntimeError('Hub地址不是字面loopback')
            result['address'] = address
            class NoRedirect(urllib.request.HTTPRedirectHandler):
                def redirect_request(self, *a, **kw):
                    raise RuntimeError('HTTP验收不跟随重定向')
            opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())

            def request(path, want, token='', method='GET', expected_hash=None, media=None):
                headers = {}
                if token:
                    headers['Authorization'] = 'Bearer ' + token
                data = None
                if method == 'POST':
                    data = b'{}'
                    headers.update({'Content-Type':'application/json', 'Connect-Protocol-Version':'1'})
                req = urllib.request.Request('http://' + address + path, headers=headers, method=method, data=data)
                try:
                    response = opener.open(req, timeout=5)
                except urllib.error.HTTPError as exc:
                    response = exc
                with response:
                    body = response.read((128<<20)+1)
                    content_type = response.headers.get('Content-Type','')
                    if len(body) > 128<<20 or response.status != want:
                        raise RuntimeError('HTTP状态/大小不符: ' + path)
                    if media and media not in content_type:
                        raise RuntimeError('HTTP资源类型不符: ' + path)
                    if key.encode() in body:
                        raise RuntimeError('响应泄漏测试key')
                    got = hashlib.sha256(body).hexdigest()
                    if expected_hash and got != expected_hash:
                        raise RuntimeError('HTTP资源字节与构建清单不符: ' + path)
                    if want != 200 and ('text/html' in content_type or got == manifest['files']['index.html']):
                        raise RuntimeError('未实现API/缺失资源错误返回SPA')
                    checks.append({'method':method,'path':path,'status':response.status,'content_type':content_type,
                                   'bytes':len(body),'sha256':got,'cache_control':response.headers.get('Cache-Control','')})
                    return body

            request('/', 200, expected_hash=manifest['files']['index.html'], media='text/html')
            request('/sessions/h3a-read-only', 200, expected_hash=manifest['files']['index.html'], media='text/html')
            for name, expected in manifest['files'].items():
                if name == 'index.html':
                    continue
                # 限定本机asset路径，不接受清单URL/任意主机。
                if not name.startswith('assets/') or any(p in ('','..','.') for p in name.split('/')):
                    raise RuntimeError('清单含非asset路径')
                media = 'javascript' if name.endswith('.js') else 'text/css' if name.endswith('.css') else 'application/wasm' if name.endswith('.wasm') else None
                request('/'+urllib.parse.quote(name,safe='/'), 200, expected_hash=expected, media=media)
                if 'immutable' not in checks[-1]['cache_control']:
                    raise RuntimeError('原生asset缓存头缺失')
            request('/api/manifest', 401, media='application/json')
            request('/api/manifest', 401, token='wrong', media='application/json')
            native_manifest = json.loads(request('/api/manifest', 200, token=key, media='application/json'))
            if native_manifest['product'] != 'wscyber-local-hub':
                raise RuntimeError('不是原生Hub manifest')
            request('/cyber.rpc.chat.SessionService/ListSessions', 401, method='POST', media='application/json')
            request('/cyber.rpc.chat.SessionService/ListSessions', 200, token=key, method='POST', media='application/json')
            for path in ('/cyber.rpc.scan.ScanService/ListScans','/cyber.rpc.missing.Service/Missing','/ioa/missing','/assets/missing.js','/.wscyber-ui.json','/test.key','/state/hub.sqlite'):
                request(path,404,token=key)
            result['success'] = True
        except Exception as exc:
            result['error'] = str(exc).replace(key, '[REDACTED]')
        finally:
            if proc is not None:
                if proc.poll() is None:
                    proc.send_signal(signal.SIGTERM)
                try:
                    rest, _ = proc.communicate(timeout=15)
                except subprocess.TimeoutExpired:
                    proc.kill()
                    rest, _ = proc.communicate(timeout=5)
                    result['forced_kill'] = True
                    result['success'] = False
                stdout.write(rest or b'')
                result['exit_code'] = proc.returncode
                result['success'] = result['success'] and proc.returncode == 0
            stdout.flush()
            stderr.flush()
            # 只删除本脚本创建、已经确认不再运行的私有目录。
            if proc is None or proc.poll() is not None:
                shutil.rmtree(runtime)
            result['runtime_removed'] = not runtime.exists()
            leaked = any(key.encode() in (evidence/name).read_bytes() for name in ('hub.stdout.log','hub.stderr.log'))
            result['key_absent_from_logs'] = not leaked
            result['success'] = result['success'] and not leaked and result['runtime_removed']
            (evidence/'result.json').write_text(json.dumps(result,indent=2,ensure_ascii=False)+'\n')
    print('HTTP check', 'PASS' if result['success'] else 'FAIL', evidence/'result.json')
    return 0 if result['success'] else 1


if __name__ == '__main__':
    sys.exit(main())

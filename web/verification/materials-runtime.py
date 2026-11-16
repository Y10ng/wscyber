#!/usr/bin/env python3
"""C1 React真实验收：本人新tmpfs PG/正式二进制/loopback固定返回，不读取业务配置。"""
import argparse, base64, hashlib, http.server, json, os, pathlib, secrets, signal, socket, subprocess, threading, time, urllib.request, urllib.error
ROOT = pathlib.Path('/root/桌面/wscyber')
p = argparse.ArgumentParser(); p.add_argument('out', type=pathlib.Path); p.add_argument('binary', type=pathlib.Path); p.add_argument('snapshot', type=pathlib.Path); p.add_argument('--browser', choices=['materials-browser.mjs', 'materials-edge-browser.mjs'], default='materials-browser.mjs'); a = p.parse_args()
out = a.out.resolve(); binary = a.binary.resolve(); snapshot = a.snapshot.resolve()
assert out.is_relative_to(ROOT/'var/research-main._81s5um0') and out.name.startswith('frontend-') and not out.exists()
assert binary.is_relative_to(ROOT/'var') and snapshot.is_relative_to(ROOT/'var')
os.umask(0o077); out.mkdir()
owner = {'scope':'C1本人新隔离tmpfs PG、正式Go/React、loopback结束Decision；不触碰旧预览', 'label':'c1-ui-'+secrets.token_hex(6)}
cid = None; app = None; provider = None
class Provider(http.server.BaseHTTPRequestHandler):
    inputs = []
    def log_message(self, *args): pass
    def do_POST(self):
        data = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
        Provider.inputs.append(data)
        text = json.dumps({'kind':'conclude','summary':'C1本机fixture完成，不访问目标'}, ensure_ascii=False)
        body = json.dumps({'id':'fixture','object':'chat.completion','model':data['model'],'created':0,'choices':[{'index':0,'finish_reason':'stop','message':{'role':'assistant','content':text}}],'usage':{'prompt_tokens':120,'completion_tokens':20,'total_tokens':140}}).encode()
        self.send_response(200); self.send_header('Content-Type','application/json'); self.send_header('Content-Length', str(len(body))); self.end_headers(); self.wfile.write(body)
def save(name, data): (out/name).write_text(json.dumps(data,ensure_ascii=False,indent=2))
def cmd(argv):
    r = subprocess.run(argv,capture_output=True,text=True)
    if r.returncode: raise RuntimeError('本机fixture命令失败；参数不回显')
    return r.stdout.strip()
def wait(fn):
    for _ in range(200):
        try:
            if fn(): return
        except (OSError, urllib.error.URLError): pass
        time.sleep(.1)
    raise RuntimeError('本机fixture readiness超时')
try:
    password=secrets.token_hex(24); token=secrets.token_hex(32)
    cid=cmd(['docker','run','--pull=never','-d','--name','wscyber-'+owner['label'],'--label','wscyber.c1-ui='+owner['label'],'--memory=512m','--cpus=1','--pids-limit=128','--tmpfs','/var/lib/postgresql/data:rw,noexec,nosuid,size=256m','-e','POSTGRES_PASSWORD='+password,'-e','POSTGRES_DB=c1_ui','-p','127.0.0.1::5432','postgres:16-alpine'])
    info=json.loads(cmd(['docker','inspect',cid]))[0]; assert not info['Mounts']; pgport=int(info['NetworkSettings']['Ports']['5432/tcp'][0]['HostPort'])
    owner['container']=cid; save('ownership.json',owner)
    wait(lambda: subprocess.run(['docker','exec',cid,'pg_isready','-h','127.0.0.1','-U','postgres','-d','c1_ui'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode == 0)
    provider=http.server.ThreadingHTTPServer(('127.0.0.1',0),Provider); threading.Thread(target=provider.serve_forever,daemon=True).start()
    with socket.socket() as s: s.bind(('127.0.0.1',0)); apiport=s.getsockname()[1]
    base=f'http://127.0.0.1:{apiport}'
    env={'PATH':os.environ['PATH'],'HOME':str(out),'GOMAXPROCS':'2','GOGC':'30','WSCYBER_LISTEN':f'127.0.0.1:{apiport}','WSCYBER_DATABASE_URL':f'postgres://postgres:{password}@127.0.0.1:{pgport}/c1_ui?sslmode=disable','WSCYBER_AUTH_TOKEN':token,'WSCYBER_MODEL_ENCRYPTION_KEY':base64.b64encode(secrets.token_bytes(32)).decode(),'WSCYBER_WEB_DIR':str(snapshot),'WSCYBER_MAX_PARALLEL':'1','WSCYBER_MAX_STEPS':'2','WSCYBER_SANDBOX_NETWORK':'none','WSCYBER_MODEL_BASE_URL':f'http://127.0.0.1:{provider.server_port}/v1','WSCYBER_MODEL_API_KEY':'local-fixture-only','WSCYBER_MODEL':'c1-ui-fixture','WSCYBER_MODEL_TIMEOUT':'10s','WSCYBER_ALLOW_LOOPBACK':'true'}
    with (out/'api.log').open('wb') as log: app=subprocess.Popen([str(binary)],cwd=ROOT,env=env,stdout=log,stderr=subprocess.STDOUT)
    owner.update(api_pid=app.pid,api_starttime=pathlib.Path(f'/proc/{app.pid}/stat').read_text().split(')')[1].split()[19],binary=str(binary),binary_sha256=hashlib.sha256(binary.read_bytes()).hexdigest(),snapshot=str(snapshot),index_sha256=hashlib.sha256((snapshot/'index.html').read_bytes()).hexdigest(),ports={'api':apiport,'pg':pgport,'provider':provider.server_port}); save('ownership.json',owner)
    wait(lambda: app.poll() is None and urllib.request.urlopen(base+'/healthz',timeout=.5).status == 200)
    save('fixture-private.json',{'url':base,'token':token})
    result=subprocess.run(['node',str(ROOT/'web/verification'/a.browser),str(out)],cwd=ROOT,env={k:v for k,v in os.environ.items() if not k.startswith(('WSCYBER_','OPENAI_','ANTHROPIC_'))},timeout=480)
    save('provider-observations.json',{'calls':len(Provider.inputs),'selected_material_seen':any('C1_FIXED_BODY' in json.dumps(v,ensure_ascii=False) for v in Provider.inputs),'enabled_safety_seen':any('C1_SAFETY_ONLY' in json.dumps(v,ensure_ascii=False) for v in Provider.inputs)})
    if result.returncode: raise RuntimeError('浏览器验收失败，见browser-result.json')
    print('PASS C1真实React/API验收 '+str(out))
finally:
    if app and app.poll() is None:
        app.send_signal(signal.SIGTERM)
        try: app.wait(timeout=20)
        except subprocess.TimeoutExpired: app.kill(); app.wait()
    if provider: provider.shutdown(); provider.server_close()
    removed=False
    if cid:
        info=json.loads(cmd(['docker','inspect',cid]))[0]
        if info['Config']['Labels'].get('wscyber.c1-ui') == owner['label']:
            save('postgres-state.json',info['State']); cmd(['docker','rm','-f',cid]); removed=True
    save('cleanup.json',{'container':cid,'removed':removed,'api_exit':app.returncode if app else None,'api_pid_gone':not pathlib.Path('/proc/'+str(app.pid)).exists() if app else None,'old_previews_touched':False})

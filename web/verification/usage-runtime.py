#!/usr/bin/env python3
"""Owned B UI acceptance: fresh tmpfs PG + formal binary + loopback provider only."""
import argparse, base64, hashlib, http.server, importlib.util, json, os, pathlib, secrets, signal, socket, subprocess, threading, time, urllib.request, urllib.error
ROOT = pathlib.Path('/root/桌面/wscyber')
p = argparse.ArgumentParser(); p.add_argument('out', type=pathlib.Path); p.add_argument('snapshot', type=pathlib.Path); p.add_argument('--from-price', action='store_true'); p.add_argument('--charts-only', action='store_true'); a = p.parse_args()
out = a.out.resolve(); assert out.is_relative_to(ROOT/'var') and not out.exists(); out.mkdir(mode=0o700)
os.umask(0o077)
spec = importlib.util.spec_from_file_location('usage_fixture', ROOT/'var/full-system-usage.hKFpjTHZ/backend-fixture.py')
fixture = importlib.util.module_from_spec(spec); spec.loader.exec_module(fixture)
label = 'usage-ui-'+secrets.token_hex(6); cid = None; app = None; server = None
owner = {'label': label, 'scope': '新PG、正式Go/React产物、双协议loopback固定响应；非真实供应商或外部目标'}
def save(name, data):
    (out/name).write_text(json.dumps(data, ensure_ascii=False, indent=2)); (out/name).chmod(0o600)
def cmd(argv):
    r = subprocess.run(argv, capture_output=True, text=True)
    if r.returncode: raise RuntimeError('local fixture command failed; private arguments withheld')
    return r.stdout.strip()
def port():
    with socket.socket() as s: s.bind(('127.0.0.1', 0)); return s.getsockname()[1]
def wait(fn):
    for _ in range(200):
        try:
            value = fn()
            if value: return value
        except (OSError, urllib.error.URLError): pass
        time.sleep(.1)
    raise RuntimeError('isolated fixture readiness timed out')
try:
    password = secrets.token_hex(24); token = secrets.token_hex(32)
    cid = cmd(['docker','run','--pull=never','-d','--name','wscyber-'+label,'--label','wscyber.usage-ui='+label,'--memory=512m','--cpus=1','--pids-limit=128','--tmpfs','/var/lib/postgresql/data:rw,noexec,nosuid,size=256m','-e','POSTGRES_PASSWORD='+password,'-e','POSTGRES_DB=usage_ui','-p','127.0.0.1::5432','postgres:16-alpine'])
    info = json.loads(cmd(['docker','inspect',cid]))[0]; assert not info['Mounts']
    pgport = int(info['NetworkSettings']['Ports']['5432/tcp'][0]['HostPort'])
    owner['container'] = cid; save('ownership.json', owner)
    wait(lambda: subprocess.run(['docker','exec',cid,'pg_isready','-h','127.0.0.1','-U','postgres','-d','usage_ui'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0)
    server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), fixture.Provider)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    apiport = port(); base = f'http://127.0.0.1:{apiport}'
    binary = ROOT/'var/full-system-usage.hKFpjTHZ/wscyber-b-preview-02'
    env = {'PATH':os.environ['PATH'], 'HOME':str(out), 'GOMAXPROCS':'2','GOGC':'30','WSCYBER_LISTEN':f'127.0.0.1:{apiport}','WSCYBER_DATABASE_URL':f'postgres://postgres:{password}@127.0.0.1:{pgport}/usage_ui?sslmode=disable','WSCYBER_AUTH_TOKEN':token,'WSCYBER_MODEL_ENCRYPTION_KEY':base64.b64encode(secrets.token_bytes(32)).decode(),'WSCYBER_WEB_DIR':str(a.snapshot.resolve()),'WSCYBER_MAX_PARALLEL':'1','WSCYBER_MAX_STEPS':'3','WSCYBER_SANDBOX_NETWORK':'none'}
    with (out/'api.log').open('wb') as log: app = subprocess.Popen([str(binary)], cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT)
    owner.update(api_pid=app.pid, api_starttime=pathlib.Path(f'/proc/{app.pid}/stat').read_text().split(')')[1].split()[19], binary_sha256=hashlib.sha256(binary.read_bytes()).hexdigest(), ports={'api':apiport,'provider':server.server_port,'pg':pgport}); save('ownership.json',owner)
    def api(method, path, body=None, expected=200):
        headers={'Authorization':'Bearer '+token}
        if body is not None: headers['Content-Type']='application/json'
        if method=='POST' and path=='/api/explorations': headers['Idempotency-Key']=secrets.token_hex(16)
        req=urllib.request.Request(base+path, headers=headers, data=json.dumps(body).encode() if body is not None else None, method=method)
        try:
            with urllib.request.urlopen(req,timeout=10) as r: status=r.status; value=json.load(r)
        except urllib.error.HTTPError as e: status=e.code; value=json.load(e)
        assert status==expected, (path,status,expected)
        return value
    wait(lambda: app.poll() is None and api('GET','/healthz').get('status')=='ok')
    configs=[]; missions=[]
    terms={'currency':'USD','input_per_million':'2','output_per_million':'8','cache_read_per_million':'0.5','cache_write_5m_per_million':'2.5','cache_write_1h_per_million':'4'}
    for index, protocol in enumerate(['openai_chat','anthropic_messages']):
        ident=secrets.token_hex(16); model='ui-fixture-'+str(index)
        config=api('PATCH','/api/model-configs/'+ident, {'name':'UI隔离 '+protocol,'protocol':protocol,'base_url':f'http://127.0.0.1:{server.server_port}'+('/v1' if index==0 else ''),'model':model,'auth':'bearer','api_key':'fixture-provider-key','timeout_seconds':10,'max_output_tokens':128,'enabled':True,'expected_revision':0,'headers':[],'parameters':[]},201)
        configs.append(config)
        api('PUT','/api/model-prices/'+secrets.token_hex(16), {'expected_revision':0,'source':'managed','model_config_id':ident,'protocol':protocol,'model':model,'enabled':True,**terms},201)
        started=api('POST','/api/explorations',{'description':'B_UI_INPUT_SENTINEL','goal':'仅结束研究方向，不访问目标','allowed_origins':['http://127.0.0.1:9'],'model_config_id':ident,'record_model_content':index==1},202)
        mid=started['mission']['id']; missions.append(mid)
        wait(lambda: (lambda rows: rows and rows[0]['state']=='completed')(api('GET','/api/model-calls?mission_id='+mid)['items']))
        testid=secrets.token_hex(16)
        api('POST',f'/api/model-configs/{ident}/tests/{testid}',{'config_revision':config['revision'],'confirm_billable':True},202)
        wait(lambda: api('GET',f'/api/model-configs/{ident}/tests/{testid}')['status']=='completed')
    settings=api('GET','/api/model-settings'); api('PUT','/api/model-settings',{'expected_revision':settings['revision'],'default_config_id':configs[0]['id']})
    summary=api('GET','/api/model-usage')['summary']; assert (summary['calls'],summary['input_tokens'],summary['output_tokens'],summary['total_tokens'])==('4','560','100','660')
    save('fixture-private.json',{'url':base,'token':token,'configs':configs,'missions':missions,'provider':f'http://127.0.0.1:{server.server_port}'})
    save('api-seed.json',{'summary':summary,'calls':api('GET','/api/model-calls')['items']})
    driver = ROOT/'web/verification'/('usage-chart-browser.mjs' if a.charts_only else 'usage-browser.mjs')
    r=subprocess.run(['node',str(driver),str(out)]+(['--from-price'] if a.from_price else []),cwd=ROOT,env={k:v for k,v in os.environ.items() if not k.startswith(('WSCYBER_','OPENAI_','ANTHROPIC_'))},timeout=420)
    if r.returncode: raise RuntimeError('headless UI acceptance failed; see browser-result.json')
    print('PASS isolated B UI runtime; artifacts '+str(out))
finally:
    if app and app.poll() is None:
        app.send_signal(signal.SIGTERM)
        try: app.wait(timeout=20)
        except subprocess.TimeoutExpired: app.kill(); app.wait()
    if server: server.shutdown(); server.server_close()
    removed=False
    if cid:
        info=json.loads(cmd(['docker','inspect',cid]))[0]
        if info['Config']['Labels'].get('wscyber.usage-ui')==label:
            save('postgres-state.json',info['State'])
            logs=subprocess.run(['docker','logs',cid],capture_output=True)
            (out/'postgres.log').write_bytes(logs.stdout+logs.stderr)
            cmd(['docker','rm','-f',cid]); removed=True
    save('cleanup.json',{'container':cid,'removed':removed,'api_exit':app.returncode if app else None,'other_resources_touched':False})

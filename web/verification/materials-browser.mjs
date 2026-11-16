import { chromium } from '/root/桌面/wscyber/var/react-acceptance/node_modules/playwright-core/index.mjs'
import fs from 'node:fs/promises'
import assert from 'node:assert/strict'
import { randomBytes } from 'node:crypto'
const out = process.argv[2], cfg = JSON.parse(await fs.readFile(out + '/fixture-private.json', 'utf8'))
assert.ok(out.startsWith('/root/桌面/wscyber/var/research-main._81s5um0/frontend-'))
const browser = await chromium.launch({ executablePath:'/usr/bin/chromium', headless:false, args:['--no-sandbox','--disable-dev-shm-usage','--disable-background-networking','--start-maximized'] })
const ctx = await browser.newContext({ viewport:null, serviceWorkers:'block' })
const page = await ctx.newPage(); page.setDefaultTimeout(12000)
const checks=[], screenshots=[], errors=[], external=[]; let phase='login', faults=false
const counts={libraryPut:0,libraryDelete:0,libraryBody:0,missionBody:0,create:0}; const createBodies=[]
await ctx.route('**/*', route => { if (new URL(route.request().url()).origin === cfg.url) return route.continue(); external.push(route.request().url()); return route.abort() })
page.on('pageerror', error => errors.push(error.message))
page.on('request', req => { const url=new URL(req.url()); if (/\/api\/library\/items\/[^/]+$/.test(url.pathname)) { if(req.method()==='PUT') counts.libraryPut++; if(req.method()==='DELETE') counts.libraryDelete++; if(req.method()==='GET') counts.libraryBody++ } if (/\/api\/missions\/[^/]+\/materials\//.test(url.pathname)) counts.missionBody++; if(url.pathname==='/api/explorations' && req.method()==='POST') { counts.create++; createBodies.push({key:req.headers()['idempotency-key'],body:req.postDataJSON()}) } })
page.on('console', msg => { if(msg.type()==='error' && !faults) errors.push(msg.text().slice(0,180)) })
const save=(name,value)=>fs.writeFile(out+'/'+name,JSON.stringify(value,null,2),{mode:0o600})
const shot=async name=>{await page.screenshot({path:out+'/'+name,animations:'disabled'});screenshots.push(name)}
const visible=async text=>page.waitForFunction(text=>document.body.innerText.includes(text),text)
const accessibleName=name=>new RegExp('(?:^|\\s)'+[...name].join('\\s*')+'$')
const button=name=>page.getByRole('button',{name:accessibleName(name)})
const dialog=()=>page.getByRole('dialog')
async function login(){await page.locator('#auth-token-input').fill(cfg.token);await button('进入工作台').click()}
async function api(method,path,data,expected=200){const r=await ctx.request.fetch(cfg.url+'/api'+path,{method,headers:{Authorization:'Bearer '+cfg.token,...(['PUT','DELETE','POST'].includes(method)?{'Idempotency-Key':randomBytes(16).toString('hex')}:{})},...(data===undefined?{}:{data})});assert.equal(r.status(),expected,path);return r.json()}
async function allFiles(){return (await api('GET','/library/items?all=true&kind=file&limit=100')).items}
async function editAPI(id,content){const old=await api('GET','/library/items/'+id);return api('PUT','/library/items/'+id,{expected_token:old.token,parent_id:old.parent_id,kind:old.kind,name:old.name,description:old.description,content})}
async function newExploration(){await button('新建探索').click();await page.locator('#exploration-description').fill('C1本机资料测试');await page.locator('#exploration-goal').fill('结束研究，不访问外部目标');await page.locator('#exploration-origins').fill('http://127.0.0.1:9');if(await button('高级设置').getAttribute('aria-expanded')==='false') await button('高级设置').click();await page.getByRole('region',{name:'任务资料选择'}).waitFor()}
async function chooseFile(name){await page.getByRole('checkbox',{name:'选用 '+name,exact:true}).check()}
async function safety(){await page.locator('#exploration-safety-enabled').click();await page.locator('#exploration-safety').fill('C1_SAFETY_ONLY：仅对授权本机资料进行研究');await page.locator('#exploration-recording').click()}
async function start(){await button('开始探索').click();await page.waitForURL(/\/projects\/[0-9a-f-]+\/threads\//);return new URL(page.url()).pathname.split('/')[2]}
async function openLibrary(){await button('资料库').click();await button('新建资料').waitFor()}
async function closeModal(){await dialog().locator('.ant-modal-footer').getByRole('button',{name:accessibleName('关闭')}).click();await dialog().waitFor({state:'hidden'})}
async function assertFits(){const size=await page.evaluate(()=>({w:innerWidth,h:innerHeight,scroll:document.documentElement.scrollWidth}));assert.ok(size.scroll<=size.w+1,JSON.stringify(size));const boxes=await page.locator('.ant-modal[role="dialog"],.ant-modal-body,.ant-modal-footer').evaluateAll(nodes=>nodes.map(n=>{const r=n.getBoundingClientRect();return{x:r.x,y:r.y,right:r.right,bottom:r.bottom}}));for(const b of boxes)assert.ok(b.x>=-1&&b.y>=-1&&b.right<=size.w+1&&b.bottom<=size.h+1,JSON.stringify({size,b}))}
try {
  await page.goto(cfg.url+'/library');await login();await button('新建资料').waitFor()
  const maximized=await page.evaluate(()=>({innerWidth,innerHeight,outerWidth,outerHeight,screenWidth:screen.width,screenHeight:screen.height}));await save('maximized-window.json',maximized)
  phase='folder and bounded import'
  await button('新建文件夹').click();await dialog().getByLabel('名称',{exact:true}).fill('授权方法');await dialog().getByRole('button',{name:accessibleName('保存')}).click();await dialog().waitFor({state:'hidden'})
  const folder=(await api('GET','/library/folders')).items[0]
  await page.locator('button.material-row').filter({hasText:'授权方法'}).click();await button('新建资料').click()
  const raw='﻿# C1_FIXED_BODY\r\n<script>只是资料，不执行</script>\r\n![不加载外链](https://invalid.example/pixel)\r\n'
  await dialog().getByLabel('导入本地文本资料',{exact:true}).setInputFiles({name:'方法.md',mimeType:'text/markdown',buffer:Buffer.from(raw)})
  await page.waitForFunction(raw=>document.querySelector('.material-editor textarea:last-child')?.value===raw,raw.replace(/\r\n/g,'\n'))
  await dialog().getByLabel('说明',{exact:true}).fill('明确授权内的业务方法');await dialog().getByRole('button',{name:accessibleName('保存')}).click();await dialog().waitFor({state:'hidden'})
  let source=(await allFiles())[0];assert.equal(source.parent_id,folder.id)
  assert.equal(counts.libraryBody,0,'列表/保存不得预读正文')
  await page.locator('button.material-row').filter({hasText:'方法.md'}).click();await page.locator('.material-text').waitFor();assert.equal(await page.locator('.material-text').textContent(),raw);assert.equal(await page.locator('.material-detail img,.material-detail script').count(),0)
  await shot('library-maximized-light.png');await button('切换到暗色主题').click();await shot('library-maximized-dark.png');await button('切换到浅色主题').click()
  checks.push('真实文件夹创建、UTF8 BOM/CRLF单文件导入、无预读正文、纯文本无外链执行；真实最大化浅暗')

  phase='draft, focus, local validation'
  await button('编辑资料').click();const editor=dialog();await editor.getByLabel('说明',{exact:true}).fill('保留的编辑草稿')
  const tabbables=await editor.locator('button,input,textarea,[tabindex]').evaluateAll(nodes=>nodes.filter(n=>n.tabIndex>=0&&!n.disabled&&n.checkVisibility({checkVisibilityCSS:true})).map(n=>n.id||n.textContent));assert.ok(tabbables.length>3)
  await editor.getByRole('button',{name:accessibleName('保存')}).focus();await page.keyboard.press('Tab');assert.ok(await page.evaluate(()=>Boolean(document.activeElement?.closest('.ant-modal'))))
  await page.keyboard.press('Escape');await dialog().waitFor({state:'hidden'});await button('项目列表').click();await openLibrary();await button('继续编辑草稿').click();assert.equal(await dialog().getByLabel('说明',{exact:true}).inputValue(),'保留的编辑草稿')
  await dialog().getByRole('button',{name:accessibleName('保存')}).click();await dialog().waitFor({state:'hidden'});source=(await allFiles())[0]
  checks.push('编辑草稿离页/取消后保留、正向Tab不逃逸、Escape关闭后可继续保存')

  phase='selection safety and fixed configuration'
  await newExploration();const readsBefore=counts.libraryBody;await chooseFile('方法.md');assert.equal(counts.libraryBody,readsBefore)
  await safety();const mission=await start();const submission=createBodies.at(-1);assert.deepEqual(submission.body.materials,[{id:source.id,token:source.token}]);assert.equal(submission.body.record_model_content,true);assert.ok(submission.body.safety_prompt_enabled)
  await button('研究配置').click();await dialog().getByRole('button',{name:'查看 方法.md',exact:true}).waitFor();assert.equal(counts.missionBody,0);await dialog().getByRole('button',{name:'查看 方法.md',exact:true}).click();await dialog().locator('.material-text').first().waitFor();assert.equal(await dialog().locator('.material-text').first().textContent(),raw);await shot('mission-fixed-config.png');await closeModal()
  await editAPI(source.id,'库已修改，不能覆盖旧任务')
  const fixed=await api('GET',`/missions/${mission}/materials/${source.id}`);assert.equal(fixed.content,raw)
  checks.push('正式创建选用资料/原文/安全一起固定，配置与正文按需读取，库修改不污染旧任务')

  phase='material stale authoritative 409'
  await newExploration();await chooseFile('方法.md');await editAPI(source.id,'再次变化')
  faults=true;await button('开始探索').click();await visible('选用资料已变化，请刷新选择后重新提交。');assert.equal(await page.locator('#exploration-description').isEnabled(),true);faults=false
  await button('刷新选择').click();await page.waitForFunction(()=>!document.querySelector('.material-selection .ant-btn-loading'))
  const refreshed=await start();assert.notEqual(refreshed,mission)
  checks.push('真实material_selection_stale 409回到草稿，显式刷新元数据后可重新选用提交')

  phase='library lost receipt and historical replay'
  await openLibrary();await button('新建资料').click();await dialog().getByLabel('名称',{exact:true}).fill('待核对.txt');await dialog().getByLabel('正文',{exact:true}).fill('原请求内容')
  const libraryPattern='**/api/library/items/*';let lostKey=''
  await page.route(libraryPattern,async route=>{if(route.request().method()!=='PUT')return route.continue();lostKey=route.request().headers()['idempotency-key'];await route.fetch();await route.abort('failed')},{times:1})
  const putBefore=counts.libraryPut;faults=true;await dialog().getByRole('button',{name:accessibleName('保存')}).click();await visible('写入结果待核对');await dialog().getByRole('button',{name:'暂时关闭',exact:true}).click();await button('项目列表').click();await openLibrary()
  await page.route('**/api/library/operations/*',route=>route.fulfill({status:404,contentType:'application/json',body:'{"error":"fixture尚未查到"}'}),{times:1});await button('核对写入回执').click();await visible('不代表原请求未发送');assert.equal(counts.libraryPut,putBefore+1)
  const saved=(await allFiles()).find(v=>v.name==='待核对.txt');await editAPI(saved.id,'回执之后的实时内容')
  await button('核对写入回执').click();await visible('写入回执已确认');assert.equal(counts.libraryPut,putBefore+1);assert.ok(lostKey);faults=false
  await page.locator('button.material-row').filter({hasText:'待核对.txt'}).click();await visible('回执之后的实时内容')
  checks.push('真实提交后断连保留同键，离页不丢身份；operations404只核对不重发，历史回执不冒充实时正文')

  phase='raw switch materials safety x late-create and 401'
  await newExploration();await chooseFile('方法.md');await safety()
  let release;let sentResolve;const sent=new Promise(resolve=>{sentResolve=resolve});const held=new Promise(resolve=>{release=resolve})
  await page.route('**/api/explorations',async route=>{const response=await route.fetch();sentResolve();await held;try{await route.fulfill({response})}catch{}},{times:1})
  const postBefore=counts.create;await button('开始探索').click();await sent;const frozen=createBodies.at(-1)
  await button('暂时关闭').click();await page.route('**/api/library/folders',route=>route.fulfill({status:401,contentType:'application/json',body:'{"error":"fixture认证失效"}'}),{times:1})
  faults=true;await button('项目列表').click();await button('资料库').click();await page.locator('#auth-token-input').waitFor();assert.ok(!(await page.locator('body').innerText()).includes('C1_SAFETY_ONLY'));release();await login();await button('核对探索提交').click()
  assert.equal(await page.locator('#exploration-recording').getAttribute('aria-checked'),'true');assert.equal(await page.locator('#exploration-safety').inputValue(),'C1_SAFETY_ONLY：仅对授权本机资料进行研究');assert.equal(await page.locator('#exploration-description').isDisabled(),true)
  await button('核对提交状态').click();await page.waitForURL(/\/projects\/[0-9a-f-]+\/threads\//);assert.equal(counts.create,postBefore+1);assert.equal(createBodies.at(-1).key,frozen.key);faults=false
  checks.push('真实创建已提交但响应迟到×401：立即遮蔽，重认证保留原文开关/资料/安全及原键，只GET核对无第二次POST')

  phase='library draft x unauthorized'
  await openLibrary();await button('新建资料').click();await dialog().getByLabel('名称',{exact:true}).fill('401草稿.txt');await dialog().getByLabel('正文',{exact:true}).fill('C1_PRIVATE_DRAFT');await dialog().getByRole('button',{name:'暂时关闭',exact:true}).click()
  await page.route('**/api/library/folders',route=>route.fulfill({status:401,contentType:'application/json',body:'{}'}),{times:1});faults=true;await button('刷新').click();await page.locator('#auth-token-input').waitFor();assert.ok(!(await page.locator('body').innerText()).includes('C1_PRIVATE_DRAFT'));await login();await button('继续编辑草稿').click();assert.equal(await dialog().getByLabel('正文',{exact:true}).inputValue(),'C1_PRIVATE_DRAFT');await dialog().getByRole('button',{name:'丢弃草稿',exact:true}).click();faults=false
  checks.push('资料编辑草稿401即时遮蔽，重认证恢复内存草稿，显式丢弃才清空')

  phase='responsive library and full advanced form'
  for(const [width,height] of [[1440,900],[1920,1080],[390,844]]){
    await page.setViewportSize({width,height});await openLibrary()
    for(const mode of ['light','dark']){
      if(mode==='dark')await button('切换到暗色主题').click();await assertFits();await shot(`library-${width}-${mode}.png`)
      await newExploration();await chooseFile('方法.md');await safety();await assertFits();await shot(`create-${width}-${mode}.png`)
      await dialog().getByRole('button',{name:accessibleName('清空草稿')}).click();await button('暂时关闭').click()
    }
    await button('切换到浅色主题').click()
  }
  checks.push('1440/1920/390浅暗资料库和完整高级表单截图，Modal及整页无横溢出')

  phase='nonempty folder, deletion and fixed mission retention'
  await page.setViewportSize({width:1440,height:900});await openLibrary();await page.locator('button.material-row').filter({hasText:'授权方法'}).click();await button('当前文件夹').click();await button('删除文件夹').click();faults=true;await dialog().getByRole('button',{name:'确认删除',exact:true}).click();await visible('目录非空');faults=false
  await page.locator('button.material-row').filter({hasText:'方法.md'}).click();await button('删除资料').click();await visible('已固定到旧任务的内容仍会保留');await dialog().getByRole('button',{name:'确认删除',exact:true}).click();await visible('写入回执已确认')
  await api('GET','/library/items/'+source.id,undefined,404);assert.equal((await api('GET',`/missions/${mission}/materials/${source.id}`)).content,raw)
  await button('当前文件夹').click();await button('删除文件夹').click();await dialog().getByRole('button',{name:'确认删除',exact:true}).click();await visible('写入回执已确认')
  checks.push('真实非空文件夹409；资料删除不移除任务固定内容；清空后目录可删除')
  assert.equal(external.length,0);assert.equal(errors.length,0,errors.join('\n'))
  await save('browser-result.json',{passed:true,checks,screenshots,counts,errors,external})
} catch(error){await shot('failure.png').catch(()=>{});await save('browser-result.json',{passed:false,phase,message:error.message,checks,screenshots,counts,errors,external});console.error('C1 browser failed at '+phase+': '+error.message);process.exitCode=1}
finally{await browser.close()}

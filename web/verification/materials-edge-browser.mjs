// 仅基础文本/目录管理边界；不扩展手工选材或自动skill契约。
import { chromium } from '/root/桌面/wscyber/var/react-acceptance/node_modules/playwright-core/index.mjs'
import fs from 'node:fs/promises'
import assert from 'node:assert/strict'
import { randomBytes } from 'node:crypto'
const out=process.argv[2], cfg=JSON.parse(await fs.readFile(out+'/fixture-private.json','utf8'))
assert.ok(out.startsWith('/root/桌面/wscyber/var/research-main._81s5um0/frontend-'))
const browser=await chromium.launch({executablePath:'/usr/bin/chromium',headless:false,args:['--no-sandbox','--disable-dev-shm-usage','--disable-background-networking','--start-maximized']})
const ctx=await browser.newContext({viewport:null,serviceWorkers:'block'}), page=await ctx.newPage();page.setDefaultTimeout(12000)
const checks=[],screenshots=[],external=[],errors=[],puts=[];let phase='login',faults=false
await ctx.route('**/*',route=>{if(new URL(route.request().url()).origin===cfg.url)return route.continue();external.push(route.request().url());return route.abort()})
page.on('pageerror',error=>errors.push(error.message))
page.on('console',msg=>{if(msg.type()==='error'&&!faults)errors.push(msg.text().slice(0,180))})
page.on('request',req=>{if(req.method()==='PUT'&&new URL(req.url()).pathname.startsWith('/api/library/items/'))puts.push({key:req.headers()['idempotency-key'],body:req.postDataJSON()})})
const name=s=>new RegExp('(?:^|\\s)'+[...s].join('\\s*')+'$'),button=s=>page.getByRole('button',{name:name(s)}),dialog=()=>page.getByRole('dialog')
const visible=s=>page.waitForFunction(s=>document.body.innerText.includes(s),s)
const save=(file,value)=>fs.writeFile(out+'/'+file,JSON.stringify(value,null,2),{mode:0o600})
const shot=async file=>{await page.screenshot({path:out+'/'+file,animations:'disabled'});screenshots.push(file)}
const library=async()=>{await button('资料库').click();await button('新建资料').waitFor()}
const login=async()=>{await page.locator('#auth-token-input').fill(cfg.token);await button('进入工作台').click();await button('资料库').waitFor()}
const saveEditor=async()=>{await dialog().getByRole('button',{name:name('保存')}).click();await dialog().waitFor({state:'hidden'})}
const row=s=>page.locator('button.material-row').filter({hasText:s})
async function api(method,path,data,expected=200){const r=await ctx.request.fetch(cfg.url+'/api'+path,{method,headers:{Authorization:'Bearer '+cfg.token,...(['PUT','DELETE'].includes(method)?{'Idempotency-Key':randomBytes(16).toString('hex')}:{})},...(data===undefined?{}:{data})});assert.equal(r.status(),expected,path);return r.json()}
async function newItem(kind,title,content='',parent_id=''){return api('PUT','/library/items/'+randomBytes(16).toString('hex'),{kind,name:title,content,parent_id,description:''})}
async function fits(){const dimensions=await page.evaluate(()=>({w:innerWidth,h:innerHeight,scroll:document.documentElement.scrollWidth}));assert.ok(dimensions.scroll<=dimensions.w+1,JSON.stringify(dimensions))}
try{
  await page.goto(cfg.url+'/library');await login()
  phase='bounded local import and failed reads'
  await button('新建资料').click();await dialog().getByLabel('名称',{exact:true}).fill('保留.txt');await dialog().getByLabel('正文',{exact:true}).fill('不得被失败导入覆盖')
  for(const [filename,buffer,error] of [['大.md',Buffer.alloc(32769,65),'文件须为 1–32,768 字节。'],['坏.txt',Buffer.from([255]),'文件不是有效 UTF-8 文本。'],['空.txt',Buffer.alloc(0),'文件须为 1–32,768 字节。'],['脚本.js',Buffer.from('text'),'请选择一个 .md 或 .txt 文件。'],['NUL.md',Buffer.from([65,0,66]),'正文须为非空 UTF-8 文本']]){
    await dialog().getByLabel('导入本地文本资料',{exact:true}).setInputFiles({name:filename,mimeType:'text/plain',buffer});await visible(error);assert.equal(await dialog().getByLabel('正文',{exact:true}).inputValue(),'不得被失败导入覆盖');assert.equal(puts.length,0)
  }
  await page.evaluate(()=>{window.originalFileRead=File.prototype.arrayBuffer;File.prototype.arrayBuffer=async()=>{throw new Error('本地读取失败fixture')}})
  await dialog().getByLabel('导入本地文本资料',{exact:true}).setInputFiles({name:'读取失败.txt',mimeType:'text/plain',buffer:Buffer.from('ignored')});await visible('本地读取失败fixture');assert.equal(await dialog().getByLabel('名称',{exact:true}).inputValue(),'保留.txt')
  await page.evaluate(()=>{File.prototype.arrayBuffer=window.originalFileRead})
  const longName='长名称'.repeat(18)+'.md',longBody='L'.repeat(32768)
  await dialog().getByLabel('导入本地文本资料',{exact:true}).setInputFiles({name:longName,mimeType:'text/markdown',buffer:Buffer.from(longBody)})
  await page.waitForFunction(()=>document.querySelector('.material-editor textarea:last-child')?.value.length===32768)
  await saveEditor();assert.equal(puts.length,1);const file=(await api('GET','/library/items?all=true&kind=file')).items[0]
  assert.equal((await api('GET','/library/items/'+file.id)).content,longBody)
  checks.push('超限/空/非法UTF8/NUL/扩展名/读取失败均保留草稿且零写请求，32768字节边界及长名称完整保存')

  phase='long text responsive and keyboard'
  await row(longName).click();await page.locator('.material-text').waitFor()
  for(const appearance of ['light','dark']){
    if(appearance==='dark')await button('切换到暗色主题').click()
    await page.setViewportSize({width:390,height:844});await fits();await shot('long-text-390-'+appearance+'.png')
  }
  await button('切换到浅色主题').click();await page.setViewportSize({width:1440,height:900});await button('编辑资料').click();await dialog().getByRole('button',{name:name('暂时关闭')}).click();await dialog().waitFor({state:'hidden'});await button('继续编辑草稿').click()
  await dialog().locator('button.ant-modal-close').focus();await page.keyboard.press('Shift+Tab');assert.ok(await page.evaluate(()=>Boolean(document.activeElement?.closest('.ant-modal'))));await page.keyboard.press('Tab');assert.ok(await page.evaluate(()=>Boolean(document.activeElement?.closest('.ant-modal'))))
  await page.keyboard.press('Escape');await dialog().waitFor({state:'hidden'});await page.waitForFunction(()=>document.activeElement?.textContent?.includes('继续编辑草稿'))
  checks.push('32768字节连续长正文/长名称390浅暗无横溢出，正反Tab圈定/Escape及返回触发按钮焦点')

  phase='moving a file through parent selector'
  const folder=(await newItem('folder','移动目的目录')).item
  await button('刷新').click();await button('继续编辑草稿').click();await dialog().getByLabel('所属目录',{exact:true}).click();await page.locator('.ant-select-item-option-content').filter({hasText:/^\/移动目的目录$/}).click();await saveEditor()
  assert.equal((await api('GET','/library/items/'+file.id)).parent_id,folder.id)
  await row('移动目的目录').click();await row(longName).waitFor();await row(longName).click();await button('编辑资料').click();await dialog().getByLabel('所属目录',{exact:true}).click();await page.locator('.ant-select-item-option-content').filter({hasText:/^\/$/}).click();await saveEditor();assert.equal((await api('GET','/library/items/'+file.id)).parent_id,'')
  checks.push('通过真实所属目录选择移动文本资料，正文不改变')

  phase='library pending write, 401, late response, receipt ownership'
  await button('项目列表').click();await library();await button('新建资料').click();await dialog().getByLabel('名称',{exact:true}).fill('迟到401.txt');await dialog().getByLabel('正文',{exact:true}).fill('C1_PRIVATE_PENDING')
  let release,submitted;const held=new Promise(resolve=>release=resolve),sent=new Promise(resolve=>submitted=resolve)
  await page.route('**/api/library/items/*',async route=>{if(route.request().method()!=='PUT')return route.continue();const response=await route.fetch();submitted();await held;try{await route.fulfill({response})}catch{}},{times:1})
  const before=puts.length;await dialog().getByRole('button',{name:name('保存')}).click();await sent;await dialog().getByRole('button',{name:name('暂时关闭')}).click();await button('项目列表').click()
  await page.route('**/api/library/folders',route=>route.fulfill({status:401,contentType:'application/json',body:'{}'}),{times:1});faults=true;await button('资料库').click();await page.locator('#auth-token-input').waitFor();assert.ok(!(await page.locator('body').innerText()).includes('C1_PRIVATE_PENDING'))
  release();await login();await button('核对写入回执').waitFor();await button('继续编辑草稿').click();assert.equal(await dialog().getByLabel('正文',{exact:true}).inputValue(),'C1_PRIVATE_PENDING');assert.equal(await dialog().getByLabel('正文',{exact:true}).isDisabled(),true);await dialog().getByRole('button',{name:name('暂时关闭')}).click()
  await page.route('**/api/library/operations/*',async route=>{const response=await route.fetch(),body=await response.json();body.item_id='0'.repeat(32);await route.fulfill({response,json:body})},{times:1});await button('核对写入回执').click();await visible('未能核对回执，已保留原请求身份。');assert.equal(puts.length,before+1)
  await button('核对写入回执').click();await visible('写入回执已确认');assert.equal(puts.length,before+1);faults=false
  checks.push('资料写入已提交/响应迟到×401遮蔽及重认证锁定草稿，错误归属回执不解锁，继续仅GET收敛')

  phase='stale body read after newer selection'
  const second=(await newItem('file','较新正文.txt','NEWER_CURRENT_BODY')).item;await button('刷新').click();await row('较新正文.txt').waitFor()
  let releaseRead,startedRead;const readHeld=new Promise(resolve=>releaseRead=resolve),readSent=new Promise(resolve=>startedRead=resolve)
  await page.route('**/api/library/items/'+file.id,async route=>{const response=await route.fetch();startedRead();await readHeld;try{await route.fulfill({response})}catch{}},{times:1})
  await row(longName).click();await readSent;await row('较新正文.txt').click();await visible('NEWER_CURRENT_BODY');releaseRead();await page.waitForLoadState('networkidle');assert.equal(await page.locator('.material-text').textContent(),'NEWER_CURRENT_BODY')
  await api('GET','/library/items/'+'f'.repeat(32),undefined,404)
  await api('DELETE','/library/items/'+second.id,{expected_token:second.token});await button('关闭查看').click();faults=true;await row('较新正文.txt').click();await page.locator('.ant-alert-error').waitFor();assert.equal(await page.locator('.material-text').count(),0);faults=false
  checks.push('较早正文迟到不覆盖较新选择，已删除/无归属资料404不显示旧正文')

  phase='explicit logout clears memory-only draft'
  await button('新建资料').click();await dialog().getByLabel('名称',{exact:true}).fill('退出草稿.txt');await dialog().getByLabel('正文',{exact:true}).fill('LOGOUT_DRAFT');await dialog().getByRole('button',{name:name('暂时关闭')}).click();await button('退出').click();await page.locator('#auth-token-input').waitFor();await login();await library();assert.equal(await button('继续编辑草稿').count(),0)
  assert.ok(await page.evaluate(()=>!JSON.stringify({...localStorage,...sessionStorage}).includes('LOGOUT_DRAFT')))
  checks.push('显式退出清空库内存草稿，不写localStorage/sessionStorage')

  phase='deleted current folder returns to available root'
  await row('移动目的目录').click();await button('当前文件夹').click();await button('删除文件夹').click();await dialog().getByRole('button',{name:name('确认删除')}).click();await visible('写入回执已确认')
  await page.waitForFunction(()=>![...document.querySelectorAll('button')].some(n=>n.textContent==='当前文件夹'))
  await button('新建资料').click();await dialog().getByLabel('名称',{exact:true}).fill('删除目录后.txt');await dialog().getByLabel('正文',{exact:true}).fill('root still usable');await saveEditor();const restored=(await api('GET','/library/items?all=true&kind=file')).items.find(v=>v.name==='删除目录后.txt');assert.equal(restored.parent_id,'')
  checks.push('删除当前空文件夹后回到真实根目录，新建不会引用已删parent')
  assert.equal(external.length,0);assert.equal(errors.length,0,errors.join('\n'))
  await save('browser-result.json',{passed:true,checks,screenshots,putCount:puts.length,errors,external})
}catch(error){await shot('failure.png').catch(()=>{});await save('browser-result.json',{passed:false,phase,message:error.message,checks,screenshots,putCount:puts.length,errors,external});console.error('C1 library edge failed at '+phase+': '+error.message);process.exitCode=1}
finally{await browser.close()}

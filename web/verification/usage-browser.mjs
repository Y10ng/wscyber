import { chromium } from '/root/桌面/wscyber/var/react-acceptance/node_modules/playwright-core/index.mjs'
import fs from 'node:fs/promises'
import assert from 'node:assert/strict'
import { randomBytes } from 'node:crypto'
const out = process.argv[2]
assert.ok(out.startsWith('/root/桌面/wscyber/var/'))
const cfg = JSON.parse(await fs.readFile(out + '/fixture-private.json', 'utf8'))
const seed = JSON.parse(await fs.readFile(out + '/api-seed.json', 'utf8'))
const raw = seed.calls.find(c => c.record_model_content)
const fromPrice = process.argv.includes('--from-price')
const browser = await chromium.launch({ executablePath: '/usr/bin/chromium', headless: true, args: ['--no-sandbox', '--disable-dev-shm-usage', '--disable-background-networking'] })
const ctx = await browser.newContext({ viewport: { width: 1440, height: 1000 }, serviceWorkers: 'block' })
const externalRequests = []
await ctx.route('**/*', r => {
  if (new URL(r.request().url()).origin === cfg.url) return r.continue()
  externalRequests.push(new URL(r.request().url()).origin); return r.abort()
})
const page = await ctx.newPage(); page.setDefaultTimeout(10000)
const checks = [], screenshots = [], pageErrors = [], consoleErrors = [], requestCounts = { raw: 0, pricePUT: 0, explorationPOST: 0 }
let phase = 'login', faultMode = false
page.on('pageerror', e => pageErrors.push(e.name))
page.on('console', m => { if (m.type() === 'error' && !faultMode) consoleErrors.push(m.text().slice(0, 120)) })
page.on('request', r => {
  if (/\/api\/model-calls\/[^/]+\/content(?:\?|$)/.test(r.url())) requestCounts.raw++
  if (/\/api\/model-prices\/[^/]+$/.test(r.url()) && r.method() === 'PUT') requestCounts.pricePUT++
  if (r.url().endsWith('/api/explorations') && r.method() === 'POST') requestCounts.explorationPOST++
})
const save = async (name, data) => fs.writeFile(out + '/' + name, JSON.stringify(data, null, 2), { mode: 0o600 })
async function api(method, path, data) {
  const r = await ctx.request.fetch(cfg.url + '/api' + path, { method, headers: { Authorization: 'Bearer ' + cfg.token }, ...(data ? { data } : {}) })
  assert.ok(r.ok(), `${method} ${path} ${r.status()}`); return r.json()
}
async function waitForAPI(fn) {
  for (let i = 0; i < 100; i++) { const v = await fn(); if (v) return v; await new Promise(r => setTimeout(r, 100)) }
  throw new Error('fixture API condition timed out')
}
async function login() { await page.locator('#auth-token-input').fill(cfg.token); await page.getByRole('button', { name: '进入工作台', exact: true }).click() }
async function enter(path) { await page.goto(cfg.url + path); await login() }
async function shot(name) { await page.screenshot({ animations: 'disabled', path: out + '/' + name }); screenshots.push(name) }
async function visibleText(text) { await page.waitForFunction(t => document.body.innerText.includes(t), text) }
async function noOverflow() { assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), 'whole document overflow') }
async function closeDrawer() { await page.locator('.ant-drawer-close').click(); await page.getByRole('dialog').waitFor({ state: 'hidden' }) }
async function assertModalFits() {
  const boxes = await page.locator('.ant-modal[role="dialog"], .ant-modal-body, .ant-modal-footer').evaluateAll(nodes => nodes.map(n => { const r = n.getBoundingClientRect(); return { x:r.x, y:r.y, right:r.right, bottom:r.bottom, h:r.height } }))
  assert.equal(boxes.length, 3)
  const viewport = page.viewportSize(); for (const b of boxes) assert.ok(b.h > 0 && b.x >= -1 && b.y >= -1 && b.right <= viewport.width + 1 && b.bottom <= viewport.height + 1, JSON.stringify(b))
}
try {
  await enter('/usage')
  await visibleText('0.001724')
  if (!fromPrice) {
  await page.getByRole('button', { name: /^查看调用 / }).first().waitFor()
  assert.equal(await page.getByRole('button', { name: /^查看调用 / }).count(), 4)
  assert.equal(requestCounts.raw, 0)
  checks.push('真实API四条调用，560/100/660及USD0.001724，列表未读取原文')

  phase = 'keyboard and chart reobserve'
  const plot = page.getByRole('group', { name: '每日已知 tokens', exact: true })
  await plot.focus(); await plot.press('End'); await page.locator('.daily-usage-tooltip').waitFor()
  assert.match(await page.locator('.daily-usage-tooltip').innerText(), /560/)
  await plot.press('Home'); await visibleText('0 次调用'); await plot.press('End')
  await plot.press('Escape'); assert.equal(await page.locator('.daily-usage-tooltip').count(), 0)
  await page.getByText('精确表格', { exact: true }).click()
  await page.getByRole('columnheader', { name: '已知输入', exact: true }).waitFor()
  await page.getByText('趋势图', { exact: true }).click()
  await page.setViewportSize({ width:1920, height:1080 })
  await page.waitForFunction(() => Number(document.querySelector('.daily-usage svg')?.getAttribute('width')) > 1400)
  await page.setViewportSize({ width:1440, height:1000 })
  await page.waitForFunction(() => Number(document.querySelector('.daily-usage svg')?.getAttribute('width')) < 1400)
  checks.push('图→表→图后ResizeObserver重新订阅，1920/1440重排；End/Home/Escape精确读数')

  phase = 'responsive screenshots'
  for (const [width,height] of [[1440,1000],[1920,1080],[390,844]]) {
    await page.setViewportSize({ width, height })
    for (const mode of ['light','dark']) {
      if (mode === 'dark') await page.getByRole('button', { name: '切换到暗色主题', exact:true }).click()
      await noOverflow(); await shot(`usage-${width}-${mode}.png`)
      if (width === 390) {
        await page.locator('.daily-usage').scrollIntoViewIfNeeded()
        await shot(`usage-chart-${width}-${mode}.png`)
        await page.locator('.ws-usage-filters').scrollIntoViewIfNeeded()
      }
      const font = await page.locator('.ant-table-cell').first().evaluate(n => getComputedStyle(n).fontSize)
      assert.equal(font, '14px')
    }
    await page.getByRole('button', { name: '切换到浅色主题', exact:true }).click()
  }
  await page.setViewportSize({ width:1440, height:1000 })
  checks.push('1440/1920/390浅暗真实数据截图，表格14px，无整页横溢出')

  phase = 'raw explicit lifecycle'
  await page.getByRole('button', { name: `查看调用 ${raw.id}`, exact:true }).click()
  await page.getByRole('button', { name:'查看模型输入与返回', exact:true }).waitFor()
  assert.equal(requestCounts.raw, 0)
  await page.getByRole('button', { name:'查看模型输入与返回', exact:true }).click()
  await page.locator('.evidence-preview').first().waitFor()
  assert.equal(requestCounts.raw, 1)
  assert.ok((await page.locator('.ant-drawer-body').innerText()).includes('B_UI_INPUT_SENTINEL'))
  assert.ok(!(await page.locator('.ant-drawer-body').innerText()).includes('fixture-provider-key'))
  await visibleText('已移除已知连接凭据（非未改原件）'); await shot('raw-explicit.png')
  await page.getByRole('button', { name:'收起模型输入与返回', exact:true }).click()
  assert.equal(await page.locator('.evidence-preview').count(), 0)
  await closeDrawer()
  const disabled = seed.calls.find(c => !c.record_model_content)
  await page.getByRole('button', { name:`查看调用 ${disabled.id}`, exact:true }).click()
  await visibleText('未启用记录'); assert.equal(await page.getByRole('button', { name:'查看模型输入与返回', exact:true }).count(), 0)
  await closeDrawer()
  checks.push('详情不预读；显式原文读取/收起卸载/凭据移除标签/未启用态')

  }
  phase = 'price actual save'
  await page.getByRole('tab', { name:'价格', exact:true }).click()
  await page.getByRole('button', { name:'编辑价格', exact:true }).first().waitFor()
  const chatRow = page.getByRole('row').filter({ hasText:'ui-fixture-0' })
  await chatRow.getByRole('button', { name:'编辑价格', exact:true }).click()
  await page.locator('#price-input_per_million').fill('3')
  await assertModalFits(); await shot('price-modal-1440.png')
  await page.setViewportSize({ width:390, height:844 }); await assertModalFits()
  await page.locator('#price-cache_write_1h_per_million').scrollIntoViewIfNeeded()
  await page.locator('#price-cache_write_1h_per_million').focus()
  const lastPriceBox = await page.locator('#price-cache_write_1h_per_million').boundingBox()
  assert.ok(lastPriceBox && lastPriceBox.y >= 0 && lastPriceBox.y + lastPriceBox.height <= 844)
  await page.locator('#price-cache_write_1h_per_million').press('Tab')
  assert.ok(await page.getByRole('button',{name:'放弃草稿',exact:true}).evaluate(n=>document.activeElement===n))
  await shot('price-modal-390.png')
  await page.setViewportSize({ width:1440, height:1000 })
  const priceBefore = requestCounts.pricePUT
  await page.getByRole('button', { name:'保存价格', exact:true }).click()
  await page.getByRole('dialog').waitFor({ state:'hidden' }); assert.equal(requestCounts.pricePUT, priceBefore + 1)
  const old = await api('GET','/model-calls/'+seed.calls.find(c=>c.model==='ui-fixture-0').id)
  assert.equal(old.cost.amount, '0.00034')
  checks.push('价格真实PUT一次，编辑模态1440/390在视口内，历史snapshot仍USD0.00034')

  phase = 'create recording draft and submit'
  await page.getByRole('button', { name:/新建探索$/ }).click()
  await page.getByRole('button', { name:'高级设置', exact:true }).click()
  assert.equal(await page.locator('#exploration-recording').getAttribute('aria-checked'), 'false')
  await page.locator('#exploration-recording').click()
  await page.locator('#exploration-description').fill('B_BROWSER_CREATED_INPUT')
  await page.locator('#exploration-goal').fill('仅结束，不访问目标')
  await page.locator('#exploration-origins').fill('http://127.0.0.1:9')
  await page.getByRole('button', { name:'暂时关闭', exact:true }).click()
  await page.getByRole('button', { name:/新建探索$/ }).click()
  assert.equal(await page.locator('#exploration-recording').getAttribute('aria-checked'), 'true')
  assert.equal(await page.locator('#exploration-description').inputValue(), 'B_BROWSER_CREATED_INPUT')
  await assertModalFits(); await shot('creation-recording.png')
  const createdResponse = page.waitForResponse(r=>r.url().endsWith('/api/explorations') && r.request().method()==='POST')
  await page.getByRole('button', { name:/开始探索$/ }).click()
  const created = await (await createdResponse).json()
  assert.equal(requestCounts.explorationPOST, 1)
  const newCall = await waitForAPI(async()=>{ const rows=(await api('GET','/model-calls?mission_id='+created.mission.id)).items; return rows[0]?.state==='completed' ? rows[0] : null })
  assert.equal(newCall.record_model_content,true); assert.equal(newCall.cost.amount,'0.00042')
  await page.getByRole('tab', { name:'模型调用', exact:true }).click()
  await visibleText('任务全历史'); await visibleText('0.00042')
  assert.equal(await page.getByRole('button', { name:/^查看调用 / }).count(), 1)
  await page.getByRole('button',{name:`查看调用 ${newCall.id}`,exact:true}).click()
  await page.getByRole('button',{name:'查看模型输入与返回',exact:true}).click()
  await visibleText('B_BROWSER_CREATED_INPUT'); await shot('mission-created-call.png'); await closeDrawer()
  checks.push('原文默认关；关闭重开保留开启草稿；真实UI只提交一次，任务汇总/列表/原文与新价格USD0.00042一致')

  phase = 'pagination actual API'
  for(let i=0;i<24;i++) {
    const testid=randomBytes(16).toString('hex'), c=cfg.configs[0]
    await api('POST',`/model-configs/${c.id}/tests/${testid}`,{config_revision:c.revision,confirm_billable:true})
    await waitForAPI(async()=> (await api('GET',`/model-configs/${c.id}/tests/${testid}`)).status==='completed')
  }
  await enter('/usage')
  await visibleText('第 1 页')
  assert.equal(await page.getByRole('button', { name:/^查看调用 / }).count(),25)
  await page.getByRole('button',{name:'下一页',exact:true}).click(); await visibleText('第 2 页')
  assert.equal(await page.getByRole('button', { name:/^查看调用 / }).count(),4)
  await page.getByRole('button',{name:'上一页',exact:true}).click(); await visibleText('第 1 页')
  await page.locator('[aria-label="开始日期"]').fill('2025-01-01'); await page.locator('[aria-label="结束日期（不含）"]').fill('2025-01-02')
  await save('filter-buttons.json', await page.locator('.ws-usage-filters button').evaluateAll(nodes=>nodes.map(n=>({text:n.textContent,label:n.getAttribute('aria-label'),type:n.getAttribute('type')}))))
  await page.locator('.ws-usage-filters button[type="submit"]').click(); await visibleText('此范围暂无调用记录')
  await shot('empty-filter.png')
  checks.push('真实29条调用25+4游标往返；显式日期筛选清游标与旧数据，空范围不伪造记录')

  phase = 'fault raw late and 401'
  faultMode=true
  await enter('/usage/calls/'+raw.id)
  await page.getByRole('button',{name:'查看模型输入与返回',exact:true}).waitFor()
  let release, reached
  const reachedP=new Promise(r=>{reached=r}), releaseP=new Promise(r=>{release=r})
  const rawPattern='**/api/model-calls/'+raw.id+'/content*'
  const rawBody=await api('GET','/model-calls/'+raw.id+'/content')
  await page.route(rawPattern,async r=>{reached();await releaseP;await r.fulfill({json:rawBody}).catch(()=>{})})
  await page.getByRole('button',{name:'查看模型输入与返回',exact:true}).click(); await reachedP
  await closeDrawer(); release(); await page.unroute(rawPattern)
  assert.equal(await page.locator('.evidence-preview').count(),0)
  await page.getByRole('button',{name:'下一页',exact:true}).click(); await visibleText('第 2 页')
  await page.getByRole('button',{name:`查看调用 ${raw.id}`,exact:true}).click()
  await page.route(rawPattern,r=>r.fulfill({status:401,json:{error:'fixture'}}))
  await page.getByRole('button',{name:'查看模型输入与返回',exact:true}).click()
  await page.locator('#auth-token-input').waitFor(); assert.equal(await page.locator('.evidence-preview').count(),0)
  await page.unroute(rawPattern); await login()
  checks.push('故障注入：原文迟到后关闭不恢复，401立即卸载原文并要求重认证')

  phase = 'fault price conflict and navigation'
  await enter('/usage/prices')
  await page.getByRole('row').filter({hasText:'ui-fixture-0'}).getByRole('button',{name:'编辑价格',exact:true}).click()
  await page.locator('#price-input_per_million').fill('4')
  const putPattern='**/api/model-prices/*'
  await page.route(putPattern,r=>r.request().method()==='PUT'?r.fulfill({status:409,json:{error:'fixture'}}):r.continue())
  const pendingBefore=requestCounts.pricePUT
  await page.getByRole('button',{name:'保存价格',exact:true}).click(); await visibleText('请先核对当前保存结果')
  assert.equal(requestCounts.pricePUT,pendingBefore+1)
  await page.getByRole('button',{name:'暂时关闭',exact:true}).click()
  await page.getByRole('tab',{name:'调用与用量',exact:true}).click(); await page.getByRole('tab',{name:'价格',exact:true}).click()
  await page.getByRole('button',{name:'核对价格保存',exact:true}).click()
  await page.getByRole('button',{name:'核对保存结果',exact:true}).click()
  await page.getByRole('button',{name:'按当前读取继续编辑',exact:true}).waitFor()
  assert.equal(requestCounts.pricePUT,pendingBefore+1)
  await page.getByRole('button',{name:'按当前读取继续编辑',exact:true}).click()
  assert.equal(await page.locator('#price-input_per_million').inputValue(),'3')
  await page.getByRole('button',{name:'放弃草稿',exact:true}).click(); await page.unroute(putPattern)
  checks.push('故障注入：价格409只发一次PUT，导航保留身份，GET核对后显式恢复当前值，无自动保存')

  phase = 'fault uncertain price across 401'
  await page.getByRole('row').filter({hasText:'ui-fixture-0'}).getByRole('button',{name:'编辑价格',exact:true}).click()
  await page.locator('#price-input_per_million').fill('4')
  await page.route(putPattern,r=>r.request().method()==='PUT'?r.abort('connectionreset'):r.continue())
  const uncertainBefore=requestCounts.pricePUT
  await page.getByRole('button',{name:'保存价格',exact:true}).click(); await visibleText('保存结果待核对')
  await page.getByRole('button',{name:'暂时关闭',exact:true}).click()
  await page.route('**/api/model-prices',r=>r.fulfill({status:401,json:{error:'fixture'}}))
  await page.getByRole('button',{name:'刷新价格',exact:true}).click(); await page.locator('#auth-token-input').waitFor()
  await page.unroute('**/api/model-prices'); await page.unroute(putPattern); await login()
  await page.getByRole('button',{name:'核对价格保存',exact:true}).click()
  await page.getByRole('button',{name:'核对保存结果',exact:true}).click()
  await page.getByRole('button',{name:'按当前读取继续编辑',exact:true}).waitFor()
  assert.equal(requestCounts.pricePUT,uncertainBefore+1)
  await page.getByRole('button',{name:'按当前读取继续编辑',exact:true}).click()
  await page.getByRole('button',{name:'放弃草稿',exact:true}).click()
  checks.push('故障注入：价格断连后401/重认证保留原ID，仅显式GET核对，无自动PUT')

  phase = 'price late success after leaving'
  await page.getByRole('row').filter({hasText:'ui-fixture-0'}).getByRole('button',{name:'编辑价格',exact:true}).click()
  await page.locator('#price-input_per_million').fill('4')
  let allowPrice, priceSaved, priceReleased
  const priceWait=new Promise(r=>{allowPrice=r}), savedWait=new Promise(r=>{priceSaved=r}), releasedWait=new Promise(r=>{priceReleased=r})
  let savedPriceID
  await page.route(putPattern,async r=>{
    if(r.request().method()!=='PUT') return r.continue()
    const response=await r.fetch(), body=await response.json()
    savedPriceID=body.id; priceSaved(); await priceWait
    await r.fulfill({status:response.status(),json:body}).catch(()=>{}); priceReleased()
  })
  const lateBefore=requestCounts.pricePUT
  await page.getByRole('button',{name:'保存价格',exact:true}).click(); await savedWait
  await page.getByRole('button',{name:'暂时关闭',exact:true}).click()
  await page.getByRole('tab',{name:'调用与用量',exact:true}).click()
  allowPrice(); await releasedWait; await page.unroute(putPattern)
  await page.getByRole('tab',{name:'价格',exact:true}).click()
  await page.getByRole('button',{name:'核对价格保存',exact:true}).click()
  await page.getByRole('button',{name:'核对保存结果',exact:true}).click()
  await visibleText('已核对：价格已保存')
  assert.equal(requestCounts.pricePUT,lateBefore+1)
  const savedPrice=(await api('GET','/model-prices')).items.find(p=>p.id===savedPriceID)
  assert.equal(savedPrice.input_per_million,'4')
  checks.push('真实PUT成功但回执延迟至离页后：不自动接纳旧回执、不重发；回页显式GET原ID核对成功')

  phase = 'wrong mission and unavailable API'
  await enter(`/projects/${cfg.missions[0]}/model-calls/${raw.id}`)
  await visibleText('记录不属于当前项目'); assert.equal(await page.locator('.evidence-preview').count(),0)
  await closeDrawer()
  await page.route('**/api/missions/*/model-usage*',r=>r.fulfill({status:503,json:{error:'fixture'}}))
  await page.getByRole('button',{name:'刷新记录',exact:true}).click(); await visibleText('服务暂不可用')
  assert.equal(await page.getByRole('button',{name:/^查看调用 /}).count(),1)
  await page.unroute('**/api/missions/*/model-usage*')
  const storedValues=await page.evaluate(()=>JSON.stringify({local:{...localStorage},session:{...sessionStorage}}))
  for (const secret of [cfg.token,'B_UI_INPUT_SENTINEL','B_BROWSER_CREATED_INPUT','fixture-provider-key']) assert.ok(!storedValues.includes(secret))
  const storage=await page.evaluate(()=>({local:Object.keys(localStorage),session:Object.keys(sessionStorage)}))
  assert.ok(!JSON.stringify(storage).includes('model') && !JSON.stringify(storage).includes('content'))
  checks.push('跨项目详情404拒绝，无全局回退；项目usage503局部错误；浏览器无模型原文持久化')
  phase = 'fault raw pending missing truncated and inert HTML'
  await enter('/usage/calls/'+raw.id)
  const htmlBody=Buffer.from('<img src="https://invalid.example/never" onerror="window.__rawExecuted=true">').toString('base64')
  for (const state of ['pending','missing','available']) {
    await page.route(rawPattern,r=>r.fulfill({json:{call_id:raw.id,recorded:true,input:{state,body_base64:state==='available'?htmlBody:null,media_type:'text/html',complete:false,redacted:true},response:null}}))
    await page.getByRole('button',{name:'查看模型输入与返回',exact:true}).click()
    await visibleText(state==='pending'?'尚在执行':state==='missing'?'内容缺失':'捕获已截断或不完整')
    if (state==='available') {
      await page.locator('.evidence-preview').waitFor()
      assert.equal(await page.locator('img[src="https://invalid.example/never"]').count(),0)
      assert.equal(await page.evaluate(()=>Boolean(window.__rawExecuted)),false)
      assert.deepEqual(externalRequests, [])
      await shot('raw-truncated-html-fixture.png')
    } else assert.equal(await page.locator('.evidence-preview').count(),0)
    await page.getByRole('button',{name:'收起模型输入与返回',exact:true}).click(); await page.unroute(rawPattern)
  }
  await closeDrawer()
  checks.push('传输fixture：原文pending/missing/截断/脱敏分别标示，HTML仅为字节文本不执行/不加载外链')

  phase = 'fault unknown partial multiple currencies'
  const aggregate=await api('GET','/model-usage'), callPage=await api('GET','/model-calls')
  aggregate.summary={...aggregate.summary,missing_usage_calls:'2',invalid_usage_calls:'1',unpriced_calls:'2',estimated_costs:[{currency:'USD',amount:'0.000001',priced_calls:'1'},{currency:'CNY',amount:'0.500001',priced_calls:'1'}]}
  aggregate.daily=aggregate.daily.map((d,i)=>({...d,input_tokens:i===3?null:d.input_tokens,output_tokens:i===3?null:d.output_tokens,missing_usage_calls:i===3?'2':d.missing_usage_calls}))
  callPage.items=callPage.items.slice(0,3).map((c,i)=>({...c,model:'超长模型名称-'.repeat(30),usage:{...c.usage,status:i===0?'partial':i===1?'missing':'invalid',input_tokens:i===0?'9007199254740993123':null,output_tokens:null,total_tokens:null},cost:{...c.cost,status:i===0?'unpriced':'usage_missing',amount:null}}))
  await page.route('**/api/model-usage?*',r=>r.fulfill({json:aggregate}))
  await page.route('**/api/model-calls?*',r=>r.fulfill({json:callPage}))
  await enter('/usage'); await visibleText('用量不完整，曲线留空')
  await visibleText('CNY 0.500001'); await visibleText('USD 0.000001')
  await visibleText('9,007,199,254,740,993,123'); await visibleText('未配置价格'); await visibleText('部分上报')
  await page.getByText('精确表格',{exact:true}).click(); await visibleText('未知')
  await noOverflow(); await shot('partial-currencies-long-model-fixture.png')
  await page.unroute('**/api/model-usage?*'); await page.unroute('**/api/model-calls?*')
  checks.push('传输fixture：部分/未知/无效计数、缺价、多币种分别显示、长模型与大整数精确保留，缺口曲线留空')
  assert.deepEqual(pageErrors,[]); assert.deepEqual(consoleErrors,[])
  await save('browser-result.json',{passed:true,checks,screenshots,requestCounts,pageErrors,consoleErrors,storage})
  console.log('PASS B UI browser '+checks.length+' groups')
} catch(e) {
  await shot('failure.png').catch(()=>{})
  await save('browser-result.json',{passed:false,phase,checks,screenshots,requestCounts,pageErrors,consoleErrors,error:String(e).slice(0,1600)})
  console.error('FAIL B UI phase: '+phase); process.exitCode=1
} finally { await browser.close() }

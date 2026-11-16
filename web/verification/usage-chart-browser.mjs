import { chromium } from '/root/桌面/wscyber/var/react-acceptance/node_modules/playwright-core/index.mjs'
import fs from 'node:fs/promises'
import assert from 'node:assert/strict'
const out=process.argv[2]
assert.ok(out.startsWith('/root/桌面/wscyber/var/'))
const cfg=JSON.parse(await fs.readFile(out+'/fixture-private.json','utf8'))
const browser=await chromium.launch({executablePath:'/usr/bin/chromium',headless:true,args:['--no-sandbox','--disable-dev-shm-usage','--disable-background-networking']})
const ctx=await browser.newContext({viewport:{width:1440,height:1000},serviceWorkers:'block'})
await ctx.route('**/*',r=>new URL(r.request().url()).origin===cfg.url?r.continue():r.abort())
const page=await ctx.newPage();page.setDefaultTimeout(10000)
const checks=[], screenshots=[], errors=[]
let phase='actual chart', releaseConfig
page.on('pageerror',e=>errors.push(e.name))
async function shot(name){await page.screenshot({path:out+'/'+name,animations:'disabled'});screenshots.push(name)}
async function login(){await page.locator('#auth-token-input').fill(cfg.token);await page.getByRole('button',{name:'进入工作台',exact:true}).click()}
async function ready(){await page.locator('.usage-end-label').first().waitFor()}
async function labels(){return page.locator('.usage-leader').evaluateAll(ns=>ns.map(n=>{const m=n.getAttribute('d').match(/^M[^,]+,([\d.]+) L[^,]+,([\d.]+)$/);return m?{point:Number(m[1]),label:Number(m[2])}:null}))}
async function bounds(){return page.locator('.daily-usage').evaluate(n=>{const s=n.querySelector('.daily-usage-scroll'),t=n.querySelector('.daily-usage-tooltip');const a=s.getBoundingClientRect(),b=t.getBoundingClientRect();return {scroll:s.scrollLeft,left:b.left,right:b.right,top:b.top,bottom:b.bottom,viewLeft:a.left,viewRight:a.right}})}
try{
  const configGate=new Promise(r=>{releaseConfig=r})
  await page.route('**/api/model-configs',async r=>{const response=await r.fetch(), body=await response.json();await configGate;await r.fulfill({json:body}).catch(()=>{})})
  await page.goto(cfg.url+'/usage');await login();await ready()
  const plot=page.getByRole('group',{name:'每日已知 tokens',exact:true})
  await plot.focus();await plot.press('End')
  assert.match(await page.locator('.daily-usage-tooltip').innerText(),/560/)
  const text=await page.locator('.daily-usage-tooltip').innerText()
  const configResponse=page.waitForResponse(r=>r.url().endsWith('/api/model-configs'))
  releaseConfig();await configResponse
  await page.evaluate(()=>new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r))))
  assert.equal(await page.locator('.daily-usage-tooltip').innerText(),text)
  assert.ok(await plot.evaluate(n=>document.activeElement===n))
  await page.unroute('**/api/model-configs')
  await plot.press('Escape')
  for(const l of await labels()){assert.ok(l);assert.ok(Math.abs(l.point-l.label)<1)}
  checks.push('真实560/100曲线直标贴近各自末点；配置迟到后键盘仍可读数')
  for(const [width,height] of [[1440,1000],[1920,1080],[390,844]]){
    await page.setViewportSize({width,height})
    for(const mode of ['light','dark']){
      if(mode==='dark')await page.getByRole('button',{name:'切换到暗色主题',exact:true}).click()
      await page.locator('.daily-usage').scrollIntoViewIfNeeded()
      assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1))
      await shot(`final-usage-${width}-${mode}.png`)
    }
    await page.getByRole('button',{name:'切换到浅色主题',exact:true}).click()
  }
  await plot.focus();await plot.press('End')
  await page.waitForFunction(()=>document.querySelector('.daily-usage-scroll').scrollLeft>0)
  let b=await bounds();assert.ok(b.left>=b.viewLeft&&b.right<=b.viewRight+1)
  await shot('keyboard-end-390.png')
  await plot.press('Home');await page.waitForFunction(()=>document.querySelector('.daily-usage-scroll').scrollLeft===0)
  b=await bounds();assert.ok(b.left>=b.viewLeft&&b.right<=b.viewRight+1)
  await plot.press('Escape')
  await page.getByText('精确表格',{exact:true}).click();await page.getByRole('columnheader',{name:'已知输入',exact:true}).waitFor()
  await page.getByText('趋势图',{exact:true}).click();await page.setViewportSize({width:1920,height:1080})
  await page.waitForFunction(()=>Number(document.querySelector('.daily-usage svg').getAttribute('width'))>1400)
  await page.setViewportSize({width:1440,height:1000})
  await page.waitForFunction(()=>Number(document.querySelector('.daily-usage svg').getAttribute('width'))<1400)
  checks.push('六视口浅暗无全页溢出；390键盘首尾跟随局部滚动且tooltip在可见区；图表切换重观察')

  const response=await ctx.request.get(cfg.url+'/api/model-usage',{headers:{Authorization:'Bearer '+cfg.token}})
  const base=await response.json()
  let fixture
  await page.route('**/api/model-usage?*',r=>r.fulfill({json:fixture}))
  for(const scenario of ['adjacent','coincident','extreme','93-days']){
    phase=scenario
    fixture=structuredClone(base)
    fixture.daily=Array.from({length:scenario==='93-days'?93:7},(_,i)=>({...base.daily[0],date:new Date(Date.UTC(2026,7,i+1)).toISOString().slice(0,10),calls:'1',missing_usage_calls:'0',input_tokens:scenario==='extreme'?'9007199254740993123':'100',output_tokens:scenario==='extreme'?'1':scenario==='adjacent'?'99':scenario==='93-days'?'20':'100'}))
    const refresh=page.locator('section[aria-label="全局模型用量"] .ant-card-extra button:not(.ant-btn-loading)').filter({hasText:'刷新记录'})
    await refresh.waitFor()
    await Promise.all([page.waitForResponse(r=>r.url().includes('/api/model-usage?')), refresh.click()])
    await page.waitForFunction(()=>document.querySelector('.daily-usage')?.getAttribute('aria-busy')==='false')
    await page.waitForFunction(d=>document.querySelector('.daily-usage svg')?.textContent.includes(d),scenario==='93-days'?'11-01':'08-07')
    if(scenario==='extreme')await page.waitForFunction(()=>document.querySelector('.daily-usage svg')?.textContent.includes('1.2e19'))
    const ls=await labels();assert.equal(ls.length,2)
    assert.ok(Math.abs(ls[0].label-ls[1].label)>=24)
    for(const l of ls)assert.ok(l.label>=24&&l.label<=208&&Math.abs(l.label-l.point)<=24)
    if(scenario==='93-days'){
      const ticks=await page.locator('.daily-usage svg > text[y="236"]').evaluateAll(ns=>ns.map(n=>{const b=n.getBBox();return {left:b.x,right:b.x+b.width}}))
      for(let i=1;i<ticks.length;i++)assert.ok(ticks[i].left>=ticks[i-1].right+4)
    }
    await page.locator('.daily-usage').scrollIntoViewIfNeeded();await shot(`chart-${scenario}-fixture.png`)
    await plot.focus();await plot.press('End')
    if(scenario==='extreme'){
      assert.match(await page.locator('.daily-usage-tooltip').innerText(),/9,007,199,254,740,993,123/)
      await page.setViewportSize({width:390,height:844})
      await page.waitForFunction(()=>Number(document.querySelector('.daily-usage svg').getAttribute('width'))===620)
      await plot.press('End')
      await page.waitForFunction(()=>document.querySelector('.daily-usage-scroll').scrollLeft>0)
      await page.locator('.daily-usage').scrollIntoViewIfNeeded()
      b=await bounds();assert.ok(b.left>=b.viewLeft&&b.right<=b.viewRight+1&&b.top>=0&&b.bottom<=844)
      await shot('chart-extreme-tooltip-390.png');await page.setViewportSize({width:1440,height:1000})
    }
    await plot.press('Escape')
  }
  await page.emulateMedia({forcedColors:'active'})
  assert.equal(await page.locator('.usage-output .usage-line').evaluate(n=>getComputedStyle(n).strokeDasharray),'7px, 4px')
  await shot('chart-forced-colors.png')
  await page.emulateMedia({forcedColors:'none',media:'print'})
  assert.equal(await page.locator('.usage-output .usage-line').evaluate(n=>getComputedStyle(n).strokeDasharray),'7px, 4px')
  await shot('chart-print.png');await page.emulateMedia({media:'screen'})
  checks.push('传输fixture：末点相邻/重合分离24px，极端比例/精确大整数，93日轴标签不碰撞；forced-colors/print线型区分')
  assert.deepEqual(errors,[])
  await fs.writeFile(out+'/browser-result.json',JSON.stringify({passed:true,checks,screenshots,errors},null,2),{mode:0o600})
  console.log('PASS B chart browser '+checks.length+' groups')
}catch(e){
  releaseConfig?.();await shot('failure.png').catch(()=>{})
  await fs.writeFile(out+'/browser-result.json',JSON.stringify({passed:false,phase,checks,screenshots,errors,error:String(e).slice(0,1500)},null,2),{mode:0o600})
  console.error('FAIL chart '+phase);process.exitCode=1
}finally{await browser.close()}

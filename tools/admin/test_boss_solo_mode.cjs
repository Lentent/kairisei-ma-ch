'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'C:/Users/lenovo/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules/playwright');
const root=path.resolve(__dirname,'../../server/internal/admin/web');
const routes={'/admin':'admin_ui.html','/admin.js':'admin_ui.js','/admin.css':'admin_ui.css','/gacha-legacy.js':'admin_gacha_legacy.js','/cdk-admin.js':'admin_cdk.js'};
for(const n of fs.readdirSync(root).filter(n=>n.startsWith('admin_')&&n.endsWith('.js')))routes['/'+n.slice(6).replaceAll('_','-')]=n;
const bosses=[{boss_id:11,difficulty:'上级'},{boss_id:12,difficulty:'超级'}].map(b=>({...b,group_id:700000010,name:'冰龙',targets:[]}));
const group={group_id:700000010,name:'冰龙',category:'3d',boss_ids:[11,12],boss_count:2,difficulties:['上级','超级'],bosses};
// Deliberately omit solo_disabled in initial/default rows to simulate old data.
let rules=bosses.map(b=>({boss_id:b.boss_id,standard:true,own_deck:true,continue:true,bp_use:20,bp_use_half:10,own_deck_boss_id:b.boss_id+100,default:{boss_id:b.boss_id,standard:true,own_deck:true,continue:true}}));
let revision=0,conflict=false;const writes=[];
const server=http.createServer(async(req,res)=>{
 try{
  const u=new URL(req.url,'http://localhost');
  if(routes[u.pathname]){res.setHeader('Content-Type',u.pathname.endsWith('.js')?'text/javascript':u.pathname.endsWith('.css')?'text/css':'text/html');res.end(fs.readFileSync(path.join(root,routes[u.pathname])));return}
  let raw='';for await(const c of req)raw+=c;let data={state:'PASS'};
  if(u.pathname==='/api/status')data={state:'PASS',accounts:{bound:1,guests:0,system:0},maintenance:{enabled:false},publication:{mode:'all'},server_time_utc:new Date().toISOString()};
  if(u.pathname==='/api/boss-groups')data={state:'PASS',groups:[group]};
  if(u.pathname==='/api/boss-policy')data={state:'PASS',publication:{mode:'all',group_ids:[],revision:0}};
  if(u.pathname==='/api/boss-rules'){
   if(req.method==='PUT'){
    const input=JSON.parse(raw);assert.equal(req.headers['x-kairisei-admin-action'],'apply');
    if(conflict||input.expected_revision!==revision){res.statusCode=409;res.end(JSON.stringify({state:'FAIL',error:'conflict'}));return}
    writes.push(input);rules=rules.map(r=>({...r,...input.rules.find(v=>v.boss_id===r.boss_id)}));revision++;
   }
   data={state:'PASS',revision,rules};
  }
  res.setHeader('Content-Type','application/json');res.end(JSON.stringify(data));
 }catch(e){res.statusCode=500;res.end(JSON.stringify({error:e.message}))}
});
(async()=>{
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 const browser=await chromium.launch({executablePath:process.env.CHROME_PATH||'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true});
 const page=await browser.newPage({viewport:{width:1440,height:1000}}),errors=[];
 page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());
 const field=(id,k)=>page.locator(`[data-boss="${id}"] [data-rule="${k}"]`);
 const open=async()=>{await page.locator('[data-boss-detail="700000010"]').click();await field(11,'solo_disabled').waitFor()};
 const save=async()=>{await page.locator('#boss-detail-save').click();await page.locator('#review-confirm').click();await page.waitForFunction(()=>!bossRuleEditor.busy&&!bossRuleEditor.dirty)};
 try{
  await page.goto(`http://127.0.0.1:${server.address().port}/admin#bosses`);await page.waitForFunction(()=>state.loaded.has('bosses'));await open();
  assert.equal(await field(11,'solo_disabled').isChecked(),false);
  await field(11,'solo_disabled').check();
  assert.equal(await field(11,'standard').isChecked(),true);
  assert.equal(await field(11,'own_deck').isDisabled(),true);
  assert.equal(await field(11,'own_deck').isChecked(),false);
  assert.equal(await field(12,'solo_disabled').isChecked(),false);
  assert.equal(await field(12,'own_deck').isEnabled(),true);
  await save();assert.equal(writes[0].rules[0].solo_disabled,true);assert.equal(writes[0].rules[0].own_deck,true);assert.equal(writes[0].rules[1].solo_disabled,false);
  await page.locator('#boss-detail-close').click();await open();assert.equal(await field(11,'solo_disabled').isChecked(),true);
  const shots=process.env.BOSS_SOLO_SCREENSHOTS;if(shots){fs.mkdirSync(shots,{recursive:true});await page.screenshot({path:path.join(shots,'boss-solo-desktop.png')})}
  await page.setViewportSize({width:390,height:844});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
  if(shots)await page.screenshot({path:path.join(shots,'boss-solo-mobile.png')});await page.setViewportSize({width:1440,height:1000});
  await field(11,'solo_disabled').uncheck();assert.equal(await field(11,'own_deck').isEnabled(),true);assert.equal(await field(11,'own_deck').isChecked(),true);await save();
  await page.locator('[data-col="solo_disabled"][data-value="1"]').click();assert.equal(await field(12,'solo_disabled').isChecked(),true);await save();
  await page.locator('#boss-detail-default').click();assert.equal(await field(11,'solo_disabled').isChecked(),false);assert.equal(await field(12,'solo_disabled').isChecked(),false);await save();
  // Failed save must preserve the draft rather than claiming it was applied.
  conflict=true;await field(11,'solo_disabled').check();await page.locator('#boss-detail-save').click();await page.locator('#review-confirm').click();await page.waitForFunction(()=>!bossRuleEditor.busy);
  assert.equal(await field(11,'solo_disabled').isChecked(),true);assert.equal(await page.evaluate(()=>bossRuleEditor.dirty),true);assert.equal(writes.length,4);assert.equal(rules[0].solo_disabled,false);
  assert.deepEqual(errors,[]);console.log('PASS: per-difficulty solo toggle, rooms preserved, own-deck suppression/restoration, legacy defaults, save/reopen, bulk/default, conflict draft, responsive layout');
 }finally{await browser.close();server.close()}
})().catch(e=>{console.error(e);process.exitCode=1;server.close()});

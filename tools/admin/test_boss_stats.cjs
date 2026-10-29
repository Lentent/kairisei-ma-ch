'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'C:/Users/lenovo/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules/playwright');
const root=path.resolve(__dirname,'../../server/internal/admin/web');
const routes={'/admin':'admin_ui.html','/admin.js':'admin_ui.js','/admin.css':'admin_ui.css','/gacha-legacy.js':'admin_gacha_legacy.js','/cdk-admin.js':'admin_cdk.js'};
for(const n of fs.readdirSync(root).filter(n=>n.startsWith('admin_')&&n.endsWith('.js')))routes['/'+n.slice(6).replaceAll('_','-')]=n;
const boss={boss_id:11,group_id:700000010,name:'冰龙',difficulty:'超级',targets:[0,1].flatMap(w=>[0,1].map(i=>({battle_index:w,enemy_index:i,name:i?'右翼':'本体',stats:{enemy_id:i+1,attribute:'ICE',hp:200000,attack:1000,magic:1500,recovery:0,defense:200,magic_defense:300,damage_reduction:0,attribute_fixed:[0,0,0,0,0],attribute_rates:Array(9).fill(100),status_resistances:Array(15).fill(0),dot_reductions:Array(5).fill(0)}})))};
const group={group_id:700000010,name:'冰龙',category:'3d',boss_ids:[11],boss_count:1,difficulties:['超级'],max_segments:2,bosses:[boss]};
const originalActions=boss.targets.map(t=>({battle_index:t.battle_index,enemy_index:t.enemy_index,slot:1,skill_id:99,function_id:99,name:'冰霜吐息',category:'skill',rule:'原行动概率 100%',branch:'沿用原分支判断',effects:['魔法攻击，原每段基础威力：固定 5000'],editable:true}));
let revision=0,overrides=[],conflict=false;const writes=[];
const response=()=>({state:'PASS',revision,boss,overrides,original_actions:originalActions});
const server=http.createServer(async(req,res)=>{
 try{
  const u=new URL(req.url,'http://localhost');
  if(routes[u.pathname]){res.setHeader('Content-Type',u.pathname.endsWith('.js')?'text/javascript':u.pathname.endsWith('.css')?'text/css':'text/html');res.end(fs.readFileSync(path.join(root,routes[u.pathname])));return}
  let raw='';for await(const c of req)raw+=c;let data={state:'PASS'};
  if(u.pathname==='/api/status')data={state:'PASS',accounts:{bound:1,guests:0,system:0},maintenance:{enabled:false},publication:{mode:'all'},server_time_utc:new Date().toISOString()};
  if(u.pathname==='/api/boss-groups')data={state:'PASS',groups:[group]};
  if(u.pathname==='/api/boss-policy')data={state:'PASS',publication:{mode:'all',group_ids:[],revision:0}};
  if(u.pathname==='/api/boss-rules')data={state:'PASS',revision:0,rules:[{boss_id:11,standard:true,own_deck:false,continue:true,bp_use:20,bp_use_half:10,default:{boss_id:11,standard:true,own_deck:false,continue:true}}]};
  if(u.pathname==='/api/boss-stats'){
   if(req.method==='PUT'){
    const input=JSON.parse(raw);assert.equal(req.headers['x-kairisei-admin-action'],'apply');assert.equal(input.boss_id,11);
    if(conflict||input.expected_revision!==revision){res.statusCode=409;res.end(JSON.stringify({state:'FAIL',error:'conflict'}));return}
    writes.push(input);overrides=input.overrides;revision++;
   }
   data=response();
  }
  res.setHeader('Content-Type','application/json');res.end(JSON.stringify(data));
 }catch(e){res.statusCode=500;res.end(JSON.stringify({error:e.message}))}
});
(async()=>{
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 const browser=await chromium.launch({executablePath:process.env.CHROME_PATH||'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true});
 const page=await browser.newPage({viewport:{width:1440,height:1000}}),errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('dialog',d=>d.accept());
 const open=async()=>{await page.locator('[data-boss-detail="700000010"]').click();await page.locator('[data-boss-stats="11"]').click();await page.locator('#bs-hp').waitFor()};
 const save=async()=>{await page.locator('#boss-stats-save').click();await page.waitForFunction(()=>!bossStatsEditor.busy&&!bossStatsEditor.dirty)};
 try{
  await page.goto(`http://127.0.0.1:${server.address().port}/admin#bosses`);await page.waitForFunction(()=>state.loaded.has('bosses'));await open();
  assert.equal(await page.locator('#boss-stats-mode').inputValue(),'package');assert.equal(await page.locator('#bs-hp').isDisabled(),true);assert.equal(await page.locator('#bs-hp').getAttribute('placeholder'),'200000');
  assert.match(await page.locator('#boss-stats-modal').innerText(),/普通单人战斗仍使用手机资源包原值/);
  await page.locator('#boss-stats-mode').selectOption('custom');await page.locator('#bs-hp').fill('999999');await page.locator('#bs-attack').fill('0');
  await page.locator('#boss-stats-fields summary').click();await page.locator('#bs-attribute_rates-0').fill('50');
  await page.locator('[data-stats-tab="skills"]').click();await page.locator('#bo-0-0-power').fill('8000');
  await page.locator('#boss-stats-wave').selectOption('1');assert.equal(await page.locator('#bo-2-0-power').inputValue(),'');
  await page.locator('[data-stats-tab="stats"]').click();assert.equal(await page.locator('#bs-hp').inputValue(),'');await page.locator('#bs-hp').fill('777777');
  await page.locator('#boss-stats-part').selectOption('3');assert.equal(await page.locator('#bs-hp').inputValue(),'');
  await save();assert.equal(writes.length,1);assert.equal(writes[0].overrides.length,2);
  assert.deepEqual(writes[0].overrides[0].values,{hp:999999,attack:0,'attribute_rates:0':50});
  assert.deepEqual(writes[0].overrides[0].original_skills,[{slot:1,skill_id:99,function_id:99,power:8000}]);
  assert.equal(writes[0].overrides[1].values.hp,777777);
  assert.equal(await page.evaluate(()=>state.groups[0].bosses[0].targets[0].stats.hp),999999);
  await page.locator('#boss-stats-cancel').click();await open();assert.equal(await page.locator('#bs-hp').inputValue(),'999999');
  await page.locator('#boss-stats-mode').selectOption('package');assert.equal(await page.locator('#bs-hp').inputValue(),'');assert.equal(await page.locator('#bs-hp').isDisabled(),true);
  await page.locator('#boss-stats-mode').selectOption('custom');assert.equal(await page.locator('#bs-hp').inputValue(),'999999');
  // Invalid values cannot be hidden by switching parts or tabs, nor saved.
  await page.locator('#bs-hp').fill('0');await page.locator('#boss-stats-part').selectOption('1');assert.equal(await page.locator('#boss-stats-part').inputValue(),'0');
  await page.locator('#boss-stats-save').click();assert.equal(writes.length,1);await page.locator('#bs-hp').fill('999999');
  await page.locator('#bs-attack').fill('');await save();assert.equal(writes[1].overrides[0].values.attack,undefined);
  const shots=process.env.BOSS_STATS_SCREENSHOTS;if(shots){fs.mkdirSync(shots,{recursive:true});await page.screenshot({path:path.join(shots,'boss-stats-desktop.png')})}
  await page.setViewportSize({width:390,height:844});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
  const box=await page.locator('#boss-stats-modal .dialog').boundingBox();assert.ok(box.x>=0&&box.x+box.width<=390);
  if(shots)await page.screenshot({path:path.join(shots,'boss-stats-mobile.png')});
  // Conflict keeps all user inputs for recovery.
  conflict=true;await page.locator('#bs-hp').fill('555555');await page.locator('#boss-stats-save').click();await page.waitForFunction(()=>!bossStatsEditor.busy);assert.equal(await page.locator('#bs-hp').inputValue(),'555555');assert.match(await page.locator('#boss-stats-error').innerText(),/未保存/);assert.equal(writes.length,2);conflict=false;
  await page.locator('#boss-stats-part-reset').click();await save();assert.equal(writes[2].overrides.length,1);assert.equal(writes[2].overrides[0].battle_index,1);
  await page.locator('#boss-stats-package').click();await save();assert.deepEqual(writes[3].overrides,[]);
  await page.locator('#boss-stats-cancel').click();await open();assert.equal(await page.locator('#boss-stats-mode').inputValue(),'package');assert.equal(await page.locator('#bs-hp').getAttribute('placeholder'),'200000');
  assert.deepEqual(errors,[]);console.log('PASS: direct native boss edit entry, package defaults, sparse fields, explicit zero, wave/part/skill isolation, save/reopen, mode switch, validation, conflict, restore, responsive layout');
 }finally{await browser.close();server.close()}
})().catch(e=>{console.error(e);process.exitCode=1;server.close()});

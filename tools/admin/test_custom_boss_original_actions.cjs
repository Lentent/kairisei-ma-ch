'use strict';
// Browser regression: native actions are editable without copying the AI schedule.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'C:/Users/lenovo/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules/playwright');
const root=path.resolve(__dirname,'../../server/internal/admin/web');
const routes={'/admin':'admin_ui.html','/admin.js':'admin_ui.js','/admin.css':'admin_ui.css','/gacha-legacy.js':'admin_gacha_legacy.js','/cdk-admin.js':'admin_cdk.js'};
for(const name of fs.readdirSync(root).filter(s=>s.startsWith('admin_')&&s.endsWith('.js')))routes['/'+name.slice(6).replaceAll('_','-')]=name;
const source={boss_id:11,name:'测试原有行动',difficulty:'超级',targets:[0,1].flatMap(w=>[0,1].map(i=>({battle_index:w,enemy_index:i,name:i?'左翼':'本体',stats:{enemy_id:i+1,attribute:'FIRE',hp:100000,attack:1000,magic:1000,recovery:0,defense:300,magic_defense:500,damage_reduction:0,attribute_fixed:[0,0,0,0,0]},actions:null})))};
let revision=1,boss={boss_id:890000001,source_boss_id:11,name:'可编辑复制品',difficulty:'超级',bp_use:20,bp_use_half:10,continue:true,enabled:false,targets:structuredClone(source.targets)};
let failSkills=true; const writes=[];
const originalActions=source.targets.flatMap(t=>[
  {battle_index:t.battle_index,enemy_index:t.enemy_index,slot:1,skill_id:99,function_id:99,name:'火焰打击',category:'skill',rule:'原行动概率 100% · 按原 AI 条件 #12 判断',branch:'沿用原分支判断：TURN (1, 3)',effects:['单体物理攻击，原攻击次数 1。原每段基础威力：固定 5000'],editable:true},
  {battle_index:t.battle_index,enemy_index:t.enemy_index,slot:1,skill_id:99,function_id:100,name:'火焰打击（强化）',category:'skill',rule:'原行动概率 100%',branch:'沿用原分支判断：SELF_HP_PER (0, 50)',effects:['单体物理攻击'],editable:true},
  {battle_index:t.battle_index,enemy_index:t.enemy_index,slot:-1,skill_id:88,function_id:88,name:'开场被动',category:'passive',rule:'按模板开场被动执行',effects:['增加物攻'],editable:false,reason:'开场被动暂不支持编辑'}
]);
const server=http.createServer(async(req,res)=>{
 try{
  const url=new URL(req.url,'http://local');
  if(routes[url.pathname]){res.setHeader('Content-Type',url.pathname.endsWith('.js')?'text/javascript':url.pathname.endsWith('.css')?'text/css':'text/html');res.end(fs.readFileSync(path.join(root,routes[url.pathname])));return}
  let raw='';for await(const chunk of req)raw+=chunk;let data={state:'PASS'};
  if(url.pathname==='/api/status')data={state:'PASS',accounts:{bound:1,guests:0,system:0},maintenance:{enabled:false},publication:{mode:'all'},server_time_utc:new Date().toISOString()};
  if(url.pathname==='/api/custom-boss-buffs')data={state:'PASS',buffs:[]};
  if(url.pathname==='/api/custom-boss-skills'){
   if(failSkills){res.statusCode=503;data={state:'FAIL',error:'测试读取失败'}}
   else data={state:'PASS',skills:[],original_actions:originalActions};
  }
  if(url.pathname==='/api/custom-bosses'){
   if(req.method==='GET')data={state:'PASS',revision,sources:[source],bosses:[boss]};
   else{
    const input=JSON.parse(raw);assert.equal(req.headers['x-kairisei-admin-action'],'apply');assert.equal(input.expected_revision,revision);
    writes.push(input);boss={...boss,...input};delete boss.expected_revision;data={state:'PASS',revision:++revision,boss};
   }
  }
  res.setHeader('Content-Type','application/json');res.end(JSON.stringify(data));
 }catch(e){res.statusCode=500;res.end(JSON.stringify({error:e.message}))}
});
(async()=>{
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 const browser=await chromium.launch({executablePath:process.env.CHROME_PATH||'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true});
 const page=await browser.newPage({viewport:{width:1440,height:1000}}),errors=[];page.on('pageerror',e=>errors.push(e.message));
 try{
  await page.goto(`http://127.0.0.1:${server.address().port}/admin#custom-bosses`);
  await page.waitForFunction(()=>state.loaded.has('custom-bosses'));
  await page.locator('#cb-tab-actions').click();
  await page.locator('[data-original-retry]').first().waitFor();failSkills=false;
  await page.locator('[data-original-retry]').first().click();
  await page.locator('#co-0-0-power').waitFor();
  assert.equal(await page.locator('#ca-0-mode').inputValue(),'original');
  assert.equal(await page.locator('[data-original-list="0"] [data-original-slot]').count(),3);
  assert.match(await page.locator('[data-original-list="0"]').innerText(),/固定 5000/);
  assert.equal(await page.locator('[data-original-list="0"] [data-original-slot="-1"] input').count(),0);
  await page.locator('#co-0-0-power').fill('2345');await page.locator('#co-0-0-power_rate').fill('50');await page.locator('#co-0-0-hits').fill('2');
  await page.locator('#co-0-1-power').fill('6789');
  await page.locator('#custom-boss-part').selectOption('1');assert.equal(await page.locator('#co-1-0-power').inputValue(),'');
  await page.locator('[data-boss-wave="1"]').click();assert.equal(await page.locator('#co-2-0-power').inputValue(),'');
  await page.locator('[data-boss-wave="0"]').click();
  await page.locator('#ca-0-mode').selectOption('custom');assert.match(await page.locator('[data-original-list="0"]').innerText(),/不会执行/);
  await page.locator('#ca-0-mode').selectOption('combined');assert.equal(await page.locator('#co-0-0-power').inputValue(),'2345');
  await page.locator('#ca-0-mode').selectOption('original');
  await page.locator('#custom-boss-save').click();await page.waitForFunction(()=>!customBossEditor.busy&&!customBossEditor.dirty);
  assert.equal(writes.length,1);assert.equal(writes[0].targets[0].actions,null);
  assert.deepEqual(writes[0].targets[0].original_skills,[{slot:1,skill_id:99,function_id:99,power:2345,power_rate:50,hits:2},{slot:1,skill_id:99,function_id:100,power:6789}]);
  assert.equal(writes[0].targets[1].original_skills,undefined);assert.equal(writes[0].targets[2].original_skills,undefined);
  await page.reload();await page.waitForFunction(()=>state.loaded.has('custom-bosses'));await page.locator('#cb-tab-actions').click();
  await page.locator('#co-0-0-power').waitFor();assert.equal(await page.locator('#co-0-0-power').inputValue(),'2345');
  // Validation reveals the hidden action pane and rejects out-of-range input.
  await page.locator('#co-0-0-hits').fill('21');await page.locator('#cb-tab-basic').click();await page.locator('#custom-boss-save').click();
  assert.equal(writes.length,1);assert.equal(await page.locator('#co-0-0-hits').isVisible(),true);
  await page.locator('[data-original-list="0"] [data-original-reset]').first().click();
  assert.equal(await page.locator('#co-0-0-power').inputValue(),'');assert.equal(await page.locator('#co-0-1-power').inputValue(),'6789');
  await page.locator('#custom-boss-save').click();await page.waitForFunction(()=>!customBossEditor.busy&&!customBossEditor.dirty);
  assert.deepEqual(writes[1].targets[0].original_skills,[{slot:1,skill_id:99,function_id:100,power:6789}]);
  await page.locator('#co-0-1-power').fill('');await page.locator('#custom-boss-save').click();await page.waitForFunction(()=>!customBossEditor.busy&&!customBossEditor.dirty);
  assert.equal(writes[2].targets[0].original_skills,undefined);
  await page.locator('#co-0-0-power').fill('123');await page.locator('#custom-boss-reset').click();await page.locator('#custom-boss-reset').click();assert.equal(await page.locator('#co-0-0-power').inputValue(),'');
  const screenshots=process.env.CUSTOM_BOSS_SCREENSHOTS;if(screenshots){fs.mkdirSync(screenshots,{recursive:true});await page.screenshot({path:path.join(screenshots,'original-actions-desktop.png'),fullPage:true})}
  await page.setViewportSize({width:390,height:844});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
  if(screenshots)await page.screenshot({path:path.join(screenshots,'original-actions-mobile.png'),fullPage:true});
  assert.deepEqual(errors,[]);console.log('PASS: original actions, load retry, branch/part/wave isolation, mode switching, save/reload, validation, reset and mobile layout');
 }finally{await browser.close();server.close()}
})().catch(e=>{console.error(e);process.exitCode=1;server.close()});

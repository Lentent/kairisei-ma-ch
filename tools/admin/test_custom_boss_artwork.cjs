'use strict';
// Browser regression test: PNG import, per-wave editing, save/reload and export.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'C:/Users/lenovo/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules/playwright');
const root=path.resolve(__dirname,'../../server/internal/admin/web');
const routes={'/admin':'admin_ui.html','/admin.js':'admin_ui.js','/admin.css':'admin_ui.css','/gacha-legacy.js':'admin_gacha_legacy.js','/cdk-admin.js':'admin_cdk.js'};
function testPNG(red=220){
 const crc=raw=>{let n=0xffffffff;for(const b of raw){n^=b;for(let i=0;i<8;i++)n=n&1?(n>>>1)^0xedb88320:n>>>1}return(n^0xffffffff)>>>0};
 const chunk=(name,raw)=>{const body=Buffer.concat([Buffer.from(name),raw]),size=Buffer.alloc(4),hash=Buffer.alloc(4);size.writeUInt32BE(raw.length);hash.writeUInt32BE(crc(body));return Buffer.concat([size,body,hash])};
 const header=Buffer.alloc(13);header.writeUInt32BE(64,0);header.writeUInt32BE(128,4);header[8]=8;header[9]=6;
 const pixels=Buffer.alloc((64*4+1)*128);for(let y=0;y<128;y++)for(let x=0;x<64;x++){const i=y*257+1+x*4;pixels[i]=red;pixels[i+1]=80;pixels[i+3]=255}
 return Buffer.concat([Buffer.from([137,80,78,71,13,10,26,10]),chunk('IHDR',header),chunk('IDAT',require('node:zlib').deflateSync(pixels)),chunk('IEND',Buffer.alloc(0))]);
}
for(const name of fs.readdirSync(root).filter(s=>s.startsWith('admin_')&&s.endsWith('.js')))routes['/'+name.slice(6).replaceAll('_','-')]=name;
const source={boss_id:11,group_id:700000010,name:'测试2D模板',battle_model:'2d',difficulty:'超级',targets:[0,1].flatMap(w=>[0,1].map(i=>({battle_index:w,enemy_index:i,name:i?'左翼':'本体',static_image_id:10002013,stats:{enemy_id:i+1,attribute:'FIRE',hp:100000,attack:1000,magic:1000,recovery:0,defense:300,magic_defense:500,damage_reduction:0,attribute_fixed:[0,0,0,0,0]}})))};
const threeD={...source,boss_id:12,name:'测试3D模板',battle_model:'3d',targets:source.targets.map(t=>({...t,static_image_id:0}))};
let revision=0,bosses=[{...threeD,boss_id:890000002,source_boss_id:12,name:'已制作3D',bp_use:20,bp_use_half:10}],conflict=false,exportCount=0;const writes=[];
const server=http.createServer(async(req,res)=>{
 try{
  const url=new URL(req.url,'http://local');
  if(routes[url.pathname]){res.setHeader('Content-Type',url.pathname.endsWith('.js')?'text/javascript':url.pathname.endsWith('.css')?'text/css':'text/html');res.end(fs.readFileSync(path.join(root,routes[url.pathname])));return}
  let raw='';for await(const chunk of req)raw+=chunk;let data={state:'PASS'};
  if(url.pathname==='/api/status')data={state:'PASS',accounts:{bound:1,guests:0,system:0},maintenance:{enabled:false},publication:{mode:'all'},server_time_utc:new Date().toISOString()};
  if(url.pathname==='/api/catalog')data={state:'PASS',entries:[],total:0};
  if(url.pathname==='/api/boss-policy')data={state:'PASS',publication:{mode:'all',group_ids:[],revision:0}};
  if(url.pathname==='/api/boss-groups')data={state:'PASS',groups:[]};
  if(url.pathname==='/api/custom-boss-buffs')data={state:'PASS',buffs:[]};
  if(url.pathname==='/api/custom-bosses/export'){
   assert.equal(req.headers['x-kairisei-admin-action'],'apply');const input=JSON.parse(raw);assert.equal(input.expected_revision,revision);assert.equal(input.boss_id,890000001);exportCount++;
   res.setHeader('Content-Type','application/zip');res.end(Buffer.from('PK test download'));return;
  }
  if(url.pathname==='/api/custom-bosses'){
   if(req.method==='GET')data={state:'PASS',revision,sources:[threeD,source],bosses,appearance_current:{890000001:false}};
   else{
    const input=JSON.parse(raw);assert.equal(req.headers['x-kairisei-admin-action'],'apply');
    if(conflict||input.expected_revision!==revision){res.statusCode=409;data={state:'FAIL',error:'conflict'}}
    else if(req.method==='POST'){
     assert.equal(input.source_boss_id,11);const b={boss_id:890000001,group_id:790000001,source_boss_id:11,name:'测试2D模板（自定义）',difficulty:'超级',bp_use:20,bp_use_half:10,continue:true,enabled:false,targets:structuredClone(source.targets)};bosses=[b];data={state:'PASS',revision:++revision,boss:b,appearance_current:true};
    }else{writes.push(input);const b={...bosses[0],...input};delete b.expected_revision;bosses=[b];data={state:'PASS',revision:++revision,boss:b,appearance_current:false}}
   }
  }
  res.setHeader('Content-Type','application/json');res.end(JSON.stringify(data));
 }catch(err){res.statusCode=500;res.end(JSON.stringify({error:err.message}))}
});
(async()=>{
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 const browser=await chromium.launch({executablePath:process.env.CHROME_PATH||'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true});
 const page=await browser.newPage({viewport:{width:1440,height:1000}}),errors=[];page.on('pageerror',e=>{errors.push(e.message);console.error(e.message)});
 const screenshotDir=process.env.CUSTOM_BOSS_SCREENSHOTS; if(screenshotDir)fs.mkdirSync(screenshotDir,{recursive:true});
 try{
  await page.goto(`http://127.0.0.1:${server.address().port}/admin#custom-bosses`);await page.waitForFunction(()=>state.loaded.has('custom-bosses'));
  assert.equal(await page.locator('#custom-boss-list [data-edit-custom-boss]').count(),0);
  await page.locator('[data-view="custom-bosses-3d"]').click();
  assert.equal(await page.locator('#view-title').innerText(),'自制3DBOSS');
  assert.equal(await page.locator('#custom-boss-list [data-edit-custom-boss]').count(),1);
  assert.equal(await page.locator('#custom-boss-layout').isVisible(),false);
  assert.equal(await page.locator('#custom-boss-cover').isVisible(),false);
  assert.equal(await page.locator('#cb-tab-appearance').innerText(),'02 部位名称');
  await page.locator('#cb-tab-appearance').click();
  assert.equal(await page.locator('#custom-boss-targets [data-part-artwork]').count(),0);
  assert.equal(await page.locator('#custom-boss-targets .cb-artwork-preview').count(),0);
  assert.doesNotMatch(await page.locator('#custom-boss-part-scope').innerText(),/PNG|2D|图片排列|模板拼接|独立排列|选关封面/);
  assert.equal(await page.locator('#cb-0-name').inputValue(),'本体');
  assert.equal(await page.locator('[data-boss-export]').first().innerText(),'生成部位名称更新包');
  await page.locator('#cb-tab-stats').click();
  assert.equal(await page.locator('#cb-0-hp').isVisible(),true);

  await page.locator('#custom-boss-open').click();
  assert.equal(await page.locator('#custom-boss-source option').count(),1);
  assert.equal(await page.locator('#custom-boss-source').inputValue(),'12');
  await page.reload();await page.waitForFunction(()=>state.loaded.has('custom-bosses'));
  assert.equal(await page.locator('#view-title').innerText(),'自制3DBOSS');
  await page.evaluate(()=>openCustomBossEditor(11));
  assert.equal(await page.locator('#view-title').innerText(),'自制2DBOSS');
  assert.equal(await page.locator('#custom-boss-source').inputValue(),'11');
  await page.locator('#custom-boss-search').fill('2D');
  assert.equal(await page.locator('#custom-boss-source option').count(),1);assert.match(await page.locator('#custom-boss-source').innerText(),/2D 可换图/);
  await page.locator('#custom-boss-create').click();await page.waitForFunction(()=>customBossEditor.draft&&!customBossEditor.busy);
  await page.locator('#cb-tab-appearance').click();assert.equal(await page.locator('#cb-0-name').inputValue(),'本体');
  await page.locator('#cb-0-name').fill('机械核心');
  await page.locator('[data-view="custom-bosses-3d"]').click();
  assert.equal(await page.locator('#view-title').innerText(),'自制2DBOSS');
  assert.equal(await page.locator('#cb-0-name').inputValue(),'机械核心');
  assert.equal(await page.evaluate(()=>customBossEditor.dirty),true);
  await page.locator('#cb-0-artwork').setInputFiles({name:'invalid.png',mimeType:'image/png',buffer:Buffer.from('not a png')});await page.waitForFunction(()=>!customBossEditor.busy);
  assert.match(await page.locator('#custom-boss-error').innerText(),/PNG格式/);
  const art=process.env.CUSTOM_BOSS_TEST_PNG?fs.readFileSync(process.env.CUSTOM_BOSS_TEST_PNG):testPNG();
  await page.locator('#cb-0-artwork').setInputFiles({name:'custom-boss.png',mimeType:'image/png',buffer:art});await page.waitForFunction(()=>!customBossEditor.busy);
  assert.equal(await page.locator('#cb-0-preview img').count(),1);assert.equal(await page.locator('[data-boss-export]').first().isDisabled(),true);
  assert.equal(await page.locator('#custom-boss-cover-preview img').getAttribute('src'),'data:image/png;base64,'+art.toString('base64'));
  assert.equal(await page.locator('#cb-0-transform-x').isEnabled(),true);
  await page.locator('#cb-0-transform-x').fill('12');await page.locator('#cb-0-transform-y').fill('-8');await page.locator('#cb-0-transform-scale').fill('75');
  assert.deepEqual(await page.evaluate(()=>customBossEditor.draft.targets[0].artwork_transform),{x:12,y:-8,scale:75});
  assert.match(await page.locator('#cb-0-preview img').getAttribute('style'),/translate\(12%,-8%\) scale\(0.75\)/);
  assert.equal(await page.locator('[data-wave-composite]').first().locator('img').count(),1);
  await page.locator('[data-selection-settings]').first().click();await page.locator('#custom-boss-name').fill('自制机械试炼');
  assert.equal(await page.locator('#custom-boss-selection-title').innerText(),'自制机械试炼');
  await page.locator('#custom-boss-layout').selectOption('separate');
  await page.locator('#custom-boss-layout').selectOption('template');
  const cover=testPNG(40);await page.locator('#custom-boss-cover').setInputFiles({name:'cover.png',mimeType:'image/png',buffer:cover});await page.waitForFunction(()=>!customBossEditor.busy);
  assert.equal(await page.locator('#custom-boss-cover-preview img').getAttribute('src'),'data:image/png;base64,'+cover.toString('base64'));
  await page.locator('#cb-tab-appearance').click();assert.match(await page.locator('[data-selection-name]').first().innerText(),/自制机械试炼/);
  await page.locator('[data-boss-wave="1"]').click();assert.equal(await page.locator('#cb-2-name').inputValue(),'本体');assert.equal(await page.locator('#cb-2-preview img').count(),0);
  await page.locator('#cb-2-name').fill('觉醒核心');await page.locator('[data-boss-wave="0"]').click();assert.equal(await page.locator('#cb-0-name').inputValue(),'机械核心');
  await page.locator('#custom-boss-save').click();await page.waitForFunction(()=>!customBossEditor.busy&&!customBossEditor.dirty);
  assert.equal(writes[0].targets[0].name,'机械核心');assert.equal(writes[0].targets[2].name,'觉醒核心');assert.equal(writes[0].targets[0].artwork,art.toString('base64'));assert.equal(source.targets[0].name,'本体');
  assert.deepEqual(writes[0].targets[0].artwork_transform,{x:12,y:-8,scale:75});
  assert.equal(writes[0].name,'自制机械试炼');assert.equal(writes[0].artwork_layout,'template');assert.equal(writes[0].cover_artwork,cover.toString('base64'));
  await page.locator('[data-view="custom-bosses-3d"]').click();
  assert.equal(await page.locator('#custom-boss-list [data-edit-custom-boss="890000001"]').count(),0);
  await page.evaluate(()=>openCustomBossEditor(0,890000001));
  assert.equal(await page.locator('#view-title').innerText(),'自制2DBOSS');
  assert.equal(await page.locator('#custom-boss-layout').isVisible(),true);
  await page.locator('#cb-tab-appearance').click();
  const download=page.waitForEvent('download');await page.locator('[data-boss-export]').first().click();await download;await page.waitForFunction(()=>!customBossEditor.busy);assert.equal(exportCount,1);
  if(screenshotDir)await page.screenshot({path:path.join(screenshotDir,'desktop.png'),fullPage:true});
  await page.reload();await page.waitForFunction(()=>state.loaded.has('custom-bosses'));await page.locator('#cb-tab-appearance').click();assert.equal(await page.locator('#cb-0-name').inputValue(),'机械核心');assert.equal(await page.locator('#cb-0-preview img').count(),1);
  assert.equal(await page.locator('#cb-0-transform-x').inputValue(),'12');
  conflict=true;await page.locator('#cb-0-name').fill('冲突时保留设计');await page.locator('#custom-boss-save').click();await page.waitForFunction(()=>!customBossEditor.busy);assert.equal(await page.evaluate(()=>customBossEditor.dirty),true);assert.equal(await page.locator('#cb-0-name').inputValue(),'冲突时保留设计');
  await page.locator('#custom-boss-reset').click();await page.locator('#custom-boss-reset').click();assert.equal(await page.locator('#cb-0-name').inputValue(),'机械核心');
  await page.setViewportSize({width:390,height:844});await page.locator('#cb-tab-appearance').click();assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
  if(screenshotDir)await page.screenshot({path:path.join(screenshotDir,'mobile.png'),fullPage:true});
  await page.locator('[data-transform-reset="0"]').click();assert.equal(await page.locator('#cb-0-transform-scale').inputValue(),'100');assert.equal(await page.evaluate(()=>customBossEditor.draft.targets[0].artwork_transform),undefined);
  await page.locator('[data-part-artwork-reset="0"]').click();assert.equal(await page.locator('#cb-0-transform-x').isDisabled(),true);assert.equal(await page.locator('#cb-0-preview img').count(),0);assert.equal(await page.evaluate(()=>customBossEditor.dirty),true);
  assert.equal(await page.locator('#custom-boss-cover-preview img').getAttribute('src'),'data:image/png;base64,'+cover.toString('base64'));
  await page.locator('#cb-tab-basic').click();assert.equal(await page.locator('#custom-boss-layout').inputValue(),'template');
  await page.locator('#custom-boss-cover-reset').click();assert.equal(await page.locator('#custom-boss-cover-preview img').count(),0);
  assert.deepEqual(errors,[]);console.log('PASS: selection name, automatic and explicit cover, layout selection, PNG validation, independent waves, save/reload, export, conflicts, restore and mobile layout');
 }finally{await browser.close();server.close()}
})().catch(err=>{console.error(err);process.exitCode=1;server.close()});

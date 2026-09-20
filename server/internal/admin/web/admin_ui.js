'use strict';
const cardJobName=job=>({0:'通用',1:'佣兵',2:'富豪',3:'盗贼',4:'歌姬'}[job??0]||'');
const cardMatchesJob=(card,job)=>job===0||(card.arthur_type??0)===(job===-1?0:job);
const cardStars=card=>card.rarity?`${card.rarity} 星`:'';
const cardSourceDescription=card=>cardFacts(card);
const state={loading:new Set(),publishing:new Set(),status:null,accounts:[],groups:[],policy:null,gachaPresets:[],gachaPolicy:null,gachaSelected:new Set(),audit:[],selected:new Set(),mode:'all',grantUser:null,catalog:[],catalogKind:'card',catalogSource:'',catalogJob:0,catalogRarity:0,catalogTotal:0,catalogPage:0,catalogRequest:0,mailReward:null,loaded:new Set(),currentView:'dashboard',bossCatalog:'activity',bossKind:'all',bossPage:0};
const titles={'player-policy':'公告与奖励',drops:'Boss 掉落',exchanges:'兑换所配置','pool-editor':'卡池配置',dashboard:'运行概览',accounts:'账号管理',mail:'礼物发放',bosses:'Boss 发布',gachas:'扭蛋发布',audit:'操作审计',settings:'运营设置'};
const $=s=>document.querySelector(s), $$=s=>[...document.querySelectorAll(s)];
const esc=v=>String(v??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const num=v=>Number(v||0).toLocaleString('zh-CN');
async function api(path,options={}){const opt={...options,headers:{...(options.headers||{})}};if(opt.body){opt.headers['Content-Type']='application/json';opt.headers['X-Kairisei-Admin-Action']='apply'}const r=await fetch(path,opt);let data;try{data=await r.json()}catch{throw new Error('后台返回了无效 JSON')}if(!r.ok||data.state==='FAIL')throw new Error(data.error||`HTTP ${r.status}`);return data}
function toast(message,error=false){const el=$('#toast');el.textContent=message;el.className='toast show'+(error?' error':'');clearTimeout(toast.timer);toast.timer=setTimeout(()=>el.className='toast',3500)}
async function switchView(name){state.currentView=name;$$('.view').forEach(v=>v.classList.toggle('active',v.id===name));$$('.nav button').forEach(v=>v.classList.toggle('active',v.dataset.view===name));$('#view-title').textContent=titles[name]||name;updatePolicyControls();await loadView(name)}
async function loadView(name,force=false){
  if(policyBusy(name)||(state.loaded.has(name)&&!force))return;
  if(force&&adminPolicyDirty(name)&&!confirm('当前页面尚有未保存的修改，刷新会放弃修改。是否继续？'))return;
  state.loading.add(name);updatePolicyControls();
  try{
    if(name==='player-policy')await loadPlayerPolicy();
    else if(name==='dashboard'){state.status=await api('/api/status');renderDashboard()}
    else if(name==='accounts')await loadAccountsPage();
    else if(name==='mail')await loadMailWorkspace();
    else if(name==='bosses')await loadBossPublication(state.bossCatalog);
    else if(name==='gachas'){
      const [presets,policy]=await Promise.all([api('/api/gacha-presets'),api('/api/gacha-policy')]);
      state.gachaPresets=presets.presets;state.gachaPolicy=policy.publication;
      state.gachaSelected=new Set(state.gachaPolicy.group_ids||[]);renderGachas();
    }
    else if(name==='pool-editor')await loadPoolEditor();
    else if(name==='audit')await loadAuditPage();
    else if(name==='settings')await loadRuntimeSettings();
    else if(name==='drops')await loadDropEditor();
    else if(name==='exchanges')await loadExchangeEditor();
    state.loaded.add(name);if(force)toast('当前页面已刷新');
  }catch(e){toast(e.message,true)}
  finally{state.loading.delete(name);updatePolicyControls()}
}
function renderDashboard(){const s=state.status,p=s.publication;$('#m-state').textContent=s.state;$('#m-endpoint').textContent=s.game_endpoint;$('#m-accounts').textContent=num(s.account_count);$('#m-bosses').textContent=num(s.boss_count);$('#m-groups').textContent=`${num(s.boss_group_count)} 个目录组`;$('#m-rooms').textContent=num(s.active_rooms);$('#deployment-facts').innerHTML=[['客户端 Profile',s.client_profile],['游戏 API',s.game_endpoint],['BattleSv 端口',s.battle_port],['SQLite schema',s.sqlite_schema_version],['服务端 UTC',s.server_time_utc]].map(([a,b])=>`<div class="fact"><span>${esc(a)}</span><code>${esc(b)}</code></div>`).join('');const now=Date.parse(s.server_time_utc)/1000;
  const scheduled=now<(p.start_unix||0), expired=p.end_unix&&now>=p.end_unix;
  const selection=p.mode==='all'?`${s.boss_group_count} 个目录组`:`${(p.group_ids||[]).length} 个选中目录组`;
  $('#policy-pill').textContent=scheduled?'尚未开始':expired?'排期已结束':p.mode==='all'?'全部开放':`开放 ${(p.group_ids||[]).length} 组`;
  $('#policy-copy').textContent=`${scheduled?'等待排期开始':expired?'排期已结束，当前不发布活动 BOSS':'当前发布 '+selection}。此处为活动／素材目录；往期 BOSS 请在发布页的独立 Tab 查看。`;
  renderInsights();
}
function renderPager(key,page,total,size,onPage){
  const pages=Math.max(1,Math.ceil(total/size));page=Math.max(0,Math.min(page,pages-1));
  $(`#${key}-page-info`).textContent=`${page+1} / ${pages} 页 · 共 ${total} 项`;
  const input=$(`#${key}-page`);input.value=page+1;input.max=pages;
  $(`#${key}-prev`).disabled=page===0;$(`#${key}-next`).disabled=page===pages-1;
  $(`#${key}-prev`).onclick=()=>onPage(page-1);$(`#${key}-next`).onclick=()=>onPage(page+1);
  input.onchange=()=>{const next=Number(input.value);if(Number.isInteger(next)&&next>=1&&next<=pages)onPage(next-1);else input.value=page+1};
  return page;
}
async function loadBossPublication(catalog){
  const [groups,policy]=await Promise.all([api('/api/boss-groups?catalog='+catalog),api('/api/boss-policy?catalog='+catalog)]);
  state.bossCatalog=catalog;state.groups=groups.groups;state.policy=policy.publication;state.mode=state.policy.mode;state.selected=new Set(state.policy.group_ids||[]);
  $('#boss-start').value=localTimeInput(state.policy.start_unix);$('#boss-end').value=localTimeInput(state.policy.end_unix);updateBossDifficultyFilter();state.bossPage=0;renderBosses();
}
$$('#boss-catalogs button').forEach(b=>b.onclick=async()=>{
  const catalog=b.dataset.catalog;if(catalog===state.bossCatalog||policyBusy('bosses'))return;
  if(adminPolicyDirty('bosses')&&!confirm('当前目录有未保存的修改，切换将放弃这些修改。是否继续？'))return;
  state.loading.add('bosses');updatePolicyControls();
  try{await loadBossPublication(catalog)}catch(e){toast(e.message,true)}finally{state.loading.delete('bosses');updatePolicyControls()}
});
function filteredGroups(){const q=$('#boss-search').value.trim();return state.groups.filter(g=>(state.bossKind==='all'||g.category===state.bossKind)&&bossMatchesFilters(g)&&matchesWords(`${g.name} ${g.past_name||''} ${g.group_id} ${g.picture_id} ${g.boss_ids.join(' ')} ${(g.difficulties||[]).join(' ')}`,q))}
function retryImage(img){const attempt=Number(img.dataset.retry||0);if(attempt<2){img.dataset.retry=String(attempt+1);const url=new URL(img.src,location.href);url.searchParams.set('_retry',`${attempt+1}-${Date.now()}`);setTimeout(()=>img.src=url.pathname+url.search,150*(attempt+1));return}img.dataset.failed='true';img.hidden=true}
function image(url,label){return url?`<img src="${esc(url)}" alt="" loading="lazy" data-retry="0" onerror="retryImage(this)"><span>${esc((label||'?').slice(0,1))}</span>`:`<span>${esc((label||'?').slice(0,1))}</span>`}
function renderBosses(){$$('#boss-catalogs button').forEach(b=>b.classList.toggle('active',b.dataset.catalog===state.bossCatalog));const rows=filteredGroups();state.bossPage=renderPager('boss',state.bossPage,rows.length,120,page=>{state.bossPage=page;renderBosses()});const visible=rows.slice(state.bossPage*120,(state.bossPage+1)*120);$$('#boss-kinds button').forEach(b=>b.classList.toggle('active',b.dataset.kind===state.bossKind));$('#mode-all').classList.toggle('active',state.mode==='all');$('#mode-selected').classList.toggle('active',state.mode==='allowlist');$('#boss-revision').textContent=`revision ${state.policy?.revision||0}`;$('#boss-summary').textContent=`匹配 ${rows.length}/${state.groups.length} 组 · 本页 ${visible.length} 组 · 已选 ${state.selected.size} 组`;$('#save-policy').disabled=policyBusy('bosses')||!state.policy;$('#boss-rows').innerHTML=visible.map(g=>`<div class="boss-card ${state.selected.has(g.group_id)?'selected':''}"><label class="boss-select"><input class="check boss-check" type="checkbox" data-id="${g.group_id}" ${state.selected.has(g.group_id)?'checked':''} ${state.mode==='all'?'disabled':''}></label><div class="boss-art">${image(g.image_url,g.name)}</div><div class="boss-copy"><span class="name">${esc(g.name)}</span><code>${g.group_id}</code><button class="secondary" data-boss-detail="${g.group_id}">各难度规则与属性</button>${g.past_name?`<span class="sub">${esc(g.past_name)}</span>`:''}<span class="sub">${g.category==='material'?'素材副本':g.category==='3d'?'3D Boss':'2D Boss'} · ${g.bosses?.length||g.boss_count} 个难度 · 最多 ${g.max_segments||1} 波</span><span class="sub">${esc((g.difficulties||[]).join(' / ')||'未标注难度')}</span><span class="sub">${esc(g.boss_ids.slice(0,4).join(', '))}${g.boss_ids.length>4?'…':''}</span></div></div>`).join('')||'<div class="empty">没有匹配 Boss 组</div>';$$('.boss-check').forEach(c=>c.onchange=()=>{const id=Number(c.dataset.id);c.checked?state.selected.add(id):state.selected.delete(id);renderBosses()})}
function renderGachas(){const rows=filteredGachas();$('#gacha-revision').textContent=`revision ${state.gachaPolicy?.revision||0}`;$('#gacha-summary').textContent=`匹配 ${rows.length}/${state.gachaPresets.length} 个常规卡池组 · 已开放 ${state.gachaSelected.size} 个`;$('#gacha-rows').innerHTML=rows.map(g=>`<div class="gacha-card ${state.gachaSelected.has(g.group_id)?'selected':''}"><label class="gacha-select"><input class="check gacha-check" type="checkbox" data-id="${g.group_id}" ${state.gachaSelected.has(g.group_id)?'checked':''}></label><div class="gacha-art">${image(g.image_url,g.name)}</div><div class="gacha-copy"><span class="name">${esc(g.name)}</span><code>Group ${g.group_id}</code><span class="sub">${esc(g.payment_item||`物品 ${g.payment_item_id}`)} × ${g.price} · 每次 ${g.draw_count} 抽</span><span class="sub">卡池 ${g.card_count} 张 · Gacha ${esc(g.gacha_ids.join(', '))}</span></div></div>`).join('')||'<div class="empty">暂无活动扭蛋预设</div>';$$('.gacha-check').forEach(c=>c.onchange=()=>{const id=Number(c.dataset.id);c.checked?state.gachaSelected.add(id):state.gachaSelected.delete(id);renderGachas()})}
$$('.nav button').forEach(b=>b.onclick=()=>switchView(b.dataset.view));$$('[data-jump]').forEach(b=>b.onclick=()=>switchView(b.dataset.jump));$('#refresh').onclick=()=>loadView(state.currentView,true);$('#boss-search').oninput=()=>{state.bossPage=0;renderBosses()};
$$('#boss-kinds button').forEach(b=>b.onclick=()=>{state.bossKind=b.dataset.kind;state.bossPage=0;renderBosses()});
$('#mode-all').onclick=()=>{state.mode='all';renderBosses()};$('#mode-selected').onclick=()=>{state.mode='allowlist';renderBosses()};$('#select-visible').onclick=()=>{filteredGroups().forEach(g=>state.selected.add(g.group_id));state.mode='allowlist';renderBosses()};$('#clear-selected').onclick=()=>{state.selected.clear();state.mode='allowlist';renderBosses()};
function policyBusy(name){return state.loading.has(name)||state.publishing.has(name)}
function updatePolicyControls(){
  for(const name of ['bosses','gachas','pool-editor','settings','drops','exchanges','player-policy'])$('#'+name).inert=policyBusy(name);
  if(typeof updatePoolControls==='function')updatePoolControls();
  $('#save-policy').disabled=policyBusy('bosses')||!state.policy;
  $('#gacha-save').disabled=policyBusy('gachas')||!state.gachaPolicy;
  $('#refresh').disabled=policyBusy(state.currentView);
  if(typeof updateDraftIndicators==='function')updateDraftIndicators();
}
$('#save-policy').onclick=async()=>{
  if(policyBusy('bosses')||!state.policy)return;

  const body={mode:state.mode,group_ids:state.mode==='all'?[]:[...state.selected],start_unix:unixInput('#boss-start'),end_unix:unixInput('#boss-end'),expected_revision:state.policy.revision};
  if(!confirm(`${state.bossCatalog==='past'?'往期 BOSS':'活动／素材副本'}发布预览：${body.mode==='all'?'全部开放':`仅开放 ${body.group_ids.length} 组`}\n开始：${describeTime(body.start_unix)}\n结束：${describeTime(body.end_unix)}\n确认发布目录排期？`))return;
  state.publishing.add('bosses');updatePolicyControls();
  try{
    const data=await api('/api/boss-policy?catalog='+state.bossCatalog,{method:'PUT',body:JSON.stringify(body)});
    state.policy=data.publication;state.mode=state.policy.mode;state.selected=new Set(state.policy.group_ids||[]);
    state.loaded.delete('dashboard');state.loaded.delete('audit');renderBosses();toast('Boss 发布设置已保存，按排期开放');
  }catch(e){toast(e.message,true)}finally{state.publishing.delete('bosses');updatePolicyControls()}
};
$('#gacha-select-all').onclick=()=>{state.gachaSelected=new Set(state.gachaPresets.map(g=>g.group_id));renderGachas()};
$('#gacha-clear-all').onclick=()=>{state.gachaSelected.clear();renderGachas()};
$('#gacha-save').onclick=async()=>{
  if(policyBusy('gachas')||!state.gachaPolicy)return;
  const body={group_ids:[...state.gachaSelected].sort((a,b)=>a-b),expected_revision:state.gachaPolicy.revision};
  if(!confirm(`确认即时发布 ${body.group_ids.length} 个常规卡池组？`))return;
  state.publishing.add('gachas');updatePolicyControls();
  try{
    const data=await api('/api/gacha-policy',{method:'PUT',body:JSON.stringify(body)});
    state.gachaPolicy=data.publication;state.gachaSelected=new Set(state.gachaPolicy.group_ids||[]);
    state.loaded.delete('audit');renderGachas();toast('扭蛋发布策略已即时生效');
  }catch(e){toast(e.message,true)}finally{state.publishing.delete('gachas');updatePolicyControls()}
};
document.addEventListener('DOMContentLoaded',()=>loadView('dashboard'));

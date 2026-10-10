'use strict';
const cardJobName=job=>({0:'通用',1:'佣兵',2:'富豪',3:'盗贼',4:'歌姬'}[job??0]||'');
const cardMatchesJob=(card,job)=>job===0||(card.arthur_type??0)===(job===-1?0:job);
const cardStars=card=>card.rarity?`${card.rarity} 星`:'';
const cardSourceDescription=card=>cardFacts(card);
const state={loading:new Set(),publishing:new Set(),status:null,accounts:[],groups:[],policy:null,gachaPresets:[],gachaPolicy:null,gachaSelected:new Set(),audit:[],selected:new Set(),mode:'all',grantUser:null,catalog:[],catalogKind:'card',catalogSource:'',catalogJob:0,catalogRarity:0,catalogTotal:0,catalogPage:0,catalogRequest:0,mailReward:null,loaded:new Set(),currentView:'dashboard',bossCatalog:'activity',bossKind:'all',bossPage:0};
const titles={maintenance:'维护工具',evolution:'卡牌进化链','activity-rewards':'圣剑杯与探索','player-policy':'公告与奖励',drops:'Boss 掉落',exchanges:'兑换所配置',shop:'道具商店','pool-editor':'卡池配置',dashboard:'运行概览',accounts:'账号管理',mail:'礼物发放',bosses:'Boss 发布',gachas:'扭蛋发布',audit:'操作审计',settings:'运营设置'};
titles.collections="称号与礼盒";titles.cdk="礼包兑换码";titles.missions="任务管理";
titles['custom-cards']='自制卡牌';titles['dungeon-schedule']='副本日程表';
titles['custom-bosses']='自定义 Boss';
const viewMeta={
  'custom-cards':['扭蛋与商店','复制卡牌、上传卡面、组合技能效果并生成资源更新包'],
  missions:['系统','每日与成就任务、完成条件及多种奖励'],
 collections:["系统","称号、礼盒与开箱奖励"],cdk:["玩家","创建礼包码、查看兑换记录与发奖"],
  evolution:['扭蛋与商店','按星级查看进化链，独立控制每条进化方向的开放状态'],
  dashboard:['概览','服务状态、玩家活跃、待处理事项与组队房间'],
  accounts:['玩家','筛选玩家、查看存档、调整资源与绑定，跨页选择收件人'],
  mail:['玩家','选择收件人与奖励，预览后按批次发放；中断后可从批次继续'],
  bosses:['战斗与活动','活动／往期目录开放名单、每组日期与每周排期，以及各难度入口规则'],
  'custom-bosses':['战斗与活动','复制 Boss，配置回合与出招顺序、调整伤害、借用玩家 Buff 招式'],
  'dungeon-schedule':['战斗与活动','编辑游戏内日程表的标题、说明和备注，自动匹配已发布 Boss 与开放排期'],
  drops:['战斗与活动','按难度配置怪物／部位掉落与名声奖励，新开战生效'],
  'activity-rewards':['战斗与活动','圣剑杯九档固定奖励、回合与倍率，探索逐项概率奖励'],
  'pool-editor':['扭蛋与商店','编辑卡池内容与价格 → 保存草稿并预览 → 发布'],
  gachas:['扭蛋与商店','常规卡池组的开放开关，保存后即时生效'],
  exchanges:['扭蛋与商店','兑换所与商品、价格、限兑；删除可恢复且保留兑换记录'],
  shop:['扭蛋与商店','道具、表情与扩容商品的上架、价格、页签和限购'],
  'player-policy':['系统','公告、签到、新手毕业邮件、剧情首通与看板购买'],
  settings:['系统','水晶购买、组队倍速与卡池封面来源'],
  maintenance:['系统','维护开关，以及需在维护中执行的状态、审计和卡池清理'],
  audit:['系统','全部写操作的事务审计记录，可按类型、时间与内容检索']
};
const $=s=>document.querySelector(s), $$=s=>[...document.querySelectorAll(s)];
const esc=v=>String(v??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const num=v=>Number(v||0).toLocaleString('zh-CN');
async function api(path,options={}){
  const opt={...options,headers:{...(options.headers||{})}};
  if(opt.body){opt.headers['Content-Type']='application/json';opt.headers['X-Kairisei-Admin-Action']='apply'}
  let r;
  try{r=await fetch(path,opt)}catch{const e=new Error('无法连接后台服务：网络中断或服务已停止');e.status=0;throw e}
  let data;
  try{data=await r.json()}catch{const e=new Error(`后台返回了无效 JSON（HTTP ${r.status}）`);e.status=r.status;throw e}
  if(!r.ok||data.state==='FAIL'){const e=new Error(data.error||`HTTP ${r.status}`);e.status=r.status;throw e}
  return data;
}
function toast(message,error=false){
  const stack=$('#toast-stack'),same=[...stack.children].find(el=>el.querySelector('.toast-text')?.textContent===message);
  if(same){clearTimeout(same.timer);same.timer=setTimeout(()=>same.remove(),error?9000:3500);return}
  const item=document.createElement('div');
  item.className='toast-item'+(error?' error':'');item.setAttribute('role',error?'alert':'status');
  item.innerHTML=`<svg class="icon" aria-hidden="true"><use href="#i-${error?'alert':'check'}"/></svg><span class="toast-text"></span><button class="x-button" type="button" aria-label="关闭提示"><svg class="icon"><use href="#i-close"/></svg></button>`;
  item.querySelector('.toast-text').textContent=message;
  const close=()=>item.remove();item.querySelector('button').onclick=close;
  stack.append(item);while(stack.children.length>3)stack.firstElementChild.remove();
  item.timer=setTimeout(close,error?9000:3500);
}
const conflictHint='配置已被其他页面或操作更新（版本冲突）。当前草稿仍保留在页面上，不会被强制覆盖。';
function errorText(e){return e?.status===409?conflictHint:e?.status===0?e.message:(e?.message||String(e))}
const draftExporters={'activity-rewards':'#activity-export',drops:'#drop-export',exchanges:'#exchange-export','pool-editor':'#pool-export','dungeon-schedule':'#dungeon-schedule-export'};
function viewAlert(view){return $(`#${view} > .view-alert`)}
function clearViewAlert(view){const el=viewAlert(view);if(el){el.hidden=true;el.innerHTML=''}}
function showViewAlert(view,{kind='error',title,message,actions=[]}){
  const el=viewAlert(view);if(!el)return;
  el.className='view-alert'+(kind==='error'?'':' '+kind);el.hidden=false;
  el.innerHTML=`<svg class="icon" aria-hidden="true"><use href="#i-${kind==='info'?'info':'alert'}"/></svg><div class="alert-text"><b></b><span></span></div><div class="alert-actions"></div><button class="x-button" type="button" aria-label="关闭提示"><svg class="icon"><use href="#i-close"/></svg></button>`;
  el.querySelector('b').textContent=title;el.querySelector('.alert-text span').textContent=message||'';
  for(const a of actions){const b=document.createElement('button');b.type='button';b.className=(a.primary?'primary':'secondary')+' sm';b.textContent=a.label;b.onclick=a.onClick;el.querySelector('.alert-actions').append(b)}
  el.querySelector('.x-button').onclick=()=>clearViewAlert(view);
  // Bring the problem into view: the first highlighted field, otherwise the alert itself.
  if(view===state.currentView){const target=$(`#${view} .invalid`)||el,r=target.getBoundingClientRect();if(r.top<80||r.bottom>innerHeight-70)target.scrollIntoView({block:target===el?'start':'center',behavior:'smooth'})}
}
// Save/load failures stay next to the work; 409 never becomes a forced overwrite.
function reportError(view,e,what='保存'){
  toast(errorText(e),true);
  if(!view||!viewAlert(view))return;
  if(e?.status===409){
    const actions=[];
    if(draftExporters[view])actions.push({label:'导出当前草稿',onClick:()=>$(draftExporters[view]).click()});
    actions.push({label:'重新载入最新版本',primary:true,onClick:()=>loadView(view,true)});
    showViewAlert(view,{kind:'warn',title:`${what}未完成：版本冲突`,message:conflictHint+(draftExporters[view]?'可先导出草稿留底，':'请记下需要的修改，')+'再重新载入最新版本后重做修改。',actions});
  }else showViewAlert(view,{title:`${what}失败`,message:errorText(e)+(e?.status===0?'；恢复连接后可重试。':''),actions:what==='载入'?[{label:'重试',primary:true,onClick:()=>loadView(view,true)}]:[]});
}
function setSaveState(id,text,kind=''){const el=$(id);if(!el)return;el.textContent=text;el.className=kind?`state-${kind}`:''}
function emptyRow(cols,title,hint=''){return `<tr><td colspan="${cols}" class="empty"><b>${esc(title)}</b>${hint?esc(hint):''}</td></tr>`}
function markInvalid(el,message){if(!el)return;el.classList.add('invalid');el.setAttribute('aria-invalid','true');if(message)el.title=message}
function clearInvalid(root){root.querySelectorAll('.invalid').forEach(el=>{el.classList.remove('invalid');el.removeAttribute('aria-invalid');el.removeAttribute('title')})}
// Loads every catalog page for the given filter; pages after the first run in parallel (server limit is 200 per page).
async function fetchAllCatalog(params={}){
  const size=200,query=o=>'/api/catalog?'+new URLSearchParams({...params,limit:size,offset:o});
  const first=await api(query(0)),rows=[...first.entries],offsets=[];
  for(let o=size;o<first.total;o+=size)offsets.push(o);
  const pages=new Array(offsets.length);let next=0;
  await Promise.all(Array.from({length:Math.min(6,offsets.length)},async()=>{while(next<offsets.length){const i=next++;pages[i]=(await api(query(offsets[i]))).entries}}));
  for(const page of pages)rows.push(...page);
  if(rows.length!==first.total)throw new Error('目录分页读取不完整，请刷新重试');
  return rows;
}
// Structured before/after review shown before a save. Resolves true only when the operator confirms.
function reviewChanges({title,lead='',sections=[],note='',confirmText='确认保存'}){
  const shown=sections.filter(s=>s.rows.length),total=shown.reduce((n,s)=>n+s.rows.length,0);
  const cell=v=>v===null||v===undefined||v===''?'<span class="none">—</span>':esc(v);
  $('#review-title').textContent=title;$('#review-lead').textContent=lead||(total?`共 ${total} 处变化，请逐项核对。`:'');
  $('#review-body').innerHTML=shown.map(s=>`<section class="review-section"><h3>${esc(s.title)}<span class="tag">${s.rows.length} 项</span></h3><div class="table-wrap"><table class="diff"><thead><tr><th>项目</th><th>修改前</th><th>修改后</th></tr></thead><tbody>${s.rows.slice(0,200).map(([label,before,after])=>`<tr><td>${esc(label)}</td><td class="before">${cell(before)}</td><td class="after">${cell(after)}</td></tr>`).join('')}${s.rows.length>200?`<tr><td colspan="3" class="hint">另有 ${s.rows.length-200} 项未列出</td></tr>`:''}</tbody></table></div></section>`).join('')||'<p class="review-empty">未检测到内容变化；仍可保存以刷新版本。</p>';
  $('#review-note').textContent=note;$('#review-confirm').textContent=confirmText;$('#review-confirm').disabled=false;
  $('#review-modal').classList.add('open');
  return new Promise(resolve=>{
    const done=ok=>{$('#review-confirm').onclick=$('#review-cancel').onclick=null;$('#review-modal').classList.remove('open');resolve(ok)};
    $('#review-confirm').onclick=()=>done(true);$('#review-cancel').onclick=()=>done(false);
  });
}
const diffText=v=>v===true?'是':v===false?'否':v===null||v===undefined?'':String(v);
function diffRows(pairs){return pairs.filter(([,a,b])=>diffText(a)!==diffText(b)).map(([label,a,b])=>[label,diffText(a),diffText(b)])}
// Items matched by identity (repeats counted), so a quantity or probability edit reads as one changed row.
function keyedDiff(label,before,after,key,text,name=key){
  const index=list=>{const seen=new Map();return new Map(list.map(x=>{const k=key(x),n=(seen.get(k)||0)+1;seen.set(k,n);return [`${k}#${n}`,x]}))};
  const a=index(before),b=index(after),rows=[];
  for(const [k,x] of a){const y=b.get(k);if(!y)rows.push([`${label}（移除）`,text(x),'']);else if(text(x)!==text(y))rows.push([`${label}「${name(y)}」`,text(x),text(y)])}
  for(const [k,y] of b)if(!a.has(k))rows.push([`${label}（新增）`,'',text(y)]);
  return rows;
}

async function switchView(name){
  if(!titles[name])name='dashboard';
  state.currentView=name;
  $$('.view').forEach(v=>v.classList.toggle('active',v.id===name));
  $$('.nav button').forEach(v=>{const on=v.dataset.view===name;v.classList.toggle('active',on);on?v.setAttribute('aria-current','page'):v.removeAttribute('aria-current')});
  $('#view-title').textContent=titles[name]||name;
  const [group,desc]=viewMeta[name]||['',''];$('#view-group').textContent=group;$('#view-desc').textContent=desc;
  document.title=`${titles[name]} · Kairisei MA 本地运营后台`;
  try{history.replaceState(null,'','#'+name)}catch{/* Deep links are optional. */}
  closeNavDrawer();window.scrollTo(0,0);
  if(name==='dungeon-schedule'&&state.loaded.has(name)&&!adminPolicyDirty(name))state.loaded.delete(name);
  updatePolicyControls();await loadView(name);
}
async function loadView(name,force=false){
  if(policyBusy(name)||(state.loaded.has(name)&&!force))return;
  if(force&&adminPolicyDirty(name)&&!confirm('当前页面尚有未保存的修改，刷新会放弃修改。是否继续？'))return;
  state.loading.add(name);updatePolicyControls();
  try{
    if(name==='evolution')await loadEvolutionEditor();
    else if(name==='activity-rewards')await loadActivityRewards();
    else if(name==='player-policy')await loadPlayerPolicy();
    else if(name==='missions')await loadMissions();
    else if(name==='dashboard'){state.status=await api('/api/status');renderDashboard();loadDashboardTodos()}
    else if(name==='accounts')await loadAccountsPage();
    else if(name==='mail')await loadMailWorkspace();
    else if(name==='cdk')await loadCDKWorkspace();
    else if(name==='bosses')await loadBossPublication(state.bossCatalog);
    else if(name==='custom-bosses')await loadCustomBossPage();
    else if(name==='dungeon-schedule')await loadDungeonSchedule();
    else if(name==='gachas'){
      const [presets,policy]=await Promise.all([api('/api/gacha-presets'),api('/api/gacha-policy')]);
      state.gachaPresets=presets.presets;state.gachaPolicy=policy.publication;
      state.gachaSelected=new Set(state.gachaPolicy.group_ids||[]);renderGachas();
    }
    else if(name==='pool-editor')await loadPoolEditor();
    else if(name==='collections')await loadCollections();
    else if(name==='custom-cards')await loadCustomCards();
    else if(name==='audit')await loadAuditPage();
    else if(name==='settings')await loadRuntimeSettings();
    else if(name==='shop')await loadItemShop();
    else if(name==='maintenance')await loadMaintenance();
    else if(name==='drops')await loadDropEditor();
    else if(name==='exchanges')await loadExchangeEditor();
    state.loaded.add(name);clearViewAlert(name);if(force)toast('当前页面已刷新');
  }catch(e){reportError(name,e,'载入')}
  finally{state.loading.delete(name);updatePolicyControls()}
}
// Maintenance pauses the game for every player: keep it visible on every page, not only on its own tool page.
let maintenanceBannerRevision=-1;
function setMaintenanceBanner(m){if(!m)return;const revision=Number(m.revision||0);if(revision<maintenanceBannerRevision)return;maintenanceBannerRevision=revision;const on=!!m.enabled;$('#maintenance-banner').hidden=!on;$('#nav-maintenance-flag').hidden=!on;document.body.classList.toggle('maintenance-on',on)}
function renderDashboard(){const s=state.status,p=s.publication;setMaintenanceBanner(s.maintenance);$('#m-state').textContent=s.state;$('#m-endpoint').textContent=s.game_endpoint;$('#m-accounts').textContent=num(s.account_count);$('#m-bosses').textContent=num(s.boss_count);$('#m-groups').textContent=`${num(s.boss_group_count)} 个目录组`;$('#m-rooms').textContent=num(s.active_rooms);$('#deployment-facts').innerHTML=[['客户端 Profile',s.client_profile],['游戏 API',s.game_endpoint],['BattleSv 端口',s.battle_port],['SQLite schema',s.sqlite_schema_version],['服务端 UTC',s.server_time_utc]].map(([a,b])=>`<div class="fact"><span>${esc(a)}</span><code>${esc(b)}</code></div>`).join('');const now=Date.parse(s.server_time_utc)/1000;
  const scheduled=now<(p.start_unix||0), expired=p.end_unix&&now>=p.end_unix;
  const selection=p.mode==='all'?`${s.boss_group_count} 个目录组`:`${(p.group_ids||[]).length} 个选中目录组`;
  $('#policy-pill').textContent=scheduled?'尚未开始':expired?'排期已结束':p.mode==='all'?'全部开放':`开放 ${(p.group_ids||[]).length} 组`;
  $('#policy-copy').textContent=`${scheduled?'等待排期开始':expired?'排期已结束，当前不发布活动 BOSS':'当前发布 '+selection}。此处为活动／素材目录；往期 BOSS 请在发布页的独立 Tab 查看。`;
  renderInsights();
}
// Best-effort operator to-do list; each source fails independently inside the panel.
async function loadDashboardTodos(){
  const box=$('#dashboard-todos'),items=[],now=Date.now()/1000,soon=now+3*86400,when=unix=>describeTime(unix);
  for(const [view,title] of Object.entries(titles))if(view!=='dashboard'&&state.loaded.has(view)&&adminPolicyDirty(view))items.push({text:`「${title}」有未保存的修改`,hint:'草稿仍在页面中，保存后才生效',tag:['warn','草稿'],go:()=>switchView(view)});
  const sources=[
    ['发放批次',async()=>{const data=await api('/api/mail-batches?limit=20&offset=0&status=unfinished');for(const b of data.batches)items.push({text:`礼物批次「${b.title}」未完成`,hint:`已完成 ${b.done}/${b.recipients} 名玩家 · ${formatTime(b.created_utc)}${data.total>20?` · 共 ${data.total} 个未完成`:''}`,tag:['bad','未完成'],go:async()=>{await switchView('mail');try{await openMailBatch(b.batch_id)}catch(e){toast(e.message,true)}}})}],
    ['卡池',async()=>{const data=await api('/api/gacha-editor'),pools=new Map();for(const row of data.pools)pools.set(row.base.groupid,[...(pools.get(row.base.groupid)||[]),row]);for(const group of pools.values()){const row=group[0],c=row.config,pending=group.filter(poolPendingDraft);if(pending.length)items.push({text:`卡池「${c.name}」有未发布草稿`,hint:group.length>1?`${pending.length}/${group.length} 种抽法待发布`:`草稿 v${row.draft.revision} · 已发布 v${row.live.revision}`,tag:['warn','待发布'],go:async()=>{await switchView('pool-editor');if(poolEditor.row?.gacha_id!==row.gacha_id&&!poolEditor.dirty){$('#pool-select').value=row.gacha_id;selectPool(row.gacha_id)}}});if(c.end_unix>now&&c.end_unix<soon)items.push({text:`卡池「${c.name}」即将结束`,hint:`结束于 ${when(c.end_unix)}`,tag:['info','排期'],go:()=>switchView('pool-editor')});if(c.start_unix>now&&c.start_unix<soon)items.push({text:`卡池「${c.name}」即将开始`,hint:`开始于 ${when(c.start_unix)}`,tag:['info','排期'],go:()=>switchView('pool-editor')})}}],
    ['兑换所',async()=>{const data=await api('/api/exchanges');for(const s of data.shops){if(s.deleted||s.disabled)continue;if(s.end_time>now&&s.end_time<soon)items.push({text:`兑换所「${s.name}」即将结束`,hint:`结束于 ${when(s.end_time)}`,tag:['info','排期'],go:()=>switchView('exchanges')});if(s.start_time>now)items.push({text:`兑换所「${s.name}」尚未开始`,hint:`开始于 ${when(s.start_time)}，此前玩家看不到`,tag:['info','排期'],go:()=>switchView('exchanges')})}}],
    ['公告',async()=>{const n=(await api('/api/player-policy')).config.notice;if(n.enabled&&n.end_unix&&n.end_unix<=now)items.push({text:'公告已过结束时间',hint:`结束于 ${when(n.end_unix)}；玩家现在看到“暂无公告”`,tag:['warn','公告'],go:()=>switchView('player-policy')});else if(n.enabled&&n.start_unix>now)items.push({text:'公告尚未开始显示',hint:`开始于 ${when(n.start_unix)}`,tag:['info','公告'],go:()=>switchView('player-policy')})}]
  ];
  await Promise.all(sources.map(async([name,load])=>{try{await load()}catch(e){items.push({text:`${name}读取失败`,hint:errorText(e),tag:['bad','错误']})}}));
  const rank={bad:0,warn:1,info:2};items.sort((a,b)=>rank[a.tag[0]]-rank[b.tag[0]]);
  $('#todo-count').textContent=items.length?`${items.length} 项`:'无';$('#todo-count').className='pill '+(items.some(t=>t.tag[0]!=='info')?'gold':items.length?'blue':'green');
  box.innerHTML=items.length?items.map((t,i)=>`<div class="todo"><span class="tag ${t.tag[0]}">${esc(t.tag[1])}</span><div class="todo-text"><b>${esc(t.text)}</b><span class="hint">${esc(t.hint)}</span></div>${t.go?`<button class="secondary sm" data-todo="${i}">去处理</button>`:''}</div>`).join(''):'<p class="hint">暂无未完成批次、待发布卡池、即将到期排期或未保存草稿。</p>';
  box.querySelectorAll('[data-todo]').forEach(b=>b.onclick=()=>items[Number(b.dataset.todo)].go());
}
function renderPager(key,page,total,size,onPage){
  const pages=Math.max(1,Math.ceil(total/size));page=Math.max(0,Math.min(page,pages-1));
  $(`#${key}-page-info`).textContent=`${page+1} / ${pages} 页 · 共 ${num(total)} 项`;
  const input=$(`#${key}-page`);input.value=page+1;input.max=pages;
  $(`#${key}-prev`).disabled=page===0;$(`#${key}-next`).disabled=page===pages-1;
  $(`#${key}-prev`).onclick=()=>onPage(page-1);$(`#${key}-next`).onclick=()=>onPage(page+1);
  input.onchange=()=>{const next=Number(input.value);if(Number.isInteger(next)&&next>=1&&next<=pages)onPage(next-1);else input.value=page+1};
  return page;
}
async function loadBossPublication(catalog){
  const [groups,policy]=await Promise.all([api('/api/boss-groups?catalog='+catalog),api('/api/boss-policy?catalog='+catalog)]);
  state.bossCatalog=catalog;state.groups=groups.groups;state.policy=policy.publication;state.mode=state.policy.mode;state.selected=new Set(state.policy.group_ids||[]);
  state.bossSchedules=new Map((state.policy.group_schedules||[]).map(s=>[s.group_id,structuredClone(s)]));
  $('#boss-start').value=localTimeInput(state.policy.start_unix);$('#boss-end').value=localTimeInput(state.policy.end_unix);updateBossDifficultyFilter();state.bossPage=0;renderBosses();
}
$$('#boss-catalogs button[data-catalog]').forEach(b=>b.onclick=async()=>{
  const catalog=b.dataset.catalog;if(catalog===state.bossCatalog||policyBusy('bosses'))return;
  if(adminPolicyDirty('bosses')&&!confirm('当前目录有未保存的修改，切换将放弃这些修改。是否继续？'))return;
  state.loading.add('bosses');updatePolicyControls();
  try{await loadBossPublication(catalog);clearViewAlert('bosses')}catch(e){reportError('bosses',e,'载入')}finally{state.loading.delete('bosses');updatePolicyControls()}
});
function filteredGroups(){const q=$('#boss-search').value.trim();return state.groups.filter(g=>(state.bossKind==='all'||g.category===state.bossKind)&&bossMatchesFilters(g)&&matchesWords(`${g.name} ${g.past_name||''} ${g.group_id} ${g.picture_id} ${g.boss_ids.join(' ')} ${(g.difficulties||[]).join(' ')}`,q))}
function retryImage(img){const attempt=Number(img.dataset.retry||0);if(attempt<2){img.dataset.retry=String(attempt+1);const url=new URL(img.src,location.href);url.searchParams.set('_retry',`${attempt+1}-${Date.now()}`);setTimeout(()=>img.src=url.pathname+url.search,150*(attempt+1));return}img.dataset.failed='true';img.hidden=true}
function image(url,label){return url?`<img src="${esc(url)}" alt="" loading="lazy" data-retry="0" onload="this.dataset.ok=1" onerror="retryImage(this)"><span>${esc((label||'?').slice(0,1))}</span>`:`<span>${esc((label||'?').slice(0,1))}</span>`}
const bossKindLabel=c=>c==='material'?'素材副本':c==='3d'?'3D Boss':'2D Boss';
state.bossSchedules=new Map();
const bossDayNames=['周一','周二','周三','周四','周五','周六','周日'];
function bossScheduleSummary(s){return s?`${s.weekdays?.length?s.weekdays.map(d=>bossDayNames[d-1]).join('、'):'每天'}${s.start_unix?' · '+when(s.start_unix)+' 起':''}${s.end_unix?' · '+when(s.end_unix)+' 止':''}`:'每天 · 沿用目录排期'}
function normalizedBossSchedules(schedules){return JSON.stringify([...schedules].map(s=>({group_id:s.group_id,start_unix:s.start_unix||0,end_unix:s.end_unix||0,weekdays:[...new Set(s.weekdays||[])].sort((a,b)=>a-b)})).sort((a,b)=>a.group_id-b.group_id))}
let bossScheduleGroup=null;
function closeBossSchedule(){$('#boss-schedule-modal').classList.remove('open');bossScheduleGroup=null}
$('#boss-rows').addEventListener('click',e=>{
  const button=e.target.closest('[data-boss-schedule]');if(!button||policyBusy('bosses'))return;
  bossScheduleGroup=state.groups.find(g=>g.group_id===Number(button.dataset.bossSchedule));if(!bossScheduleGroup)return;
  const s=state.bossSchedules.get(bossScheduleGroup.group_id);
  $('#boss-schedule-title').textContent=bossScheduleGroup.name+' · 独立排期';
  $('#boss-group-start').value=localTimeInput(s?.start_unix);$('#boss-group-end').value=localTimeInput(s?.end_unix);
  $$('.boss-weekdays input').forEach(c=>c.checked=!s?.weekdays?.length||s.weekdays.includes(Number(c.value)));
  $('#boss-schedule-error').hidden=true;$('#boss-schedule-modal').classList.add('open');
});
$$('[data-boss-days]').forEach(b=>b.onclick=()=>{const days=b.dataset.bossDays.split(',').map(Number);$$('.boss-weekdays input').forEach(c=>c.checked=days.includes(Number(c.value)))});
$('#boss-schedule-cancel').onclick=closeBossSchedule;
$('#boss-schedule-reset').onclick=()=>{if(!bossScheduleGroup)return;state.bossSchedules.delete(bossScheduleGroup.group_id);closeBossSchedule();renderBosses()};
$('#boss-schedule-apply').onclick=()=>{
  if(!bossScheduleGroup)return;
  const start=unixInput('#boss-group-start'),end=unixInput('#boss-group-end'),days=$$('.boss-weekdays input:checked').map(c=>Number(c.value));
  const error=!days.length?'请至少选择一个开放日；要关闭此 Boss，请从「仅选中组」的开放名单移除。':end&&end<=start?'结束时间须晚于开始时间。':'';
  $('#boss-schedule-error').hidden=!error;$('#boss-schedule-error').textContent=error;if(error)return;
  if(!start&&!end&&days.length===7)state.bossSchedules.delete(bossScheduleGroup.group_id);
  else state.bossSchedules.set(bossScheduleGroup.group_id,{group_id:bossScheduleGroup.group_id,start_unix:start,end_unix:end,weekdays:days.length===7?[]:days});
  closeBossSchedule();renderBosses();toast('独立排期已加入草稿，发布设置后生效');
};
state.bossView=(()=>{try{return localStorage.getItem('kairisei-admin-boss-view')||'list'}catch{return 'list'}})();
$$('#boss-view button').forEach(b=>b.onclick=()=>{state.bossView=b.dataset.mode;try{localStorage.setItem('kairisei-admin-boss-view',state.bossView)}catch{/* Per-browser convenience only. */}state.bossPage=0;renderBosses()});
function renderBosses(){
  $$('#boss-catalogs button').forEach(b=>b.classList.toggle('active',b.dataset.catalog===state.bossCatalog));
  $$('#boss-view button').forEach(b=>b.classList.toggle('active',b.dataset.mode===state.bossView));
  const list=state.bossView==='list',size=list?100:60;
  const rows=filteredGroups();state.bossPage=renderPager('boss',state.bossPage,rows.length,size,page=>{state.bossPage=page;renderBosses();$('#boss-rows').scrollIntoView({block:'start'})});
  const visible=rows.slice(state.bossPage*size,(state.bossPage+1)*size);
  $$('#boss-kinds button').forEach(b=>b.classList.toggle('active',b.dataset.kind===state.bossKind));
  $('#mode-all').classList.toggle('active',state.mode==='all');$('#mode-selected').classList.toggle('active',state.mode==='allowlist');
  $('#boss-revision').textContent=`revision ${state.policy?.revision||0}`;
  $('#boss-summary').innerHTML=`<b>${state.mode==='all'?'全部开放模式（仍按排期展示）':`已选 ${state.selected.size} 组`}</b><span>筛选结果 ${rows.length} / 全部 ${state.groups.length} 组 · 本页 ${visible.length} 组</span>`;
  $('#save-policy').disabled=policyBusy('bosses')||!state.policy;
  const dirty=state.policy&&adminPolicyDirty('bosses');setSaveState('#boss-draft-state',dirty?'有未发布的修改':'与已发布设置一致',dirty?'dirty':'ok');
  const check=g=>{const on=state.selected.has(g.group_id);return `<input class="check boss-check" type="checkbox" data-id="${g.group_id}" aria-label="选择${esc(g.name)}" title="${state.mode==='all'?'全部开放模式下无需勾选':'勾选即列入开放名单'}" ${on?'checked':''} ${state.mode==='all'?'disabled':''}>`};
  const openTag=g=>{const open=bossIsPublished(g);return `<span class="tag ${open?'ok':''}">${open?'当前开放':'当前关闭'}</span>`};
  const ids=g=>`${esc(g.boss_ids.slice(0,4).join(', '))}${g.boss_ids.length>4?'…':''}`;
  const schedule=g=>`<button type="button" class="secondary sm" data-boss-schedule="${g.group_id}">独立排期</button><span class="sub boss-schedule-summary">${esc(bossScheduleSummary(state.bossSchedules.get(g.group_id)))}</span>`;
  $('#boss-rows').className=list?'boss-list-wrap':'boss-grid';
  if(!visible.length)$('#boss-rows').innerHTML=`<div class="empty"><b>没有匹配的 Boss 组</b>调整搜索或点击“清除筛选”。</div>`;
  else if(list)$('#boss-rows').innerHTML=`<div class="table-wrap"><table class="boss-list"><thead><tr><th class="check-col"><span class="sr-only">选择</span></th><th><span class="sr-only">图片</span></th><th>名称</th><th>类型 / 难度</th><th>组 ID / BOSS ID</th><th>状态</th><th>出现排期</th><th><span class="sr-only">操作</span></th></tr></thead><tbody>${visible.map(g=>`<tr class="${state.selected.has(g.group_id)&&state.mode!=='all'?'selected':''}"><td>${check(g)}</td><td><div class="thumb">${image(g.image_url,g.name)}</div></td><td class="name-cell"><b>${esc(g.name)}</b>${g.past_name?`<span class="sub">${esc(g.past_name)}</span>`:''}</td><td>${bossKindLabel(g.category)} · ${g.bosses?.length||g.boss_count} 个难度 · 最多 ${g.max_segments||1} 波<span class="sub">${esc((g.difficulties||[]).join(' / ')||'未标注难度')}</span></td><td><code>${g.group_id}</code><span class="sub">${ids(g)}</span></td><td>${openTag(g)}</td><td>${schedule(g)}</td><td><button class="secondary sm" data-boss-detail="${g.group_id}">难度规则</button></td></tr>`).join('')}</tbody></table></div>`;
  else $('#boss-rows').innerHTML=visible.map(g=>{const on=state.selected.has(g.group_id);return `<div class="boss-card ${on&&state.mode!=='all'?'selected':''}"><label class="boss-select">${check(g)}</label><span class="card-state">${openTag(g)}</span><div class="boss-art">${image(g.image_url,g.name)}</div><div class="boss-copy"><span class="name" title="${esc(g.name)}">${esc(g.name)}</span>${g.past_name?`<span class="sub">${esc(g.past_name)}</span>`:''}<span class="sub">${bossKindLabel(g.category)} · ${g.bosses?.length||g.boss_count} 个难度 · 最多 ${g.max_segments||1} 波</span><span class="sub">${esc((g.difficulties||[]).join(' / ')||'未标注难度')}</span><span class="sub"><code>${g.group_id}</code> ${ids(g)}</span><div class="card-actions">${schedule(g)}<button class="secondary sm" data-boss-detail="${g.group_id}">难度规则与属性</button></div></div></div>`}).join('');
  $$('.boss-check').forEach(c=>c.onchange=()=>{const id=Number(c.dataset.id);c.checked?state.selected.add(id):state.selected.delete(id);renderBosses()});
}
function renderGachas(){
  const rows=filteredGachas();$('#gacha-revision').textContent=`revision ${state.gachaPolicy?.revision||0}`;
  $('#gacha-summary').innerHTML=`<b>已开放 ${state.gachaPresets.filter(g=>state.gachaSelected.has(g.group_id)).length} 个</b><span>筛选结果 ${rows.length} / 全部 ${state.gachaPresets.length} 个常规卡池组</span>`;
  const dirty=state.gachaPolicy&&adminPolicyDirty('gachas');setSaveState('#gacha-draft-state',dirty?'有未发布的修改':'与已发布设置一致',dirty?'dirty':'ok');
  $('#gacha-rows').innerHTML=rows.map(g=>{const on=state.gachaSelected.has(g.group_id);return `<div class="gacha-card ${on?'selected':''}"><label class="gacha-select"><input class="check gacha-check" type="checkbox" data-id="${g.group_id}" aria-label="开放${esc(g.name)}" ${on?'checked':''}></label><span class="card-state tag ${on?'ok':''}">${on?'开放':'关闭'}</span><div class="gacha-art">${image(g.image_url,g.name)}</div><div class="gacha-copy"><span class="name" title="${esc(g.name)}">${esc(g.name)}${g.publication_key==='operator'?' <span class="tag info">后台新建</span>':''}</span><span class="sub">${esc(g.payment_item||`物品 ${g.payment_item_id}`)} × ${num(g.price)} · 每次 ${g.draw_count} 抽 · 卡池 ${g.card_count} 张</span><span class="sub"><code>Group ${g.group_id}</code> Gacha ${esc(g.gacha_ids.join(', '))}</span></div></div>`}).join('')||`<div class="empty"><b>没有匹配的扭蛋组</b>调整搜索或发布状态筛选。</div>`;
  $$('.gacha-check').forEach(c=>c.onchange=()=>{const id=Number(c.dataset.id);c.checked?state.gachaSelected.add(id):state.gachaSelected.delete(id);renderGachas()});
}
$$('.nav button').forEach(b=>b.onclick=()=>switchView(b.dataset.view));$$('[data-jump]').forEach(b=>b.onclick=()=>switchView(b.dataset.jump));$('#refresh').onclick=()=>loadView(state.currentView,true);$('#boss-search').oninput=()=>{state.bossPage=0;renderBosses()};
$$('#boss-kinds button').forEach(b=>b.onclick=()=>{state.bossKind=b.dataset.kind;state.bossPage=0;renderBosses()});
$('#mode-all').onclick=()=>{state.mode='all';renderBosses()};$('#mode-selected').onclick=()=>{state.mode='allowlist';renderBosses()};$('#select-visible').onclick=()=>{filteredGroups().forEach(g=>state.selected.add(g.group_id));state.mode='allowlist';renderBosses()};$('#clear-selected').onclick=()=>{state.selected.clear();state.mode='allowlist';renderBosses()};
$('#boss-start').onchange=$('#boss-end').onchange=()=>renderBosses();
function policyBusy(name){return state.loading.has(name)||state.publishing.has(name)}
function updatePolicyControls(){
  if(typeof updateMissionControls==='function')updateMissionControls();
  for(const name of ['bosses','dungeon-schedule','gachas','pool-editor','settings','shop','drops','exchanges','player-policy','collections','custom-cards','activity-rewards','evolution','missions'])$('#'+name).inert=policyBusy(name);
  for(const name of Object.keys(titles)){const busy=policyBusy(name);busy?$('#'+name).setAttribute('aria-busy','true'):$('#'+name).removeAttribute('aria-busy')}
  document.body.classList.toggle('busy',policyBusy(state.currentView));
  if(typeof updatePoolControls==='function')updatePoolControls();
  if(typeof updateDungeonScheduleControls==='function')updateDungeonScheduleControls();
  $('#save-policy').disabled=policyBusy('bosses')||!state.policy;
  $('#gacha-save').disabled=policyBusy('gachas')||!state.gachaPolicy;
  $('#refresh').disabled=policyBusy(state.currentView);
  if(typeof updateDraftIndicators==='function')updateDraftIndicators();
}
$('#save-policy').onclick=async()=>{
  if(policyBusy('bosses')||!state.policy)return;
  const start=unixInput('#boss-start'),end=unixInput('#boss-end');
  clearInvalid($('#bosses'));
  if(start&&end&&end<=start){markInvalid($('#boss-end'),'结束时间须晚于开始时间');showViewAlert('bosses',{title:'不能发布：排期无效',message:'目录结束展示时间须晚于开始时间。'});return}
  if(state.mode==='allowlist'&&!state.selected.size&&!confirm('“仅选中组”模式下没有勾选任何目录组，发布后该目录将全部关闭。确定继续？'))return;
  const body={mode:state.mode,group_ids:state.mode==='all'?[]:[...state.selected],start_unix:start,end_unix:end,group_schedules:[...state.bossSchedules.values()],expected_revision:state.policy.revision};
  if(!confirm(`${state.bossCatalog==='past'?'往期 BOSS':'活动／素材副本'}发布预览：${body.mode==='all'?'全部开放':`仅开放 ${body.group_ids.length} 组`}\n开始：${describeTime(body.start_unix)}\n结束：${describeTime(body.end_unix)}\n独立排期：${body.group_schedules.length} 组（星期按北京时间）\n确认发布目录排期？`))return;
  state.publishing.add('bosses');updatePolicyControls();
  try{
    const data=await api('/api/boss-policy?catalog='+state.bossCatalog,{method:'PUT',body:JSON.stringify(body)});
    state.policy=data.publication;state.mode=state.policy.mode;state.selected=new Set(state.policy.group_ids||[]);
    state.bossSchedules=new Map((state.policy.group_schedules||[]).map(s=>[s.group_id,structuredClone(s)]));
    state.loaded.delete('dashboard');state.loaded.delete('audit');if(typeof dungeonSchedulePublicationChanged==='function')dungeonSchedulePublicationChanged();clearViewAlert('bosses');renderBosses();toast('Boss 发布设置已保存，按排期开放');
  }catch(e){reportError('bosses',e,'发布')}finally{state.publishing.delete('bosses');updatePolicyControls()}
};
$('#gacha-select-all').onclick=()=>{state.gachaSelected=new Set(state.gachaPresets.map(g=>g.group_id));renderGachas()};
$('#gacha-clear-all').onclick=()=>{state.gachaSelected.clear();renderGachas()};
$('#gacha-save').onclick=async()=>{
  if(policyBusy('gachas')||!state.gachaPolicy)return;
  const body={group_ids:[...state.gachaSelected].sort((a,b)=>a-b),expected_revision:state.gachaPolicy.revision};
  const before=new Set(state.gachaPolicy.group_ids||[]),opened=body.group_ids.filter(id=>!before.has(id)).length,closed=[...before].filter(id=>!state.gachaSelected.has(id)).length;
  if(!confirm(`确认即时发布 ${body.group_ids.filter(id=>state.gachaPresets.some(g=>g.group_id===id)).length} 个常规卡池组？\n本次新开放 ${opened} 个、关闭 ${closed} 个；玩家下次请求即生效。`))return;
  state.publishing.add('gachas');updatePolicyControls();
  try{
    const data=await api('/api/gacha-policy',{method:'PUT',body:JSON.stringify(body)});
    state.gachaPolicy=data.publication;state.gachaSelected=new Set(state.gachaPolicy.group_ids||[]);
    if(typeof poolEditor!=='undefined'&&poolEditor.openGroups){poolEditor.openGroups=new Set(state.gachaSelected);if(poolEditor.row)$('#pool-current .tag-link')?.replaceWith(document.createRange().createContextualFragment(poolOpenBadge(poolEditor.row)))}
    state.loaded.delete('audit');clearViewAlert('gachas');renderGachas();toast('扭蛋发布策略已即时生效');
  }catch(e){reportError('gachas',e,'发布')}finally{state.publishing.delete('gachas');updatePolicyControls()}
};

// Shell: theme, drawer navigation, keyboard and dialog focus handling.
const themeKey='kairisei-admin-theme';
function effectiveTheme(){return document.documentElement.dataset.theme||(matchMedia('(prefers-color-scheme: dark)').matches?'dark':'light')}
function renderThemeLabel(){$('#theme-label').textContent=effectiveTheme()==='dark'?'浅色':'深色';$('#theme-toggle').title=`切换为${$('#theme-label').textContent}模式`}
$('#theme-toggle').onclick=()=>{const next=effectiveTheme()==='dark'?'light':'dark';document.documentElement.dataset.theme=next;try{localStorage.setItem(themeKey,next)}catch{/* Per-browser convenience only. */}renderThemeLabel()};
matchMedia('(prefers-color-scheme: dark)').addEventListener?.('change',renderThemeLabel);renderThemeLabel();
function closeNavDrawer(){document.body.classList.remove('nav-open');$('#scrim').hidden=true;$('#nav-toggle').setAttribute('aria-expanded','false')}
$('#nav-toggle').onclick=()=>{const open=!document.body.classList.contains('nav-open');document.body.classList.toggle('nav-open',open);$('#scrim').hidden=!open;$('#nav-toggle').setAttribute('aria-expanded',String(open));if(open)$('.nav button.active')?.focus()};
$('#scrim').onclick=closeNavDrawer;
const focusable='a[href],button:not(:disabled),input:not(:disabled):not([type=hidden]),select:not(:disabled),textarea:not(:disabled),[tabindex]:not([tabindex="-1"])';
const openModal=()=>$$('.modal.open').pop();
function isVisible(el){return !!(el.offsetWidth||el.offsetHeight||el.getClientRects().length)}
for(const modal of $$('.modal')){
  modal.querySelector('.dialog-x')?.addEventListener('click',()=>{const d=modal.querySelector('[data-dismiss]');if(d&&!d.disabled)d.click()});
  new MutationObserver(()=>{
    const open=modal.classList.contains('open');if(open===!!modal.dataset.wasOpen)return;
    if(open){modal.dataset.wasOpen='1';modal.returnFocus=document.activeElement;requestAnimationFrame(()=>{const target=[...modal.querySelectorAll('.dialog-body input:not([type=checkbox]):not([type=file]),.dialog-body select,.dialog-body textarea')].find(el=>!el.disabled&&isVisible(el))||modal.querySelector('[data-dismiss]');target?.focus()})}
    else{delete modal.dataset.wasOpen;if(modal.returnFocus?.isConnected)modal.returnFocus.focus()}
  }).observe(modal,{attributes:true,attributeFilter:['class']});
}
new ResizeObserver(()=>document.documentElement.style.setProperty('--topbar-h',$('.topbar').offsetHeight+'px')).observe($('.topbar'));
addEventListener('scroll',()=>{const b=document.body;if(scrollY>64)b.classList.add('scrolled');else if(scrollY<8)b.classList.remove('scrolled')},{passive:true});
$('#global-search-pages').innerHTML=Object.entries(titles).map(([,t])=>`<option value="${esc(t)}">`).join('');
$('#global-search').onkeydown=async e=>{
  if(e.key==='Escape'){e.target.value='';e.target.blur();return}
  if(e.key!=='Enter')return;
  const q=e.target.value.trim();if(!q)return;e.target.value='';e.target.blur();
  const page=Object.entries(titles).find(([,t])=>t===q)||(q.length>=2&&!/^\d+$/.test(q)?Object.entries(titles).find(([,t])=>t.includes(q)):null);
  if(page)return switchView(page[0]);
  $('#account-search').value=q;accountPage.page=0;
  if(state.loaded.has('accounts')){await switchView('accounts');await loadAccountsPage().catch(err=>reportError('accounts',err,'载入'))}else await switchView('accounts');
  if(accountPage.total===1&&state.accounts[0])openAccountDetail(state.accounts[0].user_id);
};
document.addEventListener('keydown',e=>{
  const modal=openModal();
  if((e.ctrlKey||e.metaKey)&&!e.altKey&&e.key.toLowerCase()==='k'){e.preventDefault();if(!modal){const g=$('#global-search');if(isVisible(g))g.focus();else $('#nav-toggle').click()}return}
  if((e.ctrlKey||e.metaKey)&&!e.altKey&&e.key.toLowerCase()==='s'){e.preventDefault();if(modal)return;const b=$(`#${state.currentView} [data-save-shortcut]`);if(b&&!b.disabled&&!b.closest('[inert]'))b.click();else toast('当前页面没有可保存的内容');return}
  if(e.key==='Escape'){if(modal){const d=modal.querySelector('[data-dismiss]');if(d&&!d.disabled){e.preventDefault();d.click()}}else if(document.body.classList.contains('nav-open'))closeNavDrawer();return}
  if(e.key==='Tab'&&modal){const items=[...modal.querySelectorAll(focusable)].filter(isVisible);if(!items.length)return;const first=items[0],last=items[items.length-1];if(e.shiftKey&&document.activeElement===first){e.preventDefault();last.focus()}else if(!e.shiftKey&&document.activeElement===last){e.preventDefault();first.focus()}else if(!modal.contains(document.activeElement)){e.preventDefault();first.focus()}return}
  if(e.key==='/'&&!modal&&!e.ctrlKey&&!e.metaKey&&!e.altKey&&!/^(INPUT|SELECT|TEXTAREA)$/.test(document.activeElement?.tagName||'')){const search=[...$$('.view.active .search')].find(isVisible);if(search){e.preventDefault();search.focus();search.select?.()}}
});
document.addEventListener('DOMContentLoaded',()=>{const initial=decodeURIComponent(location.hash.replace(/^#\/?/,''));if(titles[initial]&&initial!=='dashboard'){api('/api/status').then(s=>setMaintenanceBanner(s.maintenance)).catch(()=>{});switchView(initial)}else{switchView('dashboard')}});
document.addEventListener('keydown',e=>{const label=e.target.closest?.('.file-button');if(label&&(e.key==='Enter'||e.key===' ')){e.preventDefault();label.querySelector('input[type=file]:not(:disabled)')?.click()}});

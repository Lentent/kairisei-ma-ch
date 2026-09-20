'use strict';

const attributeLabels={FIRE:'火',ICE:'冰',WIND:'风',LIGHT:'光',DARK:'暗',NONE:'无'};
const attributeName=value=>String(value||'').split('_').map(v=>attributeLabels[v]||v).filter(Boolean).join('／')||'未标注';
const matchesWords=(text,query)=>query.trim().toLowerCase().split(/\s+/).every(word=>String(text).toLowerCase().includes(word));
const attributeOptions='<option value="">全部属性</option>'+Object.entries(attributeLabels).filter(([key])=>key!=='NONE').map(([key,name])=>`<option value="${key}">${name}</option>`).join('');

function cardFacts(card){
  if(card.kind!=='card')return card.detail||'';
  const c=card.combat,p=card.parameters;
  return [c?`${attributeName(c.attribute)} · ${c.cost} COST`:'属性／费用未载入',
    p?`满级 HP ${num(p.hp)} · 物攻 ${num(p.attack)} · 魔攻 ${num(p.magic)} · 回复 ${num(p.mind)}`:'',
    c?.arthur_skill?`职业技能：${c.arthur_skill}`:'',
    [(card.source_tags||[]).includes('jp_import')?'日服卡牌':'',card.detail||'获取方式未标注'].filter(Boolean).join(' · ')].filter(Boolean).join('\n');
}

function installCardFilters(prefix,anchor,change){
  const row=document.createElement('div');row.className='toolbar';row.id=prefix+'-advanced';
  row.innerHTML=`<select id="${prefix}-attribute" aria-label="属性">${attributeOptions}</select><select id="${prefix}-cost" aria-label="费用" autocomplete="off"><option value="" selected>全部 COST</option>${Array.from({length:11},(_,i)=>`<option value="${i}">${i} COST</option>`).join('')}</select><select id="${prefix}-resource" aria-label="资源状态"><option value="">全部资源状态</option><option value="available">资源可用</option><option value="unavailable">资源缺失</option></select>`;
  $(anchor).after(row);
  row.querySelectorAll('select').forEach(el=>{el.value='';el.onchange=change});
}
function cardFilterQuery(prefix,isCard){
  const result={};
  for(const key of ['attribute','cost','resource'])result[key]=isCard?$(`#${prefix}-${key}`).value:'';
  $(`#${prefix}-advanced`).hidden=!isCard;
  return result;
}
function resetCardFilters(prefix){for(const key of ['attribute','cost','resource'])$(`#${prefix}-${key}`).value=''}
function matchesCardFilters(card,prefix){
  const f=cardFilterQuery(prefix,true);
  return (!f.attribute||(card.combat?.attribute||'').split('_').includes(f.attribute))&&
    (f.cost===''||card.combat?.cost===Number(f.cost))&&(!f.resource||(f.resource==='available'?card.resource_state!=='unavailable':card.resource_state==='unavailable'));
}
installCardFilters('catalog','#catalog-search',()=>{state.catalogPage=0;loadCatalog()});
installCardFilters('pool','#pool-search',()=>{poolEditor.page=0;renderPoolCards();renderSelectedCards()});
installCardFilters('content','#content-search',()=>{contentPicker.page=0;loadContentCatalog()});

function accountFilterQuery(){
  const q={q:$('#account-search').value.trim(),include_system:$('#account-system').checked?'1':'0',binding:$('#account-binding').value,sort:$('#account-sort').value,active:$('#account-active').checked?'1':'0'};
  for(const kind of ['created','login'])for(const side of ['after','before']){
    const value=$(`#account-${kind}-${side}`).value;
    if(value){const stamp=new Date(value);if(!Number.isFinite(stamp.getTime()))throw new Error('时间范围无效');q[`${kind}_${side}`]=stamp.toISOString()}
  }
  return q;
}
const reloadAccounts=()=>{accountPage.page=0;loadAccountsPage().catch(e=>toast(e.message,true))};
$$('#account-binding,#account-sort,#account-active,#account-created-after,#account-created-before,#account-login-after,#account-login-before').forEach(el=>el.onchange=reloadAccounts);
$('#account-reset').onclick=()=>{$$('#accounts input:not(.player-check)').forEach(el=>{if(el.type==='checkbox')el.checked=false;else if(el.id!=='accounts-page')el.value=''});$('#account-binding').value='';$('#account-sort').value='id';reloadAccounts()};

function renderAccountFacts(a){
  const battle=a.active_boss_id?`BOSS ${a.active_boss_id}${a.active_room_id?' · 房间 '+a.active_room_id:' · 单人'}（未结算）`:a.active_pvp?'PVP 未结算':'无未结算对局';
  const facts=[['账号类型',a.user_id>=1900000000?'系统伙伴':a.username?'已绑定 · '+a.username:'游客'],['等级 / 职业',`Lv.${a.level} / ${cardJobName(a.active_arthur_type)}`],['经验（总计／下级阈值）',`${num(a.experience)} / ${num(a.next_level_experience)}`],['金币 / 友情点',`${num(a.gold)} / ${num(a.friend_point)}`],['水晶',`${num(a.free_crystal+a.paid_crystal)}（免费 ${num(a.free_crystal)}／付费 ${num(a.paid_crystal)}）`],['AP / BP',`${a.ap}/${a.ap_max} · ${a.bp}/${a.bp_max}（存档值）`],['背包 / 仓库',`${num(a.card_count)}/${num(a.card_max)} · ${num(a.container_cards)}/${num(a.container_max)}`],['素材 / 道具种类',`${num(a.stack_card_kinds)} / ${num(a.item_kinds)}`],['召唤石 / 传承卡',`${num(a.spheres)} / ${num(a.buddies)}`],['卡组数',num(a.decks)],['未领取邮件',num(a.pending_mail)],['新手训练',a.training_step>=9?'已完成':`已完成 ${a.training_step}/9 步`],['历史卡组评价 / PVP 点',`${a.arthur_rank} / ${num(a.pvp_point)}`],['看板',`当前 ID ${a.navi_id} · 已解锁 ${a.unlocked_navis}`],['对局存档',battle],['最近业务请求',formatTime(a.recent_activity)],['注册时间',formatTime(a.created_utc)],['最近登录',formatTime(a.last_login_utc)],['最近保存',formatTime(a.updated_utc)],['已解锁功能位',(a.unlocked_features||[]).join('、')]];
  $('#account-detail-body').innerHTML=facts.map(([k,v])=>`<div class="fact"><span>${esc(k)}</span><b>${esc(v)}</b></div>`).join('');
}

const roomView={rows:[],page:0};
const insightPanel=document.createElement('div');insightPanel.className='panel';
insightPanel.innerHTML=`<div class="panel-head"><h2>玩家活跃与对局</h2><label><input id="insights-auto" type="checkbox" checked> 每15秒刷新</label></div><div class="panel-body"><div class="cards" id="activity-metrics"></div><p class="sub">活跃估算：最近5分钟有成功业务请求，或当前持有组队连接的玩家，按玩家ID去重。组队只统计已连接真人，不把AI和掉线席位当玩家。单人未结算仅为近期活动估算，退出但未结算的客户端可能仍计入。</p><p id="request-metrics" class="sub"></p><div class="toolbar"><input id="room-search" class="search" placeholder="搜索房间 ID、BOSS ID 或玩家 ID"><select id="room-state"><option value="">全部房间</option><option value="open">等待加入</option><option value="countdown">倒计时</option><option value="battle">战斗中</option></select><button id="room-export" class="secondary">导出当前筛选</button><span id="room-updated" class="sub"></span></div><div class="table-wrap"><table><thead><tr><th>房间 / BOSS</th><th>阶段</th><th>真人玩家</th><th>AI / 掉线</th><th>波次 / 回合 / 倍速</th></tr></thead><tbody id="room-rows"></tbody></table></div><div class="toolbar"><button class="secondary" id="room-prev">上一页</button><span id="room-page"></span><button class="secondary" id="room-next">下一页</button></div></div>`;
$('#dashboard').append(insightPanel);
function filteredRooms(){return roomView.rows.filter(r=>(!$('#room-state').value||r.state===$('#room-state').value)&&matchesWords(`${r.room_id} ${r.boss_id} ${r.players.join(' ')}`,$('#room-search').value))}
function renderRooms(){
  const rows=filteredRooms(),pages=Math.max(1,Math.ceil(rows.length/30));roomView.page=Math.min(roomView.page,pages-1);
  $('#room-rows').innerHTML=rows.slice(roomView.page*30,(roomView.page+1)*30).map(r=>`<tr><td>${r.room_id}<span class="sub">BOSS ${r.boss_id}</span></td><td>${esc({open:'等待加入',countdown:'倒计时',battle:'战斗中'}[r.state]||r.state)}</td><td>${r.players.map(id=>`<button class="secondary" data-player="${id}">#${id}</button>`).join(' ')||'—'}</td><td>${r.ai} / ${r.disconnected}</td><td>${r.state==='battle'?`${r.wave} / ${r.turn}`:'—'} · ${r.game_speed/100}×</td></tr>`).join('')||'<tr><td colspan="5" class="empty">暂无匹配房间</td></tr>';
  $('#room-page').textContent=`${roomView.page+1}/${pages} 页 · ${rows.length} 间`;$('#room-prev').disabled=roomView.page===0;$('#room-next').disabled=roomView.page===pages-1;
}
function renderInsights(){
  const s=state.status,a=s.activity,counts=s.accounts;
  $('#m-account-types').textContent=`已绑定 ${num(counts.bound)} · 游客 ${num(counts.guests)} · 系统伙伴 ${num(counts.system)}（另计）`;
  if(!a?.available){$('#activity-metrics').textContent='活动数据源未接入';return}
  const metrics=[['活跃玩家（估算）',a.online_ids.length],['组队战斗真人',a.team_players],['近期单人未结算',a.solo_players],['战斗／大厅房间',`${a.battle_rooms} / ${a.lobby_rooms}`]];
  $('#activity-metrics').innerHTML=metrics.map(([label,value])=>`<div class="metric"><span>${label}</span><strong>${value}</strong></div>`).join('');
  const h=a.http;
  $('#request-metrics').textContent=`近约5分钟业务请求 ${num(h.requests)} · HTTP失败 ${num(h.http_errors)} · 超过1秒 ${num(h.slow_requests)} · 平均 ${Number(h.mean_ms).toFixed(1)} ms。仅统计账号业务处理（含账号排队）；不含资源下载、登录鉴权失败和HTTP 200中的业务拒绝。重启后重新累计。`;
  roomView.rows=a.rooms;$('#room-updated').textContent='更新于 '+formatTime(s.server_time_utc);renderRooms();
}
$('#room-search').oninput=$('#room-state').onchange=()=>{roomView.page=0;renderRooms()};
$('#room-prev').onclick=()=>{roomView.page--;renderRooms()};$('#room-next').onclick=()=>{roomView.page++;renderRooms()};
$('#room-export').onclick=()=>downloadJSON('房间观测.json',{observed_at:state.status?.server_time_utc,rooms:filteredRooms()});
$('#room-rows').onclick=e=>{const b=e.target.closest('[data-player]');if(b)openAccountDetail(Number(b.dataset.player))};
let insightsRefreshing=false;
setInterval(async()=>{
  if(document.hidden||state.currentView!=='dashboard'||!$('#insights-auto').checked||insightsRefreshing||state.loading.has('dashboard'))return;
  insightsRefreshing=true;
  try{state.status=await api('/api/status');renderDashboard()}catch(e){$('#room-updated').textContent='刷新失败：'+e.message}finally{insightsRefreshing=false}
},15000);

const bossRuleEditor={rules:new Map(),revision:0,group:null,dirty:false,busy:false,request:0};
$('#boss-attribute').innerHTML=attributeOptions;
$('#drop-attribute').innerHTML=attributeOptions;
function bossIsPublished(g){const p=state.policy,now=Date.now()/1000;return !!p&&(!p.start_unix||now>=p.start_unix)&&(!p.end_unix||now<p.end_unix)&&(p.mode==='all'||(p.group_ids||[]).includes(g.group_id))}
function bossMatchesFilters(g){
  const attr=$('#boss-attribute').value,difficulty=$('#boss-difficulty').value,published=$('#boss-published').value;
  return (!attr||(g.bosses||[]).some(b=>b.targets.some(t=>(t.stats?.attribute||'').split('_').includes(attr))))&&(!difficulty||(g.difficulties||[]).includes(difficulty))&&(!published||bossIsPublished(g)===(published==='open'));
}
function updateBossDifficultyFilter(){const selected=$('#boss-difficulty').value;$('#boss-difficulty').innerHTML='<option value="">全部难度</option>'+[...new Set(state.groups.flatMap(g=>g.difficulties||[]))].map(v=>`<option value="${esc(v)}">${esc(v)}</option>`).join('');$('#boss-difficulty').value=selected;if($('#boss-difficulty').selectedIndex<0)$('#boss-difficulty').value=''}
$$('#boss-attribute,#boss-difficulty,#boss-published').forEach(el=>el.onchange=()=>{state.bossPage=0;renderBosses()});
$('#boss-reset').onclick=()=>{$('#boss-search').value='';['attribute','difficulty','published'].forEach(k=>$(`#boss-${k}`).value='');state.bossKind='all';state.bossPage=0;renderBosses()};
function bossStatTable(b){
  return `<div class="table-wrap"><table><thead><tr><th>波 / 部位</th><th>属性 / HP</th><th>物攻 / 魔攻</th><th>物防 / 魔防</th></tr></thead><tbody>${b.targets.map(t=>{const s=t.stats;return `<tr><td>${esc(targetLabel(t))}</td><td>${s?`${esc(attributeName(s.attribute))} / ${num(s.hp)}`:'资料未载入'}</td><td>${s?`${num(s.attack)} / ${num(s.magic)}`:'—'}</td><td>${s?`${num(s.defense)} / ${num(s.magic_defense)}`:'—'}</td></tr>`}).join('')||'<tr><td colspan="4">此难度暂无可展示的敌人基础资料</td></tr>'}</tbody></table></div>`;
}
function bossRuleForm(b){
  const row=bossRuleEditor.rules.get(b.boss_id),rule=row?.draft;
  return `<details class="boss-difficulty-row" data-boss="${b.boss_id}" open><summary><b>${esc(b.difficulty)} · #${b.boss_id}</b>${row?` · BP 单人${row.bp_use}／组队${row.bp_use_half}`:''}</summary>${rule?`<div class="toolbar"><label><input data-rule="select" type="checkbox"> 批量目标</label><label><input data-rule="standard" type="checkbox" ${rule.standard?'checked':''}> 单人／组队</label><label><input data-rule="own_deck" type="checkbox" ${rule.own_deck?'checked':''} ${row.own_deck_boss_id?'':'disabled'}> 单人（仅自己的卡组）</label><label><input data-rule="continue" type="checkbox" ${rule.continue?'checked':''}> 允许复活</label><button class="secondary" data-copy-rule="${b.boss_id}">应用到勾选难度</button><button class="secondary" data-edit-drops="${b.boss_id}">配置共用掉落</button></div>${row.own_deck_boss_id?'':'<p class="sub">此难度的自卡组战斗配置不可用。</p>'}`:'<p class="sub">该难度沿用原生入口规则</p>'}${bossStatTable(b)}</details>`;
}
async function openBossDetails(id){
  const group=state.groups.find(g=>g.group_id===id);if(!group||bossRuleEditor.busy)return;if(bossRuleEditor.dirty&&!confirm('放弃未保存的BOSS规则修改？'))return;const serial=++bossRuleEditor.request;bossRuleEditor.group=null;bossRuleEditor.dirty=false;
  $('#boss-detail-title').textContent=group.name+' · 各难度';$('#boss-detail-rules').textContent='正在读取…';$('#boss-detail-modal').classList.add('open');$('#boss-detail-save').disabled=true;
  try{const data=await api('/api/boss-rules');if(serial!==bossRuleEditor.request)return;bossRuleEditor.revision=data.revision;bossRuleEditor.rules=new Map(data.rules.map(row=>[row.boss_id,{...row,draft:{boss_id:row.boss_id,standard:row.standard,own_deck:row.own_deck,continue:row.continue}}]));bossRuleEditor.group=group;bossRuleEditor.dirty=false;renderBossRuleForms()}catch(e){if(serial!==bossRuleEditor.request)return;$('#boss-detail-rules').textContent='读取失败：'+e.message}
}
function renderBossRuleForms(){
  const group=bossRuleEditor.group;$('#boss-detail-save').disabled=bossRuleEditor.busy||!(group.bosses||[]).length;
  $('#boss-detail-rules').innerHTML=`<p class="sub">基础参数来自当前战斗编队，HP已计入编队倍率；不含开场被动、战斗BUFF和动态变身。展示 ${group.bosses?.length||0} 个难度。</p>`+(group.bosses||[]).map(bossRuleForm).join('');
}
$('#boss-rows').addEventListener('click',e=>{const b=e.target.closest('[data-boss-detail]');if(b)openBossDetails(Number(b.dataset.bossDetail))});
$('#boss-detail-rules').onchange=e=>{
  const el=e.target,kind=el.dataset.rule;if(!kind||kind==='select')return;
  const id=Number(el.closest('[data-boss]').dataset.boss),rule=bossRuleEditor.rules.get(id).draft;
  rule[kind]=el.checked;
  bossRuleEditor.dirty=true;
};
$('#boss-detail-rules').onclick=async e=>{
  const copy=e.target.closest('[data-copy-rule]'),drop=e.target.closest('[data-edit-drops]');
  if(copy){const rule=bossRuleEditor.rules.get(Number(copy.dataset.copyRule)).draft,ids=$$('#boss-detail-rules [data-rule="select"]:checked').map(el=>Number(el.closest('[data-boss]').dataset.boss));if(!ids.length)return toast('请先勾选要应用的难度');for(const id of ids)if(rule.own_deck&&!bossRuleEditor.rules.get(id).own_deck_boss_id)return toast(`难度 ${id} 缺少可用的自卡组战斗配置，未应用修改`,true);for(const id of ids)bossRuleEditor.rules.get(id).draft={...rule,boss_id:id};bossRuleEditor.dirty=true;renderBossRuleForms();toast(`已应用到 ${ids.length} 个难度草稿`)}
  if(drop){if(!closeBossDetails())return;await switchView('drops');if(dropEditor.dirty&&!confirm('切换难度将放弃掉落草稿，继续？'))return;await contentAction('drops',()=>selectDrop(Number(drop.dataset.editDrops)));$('#drop-select').value=drop.dataset.editDrops}
};
function closeBossDetails(){if(bossRuleEditor.busy)return false;if(bossRuleEditor.dirty&&!confirm('放弃未保存的BOSS规则修改？'))return false;$('#boss-detail-modal').classList.remove('open');bossRuleEditor.request++;bossRuleEditor.group=null;bossRuleEditor.dirty=false;return true}
$('#boss-detail-close').onclick=closeBossDetails;
$('#boss-detail-default').onclick=()=>{if(bossRuleEditor.busy||!bossRuleEditor.group)return;for(const b of bossRuleEditor.group.bosses||[]){const row=bossRuleEditor.rules.get(b.boss_id);if(row)row.draft=structuredClone(row.default)}bossRuleEditor.dirty=true;renderBossRuleForms()};
$('#boss-detail-save').onclick=async()=>{
  if(bossRuleEditor.busy||!bossRuleEditor.group)return;const rules=(bossRuleEditor.group.bosses||[]).map(b=>bossRuleEditor.rules.get(b.boss_id)?.draft).filter(Boolean);
  if(!rules.length||!confirm(`保存「${bossRuleEditor.group.name}」的 ${rules.length} 个难度规则？`))return;
  bossRuleEditor.busy=true;$('#boss-detail-rules').inert=true;$('#boss-detail-save').disabled=true;
  try{const data=await api('/api/boss-rules',{method:'PUT',body:JSON.stringify({expected_revision:bossRuleEditor.revision,rules})});bossRuleEditor.revision=data.revision;bossRuleEditor.dirty=false;state.loaded.delete('audit');toast('难度规则已保存，用于后续开战')}catch(e){toast(e.message,true)}finally{bossRuleEditor.busy=false;$('#boss-detail-rules').inert=false;$('#boss-detail-save').disabled=false}
};

const shopFilter=document.createElement('div');shopFilter.className='toolbar';
shopFilter.innerHTML='<input id="exchange-shop-search" class="search" placeholder="搜索兑换所名称或 ID"><select id="exchange-shop-status" aria-label="兑换所状态"><option value="">全部兑换所</option><option value="active">开放中</option><option value="closed">关闭／已结束</option><option value="deleted">已删除</option></select><button class="secondary" id="exchange-reset">清除筛选</button>';
$('#exchange-select').closest('.toolbar').before(shopFilter);
function exchangeShopMatches(s){const status=$('#exchange-shop-status').value;return matchesWords(`${s.name} ${s.trade_shopid}`,$('#exchange-shop-search').value)&&(!status||(status==='deleted'?s.deleted:status==='active'?!s.deleted&&!s.disabled&&s.end_time>Date.now()/1000:!s.deleted&&(s.disabled||s.end_time<=Date.now()/1000)))}
$('#exchange-shop-search').oninput=$('#exchange-shop-status').onchange=()=>{renderExchangeSelect();if(exchangeEditor.shop)$('#exchange-select').value=exchangeEditor.shop.trade_shopid};
$('#exchange-reset').onclick=()=>{$('#exchange-shop-search').value='';$('#exchange-shop-status').value='';renderExchangeSelect();if(exchangeEditor.shop)$('#exchange-select').value=exchangeEditor.shop.trade_shopid};

const gachaFilters=document.createElement('div');gachaFilters.className='toolbar';
gachaFilters.innerHTML='<input id="gacha-search" class="search" placeholder="搜索扭蛋名称、组 ID、卡池 ID 或消耗道具"><select id="gacha-status"><option value="">全部发布状态</option><option value="open">已勾选发布</option><option value="closed">未勾选发布</option></select><button class="secondary" id="gacha-select-filtered">勾选筛选结果</button><button class="secondary" id="gacha-clear-filtered">取消筛选结果</button>';
$('#gacha-rows').before(gachaFilters);
function filteredGachas(){const status=$('#gacha-status').value;return (state.gachaPresets||[]).filter(g=>matchesWords(`${g.name} ${g.group_id} ${g.gacha_ids.join(' ')} ${g.payment_item||''} ${g.payment_item_id}`,$('#gacha-search').value)&&(!status||state.gachaSelected.has(g.group_id)===(status==='open')))}
$('#gacha-search').oninput=$('#gacha-status').onchange=renderGachas;
$('#gacha-select-filtered').onclick=()=>{filteredGachas().forEach(g=>state.gachaSelected.add(g.group_id));renderGachas()};
$('#gacha-clear-filtered').onclick=()=>{filteredGachas().forEach(g=>state.gachaSelected.delete(g.group_id));renderGachas()};

const poolFilters=document.createElement('div');poolFilters.className='toolbar';
poolFilters.innerHTML='<input id="pool-name-search" class="search" placeholder="搜索卡池名称或 ID"><select id="pool-config-status"><option value="">全部配置状态</option><option value="open">排期内</option><option value="closed">未开始／已结束</option><option value="draft">有未发布草稿</option></select><span class="sub" id="pool-filter-count"></span>';
$('#pool-select').closest('.toolbar').before(poolFilters);
function renderPoolSelect(){
  const id=poolEditor.row?.gacha_id,status=$('#pool-config-status').value,now=Date.now()/1000;
  const rows=poolEditor.rows.filter(row=>{const c=row.config,open=(!c.start_unix||now>=c.start_unix)&&(!c.end_unix||now<c.end_unix);return matchesWords(`${c.name} ${row.gacha_id}`,$('#pool-name-search').value)&&(!status||(status==='draft'?!!row.draft.revision&&row.draft.sha256!==row.live.sha256:open===(status==='open'))) });
  const placeholder=id||!rows.length?`<option value="">${rows.length?'请选择卡池（当前草稿保留）':'没有匹配的卡池'}</option>`:'';
  $('#pool-select').innerHTML=placeholder+rows.map(row=>`<option value="${row.gacha_id}">${esc(row.config.name)} · ${row.gacha_id}</option>`).join('');
  if(id)$('#pool-select').value=id;
  $('#pool-filter-count').textContent=`匹配 ${rows.length}/${poolEditor.rows.length} 个卡池${id&&!rows.some(row=>row.gacha_id===id)?'；当前编辑对象不在筛选结果中':''}`;
}
$('#pool-name-search').oninput=$('#pool-config-status').onchange=renderPoolSelect;
for(const [value,label] of [['boss-rules','BOSS难度规则'],['account-mail-delete','删除玩家邮件'],['gacha-draft','卡池草稿'],['gacha-live','卡池发布']]){
  const option=document.createElement('option');option.value=value;option.textContent=label;$('#audit-operation').append(option);
}

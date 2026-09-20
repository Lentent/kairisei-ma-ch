'use strict';

const accountPage={page:0,total:0,request:0};
const selectedPlayers=new Map();
const auditPage={page:0,total:0,request:0,rows:[]};
const formatTime=value=>value?new Date(value).toLocaleString('zh-CN'):'—';
const playerLabel=a=>`${a.name||a.username||'未命名玩家'} · #${a.user_id}`;
function downloadJSON(name,value){const url=URL.createObjectURL(new Blob([JSON.stringify(value,null,2)],{type:'application/json'}));const a=document.createElement('a');a.href=url;a.download=name;a.click();setTimeout(()=>URL.revokeObjectURL(url),1000)}

async function loadAccountsPage(){
  const serial=++accountPage.request;
  const query=new URLSearchParams({...accountFilterQuery(),limit:50,offset:accountPage.page*50});
  const data=await api('/api/accounts?'+query);
  if(serial!==accountPage.request)return;
  accountPage.total=data.total;state.accounts=data.accounts;
  if(accountPage.page>0&&accountPage.page*50>=data.total){accountPage.page=Math.max(0,Math.ceil(data.total/50)-1);return loadAccountsPage()}
  renderAccounts();
}
function renderAccounts(){
  $('#account-count').textContent=`${num(accountPage.total)} 个匹配账号`;
  $('#account-rows').innerHTML=state.accounts.map(a=>`<tr><td>${a.user_id<1900000000?`<input class="check player-check" type="checkbox" aria-label="选择${esc(playerLabel(a))}" data-id="${a.user_id}" ${selectedPlayers.has(a.user_id)?'checked':''}>`:''}</td><td><b>${esc(a.name||'未命名')}</b><span class="sub">#${a.user_id} · Lv.${a.level} ${esc(cardJobName(a.arthur_type))} · 存档 r${a.revision}</span><span class="sub">金币 ${num(a.gold)} · 水晶 ${num(a.crystals)}</span></td><td>${esc(a.username||(a.user_id>=1900000000?'系统伙伴':'游客 · 未绑定'))}<details><summary class="sub">安装标识</summary><code>${esc(a.login_uuid)}</code></details></td><td>${esc(formatTime(a.last_login_utc))}<span class="sub">注册 ${esc(formatTime(a.created_utc))}</span></td><td><button class="secondary account-detail" data-user="${a.user_id}">详情</button> ${a.user_id<1900000000?`<button class="secondary mail-shortcut" data-user="${a.user_id}">寄礼物</button> <button class="secondary inbox-open" data-user="${a.user_id}">管理邮件</button> <button class="secondary grant" data-user="${a.user_id}">加资源</button> <button class="secondary credentials" data-user="${a.user_id}">${a.username?'管理绑定':'绑定账号'}</button>`:''}</td></tr>`).join('')||'<tr><td colspan="5" class="empty">没有匹配账号</td></tr>';
  let anchor=null;
  $$('.player-check').forEach((box,index)=>box.onclick=e=>{
    const boxes=$$('.player-check'),indices=e.shiftKey&&anchor!==null?[Math.min(anchor,index),Math.max(anchor,index)]:[index,index];
    for(let i=indices[0];i<=indices[1];i++){const a=state.accounts.find(a=>a.user_id===Number(boxes[i].dataset.id));if(box.checked){if(selectedPlayers.size>=500&&!selectedPlayers.has(a.user_id)){toast('每批最多500名玩家，请缩小范围',true);break}selectedPlayers.set(a.user_id,a)}else selectedPlayers.delete(a.user_id);boxes[i].checked=selectedPlayers.has(a.user_id)}
    anchor=index;updatePlayerSelection();
  });
  $$('.account-detail').forEach(b=>b.onclick=()=>openAccountDetail(Number(b.dataset.user)));
  $$('.inbox-open').forEach(b=>b.onclick=()=>openPlayerInbox(Number(b.dataset.user)));
  $$('.credentials').forEach(b=>b.onclick=()=>openCredentials(Number(b.dataset.user)));
  $$('.grant').forEach(b=>b.onclick=()=>openGrant(Number(b.dataset.user)));
  $$('.mail-shortcut').forEach(b=>b.onclick=()=>{const a=state.accounts.find(a=>a.user_id===Number(b.dataset.user));selectedPlayers.clear();selectedPlayers.set(a.user_id,a);updatePlayerSelection();switchView('mail')});
  renderPager('accounts',accountPage.page,accountPage.total,50,page=>{accountPage.page=page;loadAccountsPage().catch(e=>toast(e.message,true))});updatePlayerSelection();
}
function updatePlayerSelection(){
  $('#accounts-selection').textContent=`已选 ${selectedPlayers.size} 名玩家（跨页保留）`;
  $('#accounts-mail').disabled=!selectedPlayers.size;
  $('#mail-recipients').textContent=`收件人：${selectedPlayers.size} 名玩家`;
  $('#mail-recipient-preview').textContent=[...selectedPlayers.values()].slice(0,10).map(playerLabel).join('、')+(selectedPlayers.size>10?` 等${selectedPlayers.size}人`:'');
  if(typeof updateMailControls==='function')updateMailControls();
}
async function selectFilteredPlayers(){
  const filter=accountFilterQuery(),selected=[];if(filter.binding==='system')throw new Error('系统伙伴不能作为赠礼收件人');filter.include_system='0';
  for(let offset=0;;){const data=await api('/api/accounts?'+new URLSearchParams({...filter,limit:200,offset}));if(data.total>500)throw new Error('筛选结果超过500名玩家，请缩小搜索范围');selected.push(...data.accounts);offset+=data.accounts.length;if(offset>=data.total)break;if(!data.accounts.length)throw new Error('账号列表变化，请重试')}
  if(new Set([...selectedPlayers.keys(),...selected.map(a=>a.user_id)]).size>500)throw new Error('与原选择合计超过500人，请先清空选择');
  selected.forEach(a=>selectedPlayers.set(a.user_id,a));renderAccounts();
}
$('#accounts-select-page').onclick=()=>{const rows=state.accounts.filter(a=>a.user_id<1900000000);if(new Set([...selectedPlayers.keys(),...rows.map(a=>a.user_id)]).size>500)return toast('每批最多500人',true);rows.forEach(a=>selectedPlayers.set(a.user_id,a));renderAccounts()};
$('#accounts-select-filter').onclick=async()=>{const b=$('#accounts-select-filter');b.disabled=true;try{await selectFilteredPlayers()}catch(e){toast(e.message,true)}finally{b.disabled=false}};
$('#accounts-clear').onclick=()=>{selectedPlayers.clear();renderAccounts()};
$('#accounts-mail').onclick=()=>switchView('mail');
$('#mail-select-players').onclick=()=>switchView('accounts');
$('#account-search').oninput=()=>{accountPage.page=0;accountPage.request++;clearTimeout(loadAccountsPage.timer);loadAccountsPage.timer=setTimeout(()=>loadAccountsPage().catch(e=>toast(e.message,true)),200)};
$('#account-system').onchange=()=>{accountPage.page=0;loadAccountsPage().catch(e=>toast(e.message,true))};

async function loadAuditPage(){
  const serial=++auditPage.request;
  const data=await api('/api/audit?'+new URLSearchParams({q:$('#audit-search').value.trim(),operation:$('#audit-operation').value,limit:50,offset:auditPage.page*50}));
  if(serial!==auditPage.request)return;
  if(auditPage.page>0&&auditPage.page*50>=data.total){auditPage.page=Math.max(0,Math.ceil(data.total/50)-1);return loadAuditPage()}
  auditPage.rows=data.records;auditPage.total=data.total;
  $('#audit-rows').innerHTML=data.records.map(r=>`<tr><td>#${r.audit_id}<span class="sub">${esc(formatTime(r.created_utc))}</span></td><td>${esc(r.operation)}</td><td>${esc(r.target)}</td><td><details><summary>查看内容</summary><pre class="audit-details">${esc(JSON.stringify(r.payload,null,2))}</pre></details></td></tr>`).join('')||'<tr><td colspan="4" class="empty">暂无匹配操作</td></tr>';
  renderPager('audit',auditPage.page,data.total,50,page=>{auditPage.page=page;loadAuditPage().catch(e=>toast(e.message,true))});
}
$('#audit-search').oninput=()=>{auditPage.page=0;auditPage.request++;clearTimeout(loadAuditPage.timer);loadAuditPage.timer=setTimeout(()=>loadAuditPage().catch(e=>toast(e.message,true)),200)};
$('#audit-operation').onchange=()=>{auditPage.page=0;loadAuditPage().catch(e=>toast(e.message,true))};
$('#audit-export').onclick=()=>downloadJSON('操作记录.json',auditPage.rows);
function adminPolicyDirty(name){if(name==='player-policy')return playerPolicyDirty();
  if(name==='pool-editor')return poolEditor.dirty;
  if(typeof contentDirty==='function'&&contentDirty(name))return true;
  if(name==='settings')return runtimeSettingsDirty();
  const normalized=values=>JSON.stringify([...values].sort((a,b)=>a-b));
  if(name==='bosses'&&state.policy)return state.mode!==state.policy.mode||normalized(state.selected)!==normalized(state.policy.group_ids||[])||unixInput('#boss-start')!==(state.policy.start_unix||0)||unixInput('#boss-end')!==(state.policy.end_unix||0);
  if(name==='gachas'&&state.gachaPolicy)return normalized(state.gachaSelected)!==normalized(state.gachaPolicy.group_ids||[]);
  return false;
}

function updateDraftIndicators(){
  $$('.nav [data-view]').forEach(button=>{
    const dirty=adminPolicyDirty(button.dataset.view);
    button.classList.toggle('has-draft',dirty);
    button.title=dirty?'有未保存的修改，切换页面会保留草稿':'';
  });
}
document.addEventListener('input',updateDraftIndicators);
document.addEventListener('change',updateDraftIndicators);
document.addEventListener('click',updateDraftIndicators);
window.addEventListener('beforeunload',event=>{
  if(Object.keys(titles).some(adminPolicyDirty)||bossRuleEditor.dirty||state.publishing.size||state.grantBusy||state.grantRequest){event.preventDefault();event.returnValue=''}
});

async function openAccountDetail(id){
  const serial=(openAccountDetail.serial||0)+1;openAccountDetail.serial=serial;
  $('#account-detail-title').textContent=`账号 #${id}`;$('#account-detail-lead').textContent='正在加载该账号存档…';$('#account-detail-body').innerHTML='';$('#account-modal').classList.add('open');
  try{const data=await api(`/api/accounts/${id}`);if(serial!==openAccountDetail.serial)return;const a=data.account;$('#account-detail-title').textContent=`${a.name||'未命名账号'} · #${a.user_id}`;$('#account-detail-lead').textContent=`${a.login_uuid} · 存档 r${a.revision}`;renderAccountFacts(a)}
  catch(e){if(serial!==openAccountDetail.serial)return;$('#account-detail-lead').textContent='详情加载失败';toast(e.message,true)}
}
function openCredentials(id){const a=state.accounts.find(x=>x.user_id===id);if(!a||id>=1900000000)return;state.credentialUser=id;$('#credentials-title').textContent=`${a.username?'管理账号绑定':'绑定账号'} · #${id}`;$('#credentials-name').value=a.username||'';$('#credentials-name').disabled=!!a.username;$('#credentials-unbind').hidden=!a.username;$('#credentials-password').value='';$('#credentials-modal').classList.add('open')}
$('#credentials-cancel').onclick=()=>{$('#credentials-password').value='';$('#credentials-modal').classList.remove('open')};
function credentialsBusy(busy){['apply','unbind','cancel'].forEach(action=>$(`#credentials-${action}`).disabled=busy)}
async function refreshCredentials(){
  $('#credentials-password').value='';
  $('#credentials-modal').classList.remove('open');
  state.loaded.delete('audit');
  await loadView('accounts',true);
}
$('#credentials-apply').onclick=async()=>{const body={username:$('#credentials-name').value,password:$('#credentials-password').value};if(!/^[a-zA-Z0-9_]{3,32}$/.test(body.username.trim())||body.password.length<8){toast('请填写有效账号及至少 8 位密码',true);return}credentialsBusy(true);try{await api(`/api/accounts/${state.credentialUser}/credentials`,{method:'POST',body:JSON.stringify(body)});await refreshCredentials();toast('账号凭据已保存，角色进度保持不变')}catch(e){toast(e.message,true)}finally{credentialsBusy(false)}};
$('#credentials-unbind').onclick=async()=>{
  const id=state.credentialUser,username=$('#credentials-name').value;
  if(!confirm(`解除账号“${username}”与角色 #${id} 的绑定？\n原账号密码将失效，角色存档保留，已登录设备不会退出。解除后可重新绑定。`))return;
  credentialsBusy(true);
  try{
    await api(`/api/accounts/${id}/credentials`,{method:'DELETE',body:JSON.stringify({username})});
    await refreshCredentials();
    toast('已解除绑定，可点击“绑定账号”重新绑定；角色存档已保留');
  }catch(e){toast(e.message,true)}finally{credentialsBusy(false)}
};
function openGrant(id){
  if(state.grantBusy)return;
  const a=state.accounts.find(x=>x.user_id===id);if(!a)return;
  if(state.grantRequest&&!confirm('上次发放尚未确认。请先核对账号或审计，再开始新操作；是否放弃原重试编号？'))return;
  state.grantUser=id;state.grantRequest=null;
  $('#grant-level').max=state.status?.max_player_level||999;
  $('#grant-account').textContent=playerLabel(a);
  ['gold','fp','free','paid','level','rank'].forEach(x=>$(`#grant-${x}`).value=0);
  $('#grant-ap').checked=false;$('#grant-bp').checked=false;$('#grant-modal').classList.add('open');
}
function closeGrant(){
  if(state.grantBusy)return;
  if(state.grantRequest&&!confirm('发放结果尚未确认。关闭后请先核对账号和审计，避免重复操作。是否关闭？'))return;
  state.grantRequest=null;$('#grant-modal').classList.remove('open');
}
$('#grant-cancel').onclick=closeGrant;
$('#grant-modal').onclick=e=>{if(e.target.id==='grant-modal')closeGrant()};
$('#grant-apply').onclick=async()=>{
  if(state.grantBusy)return;
  if($$('#grant-modal input').some(el=>!el.reportValidity()))return;
  const id=state.grantUser;
  const body={target_level:Number($('#grant-level').value||0),target_arthur_rank:Number($('#grant-rank').value||0),gold:Number($('#grant-gold').value||0),friend_point:Number($('#grant-fp').value||0),free_crystal:Number($('#grant-free').value||0),paid_crystal:Number($('#grant-paid').value||0),fill_ap:$('#grant-ap').checked,fill_bp:$('#grant-bp').checked};
  const signature=JSON.stringify(body);
  if(state.grantRequest&&state.grantRequest.signature!==signature)return toast('上次请求尚未确认，先用原内容重试或关闭后核对账号',true);
  if(!confirm(`确认向账号 ${id} 发放所填资源？`))return;
  if(!state.grantRequest)state.grantRequest={signature,key:crypto.randomUUID()};
  body.idempotency_key=state.grantRequest.key;state.grantBusy=true;
  $$('#grant-modal input, #grant-modal button').forEach(el=>el.disabled=true);
  try{
    await api(`/api/accounts/${id}/grant`,{method:'POST',body:JSON.stringify(body)});
    state.grantRequest=null;$('#grant-modal').classList.remove('open');
    state.loaded.delete('dashboard');state.loaded.delete('accounts');state.loaded.delete('audit');
    await loadView('accounts',true);toast('资源已写入账号存档');
  }catch(e){toast(e.message+'；可用相同内容重试，不会重复增加',true)}
  finally{state.grantBusy=false;$$('#grant-modal input, #grant-modal button').forEach(el=>el.disabled=false)}
};
$('#account-detail-close').onclick=()=>$('#account-modal').classList.remove('open');$('#account-modal').onclick=e=>{if(e.target.id==='account-modal')$('#account-modal').classList.remove('open')};

$('#accounts-unselect-page').onclick=()=>{state.accounts.forEach(a=>selectedPlayers.delete(a.user_id));renderAccounts()};

const playerInbox={user:0,page:0,total:0,serial:0,busy:false,loading:false,rows:[],selected:new Map()};
function updateInboxControls(){
  const w=playerInbox;
  $$('#inbox-modal button, #inbox-modal input, #inbox-modal select').forEach(e=>e.disabled=w.busy);
  $('#inbox-delete').disabled=w.busy||w.loading||!w.selected.size;
  $('#inbox-select-page').disabled=w.busy||w.loading||!w.rows.length;
  $('#inbox-prev').disabled=w.busy||w.loading||!w.page;
  $('#inbox-next').disabled=w.busy||w.loading||(w.page+1)*50>=w.total;
  $('#inbox-info').textContent=`${w.total} 封 · 第 ${w.page+1} 页 · 已选 ${w.selected.size} 封`;
}
async function openPlayerInbox(id){
  const w=playerInbox;if(w.busy)return;
  clearTimeout(loadPlayerInbox.timer);w.user=id;w.page=0;w.selected.clear();
  $('#inbox-title').textContent=`邮件管理 · ${playerLabel(state.accounts.find(a=>a.user_id===id)||{user_id:id})}`;
  $('#inbox-search').value='';$('#inbox-filter').value='pending';$('#inbox-modal').classList.add('open');
  await loadPlayerInbox();
}
async function loadPlayerInbox(){
  const w=playerInbox,serial=++w.serial;w.loading=true;w.rows=[];
  $('#inbox-rows').innerHTML='<tr><td colspan="5">正在加载邮件…</td></tr>';updateInboxControls();
  try{
    const query=new URLSearchParams({offset:w.page*50,q:$('#inbox-search').value.trim(),status:$('#inbox-filter').value});
    const data=await api(`/api/accounts/${w.user}/mail?${query}`);if(serial!==w.serial)return;
    w.rows=data.items;w.total=data.total;
    if(w.page&&w.page*50>=w.total){w.page=Math.max(0,Math.ceil(w.total/50)-1);return loadPlayerInbox()}
    $('#inbox-rows').innerHTML=w.rows.map(p=>`<tr><td><input class="check inbox-check" type="checkbox" data-id="${esc(p.present_id)}" aria-label="选择${esc(p.title)}" ${w.selected.has(p.present_id)?'checked':''}></td><td><b>${esc(p.title)}</b><span class="sub">${esc(p.message)}</span><span class="sub">${p.issued_at_unix?esc(formatTime(p.issued_at_unix*1000)):''}</span></td><td>${esc(p.reward_name||`奖励 ${p.reward_type}/${p.reward_id}`)} × ${esc(p.quantity)}</td><td>${p.state===0?'未领取':'已领取／可删除'}</td><td><button class="secondary inbox-delete-one" data-id="${esc(p.present_id)}">删除</button></td></tr>`).join('')||'<tr><td colspan="5" class="empty">没有符合条件的邮件</td></tr>';
    $$('.inbox-check').forEach(box=>box.onchange=()=>{const p=w.rows.find(p=>p.present_id===box.dataset.id);if(box.checked){if(w.selected.size>=100){box.checked=false;return toast('每次最多选择 100 封邮件',true)}w.selected.set(p.present_id,p)}else w.selected.delete(p.present_id);updateInboxControls()});
    $$('.inbox-delete-one').forEach(b=>b.onclick=()=>deletePlayerInbox([w.rows.find(p=>p.present_id===b.dataset.id)]));
  }catch(e){if(serial===w.serial){$('#inbox-rows').innerHTML='<tr><td colspan="5">邮件加载失败，请刷新重试。</td></tr>';toast(e.message,true)}}
  finally{if(serial===w.serial){w.loading=false;updateInboxControls()}}
}
async function deletePlayerInbox(rows){
  const w=playerInbox;if(w.busy||w.loading||!rows.length)return;
  const pending=rows.filter(p=>p.state===0).length;
  if(!confirm(`删除玩家 #${w.user} 的 ${rows.length} 封邮件？\n其中 ${pending} 封尚未领取，删除后无法再领取。已领取奖励不会回收。\n\n${rows.slice(0,8).map(p=>p.title).join('\n')}${rows.length>8?'\n…':''}`))return;
  w.busy=true;updateInboxControls();
  try{
    const data=await api(`/api/accounts/${w.user}/mail/delete`,{method:'POST',body:JSON.stringify({present_ids:rows.map(p=>p.present_id)})});
    rows.forEach(p=>w.selected.delete(p.present_id));state.loaded.delete('audit');state.loaded.delete('accounts');
    toast(`已删除 ${data.deleted.length} 封邮件${data.already_absent?`，另有 ${data.already_absent} 封已不存在`:''}`);await loadPlayerInbox();
  }catch(e){toast(e.message+'；刷新列表核对后可重试',true)}finally{w.busy=false;updateInboxControls()}
}
$('#inbox-delete').onclick=()=>deletePlayerInbox([...playerInbox.selected.values()]);
$('#inbox-close').onclick=()=>{if(!playerInbox.busy){playerInbox.serial++;clearTimeout(loadPlayerInbox.timer);$('#inbox-modal').classList.remove('open')}};
$('#inbox-refresh').onclick=()=>loadPlayerInbox();
$('#inbox-prev').onclick=()=>{playerInbox.page--;loadPlayerInbox()};
$('#inbox-next').onclick=()=>{playerInbox.page++;loadPlayerInbox()};
$('#inbox-filter').onchange=()=>{playerInbox.page=0;loadPlayerInbox()};
$('#inbox-search').oninput=()=>{playerInbox.page=0;playerInbox.serial++;clearTimeout(loadPlayerInbox.timer);loadPlayerInbox.timer=setTimeout(loadPlayerInbox,200)};
$('#inbox-clear').onclick=()=>{playerInbox.selected.clear();loadPlayerInbox()};
$('#inbox-select-page').onclick=()=>{for(const p of playerInbox.rows){if(playerInbox.selected.size>=100&&!playerInbox.selected.has(p.present_id)){toast('每次最多选择 100 封邮件',true);break}playerInbox.selected.set(p.present_id,p)}$$('.inbox-check').forEach(b=>b.checked=playerInbox.selected.has(b.dataset.id));updateInboxControls()};

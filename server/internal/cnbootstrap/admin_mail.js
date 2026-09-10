'use strict';

const mailWorkspace={basket:new Map(),running:false,paused:false,busy:false,current:null,jobsPage:0,errors:[]};
const rewardKey=e=>`${e.reward_type}:${e.reward_type_id}`;
const rewardLimit=e=>[6,15,19].includes(e.reward_type)?100:10000000;
const pendingBatchKey='cn602-admin-pending-batch-v2';
let pendingBatch=null;
try{pendingBatch=JSON.parse(localStorage.getItem(pendingBatchKey)||'null')}catch{/* The server batch list remains authoritative. */}

async function loadMailWorkspace(){await Promise.all([loadCatalog(),loadMailJobs()]);if(!mailWorkspace.draftLoaded){mailWorkspace.draftLoaded=true;try{const draft=JSON.parse(localStorage.getItem('cn602-admin-reward-draft-v2')||'[]');if(draft.length)await importRewardRequests(draft)}catch(e){toast('奖励草稿未恢复：'+e.message,true)}}updatePlayerSelection();renderBasket();if(pendingBatch&&!mailWorkspace.current){try{await openMailBatch(pendingBatch.batch_id)}catch(e){toast('上次批次尚未确认创建，点击继续时会使用原编号重试',true);updateMailControls()}}}
async function loadCatalog(){
  const serial=++state.catalogRequest;
  $('#catalog-status').textContent='正在加载目录…';
  try{
    const data=await api('/api/catalog?'+new URLSearchParams({kind:state.catalogKind,source:state.catalogKind==='card'?state.catalogSource:'',arthur_type:state.catalogKind==='card'?state.catalogJob:0,q:$('#catalog-search').value.trim(),limit:120,offset:state.catalogPage*120}));
    if(serial!==state.catalogRequest)return;
    state.catalog=data.entries;state.catalogTotal=data.total;
    if(state.catalogPage>0&&state.catalogPage*120>=data.total){state.catalogPage=Math.max(0,Math.ceil(data.total/120)-1);return loadCatalog()}
    renderCatalog();
  }catch(e){if(serial!==state.catalogRequest)return;$('#catalog-status').textContent=e.message;toast(e.message,true)}
}
function renderCatalog(){
  $('#catalog-jobs').hidden=state.catalogKind!=='card';$('#catalog-sources').hidden=state.catalogKind!=='card';
  for(const [group,prop] of [['kinds','kind'],['sources','source'],['jobs','job']])$$(`#catalog-${group} button`).forEach(b=>b.classList.toggle('active',String(b.dataset[prop])===String(state['catalog'+prop[0].toUpperCase()+prop.slice(1)])));
  $('#catalog-count').textContent=`${num(state.catalogTotal)} 项`;
  $('#catalog-status').textContent=`已选 ${mailWorkspace.basket.size} 种奖励 · 本页 ${state.catalog.length} 项`;
  $('#catalog-grid').innerHTML=state.catalog.map((e,i)=>`<label class="catalog-card ${mailWorkspace.basket.has(rewardKey(e))?'selected':''} ${e.resource_state==='unavailable'?'unavailable':''}"><input type="checkbox" class="catalog-check check" data-index="${i}" ${mailWorkspace.basket.has(rewardKey(e))?'checked':''} ${e.resource_state==='unavailable'?'disabled':''} aria-label="选择${esc(e.name)}"><div class="thumb">${image(e.image_url,e.name)}</div><div class="catalog-copy"><b class="name" title="${esc(e.name)}">${esc(e.name)}</b><span class="sub">${e.reward_type_id||'货币'} ${esc(cardJobName(e.arthur_type))}</span><span class="sub">${esc(e.detail||'')}${e.resource_state==='unavailable'?' · 资源暂不可用':''}</span></div></label>`).join('')||'<div class="empty">没有匹配奖励</div>';
  let anchor=null;
  $$('.catalog-check').forEach(box=>box.onclick=e=>{
    const index=Number(box.dataset.index),first=e.shiftKey&&anchor!==null?Math.min(anchor,index):index,last=e.shiftKey&&anchor!==null?Math.max(anchor,index):index;
    try{for(let i=first;i<=last;i++){const entry=state.catalog[i];if(entry.resource_state==='unavailable')continue;if(box.checked)addReward(entry);else mailWorkspace.basket.delete(rewardKey(entry))}}catch(err){toast(err.message,true)}
    anchor=index;renderBasket();$$('.catalog-check').forEach(c=>{c.checked=mailWorkspace.basket.has(rewardKey(state.catalog[Number(c.dataset.index)]));c.closest('.catalog-card').classList.toggle('selected',c.checked)});
  });
  renderPager('catalog',state.catalogPage,state.catalogTotal,120,page=>{state.catalogPage=page;loadCatalog()});
}
function rewardDefaults(e){
  const quantity=Number($('#mail-quantity').value);
  if(!Number.isInteger(quantity)||quantity<1||quantity>10000000)throw new Error('数量必须为1–10000000的整数');
  const card=e.reward_type===6;
  return {reward_type:e.reward_type,reward_type_id:e.reward_type_id,quantity:Math.min(quantity,rewardLimit(e)),card_level:card?($('#mail-max-level').checked?e.level_max:1):0,card_fame:card?($('#mail-max-fame').checked?e.fame_max:1):0,card_love:card&&$('#mail-max-love').checked?e.love_max:0};
}
function addReward(entry,input){
  const key=rewardKey(entry);
  if(mailWorkspace.basket.has(key))return;
  if(mailWorkspace.basket.size>=120)throw new Error('每批最多120种奖励，请拆分成多个方案');
  if(entry.resource_state==='unavailable')throw new Error(`${entry.name} 资源暂不可用`);
  mailWorkspace.basket.set(key,{entry,request:input||rewardDefaults(entry)});
}
function renderBasket(){
  $('#catalog-status').textContent=`已选 ${mailWorkspace.basket.size} 种奖励 · 本页 ${state.catalog.length} 项`;
  $('#mail-basket-count').textContent=`${mailWorkspace.basket.size} 种奖励`;
  $('#mail-basket').innerHTML=[...mailWorkspace.basket.entries()].map(([key,{entry:e,request:r}])=>`<tr><td><b>${esc(e.name)}</b><span class="sub">${esc(cardJobName(e.arthur_type))} ${e.reward_type===6?`Lv.${r.card_level} · 名声${r.card_fame} · 忠诚度${r.card_love}`:''}</span></td><td><input class="basket-quantity" type="number" min="1" max="${rewardLimit(e)}" value="${r.quantity}" data-key="${key}" aria-label="${esc(e.name)}数量"></td><td><button class="secondary basket-remove" data-key="${key}" aria-label="移除${esc(e.name)}">移除</button></td></tr>`).join('')||'<tr><td colspan="3" class="empty">从左侧勾选奖励</td></tr>';
  $$('.basket-quantity').forEach(input=>input.onchange=()=>{const item=mailWorkspace.basket.get(input.dataset.key),n=Number(input.value);if(!Number.isInteger(n)||n<1||n>rewardLimit(item.entry)){input.value=item.request.quantity;return toast(`数量须为1–${rewardLimit(item.entry)}的整数`,true)}item.request.quantity=n;saveMailDraft()});
  $$('.basket-remove').forEach(b=>b.onclick=()=>{mailWorkspace.basket.delete(b.dataset.key);renderBasket();renderCatalog()});
  saveMailDraft();updateMailControls();
}
function saveMailDraft(){
  try{localStorage.setItem('cn602-admin-reward-draft-v2',JSON.stringify([...mailWorkspace.basket.values()].map(x=>x.request)))}catch{/* Export remains available if browser storage is full. */}
}
function updateMailControls(){
  const w=mailWorkspace,c=w.current;
  $('#mail-send').disabled=w.busy||w.running||!w.basket.size||!selectedPlayers.size;
  $('#mail-job-retry').disabled=w.busy||w.running||!(c?.pending.length||pendingBatch&&!c);
  $('#mail-job-pause').disabled=!w.running;$('#mail-job-export').disabled=!c;
  if(c)$('#mail-job-status').textContent=`${c.batch.title} · ${c.batch.batch_id} · 已完成 ${c.done.length}/${c.batch.user_ids.length} 名玩家${w.running?' · 发放中':c.pending.length?' · 待继续':' · 全部完成'}`;
}
async function loadMailJobs(){
  const data=await api('/api/mail-batches?'+new URLSearchParams({limit:20,offset:mailWorkspace.jobsPage*20}));
  $('#mail-jobs').innerHTML=data.batches.map(b=>`<tr><td><b>${esc(b.title)}</b><span class="sub">${esc(b.batch_id)}</span></td><td>${esc(formatTime(b.created_utc))}</td><td>${b.rewards}</td><td>${b.done}/${b.recipients}</td><td><button class="secondary mail-job-open" data-id="${esc(b.batch_id)}">查看／继续</button></td></tr>`).join('')||'<tr><td colspan="5" class="empty">暂无发放批次</td></tr>';
  $$('.mail-job-open').forEach(b=>b.onclick=async()=>{if(mailWorkspace.running)return toast('请先暂停当前批次');try{await openMailBatch(b.dataset.id)}catch(e){toast(e.message,true)}});
  const total=data.total??data.batches.length;
  $('#mail-jobs-info').textContent=`${mailWorkspace.jobsPage+1}/${Math.max(1,Math.ceil(total/20))} 页 · ${total} 个批次`;
  $('#mail-jobs-prev').disabled=mailWorkspace.jobsPage===0;$('#mail-jobs-next').disabled=(mailWorkspace.jobsPage+1)*20>=total;
}
async function openMailBatch(id){mailWorkspace.current=await api(`/api/mail-batches/${encodeURIComponent(id)}`);mailWorkspace.errors=[];$('#mail-job-errors').textContent='';updateMailControls()}
async function runMailBatch(){
  const w=mailWorkspace;if(w.running||w.busy)return;w.running=true;w.paused=false;w.errors=[];updateMailControls();
  try{
    if(!w.current&&pendingBatch){await api('/api/mail-batches',{method:'POST',body:JSON.stringify(pendingBatch)});await openMailBatch(pendingBatch.batch_id)}
    if(!w.current)return;
    const id=w.current.batch.batch_id;await openMailBatch(id);const queue=[...w.current.pending];
    for(let i=0;i<queue.length&&!w.paused;i+=10){
      const data=await api(`/api/mail-batches/${encodeURIComponent(id)}/run`,{method:'POST',body:JSON.stringify({user_ids:queue.slice(i,i+10)})});
      const delivered=new Set(data.results.filter(r=>r.ok).map(r=>r.user_id));
      data.results.filter(r=>!r.ok).forEach(r=>w.errors.push(`#${r.user_id}：${r.error}`));
      w.current.done=[...new Set([...w.current.done,...delivered])];w.current.pending=w.current.pending.filter(id=>!delivered.has(id));
      $('#mail-job-errors').textContent=w.errors.join('\n');updateMailControls();
    }
    const failures=[...w.errors];w.current=await api(`/api/mail-batches/${encodeURIComponent(id)}`);w.errors=failures;
    if(!w.current.pending.length){pendingBatch=null;try{localStorage.removeItem(pendingBatchKey)}catch{}toast('批次全部发放完成')}
    else toast(w.paused?'已暂停；已完成玩家不会重复发放':`仍有${w.current.pending.length}人未完成，可重试`,!!w.errors.length);
    state.loaded.delete('accounts');state.loaded.delete('audit');await loadMailJobs();
  }catch(e){$('#mail-job-errors').textContent=`${w.errors.join('\n')}\n连接或执行中断：${e.message}。可用同一批次重试，服务端会跳过已完成玩家。`;toast('发放已暂停，请查看批次详情',true)}finally{w.running=false;updateMailControls()}
}
$('#mail-send').onclick=async()=>{
  const w=mailWorkspace;if(w.running||w.busy)return;w.busy=true;updateMailControls();
  try{
    const body={batch_id:crypto.randomUUID(),user_ids:[...selectedPlayers.keys()],title:$('#mail-title').value,message:$('#mail-message').value,rewards:[...w.basket.values()].map(x=>({...x.request}))};
    const preview=await api('/api/mail-batches/preview',{method:'POST',body:JSON.stringify(body)});
    const list=preview.batch.rewards.map((r,i)=>`${preview.catalog[i].name} × ${r.quantity}${r.reward_type===6?`（Lv.${r.card_level}，名声${r.card_fame}，忠诚度${r.card_love}）`:''}`);
    $('#mail-review').textContent=`收件玩家 ${preview.recipient_count} 人：\n${preview.batch.user_ids.map(id=>playerLabel(selectedPlayers.get(id)||{user_id:id})).join('、')}\n\n每人收到：\n${list.join('\n')}\n\n共 ${preview.mail_count} 封礼物\n标题：${preview.batch.title}\n正文：${preview.batch.message}`;
    $('#mail-review-modal').classList.add('open');$('#mail-review-confirm').disabled=false;
    let confirmed=false;
    $('#mail-review-confirm').onclick=async()=>{
      if(confirmed||w.running||w.busy)return;confirmed=true;w.busy=true;$('#mail-review-confirm').disabled=true;updateMailControls();
      try{pendingBatch=preview.batch;localStorage.setItem(pendingBatchKey,JSON.stringify(pendingBatch));await api('/api/mail-batches',{method:'POST',body:JSON.stringify(pendingBatch)});await openMailBatch(pendingBatch.batch_id);$('#mail-review-modal').classList.remove('open');w.busy=false;await runMailBatch()}
      catch(e){toast(e.message+'；可从批次继续按钮重试',true);$('#mail-review-modal').classList.remove('open');mailWorkspace.current=null}
      finally{w.busy=false;updateMailControls()}
    };
  }catch(e){toast(e.message,true)}finally{w.busy=false;updateMailControls()}
};
$('#mail-job-retry').onclick=runMailBatch;
$('#mail-job-pause').onclick=()=>{mailWorkspace.paused=true;$('#mail-job-pause').disabled=true;$('#mail-job-status').textContent+=' · 当前小批完成后暂停'};
$('#mail-job-export').onclick=()=>downloadJSON(`礼物批次-${mailWorkspace.current.batch.batch_id}.json`,mailWorkspace.current);
$('#mail-jobs-refresh').onclick=()=>loadMailJobs().catch(e=>toast(e.message,true));
$('#mail-jobs-prev').onclick=()=>{mailWorkspace.jobsPage--;loadMailJobs().catch(e=>toast(e.message,true))};
$('#mail-jobs-next').onclick=()=>{mailWorkspace.jobsPage++;loadMailJobs().catch(e=>toast(e.message,true))};
$('#mail-review-cancel').onclick=()=>$('#mail-review-modal').classList.remove('open');
$('#catalog-select-page').onclick=()=>{try{const rows=state.catalog.filter(e=>e.resource_state!=='unavailable');if(new Set([...mailWorkspace.basket.keys(),...rows.map(rewardKey)]).size>120)throw new Error('本页与现有选择合计超过120种，请缩小范围');rows.forEach(e=>addReward(e));renderBasket();renderCatalog()}catch(e){toast(e.message,true)}};
$('#basket-clear').onclick=()=>{mailWorkspace.basket.clear();renderBasket();renderCatalog()};
$('#basket-apply-defaults').onclick=()=>{try{for(const item of mailWorkspace.basket.values())item.request=rewardDefaults(item.entry);renderBasket()}catch(e){toast(e.message,true)}};
$('#basket-export').onclick=()=>downloadJSON('奖励方案.json',{schema_version:1,rewards:[...mailWorkspace.basket.values()].map(x=>x.request)});
async function importRewardRequests(rewards){
  if(!Array.isArray(rewards)||!rewards.length||rewards.length>120)throw new Error('奖励方案需要1–120项');
  const data=await api('/api/catalog/resolve',{method:'POST',body:JSON.stringify({rewards})});
  const next=new Map();
  rewards.forEach((r,i)=>{const e=data.entries[i];if(next.has(rewardKey(e)))throw new Error('奖励方案含重复物品');next.set(rewardKey(e),{entry:e,request:{...r,card_level:e.reward_type===6?(r.card_level||1):0,card_fame:e.reward_type===6?(r.card_fame||1):0,card_love:r.card_love||0}})});
  mailWorkspace.basket=next;renderBasket();renderCatalog();
}
$('#basket-import').onchange=async e=>{try{const file=e.target.files[0];if(!file)return;if(file.size>65536)throw new Error('方案文件超过64KB');const data=JSON.parse(await file.text());if(data.schema_version!==1)throw new Error('方案版本不支持');await importRewardRequests(data.rewards);toast('已载入奖励方案，发放前会再次验证')}catch(err){toast(err.message,true)}finally{e.target.value=''}};
$('#mail-add-ids').onclick=async()=>{
  const ids=[...new Set($('#mail-recipient-list').value.split(/[\s,，]+/).filter(Boolean).map(Number))];const button=$('#mail-add-ids');button.disabled=true;
  try{if(!ids.length||ids.some(id=>!Number.isInteger(id)||id<1000001||id>=1900000000)||new Set([...selectedPlayers.keys(),...ids]).size>500)throw new Error('请输入有效玩家ID，每批最多500人');const data=await api('/api/accounts/resolve',{method:'POST',body:JSON.stringify({user_ids:ids})});const known=new Set(data.accounts.map(a=>a.user_id));if(ids.some(id=>!known.has(id)))throw new Error('这些玩家不存在：'+ids.filter(id=>!known.has(id)).join('、'));data.accounts.forEach(a=>selectedPlayers.set(a.user_id,a));updatePlayerSelection();toast('已添加收件人')}catch(e){toast(e.message,true)}finally{button.disabled=false}
};
for(const [group,prop] of [['kinds','kind'],['sources','source'],['jobs','job']])$$(`#catalog-${group} button`).forEach(b=>b.onclick=()=>{state['catalog'+prop[0].toUpperCase()+prop.slice(1)]=prop==='job'?Number(b.dataset[prop]):b.dataset[prop];state.catalogPage=0;loadCatalog()});
$('#catalog-search').oninput=()=>{state.catalogPage=0;state.catalogRequest++;clearTimeout(loadCatalog.timer);loadCatalog.timer=setTimeout(loadCatalog,200)};
window.addEventListener('beforeunload',event=>{if(mailWorkspace.running){event.preventDefault();event.returnValue=''}});
updateMailControls();

$('#catalog-unselect-page').onclick=()=>{state.catalog.forEach(e=>mailWorkspace.basket.delete(rewardKey(e)));renderBasket();renderCatalog()};

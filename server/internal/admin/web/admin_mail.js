'use strict';

const mailWorkspace={basket:new Map(),running:false,paused:false,busy:false,current:null,jobsPage:0,errors:[]};
const rewardKey=e=>`${e.reward_type}:${e.reward_type_id}`;
const rewardLimit=e=>[14,16,18].includes(e.reward_type)?1:[6,15,19].includes(e.reward_type)?100:10000000;
const pendingBatchKey='cn602-admin-pending-batch-v2';
// Set when the recipient list came from "全部玩家"; any later change to the list clears it.
let recipientScope=null;
// All real players (system partners excluded). One batch holds at most 500, so larger servers are asked to split by filters.
async function fetchAllPlayers(){
  const rows=[];
  for(let offset=0;;){const data=await api('/api/accounts?'+new URLSearchParams({include_system:'0',limit:200,offset}));if(data.total>500)throw new Error(`当前共有 ${data.total} 名玩家，超过单批 500 人上限；请在“账号管理”按注册／登录时间等条件分批选择`);rows.push(...data.accounts);offset+=data.accounts.length;if(offset>=data.total||!data.accounts.length)break}
  if(!rows.length)throw new Error('当前没有可发放礼物的玩家');
  return rows;
}
async function selectAllPlayers(){
  const rows=await fetchAllPlayers();
  if(!confirm(`将收件人设为全部 ${rows.length} 名玩家（不含系统伙伴）？${selectedPlayers.size?`\n当前已选的 ${selectedPlayers.size} 名将被替换。`:''}\n名单在预览时固定，之后注册的玩家不会收到这批礼物。`))return false;
  selectedPlayers.clear();rows.forEach(a=>selectedPlayers.set(a.user_id,a));recipientScope={count:rows.length};updatePlayerSelection();state.loaded.delete('accounts');toast(`已选择全部 ${rows.length} 名玩家`);return true;
}
let pendingBatch=null;
try{pendingBatch=JSON.parse(localStorage.getItem(pendingBatchKey)||'null')}catch{/* The server batch list remains authoritative. */}

async function loadMailWorkspace(){await Promise.all([loadCatalog(),loadMailJobs()]);if(!mailWorkspace.draftLoaded){mailWorkspace.draftLoaded=true;try{const draft=JSON.parse(localStorage.getItem('cn602-admin-reward-draft-v2')||'[]');if(draft.length)await importRewardRequests(draft)}catch(e){toast('奖励草稿未恢复：'+e.message,true)}}updatePlayerSelection();renderBasket();if(pendingBatch&&!mailWorkspace.current){try{await openMailBatch(pendingBatch.batch_id)}catch(e){toast('上次批次尚未确认创建，点击继续时会使用原编号重试',true);updateMailControls()}}}
async function loadCatalog(){
  const serial=++state.catalogRequest;
  $('#catalog-status').textContent='正在加载目录…';$('#catalog-grid').setAttribute('aria-busy','true');
  try{
    const data=await api('/api/catalog?'+new URLSearchParams({...cardFilterQuery('catalog',state.catalogKind==='card'),kind:state.catalogKind,source:state.catalogKind==='card'?state.catalogSource:'',arthur_type:state.catalogKind==='card'?state.catalogJob:0,rarity:(state.catalogKind==='card')?state.catalogRarity:0,q:$('#catalog-search').value.trim(),limit:120,offset:state.catalogPage*120}));
    if(serial!==state.catalogRequest)return;
    state.catalog=data.entries;state.catalogTotal=data.total;
    if(state.catalogPage>0&&state.catalogPage*120>=data.total){state.catalogPage=Math.max(0,Math.ceil(data.total/120)-1);return loadCatalog()}
    renderCatalog();
  }catch(e){if(serial!==state.catalogRequest)return;$('#catalog-status').textContent='目录加载失败';$('#catalog-grid').innerHTML=`<div class="empty"><b>目录加载失败</b>${esc(errorText(e))}</div>`;toast(errorText(e),true)}
  finally{if(serial===state.catalogRequest)$('#catalog-grid').removeAttribute('aria-busy')}
}
function catalogStatusText(){const here=state.catalog.filter(e=>mailWorkspace.basket.has(rewardKey(e))).length;return `<b>奖励篮已选 ${mailWorkspace.basket.size} 种</b><span>本页 ${state.catalog.length} 项，其中已选 ${here} 项</span>`}
function renderCatalog(){
  const card=state.catalogKind==='card';
  $('#catalog-rarity').hidden=!card;
  $('#catalog-jobs').closest('.chip-row').hidden=!card;$('#catalog-sources').closest('.chip-row').hidden=!card;
  for(const [group,prop] of [['kinds','kind'],['sources','source'],['jobs','job']])$$(`#catalog-${group} button`).forEach(b=>b.classList.toggle('active',String(b.dataset[prop])===String(state['catalog'+prop[0].toUpperCase()+prop.slice(1)])));
  const filters=[...$$('#catalog-kinds button.active')].map(b=>b.textContent);
  if(card){const source=$('#catalog-sources button.active'),job=$('#catalog-jobs button.active');if(source?.dataset.source)filters.push('来源：'+source.textContent);if(job&&job.dataset.job!=='0')filters.push('职业：'+job.textContent)}
  if(!$('#catalog-rarity').hidden&&state.catalogRarity)filters.push(`${state.catalogRarity} 星`);
  if(card)for(const key of ['attribute','cost','resource']){const el=$(`#catalog-${key}`);if(el?.value)filters.push(el.selectedOptions[0].textContent)}
  if($('#catalog-search').value.trim())filters.push(`搜索：${$('#catalog-search').value.trim()}`);
  $('#catalog-filter-summary').textContent=`当前筛选：${filters.join(' · ')} · 共 ${num(state.catalogTotal)} 项`;
  $('#catalog-count').textContent=`${num(state.catalogTotal)} 项`;
  $('#catalog-status').innerHTML=catalogStatusText();
  $('#catalog-grid').innerHTML=state.catalog.map((e,i)=>{const on=mailWorkspace.basket.has(rewardKey(e)),bad=e.resource_state==='unavailable';return `<label class="catalog-card ${on?'selected':''} ${bad?'unavailable':''}" title="${bad?'资源暂不可用，不能发放':''}"><input type="checkbox" class="catalog-check check" data-index="${i}" ${on?'checked':''} ${bad?'disabled':''} aria-label="选择${esc(e.name)}"><div class="thumb">${image(e.image_url,e.name)}</div><div class="catalog-copy"><b class="name" title="${esc(e.name)}">${esc(e.name)}</b><span class="meta">${catalogMeta(e)}${bad?'<span class="tag bad">资源暂不可用</span>':''}</span><span class="facts-text">${esc(e.kind==='card'?cardSourceDescription(e):e.detail||'')}</span></div></label>`}).join('')||'<div class="empty"><b>没有匹配的奖励</b>换个关键词，或点击“清除筛选”。</div>';
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
  if(!Number.isInteger(quantity)||quantity<1||quantity>10000000){markInvalid($('#mail-quantity'));throw new Error('默认数量必须为1–10000000的整数')}
  $('#mail-quantity').classList.remove('invalid');
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
  $('#catalog-status').innerHTML=catalogStatusText();
  $('#mail-basket-count').textContent=`${mailWorkspace.basket.size} / 120 种`;
  $('#mail-basket').innerHTML=[...mailWorkspace.basket.entries()].map(([key,{entry:e,request:r}])=>`<tr><td><b>${esc(e.name)}</b><span class="sub">${e.kind==='currency'?'货币':`<code>${e.reward_type_id}</code>`} ${esc(e.kind==='card'?cardJobName(e.arthur_type):'')} ${esc(cardStars(e))}${e.reward_type===6?` · Lv.${r.card_level} · 名声${r.card_fame} · 忠诚度${r.card_love}`:''}</span></td><td><input class="basket-quantity" type="number" min="1" max="${rewardLimit(e)}" value="${r.quantity}" data-key="${key}" aria-label="${esc(e.name)}数量"></td><td><button class="x-button basket-remove" data-key="${key}" aria-label="移除${esc(e.name)}"><svg class="icon"><use href="#i-close"/></svg></button></td></tr>`).join('')||emptyRow(3,'奖励篮是空的','在左侧目录中勾选奖励，或导入奖励方案。');
  $$('.basket-quantity').forEach(input=>input.onchange=()=>{const item=mailWorkspace.basket.get(input.dataset.key),n=Number(input.value);if(!Number.isInteger(n)||n<1||n>rewardLimit(item.entry)){markInvalid(input);input.value=item.request.quantity;return toast(`「${item.entry.name}」数量须为1–${rewardLimit(item.entry)}的整数，已恢复为 ${item.request.quantity}`,true)}input.classList.remove('invalid');item.request.quantity=n;saveMailDraft()});
  $$('.basket-remove').forEach(b=>b.onclick=()=>{mailWorkspace.basket.delete(b.dataset.key);renderBasket();renderCatalog()});
  saveMailDraft();updateMailControls();
}
function saveMailDraft(){
  try{localStorage.setItem('cn602-admin-reward-draft-v2',JSON.stringify([...mailWorkspace.basket.values()].map(x=>x.request)))}catch{/* Export remains available if browser storage is full. */}
}
function updateMailSteps(){
  const players=selectedPlayers.size,rewards=mailWorkspace.basket.size,title=$('#mail-title').value.trim(),message=$('#mail-message').value.trim();
  const set=(step,done,text,warn=false)=>{const li=$(`#mail-steps [data-step="${step}"]`);li.classList.toggle('done',done);li.classList.toggle('warn',warn);if(text!==undefined)li.querySelector('em').textContent=text};
  set('players',players>0,recipientScope?`全部 ${players} 名`:`${players} 名`,!players);set('rewards',rewards>0,`${rewards} 种`,!rewards);set('content',!!title&&!!message,title&&message?'已填写':'待填写',!title||!message);
  set('send',!!mailWorkspace.current&&!mailWorkspace.current.pending.length,mailWorkspace.running?'发放中…':undefined);
}
function updateMailControls(){
  const w=mailWorkspace,c=w.current;
  $('#mail-send').disabled=w.busy||w.running||!w.basket.size||!selectedPlayers.size;
  $('#mail-send').title=!selectedPlayers.size?'请先选择收件人':!w.basket.size?'请先选择奖励':'';
  $('#mail-job-retry').disabled=w.busy||w.running||!(c?.pending.length||pendingBatch&&!c);
  $('#mail-job-pause').disabled=!w.running;$('#mail-job-export').disabled=!c;$('#mail-job-reuse').disabled=!c||w.running||w.busy;
  if(c){const total=c.batch.user_ids.length,done=c.done.length;$('#mail-job-status').innerHTML=`<span>${esc(c.batch.title)} <code>${esc(c.batch.batch_id)}</code></span> <span class="tag ${w.running?'info':c.pending.length?'warn':'ok'}">${w.running?'发放中':c.pending.length?'待继续':'全部完成'}</span><span class="sub">已完成 ${done}/${total} 名玩家 · 每人 ${c.batch.rewards.length} 种奖励</span><div class="progress-line"><i style="width:${total?Math.round(done/total*100):0}%"></i></div>`}
  else if(pendingBatch)$('#mail-job-status').textContent=`上次发放「${pendingBatch.title}」尚未确认创建；点击“继续／重试”会使用原批次编号，不会重复发放。`;
  updateMailSteps();
}
async function loadMailJobs(){
  const serial=(loadMailJobs.serial||0)+1;loadMailJobs.serial=serial;
  const data=await api('/api/mail-batches?'+new URLSearchParams({limit:20,offset:mailWorkspace.jobsPage*20,q:$('#mail-jobs-search').value.trim(),status:$('#mail-jobs-status').value}));
  if(serial!==loadMailJobs.serial)return;
  $('#mail-jobs').innerHTML=data.batches.map(b=>`<tr class="${mailWorkspace.current?.batch.batch_id===b.batch_id?'selected':''}"><td class="cell-wrap"><b>${esc(b.title)}</b><span class="sub"><code>${esc(b.batch_id)}</code></span></td><td>${esc(formatTime(b.created_utc))}</td><td class="num">${b.rewards}</td><td>${b.done}/${b.recipients} ${b.done<b.recipients?'<span class="tag warn">未完成</span>':'<span class="tag ok">完成</span>'}</td><td><button class="secondary sm mail-job-open" data-id="${esc(b.batch_id)}">查看／继续</button></td></tr>`).join('')||emptyRow(5,$('#mail-jobs-search').value.trim()||$('#mail-jobs-status').value?'没有匹配的批次':'暂无发放批次',$('#mail-jobs-search').value.trim()||$('#mail-jobs-status').value?'调整搜索或状态筛选。':'确认发放后批次会出现在这里。');
  $$('.mail-job-open').forEach(b=>b.onclick=async()=>{if(mailWorkspace.running)return toast('请先暂停当前批次');try{await openMailBatch(b.dataset.id);loadMailJobs().catch(()=>{})}catch(e){toast(errorText(e),true)}});
  const total=data.total??data.batches.length;
  $('#mail-jobs-info').textContent=`${mailWorkspace.jobsPage+1} / ${Math.max(1,Math.ceil(total/20))} 页 · 共 ${total} 个批次`;
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
    else toast(w.paused?'已暂停；已完成玩家不会重复发放':`仍有${w.current.pending.length}人未完成，可点击“继续／重试未完成玩家”`,!!w.errors.length);
    if(failures.length)$('#mail-job-errors').textContent=`${failures.length} 名玩家发放失败（其他玩家不受影响，可修正后重试）：\n${failures.join('\n')}`;
    state.loaded.delete('accounts');state.loaded.delete('audit');await loadMailJobs();
  }catch(e){$('#mail-job-errors').textContent=`${w.errors.join('\n')}\n连接或执行中断：${errorText(e)}。可用同一批次重试，服务端会跳过已完成玩家。`.trim();toast('发放已暂停，请查看批次详情',true)}finally{w.running=false;updateMailControls()}
}
$('#mail-send').onclick=async()=>{
  const w=mailWorkspace;if(w.running||w.busy)return;
  clearInvalid($('#mail .mail-form'));
  const missing=[];if(!$('#mail-title').value.trim()){missing.push('邮件标题');markInvalid($('#mail-title'))}if(!$('#mail-message').value.trim()){missing.push('正文');markInvalid($('#mail-message'))}
  if(missing.length)return toast(`请填写${missing.join('和')}`,true);
  if(recipientScope){try{const now=(await api('/api/accounts?include_system=0&limit=1')).total;if(now!==recipientScope.count&&confirm(`选择“全部玩家”后玩家总数由 ${recipientScope.count} 变为 ${now}。是否重新选取全部玩家？\n选择“取消”则按原名单发放。`)&&!await selectAllPlayers())return}catch(e){return toast(errorText(e),true)}}
  w.busy=true;updateMailControls();
  try{
    const body={batch_id:crypto.randomUUID(),user_ids:[...selectedPlayers.keys()],title:$('#mail-title').value,message:$('#mail-message').value,rewards:[...w.basket.values()].map(x=>({...x.request}))};
    const preview=await api('/api/mail-batches/preview',{method:'POST',body:JSON.stringify(body)});
    clearViewAlert('mail');
    const list=preview.batch.rewards.map((r,i)=>`· ${preview.catalog[i].name} × ${r.quantity}${r.reward_type===6?`（Lv.${r.card_level}，名声${r.card_fame}，忠诚度${r.card_love}）`:''}`);
    $('#mail-review').textContent=`收件玩家 ${preview.recipient_count} 人${recipientScope?'（全部玩家，名单已固定）':''}：\n${preview.batch.user_ids.map(id=>playerLabel(selectedPlayers.get(id)||{user_id:id})).join('、')}\n\n每人收到 ${list.length} 种奖励：\n${list.join('\n')}\n\n共 ${preview.mail_count} 封礼物\n标题：${preview.batch.title}\n正文：${preview.batch.message}`;
    $('#mail-review-confirm').textContent=`确认向 ${preview.recipient_count} 人发放`;
    $('#mail-review-modal').classList.add('open');$('#mail-review-confirm').disabled=false;
    let confirmed=false;
    $('#mail-review-confirm').onclick=async()=>{
      if(confirmed||w.running||w.busy)return;confirmed=true;w.busy=true;$('#mail-review-confirm').disabled=true;updateMailControls();
      try{pendingBatch=preview.batch;localStorage.setItem(pendingBatchKey,JSON.stringify(pendingBatch));await api('/api/mail-batches',{method:'POST',body:JSON.stringify(pendingBatch)});await openMailBatch(pendingBatch.batch_id);$('#mail-review-modal').classList.remove('open');w.busy=false;$('.batch-panel').scrollIntoView({behavior:'smooth',block:'start'});await runMailBatch()}
      catch(e){toast(errorText(e)+'；可从批次“继续／重试”按钮使用原编号重试',true);$('#mail-review-modal').classList.remove('open');mailWorkspace.current=null}
      finally{w.busy=false;updateMailControls()}
    };
  }catch(e){showViewAlert('mail',{title:'预览未通过',message:errorText(e)});toast(errorText(e),true)}finally{w.busy=false;updateMailControls()}
};
$('#mail-job-retry').onclick=runMailBatch;
$('#mail-select-all-players').onclick=async()=>{const b=$('#mail-select-all-players');b.disabled=true;try{await selectAllPlayers()}catch(e){toast(errorText(e),true)}finally{b.disabled=false}};
$('#mail-clear-players').onclick=()=>{if(selectedPlayers.size>10&&!confirm(`清空全部 ${selectedPlayers.size} 名收件人？`))return;selectedPlayers.clear();recipientScope=null;updatePlayerSelection();state.loaded.has('accounts')&&renderAccounts()};
$('#mail-job-pause').onclick=()=>{mailWorkspace.paused=true;$('#mail-job-pause').disabled=true;toast('当前10人小批完成后暂停')};
$('#mail-job-export').onclick=()=>downloadJSON(`礼物批次-${mailWorkspace.current.batch.batch_id}.json`,mailWorkspace.current);
// Rebuild a recurring compensation draft from a past batch; recipients are never copied.
$('#mail-job-reuse').onclick=async()=>{
  const c=mailWorkspace.current;if(!c||mailWorkspace.running)return;
  if(!confirm(`用批次「${c.batch.title}」的 ${c.batch.rewards.length} 种奖励与标题／正文替换当前奖励篮？\n收件人不会复制，请另行选择；发放前仍会再次预览。`))return;
  try{await importRewardRequests(c.batch.rewards);$('#mail-title').value=c.batch.title;$('#mail-message').value=c.batch.message;updateMailControls();$('#mail .mail-side').scrollIntoView({behavior:'smooth',block:'start'});toast('已载入奖励与文案，请选择收件人后预览')}catch(e){toast(errorText(e),true)}
};
$('#mail-title').oninput=$('#mail-message').oninput=e=>{e.target.classList.remove('invalid');updateMailSteps()};
$('#mail-jobs-refresh').onclick=()=>loadMailJobs().catch(e=>toast(errorText(e),true));
let mailJobsTimer;$('#mail-jobs-search').oninput=()=>{clearTimeout(mailJobsTimer);mailJobsTimer=setTimeout(()=>{mailWorkspace.jobsPage=0;loadMailJobs().catch(e=>toast(errorText(e),true))},250)};
$('#mail-jobs-status').onchange=()=>{mailWorkspace.jobsPage=0;loadMailJobs().catch(e=>toast(errorText(e),true))};
$('#mail-jobs-prev').onclick=()=>{mailWorkspace.jobsPage--;loadMailJobs().catch(e=>toast(errorText(e),true))};
$('#mail-jobs-next').onclick=()=>{mailWorkspace.jobsPage++;loadMailJobs().catch(e=>toast(errorText(e),true))};
$('#mail-review-cancel').onclick=()=>$('#mail-review-modal').classList.remove('open');
$('#catalog-select-page').onclick=()=>{try{const rows=state.catalog.filter(e=>e.resource_state!=='unavailable');if(new Set([...mailWorkspace.basket.keys(),...rows.map(rewardKey)]).size>120)throw new Error('本页与现有选择合计超过120种，请缩小范围');rows.forEach(e=>addReward(e));renderBasket();renderCatalog()}catch(e){toast(e.message,true)}};
$('#basket-clear').onclick=()=>{if(mailWorkspace.basket.size>3&&!confirm(`清空奖励篮中的 ${mailWorkspace.basket.size} 种奖励？`))return;mailWorkspace.basket.clear();renderBasket();renderCatalog()};
$('#basket-apply-defaults').onclick=()=>{try{if(!mailWorkspace.basket.size)return;if(!confirm(`把默认数量与卡牌属性应用到奖励篮全部 ${mailWorkspace.basket.size} 种奖励？逐项修改过的数量会被覆盖。`))return;for(const item of mailWorkspace.basket.values())item.request=rewardDefaults(item.entry);renderBasket()}catch(e){toast(e.message,true)}};
$('#basket-export').onclick=()=>downloadJSON('奖励方案.json',{schema_version:1,rewards:[...mailWorkspace.basket.values()].map(x=>x.request)});
async function importRewardRequests(rewards){
  if(!Array.isArray(rewards)||!rewards.length||rewards.length>120)throw new Error('奖励方案需要1–120项');
  const data=await api('/api/catalog/resolve',{method:'POST',body:JSON.stringify({rewards})});
  const next=new Map();
  rewards.forEach((r,i)=>{const e=data.entries[i];if(next.has(rewardKey(e)))throw new Error('奖励方案含重复物品');next.set(rewardKey(e),{entry:e,request:{...r,card_level:e.reward_type===6?(r.card_level||1):0,card_fame:e.reward_type===6?(r.card_fame||1):0,card_love:r.card_love||0}})});
  mailWorkspace.basket=next;renderBasket();renderCatalog();
}
$('#basket-import').onchange=async e=>{try{const file=e.target.files[0];if(!file)return;if(file.size>65536)throw new Error('方案文件超过64KB');let data;try{data=JSON.parse(await file.text())}catch{throw new Error('方案文件不是有效的 JSON')}if(data.schema_version!==1)throw new Error('方案版本不支持');if(mailWorkspace.basket.size&&!confirm(`导入会替换奖励篮中现有的 ${mailWorkspace.basket.size} 种奖励，继续？`))return;await importRewardRequests(data.rewards);toast('已载入奖励方案，发放前会再次验证')}catch(err){toast(err.message,true)}finally{e.target.value=''}};
$('#mail-add-ids').onclick=async()=>{
  const input=$('#mail-recipient-list'),ids=[...new Set(input.value.split(/[\s,，]+/).filter(Boolean).map(Number))];const button=$('#mail-add-ids');button.disabled=true;input.classList.remove('invalid');
  try{const invalid=ids.filter(id=>!Number.isInteger(id)||id<1000001||id>=1900000000);if(!ids.length||invalid.length){markInvalid(input);throw new Error(invalid.length?`这些不是有效的玩家ID：${invalid.slice(0,10).join('、')}`:'请输入玩家ID')}if(new Set([...selectedPlayers.keys(),...ids]).size>500)throw new Error('与现有收件人合计超过500人');const data=await api('/api/accounts/resolve',{method:'POST',body:JSON.stringify({user_ids:ids})});const known=new Set(data.accounts.map(a=>a.user_id));if(ids.some(id=>!known.has(id))){markInvalid(input);throw new Error('这些玩家不存在：'+ids.filter(id=>!known.has(id)).join('、'))}data.accounts.forEach(a=>selectedPlayers.set(a.user_id,a));updatePlayerSelection();input.value='';toast(`已添加 ${data.accounts.length} 名收件人`)}catch(e){toast(errorText(e),true)}finally{button.disabled=false}
};
for(const [group,prop] of [['kinds','kind'],['sources','source'],['jobs','job']])$$(`#catalog-${group} button`).forEach(b=>b.onclick=()=>{state['catalog'+prop[0].toUpperCase()+prop.slice(1)]=prop==='job'?Number(b.dataset[prop]):b.dataset[prop];state.catalogPage=0;loadCatalog()});
$('#catalog-search').oninput=()=>{state.catalogPage=0;state.catalogRequest++;clearTimeout(loadCatalog.timer);loadCatalog.timer=setTimeout(loadCatalog,200)};
$('#catalog-rarity').onchange=()=>{state.catalogRarity=Number($('#catalog-rarity').value);state.catalogPage=0;loadCatalog()};
$('#catalog-reset-filters').onclick=()=>{state.catalogSource='';state.catalogJob=0;state.catalogRarity=0;$('#catalog-rarity').value='0';resetCardFilters('catalog');state.catalogPage=0;$('#catalog-search').value='';clearTimeout(loadCatalog.timer);loadCatalog()};
window.addEventListener('beforeunload',event=>{if(mailWorkspace.running){event.preventDefault();event.returnValue=''}});
updateMailControls();

$('#catalog-unselect-page').onclick=()=>{state.catalog.forEach(e=>mailWorkspace.basket.delete(rewardKey(e)));renderBasket();renderCatalog()};

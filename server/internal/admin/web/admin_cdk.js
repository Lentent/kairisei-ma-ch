'use strict';
const cdkWorkspace={rewards:[],page:0,rows:[],recordCode:'',recordPage:0,recordRequest:0,listRequest:0,busy:false};
async function loadCDKWorkspace(){
  const status=state.status||await api('/api/status');
  const link=$('#cdk-player-link');link.href=new URL('/cdk',status.game_endpoint).href;link.textContent=link.href;
  await loadCDKList();
}
function renderCDKRewards(){
  $('#cdk-rewards').innerHTML=cdkWorkspace.rewards.map((r,i)=>`<tr><td>${esc(r.name)}<span class="sub">${r.reward_type_id}</span></td><td><input type="number" min="1" max="${r.maxQuantity}" value="${r.quantity}" data-cdk-num="${i}" aria-label="${esc(r.name)}数量"></td><td>${r.reward_type===6?`<input type="number" min="1" max="${r.levelMax}" value="${r.card_level}" data-cdk-level="${i}" aria-label="等级"><input type="number" min="1" max="${r.fameMax}" value="${r.card_fame}" data-cdk-fame="${i}" aria-label="名声"><input type="number" min="0" max="${r.loveMax}" value="${r.card_love}" data-cdk-love="${i}" aria-label="爱情">`:'—'}</td><td><button class="secondary" data-cdk-remove="${i}">移除</button></td></tr>`).join('')||'<tr><td colspan="4">尚未选择奖励</td></tr>';
  for(const [attr,field] of [['num','quantity'],['level','card_level'],['fame','card_fame'],['love','card_love']])$$(`[data-cdk-${attr}]`).forEach(el=>el.oninput=()=>cdkWorkspace.rewards[Number(el.getAttribute(`data-cdk-${attr}`))][field]=Number(el.value));
  $$('[data-cdk-remove]').forEach(el=>el.onclick=()=>{cdkWorkspace.rewards.splice(Number(el.dataset.cdkRemove),1);renderCDKRewards()});
}
$('#cdk-add').onclick=()=>openContentPicker(rows=>{
  const additions=rows.filter(row=>!cdkWorkspace.rewards.some(r=>r.reward_type===row.reward_type&&r.reward_type_id===row.reward_type_id));
  if(cdkWorkspace.rewards.length+additions.length>120)throw new Error('最多120种奖励');
  for(const row of additions)cdkWorkspace.rewards.push({name:row.name,reward_type:row.reward_type,reward_type_id:row.reward_type_id,quantity:1,card_level:row.reward_type===6?1:0,card_fame:row.reward_type===6?1:0,card_love:0,levelMax:row.level_max,fameMax:row.fame_max,loveMax:row.love_max,maxQuantity:['costume','stamp','honor'].includes(row.kind)?1:[6,15,19].includes(row.reward_type)?100:10000000});
  renderCDKRewards();
},['currency','card','item','material','sphere','buddy','costume','stamp','honor']);
$('#cdk-mode').onchange=()=>{const single=$('#cdk-mode').value==='single';$('#cdk-max-uses').disabled=single;if(single)$('#cdk-max-uses').value=1};
$('#cdk-count').onchange=()=>{const batch=Number($('#cdk-count').value)>1;$('#cdk-code').disabled=batch;if(batch)$('#cdk-code').value=''};
function cdkTimeInput(id){const value=$(id).value;if(!value)return 0;const time=Math.floor(new Date(value).getTime()/1000);if(!Number.isFinite(time))throw new Error('时间格式无效');return time}
$('#cdk-create').onclick=async()=>{
  if(cdkWorkspace.busy)return;
  try{
    const rewards=cdkWorkspace.rewards.map(r=>{for(const [field,min,max] of [['quantity',1,r.maxQuantity],['card_level',r.reward_type===6?1:0,r.levelMax||0],['card_fame',r.reward_type===6?1:0,r.fameMax||0],['card_love',0,r.loveMax||0]])if(!Number.isInteger(r[field])||r[field]<min||r[field]>max)throw new Error(`${r.name}的数量或卡牌属性无效`);return {reward_type:r.reward_type,reward_type_id:r.reward_type_id,quantity:r.quantity,card_level:r.card_level,card_fame:r.card_fame,card_love:r.card_love}});
    const payload={code:$('#cdk-code').value.trim(),mode:$('#cdk-mode').value,count:Number($('#cdk-count').value),title:$('#cdk-title').value,message:$('#cdk-message').value,start_unix:cdkTimeInput('#cdk-start'),expires_unix:cdkTimeInput('#cdk-expiry'),max_uses:Number($('#cdk-max-uses').value),rewards};
    if(!Number.isInteger(payload.count)||payload.count<1||payload.count>100)throw new Error('生成数量须为1–100');
    if(!Number.isInteger(payload.max_uses)||payload.max_uses<0||payload.max_uses>10000000)throw new Error('全服总次数须为0–10000000');
    cdkWorkspace.busy=true;$('#cdk-create').disabled=true;$('#cdk-create-status').textContent='正在创建…';
    const data=await api('/api/cdk',{method:'POST',body:JSON.stringify(payload)});
    $('#cdk-generated').value=data.codes.join('\n');$('#cdk-download').disabled=false;$('#cdk-create-status').textContent=`已创建${data.codes.length}个兑换码`;$('#cdk-code').value='';cdkWorkspace.page=0;
    await loadCDKList();
  }catch(error){$('#cdk-create-status').textContent=error.message;toast(error.message,true)}finally{cdkWorkspace.busy=false;$('#cdk-create').disabled=false}
};
$('#cdk-download').onclick=()=>{const url=URL.createObjectURL(new Blob([$('#cdk-generated').value],{type:'text/plain;charset=utf-8'}));const link=document.createElement('a');link.href=url;link.download='礼包兑换码.txt';link.click();setTimeout(()=>URL.revokeObjectURL(url),1000)};
function cdkDate(time){return time?new Date(time*1000).toLocaleString('zh-CN'):'不限'}
async function loadCDKList(){
  const serial=++cdkWorkspace.listRequest;
  const data=await api(`/api/cdk?limit=20&offset=${cdkWorkspace.page*20}`);if(serial!==cdkWorkspace.listRequest)return;
  cdkWorkspace.rows=data.codes;
  cdkWorkspace.page=renderPager('cdk',cdkWorkspace.page,data.total,20,page=>{cdkWorkspace.page=page;loadCDKList().catch(e=>toast(e.message,true))});
  const now=Math.floor(Date.now()/1000);
  $('#cdk-rows').innerHTML=data.codes.map(({cdk,used})=>{const status=!cdk.enabled?'已停用':cdk.expires_unix&&now>=cdk.expires_unix?'已过期':now<cdk.start_unix?'未开始':cdk.max_uses&&used>=cdk.max_uses?'已用完':'可兑换';return `<tr><td><code>${esc(cdk.code)}</code><span class="sub">${esc(cdk.title)} · ${cdk.rewards.length}种奖励</span></td><td>${cdk.mode==='single'?'一次性码':'通用码'}<span class="sub">${used} / ${cdk.max_uses||'不限'}</span></td><td>${status}<span class="sub">开始：${cdk.start_unix?cdkDate(cdk.start_unix):'立即'}<br>结束：${cdkDate(cdk.expires_unix)}</span></td><td><button class="secondary" data-cdk-record="${esc(cdk.code)}">记录 / 奖励</button><button class="secondary" data-cdk-status="${esc(cdk.code)}">${cdk.enabled?'停用':'启用'}</button></td></tr>`}).join('')||'<tr><td colspan="4">暂无兑换码</td></tr>';
  $$('[data-cdk-status]').forEach(button=>button.onclick=async()=>{button.disabled=true;try{const row=cdkWorkspace.rows.find(r=>r.cdk.code===button.dataset.cdkStatus);await api(`/api/cdk/${encodeURIComponent(row.cdk.code)}/enabled`,{method:'PUT',body:JSON.stringify({enabled:!row.cdk.enabled,revision:row.revision})});await loadCDKList()}catch(e){toast(e.message,true)}finally{button.disabled=false}});
  $$('[data-cdk-record]').forEach(button=>button.onclick=()=>{cdkWorkspace.recordCode=button.dataset.cdkRecord;cdkWorkspace.recordPage=0;loadCDKRecords().catch(e=>toast(e.message,true))});
}
async function loadCDKRecords(){
  const serial=++cdkWorkspace.recordRequest,code=cdkWorkspace.recordCode;
  const data=await api(`/api/cdk/${encodeURIComponent(code)}/records?limit=20&offset=${cdkWorkspace.recordPage*20}`);if(serial!==cdkWorkspace.recordRequest)return;
  $('#cdk-record-title').textContent=`${code} · ${data.cdk.title} · 兑换记录`;
  $('#cdk-record-rewards').textContent=data.cdk.rewards.map((r,i)=>`${data.catalog[i]?.name||`${r.reward_type}:${r.reward_type_id}`} ×${r.quantity}${r.reward_type===6?`（等级${r.card_level} / 名声${r.card_fame} / 爱情${r.card_love}）`:''}`).join('；');
  $('#cdk-records').innerHTML=data.records.map(r=>`<tr><td>${r.user_id}</td><td>${esc(new Date(r.created_utc).toLocaleString('zh-CN'))}</td><td>${esc((r.result.present_ids||[]).join(', '))}</td></tr>`).join('')||'<tr><td colspan="3">尚无兑换记录</td></tr>';
  $('#cdk-record-pager').hidden=false;
  cdkWorkspace.recordPage=renderPager('cdk-record',cdkWorkspace.recordPage,data.total,20,page=>{cdkWorkspace.recordPage=page;loadCDKRecords().catch(e=>toast(e.message,true))});
}
$('#cdk-list-refresh').onclick=()=>loadCDKList().catch(e=>toast(e.message,true));
renderCDKRewards();

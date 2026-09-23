'use strict';
const playerPolicy={saved:null,defaults:null,revision:0,names:{},mailRewards:[],mailEntries:new Map()};
const playerLoginKinds=['cycle','beginner','total_milestones'];
function normalizePlayerNotices(p){
  const result=structuredClone(p),n=result.notice;
  const entries=Array.isArray(n.entries)?n.entries:[{enabled:true,title:n.title,body:n.body}];
  n.entries=entries.map(e=>({enabled:!!e.enabled,pinned:!!e.pinned,title:e.title,body:e.body}));
  return result;
}
function playerNoticeDraft(){
  return $$('#player-notices .notice-editor').map(row=>({enabled:row.querySelector('.notice-enabled').checked,pinned:row.querySelector('.notice-pinned').checked,title:row.querySelector('.notice-title').value.trim(),body:row.querySelector('.notice-body-input').value}));
}
function updatePlayerNotices(){
  const rows=$$('#player-notices .notice-editor'),globalEnabled=$('#player-notice-enabled').checked;
  $('#player-notice-count').textContent=`${rows.length} / 50 条公告 · ${globalEnabled?rows.filter(row=>row.querySelector('.notice-enabled').checked).length:0} 条正在显示`;
  $('#player-notice-add').disabled=rows.length>=50;
  for(const row of rows){
    const enabled=row.querySelector('.notice-enabled').checked,pinned=row.querySelector('.notice-pinned').checked;
    row.querySelector('.notice-summary-title').textContent=row.querySelector('.notice-title').value.trim()||'未填写标题';
    row.querySelector('.notice-summary-state').textContent=(pinned?'置顶 · ':'')+(enabled?'已上架':'已下架');
    row.querySelector('.notice-body-input').required=globalEnabled&&enabled;
  }
}
function renderPlayerNotices(entries,openIndex=0){
  $('#player-notices').innerHTML=entries.map((n,i)=>`<details class="notice-editor" ${i===openIndex?'open':''}>
    <summary><span class="notice-number">${i+1}</span><span class="notice-summary-title">${esc(n.title||'未填写标题')}</span><span class="pill notice-summary-state"></span></summary>
    <div class="notice-editor-body"><div class="toolbar">
      <label><input class="notice-enabled" type="checkbox" ${n.enabled?'checked':''}> 上架这条公告</label>
      <label><input class="notice-pinned" type="checkbox" ${n.pinned?'checked':''}> 置顶</label>
      <button type="button" class="secondary" data-notice-action="up" data-index="${i}" ${i===0?'disabled':''}>上移</button>
      <button type="button" class="secondary" data-notice-action="down" data-index="${i}" ${i===entries.length-1?'disabled':''}>下移</button>
      <button type="button" class="danger" data-notice-action="delete" data-index="${i}">删除公告</button>
    </div>
    <div class="field"><label for="notice-title-${i}">标题</label><input class="notice-title" id="notice-title-${i}" maxlength="80" required value="${esc(n.title)}"></div>
    <div class="field"><label for="notice-body-${i}">正文（纯文字，支持换行，最多8000字）</label><textarea class="notice-body-input" id="notice-body-${i}" maxlength="8000" rows="8">${esc(n.body)}</textarea></div>
    </div></details>`).join('')||'<p class="sub notice-empty">暂无公告，点击“新增公告”开始编辑。</p>';
  updatePlayerNotices();
}
$('#player-notice-add').onclick=()=>{
  const entries=playerNoticeDraft();if(entries.length>=50)return;
  entries.unshift({enabled:true,pinned:false,title:'',body:''});renderPlayerNotices(entries);playerPolicyChanged();$('#notice-title-0').focus();
};
$('#player-notices').onclick=event=>{
  const button=event.target.closest('[data-notice-action]');if(!button||button.disabled)return;
  const entries=playerNoticeDraft(),index=Number(button.dataset.index),action=button.dataset.noticeAction;
  let next=index;
  if(action==='delete'){
    if(!confirm(`删除公告“${entries[index].title||'未填写标题'}”？点击“保存配置”后生效。`))return;
    entries.splice(index,1);next=Math.min(index,entries.length-1);
  }else{
    next=index+(action==='up'?-1:1);if(next<0||next>=entries.length)return;
    [entries[index],entries[next]]=[entries[next],entries[index]];
  }
  renderPlayerNotices(entries,next);playerPolicyChanged();
};
function validatePlayerPolicyForm(){
  updatePlayerNotices();
  for(const el of $('#player-policy').querySelectorAll('input,textarea')){
    if(el.checkValidity())continue;
    // Invalid inputs inside collapsed editors must be revealed before focusing.
    for(let parent=el.parentElement;parent;parent=parent.parentElement)if(parent.tagName==='DETAILS')parent.open=true;
    el.reportValidity();return false;
  }
  return true;
}
function playerPolicyDraft(){
  const p=structuredClone(playerPolicy.saved);
  p.notice={...p.notice,enabled:$('#player-notice-enabled').checked,entries:playerNoticeDraft()};
  p.tutorial_completion_mail={enabled:$('#player-mail-enabled').checked,title:$('#player-mail-title').value.trim(),message:$('#player-mail-message').value.trim(),rewards:playerMailDraft()};
  p.story_first_clear_crystals=Number($('#player-story-crystals').value);
  for(const kind of playerLoginKinds)p.login_rewards[kind]=$$(`#player-login-${kind} tr`).map((row,i)=>{
    const day=structuredClone(p.login_rewards[kind][i]);day.reward.type=Number(row.querySelector('.login-kind').value);day.reward.num=Number(row.querySelector('.login-amount').value);day.comment=row.querySelector('.login-comment').value.trim();return day;
  });
  p.navigators=$$('#player-navis tr').map(row=>({navi_id:Number(row.dataset.id),enabled:row.querySelector('.navi-enabled').checked,price:Number(row.querySelector('.navi-price').value)}));
  return p;
}
function playerPolicyDirty(){return !!playerPolicy.saved&&JSON.stringify(playerPolicy.saved)!==JSON.stringify(playerPolicyDraft())}
function playerPolicyChanged(){updatePlayerNotices();const dirty=playerPolicyDirty();$('#player-policy-save').disabled=!dirty;$('#player-policy-state').textContent=dirty?'有未保存的修改':'已保存'}
function renderPlayerPolicy(p){
  $('#player-policy-version').textContent=`配置 v${playerPolicy.revision}`;
  $('#player-notice-enabled').checked=p.notice.enabled;renderPlayerNotices(p.notice.entries);
  const mail=p.tutorial_completion_mail;$('#player-mail-enabled').checked=mail.enabled;$('#player-mail-title').value=mail.title;$('#player-mail-message').value=mail.message;playerPolicy.mailRewards=structuredClone(mail.rewards||[]);renderPlayerMail();$('#player-story-crystals').value=p.story_first_clear_crystals;
  for(const kind of playerLoginKinds)$('#player-login-'+kind).innerHTML=p.login_rewards[kind].map(day=>`<tr><td>第 ${day.day} 天</td><td><select class="login-kind" aria-label="奖励类型"><option value="4" ${day.reward.type===4?'selected':''}>金币</option><option value="10" ${day.reward.type===10?'selected':''}>水晶</option></select></td><td><input class="login-amount" type="number" min="1" max="10000000" value="${day.reward.num}" aria-label="奖励数量"></td><td><input class="login-comment" maxlength="100" value="${esc(day.comment)}" aria-label="奖励说明"></td></tr>`).join('');
  $('#player-navis').innerHTML=p.navigators.map(n=>`<tr data-id="${n.navi_id}"><td><input class="navi-enabled" type="checkbox" ${n.enabled?'checked':''} aria-label="开放购买"></td><td>${esc(playerPolicy.names[n.navi_id]||'看板')}<span class="sub">${n.navi_id}</span></td><td><input class="navi-price" type="number" min="1" max="10000000" value="${n.price}" aria-label="水晶价格"></td></tr>`).join('');
  playerPolicyChanged();
}
function playerMailDraft(){return $$('#player-mail-rewards tr[data-index]').map(row=>{const r=structuredClone(playerPolicy.mailRewards[Number(row.dataset.index)]);for(const input of row.querySelectorAll('[data-field]'))r[input.dataset.field]=Number(input.value);return r})}
function rememberPlayerMailEntries(entries){for(const e of entries||[]){playerPolicy.mailEntries.set(contentKey(e),e);contentPicker.labels.set(contentKey(e),e.name)}}
function renderPlayerMail(){
  $('#player-mail-count').textContent=`${playerPolicy.mailRewards.length} / 120 种奖励`;
  $('#player-mail-rewards').innerHTML=playerPolicy.mailRewards.map((r,i)=>{const e=playerPolicy.mailEntries.get(contentKey(r)),card=r.type===6,max=[14,16,18].includes(r.type)?1:[6,15,19].includes(r.type)?100:10000000;
    const input=(field,value,min,max)=>`<input data-field="${field}" aria-label="${esc(contentLabel(r))} ${field}" type="number" required step="1" min="${min}" max="${max}" value="${value}">`;
    return `<tr data-index="${i}"><td>${esc(e?.name||contentLabel(r))}<span class="sub">${r.reward_typeid||'货币'} ${esc(card ? cardJobName(e?.arthur_type) : '')}</span></td><td>${input('num',r.num,1,max)}</td><td>${card?input('card_lv',r.card_lv,1,e?.level_max||r.card_lv):'—'}</td><td>${card?input('card_fame',r.card_fame,1,e?.fame_max||r.card_fame):'—'}</td><td>${card?input('card_love',r.card_love,0,e?.love_max||0):'—'}</td><td><button class="secondary" data-remove="${i}">移除</button></td></tr>`;
  }).join('')||'<tr><td colspan="6" class="sub">尚未选择奖励。启用前请添加至少一项。</td></tr>';
  $$('#player-mail-rewards [data-remove]').forEach(b=>b.onclick=()=>{playerPolicy.mailRewards=playerMailDraft();playerPolicy.mailRewards.splice(Number(b.dataset.remove),1);renderPlayerMail();playerPolicyChanged()});
}
$('#player-mail-add').onclick=()=>openContentPicker(rows=>{const rewards=playerMailDraft(),keys=new Set(rewards.map(contentKey)),added=rows.filter(r=>!keys.has(contentKey(r)));if(rewards.length+added.length>120)throw new Error('毕业奖励最多120种');rememberPlayerMailEntries(rows);playerPolicy.mailRewards=[...rewards,...added.map(contentReward)];renderPlayerMail();playerPolicyChanged()},['currency','card','material','item','sphere','buddy','costume','stamp','honor']);
async function loadPlayerPolicy(){const data=await api('/api/player-policy');playerPolicy.saved=normalizePlayerNotices(data.config);playerPolicy.defaults=normalizePlayerNotices(data.defaults);playerPolicy.revision=data.revision;playerPolicy.names=data.navi_names;rememberPlayerMailEntries(data.mail_reward_entries);renderPlayerPolicy(playerPolicy.saved)}
$('#player-policy').oninput=playerPolicyChanged;$('#player-policy').onchange=playerPolicyChanged;
$('#player-policy-default').onclick=()=>{if(confirm('载入包内默认配置到草稿？保存后才生效。'))renderPlayerPolicy(playerPolicy.defaults)};
$('#player-navi-batch').onclick=()=>{const price=Number($('#player-navi-batch-price').value);if(!Number.isInteger(price)||price<1||price>10000000){toast('价格须为1至10000000的整数',true);return}$$('#player-navis .navi-price').forEach(el=>el.value=price);playerPolicyChanged()};
$('#player-policy-save').onclick=()=>{
  if(!validatePlayerPolicyForm())return;
  return contentAction('player-policy',async()=>{
  const config=playerPolicyDraft(),mail=config.tutorial_completion_mail;if(mail.enabled&&(!mail.title||!mail.message||!mail.rewards.length)){toast('请填写邮件标题、正文并选择奖励',true);return}if(!confirm(`保存公告、签到、新手毕业邮件、剧情奖励和看板购买配置？\n毕业邮件${mail.enabled?'开启':'关闭'}，共${mail.rewards.length}种奖励，仅在整个新手训练完成时发放。\n已有领取记录和看板所有权保留。`))return;
  const data=await api('/api/player-policy',{method:'PUT',body:JSON.stringify({expected_revision:playerPolicy.revision,config})});playerPolicy.saved=normalizePlayerNotices(data.config);playerPolicy.revision=data.revision;rememberPlayerMailEntries(data.mail_reward_entries);renderPlayerPolicy(playerPolicy.saved);state.loaded.delete('audit');toast('公告与奖励配置已保存');
  });
};

function maximizePlayerMail(field,catalogField){
 const rewards=playerMailDraft();
 for(const r of rewards){if(r.type!==6)continue;const e=playerPolicy.mailEntries.get(contentKey(r));if(!e||!Number.isInteger(e[catalogField])||e[catalogField]<1){toast('卡牌上限数据缺失，请重新加载配置',true);return}r[field]=e[catalogField]}
 playerPolicy.mailRewards=rewards;renderPlayerMail();playerPolicyChanged();
}
$('#player-mail-max-level').onclick=()=>maximizePlayerMail('card_lv','level_max');
$('#player-mail-max-fame').onclick=()=>maximizePlayerMail('card_fame','fame_max');

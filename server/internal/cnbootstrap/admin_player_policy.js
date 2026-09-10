'use strict';
const playerPolicy={saved:null,defaults:null,revision:0,names:{},mailRewards:[],mailEntries:new Map()};
const playerLoginKinds=['cycle','beginner','total_milestones'];
function playerPolicyDraft(){
  const p=structuredClone(playerPolicy.saved);
  p.notice={enabled:$('#player-notice-enabled').checked,title:$('#player-notice-title').value.trim(),body:$('#player-notice-body').value};
  p.tutorial_completion_mail={enabled:$('#player-mail-enabled').checked,title:$('#player-mail-title').value.trim(),message:$('#player-mail-message').value.trim(),rewards:playerMailDraft()};
  p.story_first_clear_crystals=Number($('#player-story-crystals').value);
  for(const kind of playerLoginKinds)p.login_rewards[kind]=$$(`#player-login-${kind} tr`).map((row,i)=>{
    const day=structuredClone(p.login_rewards[kind][i]);day.reward.type=Number(row.querySelector('.login-kind').value);day.reward.num=Number(row.querySelector('.login-amount').value);day.comment=row.querySelector('.login-comment').value.trim();return day;
  });
  p.navigators=$$('#player-navis tr').map(row=>({navi_id:Number(row.dataset.id),enabled:row.querySelector('.navi-enabled').checked,price:Number(row.querySelector('.navi-price').value)}));
  return p;
}
function playerPolicyDirty(){return !!playerPolicy.saved&&JSON.stringify(playerPolicy.saved)!==JSON.stringify(playerPolicyDraft())}
function playerPolicyChanged(){const dirty=playerPolicyDirty();$('#player-policy-save').disabled=!dirty;$('#player-policy-state').textContent=dirty?'有未保存的修改':'已保存'}
function renderPlayerPolicy(p){
  $('#player-policy-version').textContent=`配置 v${playerPolicy.revision}`;
  $('#player-notice-enabled').checked=p.notice.enabled;$('#player-notice-title').value=p.notice.title;$('#player-notice-body').value=p.notice.body;
  const mail=p.tutorial_completion_mail;$('#player-mail-enabled').checked=mail.enabled;$('#player-mail-title').value=mail.title;$('#player-mail-message').value=mail.message;playerPolicy.mailRewards=structuredClone(mail.rewards||[]);renderPlayerMail();$('#player-story-crystals').value=p.story_first_clear_crystals;
  for(const kind of playerLoginKinds)$('#player-login-'+kind).innerHTML=p.login_rewards[kind].map(day=>`<tr><td>第 ${day.day} 天</td><td><select class="login-kind" aria-label="奖励类型"><option value="4" ${day.reward.type===4?'selected':''}>金币</option><option value="10" ${day.reward.type===10?'selected':''}>水晶</option></select></td><td><input class="login-amount" type="number" min="1" max="10000000" value="${day.reward.num}" aria-label="奖励数量"></td><td><input class="login-comment" maxlength="100" value="${esc(day.comment)}" aria-label="奖励说明"></td></tr>`).join('');
  $('#player-navis').innerHTML=p.navigators.map(n=>`<tr data-id="${n.navi_id}"><td><input class="navi-enabled" type="checkbox" ${n.enabled?'checked':''} aria-label="开放购买"></td><td>${esc(playerPolicy.names[n.navi_id]||'看板')}<span class="sub">${n.navi_id}</span></td><td><input class="navi-price" type="number" min="1" max="10000000" value="${n.price}" aria-label="水晶价格"></td></tr>`).join('');
  playerPolicyChanged();
}
function playerMailDraft(){return $$('#player-mail-rewards tr[data-index]').map(row=>{const r=structuredClone(playerPolicy.mailRewards[Number(row.dataset.index)]);for(const input of row.querySelectorAll('[data-field]'))r[input.dataset.field]=Number(input.value);return r})}
function rememberPlayerMailEntries(entries){for(const e of entries||[]){playerPolicy.mailEntries.set(contentKey(e),e);contentPicker.labels.set(contentKey(e),e.name)}}
function renderPlayerMail(){
  $('#player-mail-count').textContent=`${playerPolicy.mailRewards.length} / 120 种奖励`;
  $('#player-mail-rewards').innerHTML=playerPolicy.mailRewards.map((r,i)=>{const e=playerPolicy.mailEntries.get(contentKey(r)),card=r.type===6,max=[6,15,19].includes(r.type)?100:10000000;
    const input=(field,value,min,max)=>`<input data-field="${field}" aria-label="${esc(contentLabel(r))} ${field}" type="number" required step="1" min="${min}" max="${max}" value="${value}">`;
    return `<tr data-index="${i}"><td>${esc(e?.name||contentLabel(r))}<span class="sub">${r.reward_typeid||'货币'} ${esc(cardJobName(e?.arthur_type||0))}</span></td><td>${input('num',r.num,1,max)}</td><td>${card?input('card_lv',r.card_lv,1,e?.level_max||r.card_lv):'—'}</td><td>${card?input('card_fame',r.card_fame,1,e?.fame_max||r.card_fame):'—'}</td><td>${card?input('card_love',r.card_love,0,e?.love_max||0):'—'}</td><td><button class="secondary" data-remove="${i}">移除</button></td></tr>`;
  }).join('')||'<tr><td colspan="6" class="sub">尚未选择奖励。启用前请添加至少一项。</td></tr>';
  $$('#player-mail-rewards [data-remove]').forEach(b=>b.onclick=()=>{playerPolicy.mailRewards=playerMailDraft();playerPolicy.mailRewards.splice(Number(b.dataset.remove),1);renderPlayerMail();playerPolicyChanged()});
}
$('#player-mail-add').onclick=()=>openContentPicker(rows=>{const rewards=playerMailDraft(),keys=new Set(rewards.map(contentKey)),added=rows.filter(r=>!keys.has(contentKey(r)));if(rewards.length+added.length>120)throw new Error('毕业奖励最多120种');rememberPlayerMailEntries(rows);playerPolicy.mailRewards=[...rewards,...added.map(contentReward)];renderPlayerMail();playerPolicyChanged()},['currency','card','material','item','sphere','buddy']);
async function loadPlayerPolicy(){const data=await api('/api/player-policy');playerPolicy.saved=data.config;playerPolicy.defaults=data.defaults;playerPolicy.revision=data.revision;playerPolicy.names=data.navi_names;rememberPlayerMailEntries(data.mail_reward_entries);renderPlayerPolicy(data.config)}
$('#player-policy').oninput=playerPolicyChanged;$('#player-policy').onchange=playerPolicyChanged;
$('#player-policy-default').onclick=()=>{if(confirm('载入包内默认配置到草稿？保存后才生效。'))renderPlayerPolicy(playerPolicy.defaults)};
$('#player-navi-batch').onclick=()=>{const price=Number($('#player-navi-batch-price').value);if(!Number.isInteger(price)||price<1||price>10000000){toast('价格须为1至10000000的整数',true);return}$$('#player-navis .navi-price').forEach(el=>el.value=price);playerPolicyChanged()};
$('#player-policy-save').onclick=()=>contentAction('player-policy',async()=>{
  const section=$('#player-policy');if([...section.querySelectorAll('input,textarea')].some(el=>!el.reportValidity()))return;
  const config=playerPolicyDraft(),mail=config.tutorial_completion_mail;if(mail.enabled&&(!mail.title||!mail.message||!mail.rewards.length)){toast('请填写邮件标题、正文并选择奖励',true);return}if(!confirm(`保存公告、签到、新手毕业邮件、剧情奖励和看板购买配置？\n毕业邮件${mail.enabled?'开启':'关闭'}，共${mail.rewards.length}种奖励，仅在整个新手训练完成时发放。\n已有领取记录和看板所有权保留。`))return;
  const data=await api('/api/player-policy',{method:'PUT',body:JSON.stringify({expected_revision:playerPolicy.revision,config})});playerPolicy.saved=data.config;playerPolicy.revision=data.revision;rememberPlayerMailEntries(data.mail_reward_entries);renderPlayerPolicy(data.config);state.loaded.delete('audit');toast('公告与奖励配置已保存');
});

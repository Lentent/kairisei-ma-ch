'use strict';
const playerPolicy={saved:null,defaults:null,revision:0,names:{}};
const playerLoginKinds=['cycle','beginner','total_milestones'];
function playerPolicyDraft(){
  const p=structuredClone(playerPolicy.saved);
  p.notice={enabled:$('#player-notice-enabled').checked,title:$('#player-notice-title').value.trim(),body:$('#player-notice-body').value};
  p.initial_resources={gold:Number($('#player-initial-gold').value),crystals:Number($('#player-initial-crystals').value),friend_points:Number($('#player-initial-friend-points').value)};
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
  $('#player-initial-gold').value=p.initial_resources.gold;$('#player-initial-crystals').value=p.initial_resources.crystals;$('#player-initial-friend-points').value=p.initial_resources.friend_points;$('#player-story-crystals').value=p.story_first_clear_crystals;
  for(const kind of playerLoginKinds)$('#player-login-'+kind).innerHTML=p.login_rewards[kind].map(day=>`<tr><td>第 ${day.day} 天</td><td><select class="login-kind" aria-label="奖励类型"><option value="4" ${day.reward.type===4?'selected':''}>金币</option><option value="10" ${day.reward.type===10?'selected':''}>水晶</option></select></td><td><input class="login-amount" type="number" min="1" max="10000000" value="${day.reward.num}" aria-label="奖励数量"></td><td><input class="login-comment" maxlength="100" value="${esc(day.comment)}" aria-label="奖励说明"></td></tr>`).join('');
  $('#player-navis').innerHTML=p.navigators.map(n=>`<tr data-id="${n.navi_id}"><td><input class="navi-enabled" type="checkbox" ${n.enabled?'checked':''} aria-label="开放购买"></td><td>${esc(playerPolicy.names[n.navi_id]||'看板')}<span class="sub">${n.navi_id}</span></td><td><input class="navi-price" type="number" min="1" max="10000000" value="${n.price}" aria-label="水晶价格"></td></tr>`).join('');
  playerPolicyChanged();
}
async function loadPlayerPolicy(){const data=await api('/api/player-policy');playerPolicy.saved=data.config;playerPolicy.defaults=data.defaults;playerPolicy.revision=data.revision;playerPolicy.names=data.navi_names;renderPlayerPolicy(data.config)}
$('#player-policy').oninput=playerPolicyChanged;$('#player-policy').onchange=playerPolicyChanged;
$('#player-policy-default').onclick=()=>{if(confirm('载入包内默认配置到草稿？保存后才生效。'))renderPlayerPolicy(playerPolicy.defaults)};
$('#player-navi-batch').onclick=()=>{const price=Number($('#player-navi-batch-price').value);if(!Number.isInteger(price)||price<1||price>10000000){toast('价格须为1至10000000的整数',true);return}$$('#player-navis .navi-price').forEach(el=>el.value=price);playerPolicyChanged()};
$('#player-policy-save').onclick=()=>contentAction('player-policy',async()=>{
  const section=$('#player-policy');if([...section.querySelectorAll('input,textarea')].some(el=>!el.reportValidity()))return;
  const config=playerPolicyDraft();if(!confirm('保存公告、签到、新账号资源、剧情奖励和看板购买配置？\n已有领取记录和看板所有权保留。'))return;
  const data=await api('/api/player-policy',{method:'PUT',body:JSON.stringify({expected_revision:playerPolicy.revision,config})});playerPolicy.saved=data.config;playerPolicy.revision=data.revision;renderPlayerPolicy(data.config);state.loaded.delete('audit');toast('公告与奖励配置已保存');
});

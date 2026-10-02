'use strict';
const missionWorkspace={saved:null,rows:[],revision:0,selected:-1,entries:new Map(),limits:{max_missions:200,max_rewards:4,max_target:10000000}};
const missionKindNames={login:'登录',explore:'探索结算',level:'玩家等级',collection:'卡牌图鉴种数'};
const missionRewardKinds=['currency','card','item','material','sphere','buddy','costume','stamp','honor'];
const missionCurrencyNames={0:'亚瑟经验',4:'金币',9:'友情点',10:'免费水晶',12:'体力'};
function missionDefinition(m){
  return {id:m.id||0,title:m.title||'',description:m.description||'',kind:m.kind,daily:!!m.daily,enabled:!!m.enabled,target:m.target,rewards:structuredClone(m.rewards?.length?m.rewards:m.crystals?[{type:10,reward_typeid:0,num:m.crystals,card_lv:0,card_fame:0,card_love:0,card_skill_lv:[]}]:[])};
}
function missionPolicyDraft(){return {missions:missionWorkspace.rows.map(missionDefinition)}}
function missionPolicyDirty(){return !!missionWorkspace.saved&&JSON.stringify(missionWorkspace.saved)!==JSON.stringify(missionPolicyDraft())}
function currentMission(){return missionWorkspace.rows[missionWorkspace.selected]}
function missionIdentityChanged(m){const old=missionWorkspace.saved?.missions.find(row=>row.id===m.id);return !!old&&(old.kind!==m.kind||old.daily!==m.daily)}
function rememberMissionEntries(entries){for(const e of entries||[]){missionWorkspace.entries.set(contentKey(e),e);contentPicker.labels.set(contentKey(e),e.name)}}
function updateMissionControls(){
  const loaded=!!missionWorkspace.saved,busy=policyBusy('missions'),row=currentMission(),dirty=missionPolicyDirty();
  $('#mission-save').disabled=!loaded||busy||!dirty;
  $('#mission-add').disabled=!loaded||busy||missionWorkspace.rows.length>=missionWorkspace.limits.max_missions;
  $('#mission-revert').disabled=!loaded||busy||!dirty;
  $('#mission-copy').disabled=!row||busy||missionWorkspace.rows.length>=missionWorkspace.limits.max_missions;
  $('#mission-remove').disabled=!row||busy;
  $('#mission-fields').disabled=!row||busy;
  $('#mission-reward-add').disabled=!row||busy||row.rewards.length>=missionWorkspace.limits.max_rewards;
  const hasCards=row?.rewards.some(r=>r.type===6);
  $('#mission-card-max-level').disabled=!hasCards||busy;
  $('#mission-card-max-fame').disabled=!hasCards||busy;
  $('#mission-state').textContent=!loaded?'正在读取任务…':dirty?'有未保存的修改':'已保存';
}
function missionChanged(){updateMissionControls();updateDraftIndicators()}
function missionConditionSummary(m){return `${m.daily?'每日':'成就'} · ${missionKindNames[m.kind]||m.kind} ${m.target}`}
function renderMissionList(){
  const query=$('#mission-search').value.trim().toLowerCase(),category=$('#mission-filter-category').value,status=$('#mission-filter-state').value;
  const rows=missionWorkspace.rows.map((m,index)=>({m,index})).filter(({m})=>(category==='all'||m.daily===(category==='daily'))&&(status==='all'||m.enabled===(status==='enabled'))&&`${m.title} ${m.description} ${m.id||''} ${missionKindNames[m.kind]||''}`.toLowerCase().includes(query));
  $('#mission-count').textContent=`${missionWorkspace.rows.length} / ${missionWorkspace.limits.max_missions} 项 · 启用 ${missionWorkspace.rows.filter(m=>m.enabled).length} 项`;
  $('#mission-list').innerHTML=rows.map(({m,index})=>`<button type="button" class="mission-list-row ${index===missionWorkspace.selected?'selected':''}" data-mission-index="${index}" aria-pressed="${index===missionWorkspace.selected}"><span class="mission-list-top"><b>${esc(m.title.trim()||'未填写标题')}</b><span class="pill ${m.enabled?'gold':''}">${m.enabled?'启用':'停用'}</span></span><span class="sub">${esc(missionConditionSummary(m))}</span><span class="sub">${m.id?`ID ${m.id}`:'新增 · 保存后分配 ID'} · ${m.rewards.length} 种奖励${missionIdentityChanged(m)?' · 保存后更新 ID':''}</span></button>`).join('')||`<p class="sub mission-list-empty">${missionWorkspace.rows.length?'没有符合筛选的任务。':'暂无任务，点击“新增任务”开始配置。'}</p>`;
}
function updateMissionCondition(){
  const m=currentMission();if(!m)return;
  const growthOnly=m.kind==='level'||m.kind==='collection',dailyLogin=m.daily&&m.kind==='login';
  $('#mission-category option[value="daily"]').disabled=growthOnly;
  $('#mission-category').value=m.daily?'daily':'growth';
  $('#mission-target').readOnly=dailyLogin;$('#mission-target').max=dailyLogin?1:missionWorkspace.limits.max_target;
  $('#mission-target').value=m.target;
  $('#mission-target-label').textContent=m.kind==='level'?'目标等级':m.kind==='collection'?'目标图鉴种数':m.kind==='login'?(m.daily?'当天登录次数（固定 1 次）':'累计登录天数'):'目标探索次数';
  const help={login:m.daily?'当天登录即可完成，每日目标固定为 1。':'累计登录天数计入进度，包含已有签到天数。',explore:m.daily?'统计当天任务启用期间成功结算的探索次数。':'统计此项任务启用期间成功结算的探索次数；旧版未记录的历史探索不补计。',level:'达到目标等级即可完成，已有等级计入进度；仅支持成就分类。',collection:'卡牌图鉴中的不同卡牌种数达到目标即可完成，已有图鉴计入进度；仅支持成就分类。'};
  $('#mission-condition-help').textContent=help[m.kind]||'';
  $('#mission-identity-note').hidden=!missionIdentityChanged(m);
}
function renderMissionRewards(){
  const m=currentMission(),rewards=m?.rewards||[];
  $('#mission-reward-count').textContent=`${rewards.length} / ${missionWorkspace.limits.max_rewards} 种`;
  $('#mission-rewards').innerHTML=rewards.map((r,index)=>{
    const entry=missionWorkspace.entries.get(contentKey(r)),card=r.type===6,max=[14,16,18].includes(r.type)?1:[6,15,19].includes(r.type)?100:10000000,name=entry?.name||missionCurrencyNames[r.type]||contentLabel(r);
    const input=(field,value,min,max)=>`<input type="number" required step="1" min="${min}" max="${max}" value="${value}" data-mission-reward="${index}" data-field="${field}" aria-label="${esc(name)} ${field}" ${min===max?'readonly':''}>`;
    return `<tr><td><b>${esc(name)}</b><span class="sub">${r.reward_typeid||'货币'}${card?` · ${esc(cardJobName(entry?.arthur_type))}`:''}</span></td><td>${input('num',r.num,1,max)}</td><td>${card?input('card_lv',r.card_lv,1,entry?.level_max||r.card_lv):'—'}</td><td>${card?input('card_fame',r.card_fame,1,entry?.fame_max||r.card_fame):'—'}</td><td>${card?input('card_love',r.card_love,0,entry?.love_max||0):'—'}</td><td><button type="button" class="secondary" data-mission-reward-remove="${index}">移除</button></td></tr>`;
  }).join('')||'<tr><td colspan="6" class="sub">尚未选择奖励，请从奖励目录添加至少一种。</td></tr>';
  updateMissionControls();
}
function renderMissionEditor(){
  const m=currentMission();$('#mission-fields').hidden=!m;$('#mission-editor-empty').hidden=!!m;
  $('#mission-editor-title').textContent=m?'编辑任务':'选择一个任务';
  if(m){
    $('#mission-id').value=m.id||'保存时自动分配';$('#mission-title').value=m.title;$('#mission-description').value=m.description;
    $('#mission-kind').value=m.kind;$('#mission-enabled').checked=m.enabled;
    updateMissionCondition();renderMissionRewards();
  }
  updateMissionControls();
}
function renderMissions(){renderMissionList();renderMissionEditor();missionChanged()}
function acceptMissionPolicy(data){
  const previous=currentMission()?.id,previousIndex=missionWorkspace.selected;
  missionWorkspace.limits={...missionWorkspace.limits,...data.limits};rememberMissionEntries(data.reward_entries);
  missionWorkspace.saved={missions:(data.config.missions||[]).map(missionDefinition)};
  missionWorkspace.rows=structuredClone(missionWorkspace.saved.missions);missionWorkspace.revision=data.revision;
  missionWorkspace.selected=previous?missionWorkspace.rows.findIndex(m=>m.id===previous):-1;
  if(missionWorkspace.selected<0&&missionWorkspace.rows.length)missionWorkspace.selected=Math.min(Math.max(previousIndex,0),missionWorkspace.rows.length-1);
  $('#mission-version').textContent=`配置 v${data.revision}`;renderMissions();
}
async function loadMissions(){acceptMissionPolicy(await api('/api/missions'))}
function addMission(copy=false){
  if(!missionWorkspace.saved||policyBusy('missions')||missionWorkspace.rows.length>=missionWorkspace.limits.max_missions)return;
  const selected=currentMission();if(copy&&!selected)return;
  const m=copy?{...structuredClone(selected),id:0,title:(selected.title+' 副本').slice(0,60)}:{id:0,title:'新任务',description:'',kind:'login',daily:true,enabled:true,target:1,rewards:[{type:10,reward_typeid:0,num:1,card_lv:0,card_fame:0,card_love:0,card_skill_lv:[]}]};
  missionWorkspace.rows.push(m);missionWorkspace.selected=missionWorkspace.rows.length-1;
  $('#mission-search').value='';$('#mission-filter-category').value='all';$('#mission-filter-state').value='all';renderMissions();$('#mission-title').focus();$('#mission-title').select();
}
$('#mission-add').onclick=()=>addMission();$('#mission-copy').onclick=()=>addMission(true);
$('#mission-remove').onclick=()=>{
  const m=currentMission();if(!m||policyBusy('missions'))return;
  if(!confirm(`移除任务“${m.title||'未填写标题'}”？保存后从玩家任务列表消失，已发出的礼物保留。`))return;
  missionWorkspace.rows.splice(missionWorkspace.selected,1);missionWorkspace.selected=Math.min(missionWorkspace.selected,missionWorkspace.rows.length-1);renderMissions();
};
$('#mission-list').onclick=event=>{const button=event.target.closest('[data-mission-index]');if(!button||policyBusy('missions'))return;missionWorkspace.selected=Number(button.dataset.missionIndex);renderMissions()};
for(const id of ['#mission-search','#mission-filter-category','#mission-filter-state'])$(id).oninput=renderMissionList;
for(const [id,key] of [['#mission-title','title'],['#mission-description','description'],['#mission-target','target']])$(id).oninput=()=>{
  const m=currentMission();if(!m||policyBusy('missions'))return;m[key]=key==='target'?Number($(id).value):$(id).value;renderMissionList();missionChanged();
};
$('#mission-enabled').onchange=()=>{const m=currentMission();if(!m||policyBusy('missions'))return;m.enabled=$('#mission-enabled').checked;renderMissionList();missionChanged()};
function changeMissionCondition(){
  const m=currentMission();if(!m||policyBusy('missions'))return;m.kind=$('#mission-kind').value;m.daily=$('#mission-category').value==='daily';
  if(m.kind==='level'||m.kind==='collection')m.daily=false;if(m.daily&&m.kind==='login')m.target=1;
  updateMissionCondition();renderMissionList();missionChanged();
}
$('#mission-kind').onchange=$('#mission-category').onchange=changeMissionCondition;
$('#mission-rewards').oninput=event=>{const input=event.target.closest('[data-mission-reward]'),m=currentMission();if(!input||!m||policyBusy('missions'))return;m.rewards[Number(input.dataset.missionReward)][input.dataset.field]=Number(input.value);missionChanged()};
$('#mission-rewards').onclick=event=>{const button=event.target.closest('[data-mission-reward-remove]'),m=currentMission();if(!button||!m||policyBusy('missions'))return;m.rewards.splice(Number(button.dataset.missionRewardRemove),1);renderMissionRewards();renderMissionList();missionChanged()};
$('#mission-reward-add').onclick=()=>{
  const m=currentMission();if(!m||policyBusy('missions'))return;
  openContentPicker(entries=>{
    if(m!==currentMission()||policyBusy('missions'))throw new Error('任务已切换，请重新选择奖励');
    const keys=new Set(m.rewards.map(contentKey)),added=entries.filter(e=>!keys.has(contentKey(e)));
    if(m.rewards.length+added.length>missionWorkspace.limits.max_rewards)throw new Error(`每项任务最多 ${missionWorkspace.limits.max_rewards} 种奖励`);
    rememberMissionEntries(entries);m.rewards.push(...added.map(contentReward));renderMissionRewards();renderMissionList();missionChanged();
  },missionRewardKinds);
};
function maximizeMissionCards(field,catalogField){
  const m=currentMission();if(!m||policyBusy('missions'))return;
  for(const r of m.rewards){if(r.type!==6)continue;const entry=missionWorkspace.entries.get(contentKey(r));if(!entry?.[catalogField]){toast('卡牌上限数据缺失，请刷新任务配置',true);return}}
  for(const r of m.rewards)if(r.type===6)r[field]=missionWorkspace.entries.get(contentKey(r))[catalogField];
  renderMissionRewards();missionChanged();
}
$('#mission-card-max-level').onclick=()=>maximizeMissionCards('card_lv','level_max');$('#mission-card-max-fame').onclick=()=>maximizeMissionCards('card_fame','fame_max');
$('#mission-revert').onclick=()=>{if(!missionWorkspace.saved||policyBusy('missions')||!confirm('放弃全部未保存的任务修改？'))return;missionWorkspace.rows=structuredClone(missionWorkspace.saved.missions);missionWorkspace.selected=Math.min(missionWorkspace.selected,missionWorkspace.rows.length-1);renderMissions()};
function validateMissionDraft(){
  for(let index=0;index<missionWorkspace.rows.length;index++){
    const m=missionWorkspace.rows[index],fail=message=>{missionWorkspace.selected=index;renderMissions();toast(`${m.title||'未填写标题'}：${message}`,true);return false};
    if(!m.title.trim()||[...m.title.trim()].length>60)return fail('请填写 1–60 字标题');
    if([...m.description].length>200)return fail('任务说明最多 200 字');
    if(!Number.isInteger(m.target)||m.target<1||m.target>missionWorkspace.limits.max_target)return fail('目标须为 1–10000000 的整数');
    if(m.daily&&m.kind==='login'&&m.target!==1)return fail('每日登录目标固定为 1');
    if(m.rewards.length<1||m.rewards.length>missionWorkspace.limits.max_rewards)return fail('请选择 1–4 种奖励');
    for(const r of m.rewards){
      const entry=missionWorkspace.entries.get(contentKey(r)),max=[14,16,18].includes(r.type)?1:[6,15,19].includes(r.type)?100:10000000;
      if(!Number.isInteger(r.num)||r.num<1||r.num>max)return fail(`奖励数量须为 1–${max} 的整数`);
      if(r.type!==6)continue;
      for(const [field,min,max] of [['card_lv',1,entry?.level_max||r.card_lv],['card_fame',1,entry?.fame_max||r.card_fame],['card_love',0,entry?.love_max||0]])if(!Number.isInteger(r[field])||r[field]<min||r[field]>max)return fail('卡牌等级、名声或忠诚度超过该卡牌上限');
    }
  }
  return true;
}
$('#mission-save').onclick=()=>{
  if(!missionWorkspace.saved||!missionPolicyDirty()||policyBusy('missions')||!validateMissionDraft())return;
  return contentAction('missions',async()=>{
    const config=missionPolicyDraft();for(const m of config.missions){m.title=m.title.trim();m.description=m.description.trim()}
    const changed=config.missions.filter(missionIdentityChanged).length,kept=new Set(config.missions.filter(m=>m.id).map(m=>m.id)),removed=missionWorkspace.saved.missions.filter(m=>!kept.has(m.id)).length,added=config.missions.filter(m=>!m.id).length;
    if(!confirm(`保存 ${config.missions.length} 项任务（启用 ${config.missions.filter(m=>m.enabled).length} 项）？\n新增 ${added} 项，移除 ${removed} 项。${changed?`\n${changed} 项改变了分类或条件，将创建新任务，原领取记录不转移。`:''}\n其他修改保留领取记录，已发出的礼物保留。`))return;
    const data=await api('/api/missions',{method:'PUT',body:JSON.stringify({expected_revision:missionWorkspace.revision,config})});acceptMissionPolicy(data);state.loaded.delete('audit');toast('任务配置已保存');
  });
};

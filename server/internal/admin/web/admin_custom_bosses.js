'use strict';
const customBossEditor={revision:0,sources:[],bosses:[],draft:null,dirty:false,busy:false,request:0};
const customBossAttributes=[['FIRE','火'],['ICE','冰'],['WIND','风'],['LIGHT','光'],['DARK','暗'],['EARTH','地'],['THUNDER','雷'],['WATER','水'],['NEUTRAL','无属性'],['NULL','无'],['NONE','无']];
const customBossStatusNames=['','眩晕','沉默','魅惑','中毒','燃烧','冻结','流血','侵蚀','弱点','感电','陷阱持续伤害','封锁 COST','抽卡惩罚','治疗削减'];
function customBossError(message=''){$('#custom-boss-error').hidden=!message;$('#custom-boss-error').textContent=message}
function customBossControls(){
  const e=customBossEditor;
  $('#custom-boss-save').disabled=e.busy||!e.draft||!e.dirty;
  $('#custom-boss-create').disabled=e.busy||!$('#custom-boss-source').value;
  $('#custom-boss-close').disabled=e.busy;
  $('#custom-boss-modal .dialog-body').inert=e.busy;
  $('#custom-boss-state').textContent=e.busy?'正在保存…':e.dirty?'有未保存的修改':e.draft?'已保存':'选择模板开始制作';
}
function customBossChanged(){customBossEditor.dirty=true;customBossError();customBossControls()}
function renderCustomBossSources(selected){
  const rows=customBossEditor.sources.filter(s=>matchesWords(`${s.name} ${s.difficulty} ${s.boss_id}`,$('#custom-boss-search').value));
  $('#custom-boss-source').innerHTML=rows.map(s=>`<option value="${s.boss_id}">${esc(s.name)} · ${esc(s.difficulty)} · ${s.boss_id}</option>`).join('')||'<option value="">没有匹配的可复制难度</option>';
  if(selected&&rows.some(s=>s.boss_id===selected))$('#custom-boss-source').value=selected;
  customBossControls();
}
function renderCustomBossSelect(){
  $('#custom-boss-select').innerHTML='<option value="">选择一个 Boss 继续编辑</option>'+customBossEditor.bosses.map(b=>`<option value="${b.boss_id}">${esc(b.name)} · ${esc(b.difficulty)}${b.enabled?'':'（关闭）'}</option>`).join('');
  $('#custom-boss-select').value=customBossEditor.draft?.boss_id||'';
}
function customBossNumber(index,key,label,value,min=0,max=2000000000){
  return `<div class="field"><label for="cb-${index}-${key}">${label}</label><input id="cb-${index}-${key}" data-target="${index}" data-stat="${key}" type="number" min="${min}" max="${max}" step="1" value="${value}"></div>`;
}
function customBossArray(index,key,labels,values,min,max){
  if(!values)return '';
  return labels.map(([slot,label])=>customBossNumber(index,`${key}:${slot}`,label,values[slot],min,max)).join('');
}
function renderCustomBossForm(){
  const b=customBossEditor.draft;$('#custom-boss-form').hidden=!b;
  if(!b){customBossControls();return}
  $('#custom-boss-create-panel').open=false;
  $('#custom-boss-name').value=b.name;$('#custom-boss-difficulty').value=b.difficulty;
  $('#custom-boss-bp').value=b.bp_use;$('#custom-boss-bp-half').value=b.bp_use_half;
  $('#custom-boss-enabled').checked=b.enabled;$('#custom-boss-continue').checked=b.continue;
  const source=customBossEditor.sources.find(s=>s.boss_id===b.source_boss_id);
  $('#custom-boss-template').textContent=`沿用模板：${source?.name||b.source_boss_id} · ${source?.difficulty||''}。从组队入口进入；一个人请选择 AI 房间。`;
  $('#custom-boss-targets').innerHTML=b.targets.map((t,i)=>{
    const s=t.stats,originalAttribute=source?.targets.find(row=>row.battle_index===t.battle_index&&row.enemy_index===t.enemy_index)?.stats?.attribute;
    const attrs=originalAttribute&&!customBossAttributes.some(([v])=>v===originalAttribute)?[[originalAttribute,`模板属性：${originalAttribute}`],...customBossAttributes]:customBossAttributes;
    const attribute=`<div class="field"><label for="cb-${i}-attribute">属性</label><select id="cb-${i}-attribute" data-target="${i}" data-stat="attribute">${attrs.map(([v,n])=>`<option value="${v}" ${s.attribute===v?'selected':''}>${esc(n)}</option>`).join('')}</select></div>`;
    const base=[['hp','HP'],['attack','物攻'],['magic','魔攻'],['recovery','回复'],['defense','物防'],['magic_defense','魔防'],['damage_reduction','通用固定减伤']].map(([key,label])=>customBossNumber(i,key,label,s[key]??0,key==='hp'?1:key.includes('defense')?-2000000000:0)).join('');
    const fixed=customBossArray(i,'attribute_fixed',['火','冰','风','光','暗'].map((n,j)=>[j,n+'固定减伤']),s.attribute_fixed,-2000000000,2000000000);
    const rates=customBossArray(i,'attribute_rates',['火','冰','风','光','暗','地','雷','水','无属性'].map((n,j)=>[j,n+'伤害倍率（%）']),s.attribute_rates,0,100000);
    const statuses=customBossArray(i,'status_resistances',customBossStatusNames.slice(1).map((n,j)=>[j+1,n+'抗性（%）']),s.status_resistances,0,100);
    const dot=customBossArray(i,'dot_reductions',['中毒','燃烧','冻结','流血','感电'].map((n,j)=>[j,n+'减伤']),s.dot_reductions,0,2000000000);
    return `<div class="panel"><div class="panel-head"><h3>${esc(targetLabel(t))}</h3></div><div class="panel-body"><div class="publish-row">${attribute}${base}</div><details class="tool-drawer"><summary>属性减伤与抗性</summary><p class="hint">属性伤害倍率 100 表示原倍率，0 表示该属性伤害为零；异常抗性 100 表示完全抵抗。数值仍受原技能和被动影响。</p><div class="publish-row">${fixed}${rates}${statuses}${dot}</div></details></div></div>`;
  }).join('');
  customBossControls();
}
function selectCustomBoss(id){
  customBossEditor.draft=structuredClone(customBossEditor.bosses.find(b=>b.boss_id===id)||null);
  customBossEditor.dirty=false;customBossError();renderCustomBossSelect();renderCustomBossForm();
}
async function openCustomBossEditor(sourceID=0,bossID=0){
  if(customBossEditor.busy)return;
  if(customBossEditor.dirty&&!confirm('放弃未保存的 Boss 修改？'))return;
  const serial=++customBossEditor.request;customBossEditor.draft=null;customBossEditor.dirty=false;customBossEditor.busy=true;
  $('#custom-boss-modal').classList.add('open');$('#custom-boss-form').hidden=true;customBossError();customBossControls();
  try{
    const data=await api('/api/custom-bosses');if(serial!==customBossEditor.request)return;
    customBossEditor.revision=data.revision;customBossEditor.sources=data.sources;customBossEditor.bosses=data.bosses;
    $('#custom-boss-search').value='';renderCustomBossSources(sourceID);renderCustomBossSelect();
    $('#custom-boss-create-panel').open=!bossID;
    if(bossID)selectCustomBoss(bossID);
  }catch(e){customBossError(errorText(e))}finally{customBossEditor.busy=false;customBossControls()}
}
function closeCustomBossEditor(){
  if(customBossEditor.busy)return false;
  if(customBossEditor.dirty&&!confirm('放弃未保存的 Boss 修改？'))return false;
  $('#custom-boss-modal').classList.remove('open');customBossEditor.request++;customBossEditor.dirty=false;return true;
}
async function refreshCustomBossCatalogs(){
  state.loaded.delete('drops');state.loaded.delete('audit');state.loaded.delete('dungeon-schedule');
  try{
    if(!adminPolicyDirty('bosses'))await loadBossPublication(state.bossCatalog||'activity');
    else state.loaded.delete('bosses');
  }catch(e){state.loaded.delete('bosses');customBossError(`Boss 已保存，目录刷新失败：${errorText(e)}。重新打开 Boss 发布页面可重试。`)}
}
$('#custom-boss-open').onclick=()=>openCustomBossEditor();
$('#custom-boss-close').onclick=closeCustomBossEditor;
$('#custom-boss-search').oninput=()=>renderCustomBossSources();
$('#custom-boss-source').onchange=customBossControls;
$('#custom-boss-select').onchange=()=>{
  if(customBossEditor.dirty&&!confirm('切换会放弃未保存的 Boss 修改，继续？')){renderCustomBossSelect();return}
  selectCustomBoss(Number($('#custom-boss-select').value));
};
$('#custom-boss-create').onclick=async()=>{
  if(customBossEditor.busy||!$('#custom-boss-source').value)return;
  if(customBossEditor.dirty&&!confirm('添加新 Boss 会放弃当前未保存修改，继续？'))return;
  customBossEditor.busy=true;customBossError();customBossControls();
  try{
    const data=await api('/api/custom-bosses',{method:'POST',body:JSON.stringify({expected_revision:customBossEditor.revision,source_boss_id:Number($('#custom-boss-source').value)})});
    customBossEditor.revision=data.revision;customBossEditor.bosses.push(data.boss);selectCustomBoss(data.boss.boss_id);
    toast('已复制为独立 Boss，当前保持关闭');await refreshCustomBossCatalogs();
  }catch(e){customBossError(e?.status===409?'配置已被其他页面更新，请关闭后重新打开再制作。':errorText(e))}finally{customBossEditor.busy=false;customBossControls()}
};
for(const id of ['name','difficulty','bp','bp-half','enabled','continue'])$(`#custom-boss-${id}`).addEventListener('input',()=>{
  const b=customBossEditor.draft;if(!b)return;
  b.name=$('#custom-boss-name').value;b.difficulty=$('#custom-boss-difficulty').value;
  b.bp_use=Number($('#custom-boss-bp').value);b.bp_use_half=Number($('#custom-boss-bp-half').value);
  b.enabled=$('#custom-boss-enabled').checked;b.continue=$('#custom-boss-continue').checked;customBossChanged();
});
$('#custom-boss-targets').addEventListener('input',e=>{
  const el=e.target;if(!el.dataset.stat)return;
  const stats=customBossEditor.draft.targets[Number(el.dataset.target)].stats;
  const [key,slot]=el.dataset.stat.split(':');
  const value=key==='attribute'?el.value:Number(el.value);
  if(slot!==undefined)stats[key][Number(slot)]=value;else stats[key]=value;
  customBossChanged();
});
$('#custom-boss-save').onclick=async()=>{
  const b=customBossEditor.draft;if(customBossEditor.busy||!b||!customBossEditor.dirty)return;
  const invalid=[...$('#custom-boss-form').querySelectorAll('input[type=number]')].find(el=>!el.value||!el.checkValidity());
  if(invalid){invalid.reportValidity();invalid.focus();return}
  if(!b.name.trim()||!b.difficulty.trim()){customBossError('请填写 Boss 名称和难度名称');return}
  if(b.bp_use_half>b.bp_use){customBossError('组队体力不能超过单人体力');return}
  customBossEditor.busy=true;customBossError();customBossControls();
  const body={expected_revision:customBossEditor.revision,...Object.fromEntries(['boss_id','name','difficulty','bp_use','bp_use_half','enabled','continue','targets'].map(k=>[k,b[k]]))};
  try{
    const data=await api('/api/custom-bosses',{method:'PUT',body:JSON.stringify(body)});
    customBossEditor.revision=data.revision;customBossEditor.bosses=customBossEditor.bosses.map(row=>row.boss_id===data.boss.boss_id?data.boss:row);selectCustomBoss(data.boss.boss_id);
    toast('Boss 配置已保存，后续新开房使用新数值');await refreshCustomBossCatalogs();
  }catch(e){customBossError(e?.status===409?'配置已被其他页面更新，当前修改未保存。请关闭后重新打开。':errorText(e))}finally{customBossEditor.busy=false;customBossControls()}
};
$('#custom-boss-drops').onclick=async()=>{
  const id=customBossEditor.draft?.boss_id;if(!id||!closeCustomBossEditor())return;
  state.loaded.delete('drops');await switchView('drops');await contentAction('drops',()=>selectDrop(id),'载入');$('#drop-select').value=id;
};
$('#boss-detail-rules').addEventListener('click',e=>{
  const b=e.target.closest('[data-copy-boss],[data-custom-boss]');if(!b||!closeBossDetails())return;
  openCustomBossEditor(Number(b.dataset.copyBoss)||0,Number(b.dataset.customBoss)||0);
});

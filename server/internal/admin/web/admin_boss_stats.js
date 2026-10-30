'use strict';
const bossStatsEditor={boss:null,edits:[],revision:0,target:0,tab:'stats',mode:'package',dirty:false,busy:false,loading:false,request:0};
function bossStatsError(message=''){$('#boss-stats-error').textContent=message;$('#boss-stats-error').hidden=!message}
function bossStatsControls(){
  const e=bossStatsEditor;
  $('#boss-stats-save').disabled=e.busy||e.loading||!e.boss||!e.dirty;
  $('#boss-stats-package').disabled=e.busy||e.loading||!e.boss;
  $('#boss-stats-cancel').disabled=e.busy;
  $('#boss-stats-form').inert=e.busy;
  $('#boss-stats-fields').disabled=e.mode==='package';
}
function bossStatsChanged(){bossStatsEditor.dirty=true;bossStatsError();bossStatsControls()}
function emptyBossStatPatch(t){return {battle_index:t.battle_index,enemy_index:t.enemy_index,enemy_id:t.stats.enemy_id,values:{}}}
function setBossStatsData(data){
  const e=bossStatsEditor;e.boss=data.boss;e.revision=data.revision;e.dirty=false;
  e.edits=e.boss.targets.map(t=>structuredClone(data.overrides.find(r=>r.battle_index===t.battle_index&&r.enemy_index===t.enemy_index)||emptyBossStatPatch(t)));
  e.mode=data.overrides.length?'custom':'package';
  customBossOriginalActions.set(e.boss.boss_id,data.original_actions||[]);
  $('#boss-stats-title').textContent=`${e.boss.name} · ${e.boss.difficulty} · 原版数值`;
  $('#boss-stats-form').hidden=false;
  renderBossStats();
}
async function openBossStats(id){
  const e=bossStatsEditor;if(e.busy)return;
  if(e.dirty&&!confirm('放弃尚未保存的 BOSS 数值修改？'))return;
  const request=++e.request;e.boss=null;e.loading=true;e.dirty=false;e.target=0;e.tab='stats';
  bossStatsError();$('#boss-stats-form').hidden=true;$('#boss-stats-loading').hidden=false;$('#boss-stats-title').textContent='修改原版 BOSS 数值';
  $('#boss-stats-modal').classList.add('open');bossStatsControls();
  try{const data=await api(`/api/boss-stats?boss_id=${id}`);if(request!==e.request)return;setBossStatsData(data)}
  catch(error){if(request===e.request)bossStatsError(errorText(error))}
  finally{if(request===e.request){e.loading=false;$('#boss-stats-loading').hidden=true;bossStatsControls()}}
}
function bossStatNumber(key,label,value,min=0,max=2000000000){
  const e=bossStatsEditor,patch=e.mode==='custom'?e.edits[e.target]:null,id=`bs-${key.replace(':','-')}`;
  return `<div class="field"><label for="${id}">${label}</label><input id="${id}" data-boss-stat="${key}" type="number" step="1" min="${min}" max="${max}" placeholder="${value}" value="${patch?.values?.[key]??''}"><small class="hint">包内原值：${value}；留空使用原值</small></div>`;
}
function renderBossStatFields(t){
  const e=bossStatsEditor,s=t.stats,p=e.mode==='custom'?e.edits[e.target]:null;
  const attr=`<div class="field"><label for="bs-attribute">属性</label><select id="bs-attribute" data-boss-attribute><option value="">包内原属性：${esc(attributeName(s.attribute)||s.attribute)}</option>${customBossAttributes.map(([v,n])=>`<option value="${v}" ${p?.attribute===v?'selected':''}>${n}</option>`).join('')}</select></div>`;
  const base=[['hp','HP'],['attack','物攻'],['magic','魔攻'],['recovery','回复'],['defense','物防'],['magic_defense','魔防'],['damage_reduction','通用固定减伤']].map(([k,n])=>bossStatNumber(k,n,s[k],k==='hp'?1:k.includes('defense')?-2000000000:0,k==='hp'?3000000000:2000000000)).join('');
  const arrays=(key,labels,min,max)=>!s[key]?'':labels.map((label,j)=>label?bossStatNumber(`${key}:${j}`,label,s[key][j],min,max):'').join('');
  const advanced=arrays('attribute_fixed',['火','冰','风','光','暗'].map(n=>n+'固定减伤'),-2000000000,2000000000)+arrays('attribute_rates',['火','冰','风','光','暗','地','雷','水','无属性'].map(n=>n+'伤害倍率（%）'),0,100000)+arrays('status_resistances',customBossStatusNames.map(n=>n?n+'抗性（%）':''),0,100)+arrays('dot_reductions',['中毒','燃烧','冻结','流血','感电'].map(n=>n+'减伤'),0,2000000000);
  return `<div class="cb-stats-grid">${attr}${base}</div><details class="cb-resistances"><summary>属性减伤与异常抗性</summary><p class="hint">属性倍率 100 表示原倍率，50 表示减半，0 表示该属性伤害为零。异常抗性 100 表示完全抵抗。</p><div class="cb-stats-grid">${advanced}</div></details>`;
}
function renderBossStats(){
  const e=bossStatsEditor;if(!e.boss)return;
  const t=e.boss.targets[e.target],waves=[...new Set(e.boss.targets.map(t=>t.battle_index))];
  $('#boss-stats-mode').value=e.mode;
  $('#boss-stats-wave').innerHTML=waves.map(w=>`<option value="${w}">第 ${w+1} 波</option>`).join('');$('#boss-stats-wave').value=String(t.battle_index);
  $('#boss-stats-part').innerHTML=e.boss.targets.map((p,i)=>p.battle_index===t.battle_index?`<option value="${i}">${esc(p.name)} · 部位 ${p.enemy_index+1}</option>`:'').join('');$('#boss-stats-part').value=String(e.target);
  $$('#boss-stats-tabs button').forEach(b=>{const active=b.dataset.statsTab===e.tab;b.classList.toggle('active',active);b.setAttribute('aria-pressed',String(active))});
  $('#boss-stats-part-form').innerHTML=e.tab==='stats'?renderBossStatFields(t):`<div data-action-target="${e.target}" data-original-list="native-${e.target}">${renderCustomBossOriginalSkills({...t,actions:null,original_skills:e.mode==='custom'?e.edits[e.target].original_skills:[]},e.target,e.boss.boss_id,'bo')}</div>`;
  bossStatsControls();
}
function validBossStatsPart(){
  const invalid=[...$('#boss-stats-fields').querySelectorAll('input[type=number]')].find(n=>!n.matches(':disabled')&&!n.checkValidity());
  if(!invalid)return true;invalid.closest('details')?.setAttribute('open','');invalid.reportValidity();invalid.focus();return false;
}
$('#boss-stats-mode').onchange=event=>{bossStatsEditor.mode=event.target.value;bossStatsChanged();renderBossStats()};
$('#boss-stats-wave').onchange=event=>{if(!validBossStatsPart()){event.target.value=String(bossStatsEditor.boss.targets[bossStatsEditor.target].battle_index);return}bossStatsEditor.target=bossStatsEditor.boss.targets.findIndex(t=>t.battle_index===Number(event.target.value));renderBossStats()};
$('#boss-stats-part').onchange=event=>{if(!validBossStatsPart()){event.target.value=String(bossStatsEditor.target);return}bossStatsEditor.target=Number(event.target.value);renderBossStats()};
$('#boss-stats-tabs').onclick=event=>{const b=event.target.closest('[data-stats-tab]');if(!b||!validBossStatsPart())return;bossStatsEditor.tab=b.dataset.statsTab;renderBossStats()};
$('#boss-stats-part-form').addEventListener('input',event=>{
  const e=bossStatsEditor;if(e.busy||e.mode!=='custom')return;
  const p=e.edits[e.target],key=event.target.dataset.bossStat;
  if(key){p.values??={};if(event.target.value==='')delete p.values[key];else p.values[key]=Number(event.target.value);bossStatsChanged();return}
  const field=event.target.dataset.originalField;if(!['power','power_rate','hits'].includes(field))return;
  const card=event.target.closest('[data-original-slot]'),identity={slot:Number(card.dataset.originalSlot),skill_id:Number(card.dataset.originalSkill),function_id:Number(card.dataset.originalFunction)};
  p.original_skills??=[];let edit=p.original_skills.find(s=>s.slot===identity.slot&&s.skill_id===identity.skill_id&&s.function_id===identity.function_id);
  if(!edit){edit=identity;p.original_skills.push(edit)}
  if(event.target.value==='')delete edit[field];else edit[field]=Number(event.target.value);
  p.original_skills=p.original_skills.filter(s=>s.power!=null||s.power_rate!=null||s.hits!=null);bossStatsChanged();
});
$('#boss-stats-part-form').addEventListener('change',event=>{if(event.target.hasAttribute('data-boss-attribute')){const p=bossStatsEditor.edits[bossStatsEditor.target];if(event.target.value)p.attribute=event.target.value;else delete p.attribute;bossStatsChanged()}});
$('#boss-stats-part-form').addEventListener('click',event=>{
  const button=event.target.closest('[data-original-reset]');if(!button)return;
  const card=button.closest('[data-original-slot]'),p=bossStatsEditor.edits[bossStatsEditor.target];
  p.original_skills=(p.original_skills||[]).filter(s=>!(s.slot===Number(card.dataset.originalSlot)&&s.skill_id===Number(card.dataset.originalSkill)&&s.function_id===Number(card.dataset.originalFunction)));
  bossStatsChanged();renderBossStats();
});
$('#boss-stats-part-reset').onclick=()=>{const e=bossStatsEditor;e.edits[e.target]=emptyBossStatPatch(e.boss.targets[e.target]);bossStatsChanged();renderBossStats()};
$('#boss-stats-package').onclick=()=>{const e=bossStatsEditor;if(e.busy||!e.boss)return;e.mode='package';e.edits=e.boss.targets.map(emptyBossStatPatch);bossStatsChanged();renderBossStats()};
$('#boss-stats-cancel').onclick=()=>{const e=bossStatsEditor;if(e.busy||e.dirty&&!confirm('放弃尚未保存的 BOSS 数值修改？'))return;e.request++;e.boss=null;e.dirty=false;e.loading=false;$('#boss-stats-modal').classList.remove('open')};
$('#boss-detail-rules').addEventListener('click',event=>{const b=event.target.closest('[data-boss-stats]');if(b&&closeBossDetails())openBossStats(Number(b.dataset.bossStats))});
$('#boss-stats-save').onclick=async()=>{
  const e=bossStatsEditor;if(e.busy||!e.boss||!e.dirty||!validBossStatsPart())return;
  const overrides=e.mode==='package'?[]:e.edits.filter(p=>Object.keys(p.values||{}).length||p.attribute!=null||p.original_skills?.length);
  e.busy=true;bossStatsError();bossStatsControls();
  try{
    const data=await api('/api/boss-stats',{method:'PUT',body:JSON.stringify({expected_revision:e.revision,boss_id:e.boss.boss_id,overrides})});
    setBossStatsData(data);state.loaded.delete('audit');
    // Refresh existing cards without discarding publication/selection drafts.
    for(const g of state.groups)for(const b of g.bosses||[])if(b.boss_id===data.boss.boss_id){
      b.stats_edited=!!data.overrides.length;b.targets=structuredClone(data.boss.targets);
      for(const p of data.overrides){const t=b.targets.find(t=>t.battle_index===p.battle_index&&t.enemy_index===p.enemy_index);if(!t)continue;for(const [k,v] of Object.entries(p.values||{})){const [key,slot]=k.split(':');if(slot!=null)t.stats[key][Number(slot)]=v;else t.stats[key]=v}if(p.attribute!=null)t.stats.attribute=p.attribute}
    }
    renderBosses();toast(data.overrides.length?'BOSS 数值已保存，用于新开的组队／AI 房间':'已恢复资源包原值，用于新开的组队／AI 房间');
  }catch(error){bossStatsError(error?.status===409?'配置已被其他页面更新，本次修改未保存。请关闭后重新打开；当前草稿仍保留。':errorText(error))}
  finally{e.busy=false;bossStatsControls()}
};
window.addEventListener('beforeunload',event=>{if(bossStatsEditor.dirty||bossStatsEditor.busy){event.preventDefault();event.returnValue=''}});

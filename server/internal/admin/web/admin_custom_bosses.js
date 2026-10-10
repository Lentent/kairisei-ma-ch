'use strict';
const customBossEditor={revision:0,sources:[],bosses:[],draft:null,dirty:false,busy:false,request:0,tab:'basic',target:0,skills:new Map(),skillRequests:new Map(),actionDrafts:new Map()};
const customBossAttributes=[['FIRE','火'],['ICE','冰'],['WIND','风'],['LIGHT','光'],['DARK','暗'],['EARTH','地'],['THUNDER','雷'],['WATER','水'],['NEUTRAL','无属性'],['NULL','无'],['NONE','无']];
const customBossStatusNames=['','眩晕','沉默','魅惑','中毒','燃烧','冻结','流血','侵蚀','弱点','感电','陷阱持续伤害','封锁 COST','抽卡惩罚','治疗削减'];
let customBossBuffPresets=[];
function renderCustomBossPage(){
  const rows=customBossEditor.bosses.filter(b=>matchesWords(`${b.name} ${b.difficulty} ${b.boss_id}`,$('#custom-boss-list-search').value));
  $('#custom-boss-count').textContent=`${rows.length} / ${customBossEditor.bosses.length} 个`;
  $('#custom-boss-list').innerHTML=rows.map(b=>`<button type="button" class="cb-boss-choice ${customBossEditor.draft?.boss_id===b.boss_id?'selected':''}" data-edit-custom-boss="${b.boss_id}" aria-pressed="${customBossEditor.draft?.boss_id===b.boss_id}"><span class="cb-choice-top"><b>${esc(b.name)}</b><i class="cb-choice-status ${b.enabled?'enabled':''}" title="${b.enabled?'允许开放':'制作中'}"></i></span><span>${esc(b.difficulty)} · ${b.boss_id}</span><small>${b.targets.filter(t=>t.actions!=null).length} 个部位已编排行动</small></button>`).join('')||'<p class="empty">暂无匹配的 Boss。点击「新建 Boss」开始制作。</p>';
}
async function loadCustomBossPage(){
  const [data,buffs]=await Promise.all([api('/api/custom-bosses'),api('/api/custom-boss-buffs')]);
  customBossBuffPresets=buffs.buffs;
  const e=customBossEditor,id=e.draft?.boss_id;
  e.revision=data.revision;e.sources=data.sources;e.bosses=data.bosses;
  renderCustomBossSources();selectCustomBoss(e.bosses.some(b=>b.boss_id===id)?id:e.bosses[0]?.boss_id||0,true);
}
$('#custom-boss-list-search').oninput=renderCustomBossPage;
$('#custom-boss-list').addEventListener('click',e=>{const b=e.target.closest('[data-edit-custom-boss]');if(b)openCustomBossEditor(0,Number(b.dataset.editCustomBoss))});
function customBossError(message=''){$('#custom-boss-error').hidden=!message;$('#custom-boss-error').textContent=message}
function customBossControls(){
  const e=customBossEditor;
  $('#custom-boss-save').disabled=e.busy||!e.draft||!e.dirty;
  $('#custom-boss-create').disabled=e.busy||!$('#custom-boss-source').value;
  $('#custom-boss-reset').disabled=e.busy||!e.dirty;
  $('#custom-boss-reset').textContent=e.resetPending?'再次点击确认撤销':'撤销本次修改';
  $('#custom-boss-open').disabled=e.busy;
  $('#custom-boss-editor').inert=e.busy;
  $('#custom-boss-list').inert=e.busy;
  $('#custom-boss-status-dot').classList.toggle('dirty',e.dirty);
  $('#custom-boss-state').textContent=e.busy?'正在保存…':e.dirty?'有未保存的修改':e.draft?'已保存':'选择模板开始制作';
}
function customBossChanged(){customBossEditor.dirty=true;customBossEditor.resetPending=false;customBossError();customBossControls();updateCustomBossHeading()}
function customBossCanLeave(){if(!customBossEditor.dirty)return true;customBossError('当前 Boss 有未保存的修改。请先保存，或点击底部「撤销本次修改」后再切换。');$('#custom-boss-error').scrollIntoView({block:'center',behavior:'smooth'});return false}
function renderCustomBossSources(selected){
  const rows=customBossEditor.sources.filter(s=>matchesWords(`${s.name} ${s.difficulty} ${s.boss_id}`,$('#custom-boss-search').value));
  $('#custom-boss-source').innerHTML=rows.map(s=>`<option value="${s.boss_id}">${esc(s.name)} · ${esc(s.difficulty)} · ${s.boss_id}</option>`).join('')||'<option value="">没有匹配的可复制难度</option>';
  if(selected&&rows.some(s=>s.boss_id===selected))$('#custom-boss-source').value=selected;
  customBossControls();
}
function updateCustomBossHeading(){
  const b=customBossEditor.draft;if(!b)return;
  $('#custom-boss-title').textContent=b.name||'未命名 Boss';
  $('#custom-boss-identity').textContent=`BOSS ${b.boss_id} · ${new Set(b.targets.map(t=>t.battle_index)).size} 波 · ${b.targets.length} 个部位`;
  $('#custom-boss-opening').textContent=b.enabled?'允许开放':'制作中 · 未开放';
  $('#custom-boss-opening').className=`pill ${b.enabled?'ok':'gold'}`;
}
function updateCustomBossScope(){
  const e=customBossEditor,b=e.draft;if(!b)return;
  if(!b.targets[e.target])e.target=0;
  const t=b.targets[e.target],basic=e.tab==='basic';
  $('#custom-boss-basic').hidden=!basic;$('#custom-boss-part-scope').hidden=basic;
  $('#custom-boss-part-scope').setAttribute('aria-labelledby',`cb-tab-${e.tab==='actions'?'actions':'stats'}`);
  $$('[data-boss-tab]').forEach(el=>{const on=el.dataset.bossTab===e.tab;el.classList.toggle('active',on);el.setAttribute('aria-selected',String(on));el.tabIndex=on?0:-1});
  const waves=[...new Set(b.targets.map(row=>row.battle_index))];
  $('#custom-boss-waves').innerHTML=waves.map(w=>`<button type="button" data-boss-wave="${w}" class="${t.battle_index===w?'active':''}" aria-pressed="${t.battle_index===w}">第 ${w+1} 波</button>`).join('');
  $('#custom-boss-part').innerHTML=b.targets.map((row,i)=>row.battle_index===t.battle_index?`<option value="${i}">${esc(row.name)} · 部位 ${row.enemy_index+1}</option>`:'').join('');
  $('#custom-boss-part').value=e.target;
  $('#custom-boss-part-title').textContent=t.name||`部位 ${t.enemy_index+1}`;
  $('#custom-boss-part-note').textContent=e.tab==='actions'?'为这个波次的当前部位编排行动。其他波次和部位分别配置。':'这些是 Boss 部位自身的基础数值。HP 已包含原编队倍率。';
  $('#custom-boss-part-mode').textContent=e.tab==='actions'?(t.actions==null?'沿用模板行动':`${t.actions.length} 条自定义行动`):`敌人 ${t.stats.enemy_id}`;
  $$('[data-boss-part-panel]').forEach(el=>{
    el.hidden=Number(el.dataset.bossPartPanel)!==e.target;
    el.querySelector('.cb-part-stats').hidden=e.tab!=='stats';
    el.querySelector('[data-action-target]').hidden=e.tab!=='actions';
  });
}
$('#custom-boss-tabs').addEventListener('click',e=>{const tab=e.target.closest('[data-boss-tab]');if(tab){customBossEditor.tab=tab.dataset.bossTab;updateCustomBossScope()}});
$('#custom-boss-tabs').addEventListener('keydown',e=>{if(!['ArrowLeft','ArrowRight','Home','End'].includes(e.key))return;e.preventDefault();const tabs=$$('[data-boss-tab]'),index=tabs.indexOf(e.target),next=e.key==='Home'?0:e.key==='End'?tabs.length-1:(index+(e.key==='ArrowRight'?1:-1)+tabs.length)%tabs.length;tabs[next].click();tabs[next].focus()});
$('#custom-boss-waves').addEventListener('click',e=>{const wave=e.target.closest('[data-boss-wave]');if(wave){customBossEditor.target=customBossEditor.draft.targets.findIndex(t=>t.battle_index===Number(wave.dataset.bossWave));updateCustomBossScope()}});
$('#custom-boss-part').onchange=e=>{customBossEditor.target=Number(e.target.value);updateCustomBossScope()};
function customBossNumber(index,key,label,value,min=0,max=2000000000){
  return `<div class="field"><label for="cb-${index}-${key}">${label}</label><input id="cb-${index}-${key}" data-target="${index}" data-stat="${key}" type="number" min="${min}" max="${max}" step="1" value="${value}"></div>`;
}
function customBossArray(index,key,labels,values,min,max){
  if(!values)return '';
  return labels.map(([slot,label])=>customBossNumber(index,`${key}:${slot}`,label,values[slot],min,max)).join('');
}
function customBossActionNumber(i,j,key,label,value,min,max,optional=false){
  return `<div class="field"><label for="ca-${i}-${j}-${key}">${label}</label><input id="ca-${i}-${j}-${key}" type="number" data-action-field="${key}" min="${min}" max="${max}" step="1" ${optional?'placeholder="沿用招式"':'required'} value="${value??''}"></div>`;
}
function customBossActionSchedule(a){return `${a.turn===0?'开场':`第 ${a.turn??'—'} 回合`} · ${a.repeat_every?`每隔 ${a.repeat_every} 回合`:'仅施放一次'}`}
function renderCustomBossActions(t,i){
  const enabled=t.actions!=null;
  const mode=!enabled?'original':t.include_original_actions?'combined':'custom';
  const modeHelp={original:'只使用模板的出招逻辑，不执行自定义行动。',custom:'替换该部位的普通行动和蓄力招式。没有安排的回合不出招，空表表示不出招。',combined:'该部位先执行原逻辑，再按列表顺序追加当回合的自定义行动；没有安排自定义行动的回合仍执行原逻辑。原行动的条件、概率、次数和蓄力机制保留，自定义行动不占用原行动次数。'};
  const rows=(t.actions||[]).map((a,j)=>{
    const source=a.source_boss_id||customBossEditor.draft.source_boss_id;
    const sources=customBossEditor.sources.map(s=>`<option value="${s.boss_id}" ${s.boss_id===source?'selected':''}>${esc(s.name)} · ${esc(s.difficulty)} · ${s.boss_id}</option>`).join('');
    const targets=[['AUTO','按招式默认目标'],['RANDOM','单体：随机玩家'],['MERCENARY','单体：佣兵'],['MILLIONAIRE','单体：富豪'],['THIEF','单体：盗贼'],['SINGER','单体：歌姬']].map(([v,n])=>`<option value="${v}" ${a.target===v?'selected':''}>${n}</option>`).join('');
    return `<article class="cb-action-card" data-action-row="${j}"><div class="cb-action-head"><div><h4><span class="cb-action-number">${j+1}</span> 招式行动</h4><span class="cb-action-schedule" id="ca-${i}-${j}-schedule">${customBossActionSchedule(a)}</span></div><div class="cb-action-tools"><button type="button" class="secondary sm" data-action-command="up" ${j===0?'disabled':''}>上移</button> <button type="button" class="secondary sm" data-action-command="down" ${j===t.actions.length-1?'disabled':''}>下移</button> <button type="button" class="secondary sm" data-action-command="delete">删除</button></div></div><div class="cb-action-body"><div class="cb-field-grid">
      ${customBossActionNumber(i,j,'turn','首次回合（0 为开场）',a.turn,0,999)}${customBossActionNumber(i,j,'repeat_every','之后每隔几回合（0 为只放一次）',a.repeat_every,0,999)}
      <div class="field"><label for="ca-${i}-${j}-source">招式来源 Boss</label><select id="ca-${i}-${j}-source" data-action-field="source_boss_id">${sources}</select></div>
      <div class="field"><label for="ca-${i}-${j}-skill">招式与效果分支</label><select id="ca-${i}-${j}-skill" data-action-field="skill"><option value="${a.skill_id}:${a.function_id}">${a.skill_id?'招式 '+a.skill_id:'正在读取招式…'}</option></select></div>
      <div class="field"><label for="ca-${i}-${j}-target">单体选人方式（全体招式仍作用于全体）</label><select id="ca-${i}-${j}-target" data-action-field="target">${targets}</select></div>
    </div><div class="custom-boss-effects" id="ca-${i}-${j}-effects" aria-live="polite">选择招式后，这里显示作用对象、Buff 数值和持续回合等实际效果。</div>
    <div id="ca-${i}-${j}-damage" class="custom-boss-damage" hidden><h4>本次攻击的伤害设置</h4><div class="cb-damage-grid">${customBossActionNumber(i,j,'power','每段基础伤害（防御前）',a.power,0,100000000,true)}${customBossActionNumber(i,j,'power_rate','基础伤害倍率（%）',a.power_rate,0,1000,true)}${customBossActionNumber(i,j,'hits','每个攻击效果的攻击次数',a.hits,1,20,true)}</div><p class="hint">留空沿用招式；基础伤害填 5000 用于防御前计算，倍率填 50 表示减半。同时填写时先替换再乘倍率。这里只修改攻击，不修改招式附带的 Buff。</p></div>
    <div id="ca-${i}-${j}-buff-editor" class="custom-boss-buff-editor" hidden></div><div class="field cb-animation-field"><label for="ca-${i}-${j}-animation">施放演出</label><select id="ca-${i}-${j}-animation" data-action-field="animation"><option value="auto">自动匹配演出</option></select><p class="hint" id="ca-${i}-${j}-animation-note"></p></div></div></article>`;
  }).join('');
  const presets=customBossBuffPresets;
  return `<section class="cb-action-section" data-action-target="${i}"><div class="cb-action-mode"><div class="field"><label for="ca-${i}-mode">行动逻辑模式（当前波次、当前部位）</label><select id="ca-${i}-mode" data-action-mode aria-describedby="ca-${i}-mode-help">${[['original','只用原逻辑'],['custom','只用自定义行动'],['combined','原逻辑＋自定义行动']].map(([v,label])=>`<option value="${v}" ${mode===v?'selected':''}>${label}</option>`).join('')}</select><p class="hint" id="ca-${i}-mode-help">${modeHelp[mode]}</p></div></div><p class="hint">首次 1／间隔 2 表示第 1、3、5……回合施放，间隔 0 只放一次。自定义行动同回合按列表从上到下执行；开场（第 0 回合）也遵循所选模式。开场被动、破坏技能仍沿用模板。死亡、眩晕的部位不执行回合行动。</p><div data-action-list ${enabled?'':'hidden'}><div class="custom-boss-buff-picker"><label for="ca-${i}-buff-preset">快速添加玩家 Buff · ${presets.length} 个资源招式分支</label><input type="search" id="ca-${i}-buff-search" data-buff-search placeholder="搜索物攻、魔防、抽卡、暴击、回复、屏障等" aria-label="搜索玩家 Buff"><div class="filterbar"><select id="ca-${i}-buff-preset">${customBossBuffPresetOptions(presets)}</select><button type="button" class="secondary" data-action-command="add-buff" ${presets.length?'':'disabled'}>添加 Buff 行动</button></div><p class="hint">自动整理当前资源中的纯玩家增益、回复和减益解除招式；添加后逐项修改数值。属性、范围等沿用来源，完整效果显示在行动卡内。若想保留 Boss 原有攻击并额外加 Buff，选择「原逻辑＋自定义行动」。</p></div>${rows}<button type="button" class="secondary" data-action-command="add">添加普通／其他行动</button></div></section>`;
}
function customBossBuffPresetOptions(rows){return rows.map(p=>`<option value="${p.source_boss_id}:${p.skill_id}:${p.function_id}">${esc(p.label)}</option>`).join('')||'<option value="">没有匹配的玩家增益招式</option>'}
function updateCustomBossActionEffects(i,j){
  const a=customBossEditor.draft?.targets[i].actions?.[j];if(!a)return;
  const skill=customBossEditor.skills.get(a.source_boss_id||customBossEditor.draft.source_boss_id)?.find(s=>s.skill_id===a.skill_id&&s.function_id===a.function_id);
  const effects=$(`#ca-${i}-${j}-effects`),damage=$(`#ca-${i}-${j}-damage`);if(!effects)return;
  effects.innerHTML=skill?`<b>来源招式效果${skill.player_buff?' · 含玩家 Buff':''}</b><ul>${(skill.effects||[]).map(text=>`<li>${esc(text)}</li>`).join('')||'<li>该分支没有可显示的战斗效果。</li>'}</ul><p class="hint">这里显示来源的原始效果；本次攻击与玩家增益按下方填写的数值覆盖，留空继承来源。效果范围沿用招式；单体职业由「单体选人方式」指定。</p>`:'选择招式后，这里显示作用对象、Buff 原始数值和持续回合。';
  damage.hidden=!skill?.attack;
  damage.querySelectorAll('input').forEach(el=>{el.disabled=!skill?.attack});
  const buffs=$(`#ca-${i}-${j}-buff-editor`);
  buffs.hidden=!skill?.buff_editors?.length;
  buffs.innerHTML=(skill?.buff_editors||[]).map(b=>{const saved=a.buffs?.find(o=>o.role_index===b.role_index);return `<div class="cb-buff-role"><h4>效果 ${b.role_index+1} · ${esc(b.name)}</h4><div class="cb-field-grid">${b.fields.map(f=>`<div class="field"><label for="ca-${i}-${j}-buff-${b.role_index}-${f.key}">${esc(f.label)}</label><input id="ca-${i}-${j}-buff-${b.role_index}-${f.key}" type="number" data-buff-role="${b.role_index}" data-buff-field="${f.key}" min="${f.min}" max="${f.max}" step="1" value="${saved?.[f.key]??''}" placeholder="${f.value!=null?`沿用：${f.value}`:'沿用原公式'}"></div>`).join('')}</div></div>`}).join('')+(skill?.buff_editors?.length?'<p class="hint">留空沿用来源；填写数值后仅覆盖本次行动的对应效果。按原属性／伤害公式计算的增益，填写点数后改用固定值。一次性回复、抽卡与爆发槽没有持续回合输入。每个效果分别配置，全体招式对每位玩家应用相同设置。</p>':'');
  const animations=customBossEditor.skills.get(customBossEditor.draft.source_boss_id)||[];
  const compatible=animations.filter(s=>s.role_families?.join('|')===skill?.role_families?.join('|')&&(s.effect_2d||s.effect_3d));
  const select=$(`#ca-${i}-${j}-animation`);
  select.innerHTML='<option value="auto">自动：缺少演出时匹配原 Boss 兼容动作</option><option value="source">沿用来源招式的演出</option>'+compatible.map(s=>`<option value="${s.function_id}">${esc(s.part)} · ${esc(s.name)||'招式 '+s.skill_id} · ${s.effect_3d?'3D':''}${s.effect_2d?' / 2D':''}</option>`).join('');
  select.value=a.animation_function_id?String(a.animation_function_id):a.animation_source?'source':'auto';
  const missing=a.animation_function_id&&!compatible.some(s=>s.function_id===a.animation_function_id);
  if(missing){select.innerHTML+=`<option value="${a.animation_function_id}">已保存演出 ${a.animation_function_id}（当前分支不兼容，请重新选择）</option>`;select.value=String(a.animation_function_id)}
  $(`#ca-${i}-${j}-animation-note`).textContent=skill?`来源演出：2D ${skill.effect_2d||'未提供'}；3D ${skill.effect_3d||'未提供'}。自动模式在缺少对应演出时尝试匹配当前 Boss 的兼容招式，找不到则保留来源。更换演出只改变动作和特效，数值与作用对象按上方配置计算；动作名称可能沿用演出来源。跨模型动作需在游戏中核对。`:'';
}
async function customBossSkillOptions(source){
  const e=customBossEditor;
  if(e.skills.has(source))return e.skills.get(source);
  if(!e.skillRequests.has(source))e.skillRequests.set(source,api(`/api/custom-boss-skills?source_boss_id=${source}`).then(data=>{e.skills.set(source,data.skills);return data.skills}).finally(()=>e.skillRequests.delete(source)));
  return e.skillRequests.get(source);
}
async function loadCustomBossActionSkills(){
  const draft=customBossEditor.draft;if(!draft)return;
  await Promise.all(draft.targets.flatMap((t,i)=>(t.actions||[]).map(async(a,j)=>{
    const source=a.source_boss_id||draft.source_boss_id;
    try{
      const [skills]=await Promise.all([customBossSkillOptions(source),customBossSkillOptions(draft.source_boss_id)]);
      if(customBossEditor.draft!==draft||draft.targets[i].actions?.[j]!==a)return;
      const select=$(`#ca-${i}-${j}-skill`);if(!select)return;
      select.innerHTML='<option value="">请选择招式</option>'+skills.map(s=>`<option value="${s.skill_id}:${s.function_id}">${s.player_buff?'[玩家 Buff] ':s.attack?'[攻击] ':''}${esc(s.part)} · ${esc(s.name)||'未命名招式'} · ${s.skill_id}/${s.function_id}</option>`).join('');
      select.value=a.skill_id?`${a.skill_id}:${a.function_id}`:'';
      select.disabled=!skills.length;
      if(!skills.length)select.innerHTML='<option value="">此 Boss 没有可借用的招式</option>';
      updateCustomBossActionEffects(i,j);
    }catch(err){if(customBossEditor.draft===draft)customBossError(`读取招式失败：${errorText(err)}`)}
  })));
}
function refreshCustomBossActionEditor(i){
  const old=$(`[data-action-target="${i}"]`);
  old.outerHTML=renderCustomBossActions(customBossEditor.draft.targets[i],i);
  customBossChanged();updateCustomBossScope();loadCustomBossActionSkills();
}
$('#custom-boss-targets').addEventListener('change',e=>{
  const field=e.target.dataset.actionField,mode=e.target.hasAttribute('data-action-mode');if(!field&&!mode)return;
  const i=Number(e.target.closest('[data-action-target]').dataset.actionTarget),t=customBossEditor.draft.targets[i];
  if(mode){
    if(e.target.value==='original'){if(t.actions!=null)customBossEditor.actionDrafts.set(i,t.actions);t.actions=null;t.include_original_actions=false}
    else{t.actions=t.actions??customBossEditor.actionDrafts.get(i)??[];t.include_original_actions=e.target.value==='combined'}
    refreshCustomBossActionEditor(i);return;
  }
  const j=Number(e.target.closest('[data-action-row]').dataset.actionRow),a=t.actions[j];
  if(field==='animation'){a.animation_source=e.target.value==='source';a.animation_function_id=Number(e.target.value)||0}
  else if(field==='skill'){
    delete a.buffs;delete a.animation_function_id;delete a.animation_source;
    const [id,fn]=e.target.value.split(':').map(Number);a.skill_id=id||0;a.function_id=fn||0;
    const s=customBossEditor.skills.get(a.source_boss_id||customBossEditor.draft.source_boss_id)?.find(s=>s.skill_id===id&&s.function_id===fn);
    if(s&&!s.attack){delete a.power;delete a.power_rate;delete a.hits;['power','power_rate','hits'].forEach(key=>$(`#ca-${i}-${j}-${key}`).value='')}
    updateCustomBossActionEffects(i,j);
  }else if(field==='source_boss_id'){
    a.source_boss_id=Number(e.target.value);a.skill_id=0;a.function_id=0;delete a.power;delete a.power_rate;delete a.hits;delete a.buffs;delete a.animation_function_id;delete a.animation_source;refreshCustomBossActionEditor(i);
  }else if(field==='target')a.target=e.target.value;
  else if(e.target.value==='')delete a[field];else a[field]=Number(e.target.value);
  customBossChanged();
});
$('#custom-boss-targets').addEventListener('input',e=>{
  if(e.target.hasAttribute('data-buff-search')){const i=Number(e.target.closest('[data-action-target]').dataset.actionTarget),rows=customBossBuffPresets.filter(p=>matchesWords(`${p.label} ${(p.buff_editors||[]).map(b=>b.name).join(' ')}`,e.target.value));$(`#ca-${i}-buff-preset`).innerHTML=customBossBuffPresetOptions(rows);$(`[data-action-target="${i}"] [data-action-command="add-buff"]`).disabled=!rows.length;return}
  if(e.target.dataset.buffField){
    const i=Number(e.target.closest('[data-action-target]').dataset.actionTarget),j=Number(e.target.closest('[data-action-row]').dataset.actionRow),a=customBossEditor.draft.targets[i].actions[j],r=Number(e.target.dataset.buffRole),key=e.target.dataset.buffField;
    a.buffs??=[];let b=a.buffs.find(b=>b.role_index===r);if(!b){b={role_index:r};a.buffs.push(b)}
    if(e.target.value==='')delete b[key];else b[key]=Number(e.target.value);
    a.buffs=a.buffs.filter(b=>Object.keys(b).length>1);if(!a.buffs.length)delete a.buffs;
    customBossChanged();return;
  }
  const field=e.target.dataset.actionField;if(!['turn','repeat_every','power','power_rate','hits'].includes(field))return;
  const i=Number(e.target.closest('[data-action-target]').dataset.actionTarget),j=Number(e.target.closest('[data-action-row]').dataset.actionRow);
  const a=customBossEditor.draft.targets[i].actions[j];
  if(e.target.value==='')delete a[field];else a[field]=Number(e.target.value);
  $(`#ca-${i}-${j}-schedule`).textContent=customBossActionSchedule(a);
  customBossChanged();
});
$('#custom-boss-targets').addEventListener('click',async e=>{
  const button=e.target.closest('[data-action-command]');if(!button)return;
  const i=Number(button.closest('[data-action-target]').dataset.actionTarget),t=customBossEditor.draft.targets[i];
  const j=Number(button.closest('[data-action-row]')?.dataset.actionRow),command=button.dataset.actionCommand;
  if(command==='add-buff'){
    if(t.actions.length>=100){customBossError('每个部位最多配置100条行动');return}
    const preset=customBossBuffPresets.find(p=>`${p.source_boss_id}:${p.skill_id}:${p.function_id}`===$(`#ca-${i}-buff-preset`).value),draft=customBossEditor.draft,actions=t.actions;
    if(!preset)return;
    button.disabled=true;
    try{
      const skills=await customBossSkillOptions(preset.source_boss_id);
      if(customBossEditor.draft!==draft||draft.targets[i].actions!==actions)return;
      if(!skills.some(s=>s.skill_id===preset.skill_id&&s.function_id===preset.function_id&&s.player_buff))throw new Error('当前资源没有该 Buff 分支，请选择其他来源。');
      if(t.actions.length>=100)throw new Error('每个部位最多配置100条行动');
      t.actions.push({source_boss_id:preset.source_boss_id,turn:1,repeat_every:0,skill_id:preset.skill_id,function_id:preset.function_id,target:'AUTO'});refreshCustomBossActionEditor(i);
    }catch(err){customBossError(errorText(err))}finally{button.disabled=false}
    return;
  }else if(command==='add'){
    if(t.actions.length>=100){customBossError('每个部位最多配置100条行动');return}
    t.actions.push({source_boss_id:customBossEditor.draft.source_boss_id,turn:1,repeat_every:0,skill_id:0,function_id:0,target:'AUTO'});
  }else if(command==='delete')t.actions.splice(j,1);
  else{const k=j+(command==='up'?-1:1);if(k<0||k>=t.actions.length)return;[t.actions[j],t.actions[k]]=[t.actions[k],t.actions[j]]}
  refreshCustomBossActionEditor(i);
});
function renderCustomBossForm(){
  const b=customBossEditor.draft;$('#custom-boss-form').hidden=!b;
  $('#custom-boss-empty').hidden=!!b||!$('#custom-boss-create-panel').hidden;
  if(!b){customBossControls();return}
  $('#custom-boss-create-panel').hidden=true;
  $('#custom-boss-empty').hidden=true;
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
    return `<div data-boss-part-panel="${i}" hidden><section class="cb-part-stats"><div class="cb-stats-grid">${attribute}${base}</div><details class="cb-resistances"><summary>进阶设置 · 属性减伤与异常抗性</summary><p class="hint">属性伤害倍率 100 表示原倍率，0 表示该属性伤害为零；异常抗性 100 表示完全抵抗。数值仍受原技能和被动影响。</p><div class="cb-stats-grid">${fixed}${rates}${statuses}${dot}</div></details></section>${renderCustomBossActions(t,i)}</div>`;
  }).join('');
  updateCustomBossHeading();updateCustomBossScope();customBossControls();
}
function selectCustomBoss(id,preserveScope=false){
  if(!preserveScope){customBossEditor.target=0;customBossEditor.tab='basic'}
  customBossEditor.draft=structuredClone(customBossEditor.bosses.find(b=>b.boss_id===id)||null);
  customBossEditor.actionDrafts.clear();
  customBossEditor.dirty=false;customBossEditor.resetPending=false;customBossError();renderCustomBossPage();renderCustomBossForm();
  loadCustomBossActionSkills();
}
async function openCustomBossEditor(sourceID=0,bossID=0){
  if(customBossEditor.busy)return;
  if(!customBossCanLeave())return;
  if(state.currentView!=='custom-bosses')await switchView('custom-bosses');
  if(bossID){selectCustomBoss(bossID);return}
  customBossEditor.returnBossID=customBossEditor.draft?.boss_id;
  selectCustomBoss(0);$('#custom-boss-search').value='';renderCustomBossSources(sourceID);
  $('#custom-boss-create-panel').hidden=false;$('#custom-boss-empty').hidden=true;
  $('#custom-boss-search').focus();
}
function closeCustomBossEditor(){
  if(customBossEditor.busy)return false;
  if(!customBossCanLeave())return false;
  return true;
}
async function refreshCustomBossCatalogs(){
  renderCustomBossPage();
  state.loaded.delete('drops');state.loaded.delete('audit');state.loaded.delete('dungeon-schedule');
  try{
    if(!adminPolicyDirty('bosses'))await loadBossPublication(state.bossCatalog||'activity');
    else state.loaded.delete('bosses');
  }catch(e){state.loaded.delete('bosses');customBossError(`Boss 已保存，目录刷新失败：${errorText(e)}。重新打开 Boss 发布页面可重试。`)}
}
$('#custom-boss-open').onclick=()=>openCustomBossEditor();
$('#custom-boss-reset').onclick=()=>{
  if(customBossEditor.busy||!customBossEditor.dirty)return;
  if(!customBossEditor.resetPending){customBossEditor.resetPending=true;customBossError('再次点击「确认撤销」会丢弃本次未保存的修改，恢复到上次保存的配置。');customBossControls();return}
  selectCustomBoss(customBossEditor.draft.boss_id,true);
};
$('#custom-boss-create-cancel').onclick=()=>{$('#custom-boss-create-panel').hidden=true;selectCustomBoss(customBossEditor.returnBossID||customBossEditor.bosses[0]?.boss_id||0)};
$('#custom-boss-search').oninput=()=>renderCustomBossSources();
$('#custom-boss-source').onchange=customBossControls;
$('#custom-boss-create').onclick=async()=>{
  if(customBossEditor.busy||!$('#custom-boss-source').value)return;
  if(!customBossCanLeave())return;
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
  const invalid=[...$('#custom-boss-form').querySelectorAll('input[type=number]')].find(el=>!el.disabled&&((!el.value&&!el.dataset.buffField&&!['power','power_rate','hits'].includes(el.dataset.actionField))||!el.checkValidity()));
  if(invalid){const part=invalid.closest('[data-boss-part-panel]');customBossEditor.tab=part?(invalid.dataset.actionField||invalid.dataset.buffField?'actions':'stats'):'basic';if(part)customBossEditor.target=Number(part.dataset.bossPartPanel);updateCustomBossScope();invalid.closest('details')?.setAttribute('open','');invalid.reportValidity();invalid.focus();return}
  if(!b.name.trim()||!b.difficulty.trim()){customBossError('请填写 Boss 名称和难度名称');return}
  if(b.bp_use_half>b.bp_use){customBossError('组队体力不能超过单人体力');return}
  const incomplete=b.targets.findIndex(t=>t.actions?.some(a=>!a.skill_id||!a.function_id));
  if(incomplete!==-1){customBossEditor.tab='actions';customBossEditor.target=incomplete;updateCustomBossScope();customBossError('请为每条行动选择有效招式');const j=b.targets[incomplete].actions.findIndex(a=>!a.skill_id||!a.function_id);$(`#ca-${incomplete}-${j}-skill`).focus();return}
  customBossEditor.busy=true;customBossError();customBossControls();
  const body={expected_revision:customBossEditor.revision,...Object.fromEntries(['boss_id','name','difficulty','bp_use','bp_use_half','enabled','continue','targets'].map(k=>[k,b[k]]))};
  try{
    const data=await api('/api/custom-bosses',{method:'PUT',body:JSON.stringify(body)});
    customBossEditor.revision=data.revision;customBossEditor.bosses=customBossEditor.bosses.map(row=>row.boss_id===data.boss.boss_id?data.boss:row);selectCustomBoss(data.boss.boss_id,true);
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

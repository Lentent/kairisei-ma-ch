'use strict';
const customBossEditor={revision:0,sources:[],bosses:[],appearanceCurrent:{},draft:null,dirty:false,busy:false,request:0,tab:'basic',target:0,skills:new Map(),skillRequests:new Map(),actionDrafts:new Map()};
const customBossAttributes=[['FIRE','火'],['ICE','冰'],['WIND','风'],['LIGHT','光'],['DARK','暗'],['EARTH','地'],['THUNDER','雷'],['WATER','水'],['NEUTRAL','无属性'],['NULL','无'],['NONE','无']];
const customBossStatusNames=['','眩晕','沉默','魅惑','中毒','燃烧','冻结','流血','侵蚀','弱点','感电','陷阱持续伤害','封锁 COST','抽卡惩罚','治疗削减'];
let customBossBuffPresets=[];
const customBossOriginalActions=new Map();

function renderCustomBossOriginalSkills(t,i,source=customBossEditor.draft.source_boss_id,idPrefix='co'){
  if(!customBossOriginalActions.has(source))return '<p class="hint">正在读取原有招式…</p>';
  const entries=customBossOriginalActions.get(source).filter(a=>a.battle_index===t.battle_index&&a.enemy_index===t.enemy_index);
  const inactive=t.actions!=null&&!t.include_original_actions;
  const categories={normal:'普通攻击',skill:'普通招式／先制',special:'蓄力招式',death:'破坏招式',passive:'开场被动'};
  return `<h3>原有招式 · ${entries.length} 个分支</h3><p class="hint">直接修改下方原有攻击，无需重新添加行动。原回合条件、概率、选人、顺序和分支判断继续生效。留空继承原值；基础伤害是防御前的每段威力，倍率 50 表示减半，同时填写先替换再乘倍率。多个攻击效果分别应用设置，附带效果保持原样。</p>${inactive?'<p class="hint">当前为「只用自定义行动」，原有普通／蓄力招式不会执行；下面的修改会保留，切回含原逻辑的模式后生效。</p>':''}`+entries.map((a,j)=>{
    const saved=t.original_skills?.find(s=>s.slot===a.slot&&s.skill_id===a.skill_id&&s.function_id===a.function_id);
    const fields=[['power','每段基础伤害（防御前）',0,100000000],['power_rate','基础伤害倍率（%）',0,1000],['hits','每个攻击效果的攻击次数',1,20]];
    return `<article class="cb-action-card" data-original-slot="${a.slot}" data-original-skill="${a.skill_id}" data-original-function="${a.function_id}"><div class="cb-action-head"><div><h4>${esc(a.name||'招式 '+a.skill_id)}</h4><span class="hint">${esc(categories[a.category]||a.category)} · ${a.skill_id} / 分支 ${a.function_id}</span></div>${a.editable?'<button type="button" class="secondary sm" data-original-reset>恢复原值</button>':''}</div><div class="cb-action-body"><p class="hint">${esc(a.rule)}</p><p class="hint">${esc(a.branch||'')}</p><div class="custom-boss-effects"><b>原始效果</b><ul>${(a.effects||[]).map(s=>`<li>${esc(s)}</li>`).join('')||'<li>暂无可显示的效果资料。</li>'}</ul></div>${a.editable?`<div class="cb-damage-grid">${fields.map(([key,label,min,max])=>`<div class="field"><label for="${idPrefix}-${i}-${j}-${key}">${label}</label><input id="${idPrefix}-${i}-${j}-${key}" data-original-field="${key}" type="number" min="${min}" max="${max}" step="1" placeholder="沿用原招式" value="${saved?.[key]??''}"></div>`).join('')}</div>`:`<p class="hint">${esc(a.reason||'此招式暂不支持编辑')}，保持原有行为。</p>`}</div></article>`;
  }).join('')+(entries.length?'':'<p class="empty">当前部位没有原有招式资料。</p>');
}

function refreshCustomBossOriginalSkills(){
  const b=customBossEditor.draft;if(!b)return;
  b.targets.forEach((t,i)=>{const el=$(`[data-original-list="${i}"]`);if(el)el.innerHTML=renderCustomBossOriginalSkills(t,i)});
}

$('#custom-boss-targets').addEventListener('input',e=>{
  const key=e.target.dataset.originalField;if(!['power','power_rate','hits'].includes(key))return;
  const card=e.target.closest('[data-original-slot]'),i=Number(card.closest('[data-action-target]').dataset.actionTarget),t=customBossEditor.draft.targets[i];
  const identity={slot:Number(card.dataset.originalSlot),skill_id:Number(card.dataset.originalSkill),function_id:Number(card.dataset.originalFunction)};
  t.original_skills??=[];
  let edit=t.original_skills.find(s=>s.slot===identity.slot&&s.skill_id===identity.skill_id&&s.function_id===identity.function_id);
  if(!edit){edit=identity;t.original_skills.push(edit)}
  if(e.target.value==='')delete edit[key];else edit[key]=Number(e.target.value);
  t.original_skills=t.original_skills.filter(s=>s.power!=null||s.power_rate!=null||s.hits!=null);
  if(!t.original_skills.length)delete t.original_skills;
  customBossChanged();
});
$('#custom-boss-targets').addEventListener('click',e=>{
  if(e.target.closest('[data-original-retry]')){loadCustomBossActionSkills();return}
  const button=e.target.closest('[data-original-reset]');if(!button)return;
  const card=button.closest('[data-original-slot]'),i=Number(card.closest('[data-action-target]').dataset.actionTarget),t=customBossEditor.draft.targets[i];
  t.original_skills=(t.original_skills||[]).filter(s=>!(s.slot===Number(card.dataset.originalSlot)&&s.skill_id===Number(card.dataset.originalSkill)&&s.function_id===Number(card.dataset.originalFunction)));
  if(!t.original_skills.length)delete t.original_skills;
  card.querySelectorAll('[data-original-field]').forEach(el=>el.value='');customBossChanged();
});
function renderCustomBossPage(){
  const rows=customBossEditor.bosses.filter(b=>matchesWords(`${b.name} ${b.difficulty} ${b.boss_id}`,$('#custom-boss-list-search').value));
  $('#custom-boss-count').textContent=`${rows.length} / ${customBossEditor.bosses.length} 个`;
  $('#custom-boss-list').innerHTML=rows.map(b=>`<button type="button" class="cb-boss-choice ${customBossEditor.draft?.boss_id===b.boss_id?'selected':''}" data-edit-custom-boss="${b.boss_id}" aria-pressed="${customBossEditor.draft?.boss_id===b.boss_id}"><span class="cb-choice-top"><b>${esc(b.name)}</b><i class="cb-choice-status ${b.enabled?'enabled':''}" title="${b.enabled?'允许开放':'制作中'}"></i></span><span>${esc(b.difficulty)} · ${b.boss_id}</span><small>${b.targets.filter(t=>t.actions!=null).length} 个部位已编排行动</small></button>`).join('')||'<p class="empty">暂无匹配的 Boss。点击「新建 Boss」开始制作。</p>';
}
async function loadCustomBossPage(){
  const [data,buffs]=await Promise.all([api('/api/custom-bosses'),api('/api/custom-boss-buffs')]);
  customBossBuffPresets=buffs.buffs;
  const e=customBossEditor,id=e.draft?.boss_id;
  e.revision=data.revision;e.sources=data.sources;e.bosses=data.bosses;e.appearanceCurrent=data.appearance_current||{};
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
  $$('[data-boss-export]').forEach(el=>el.disabled=e.busy||e.dirty||!e.draft);
  $('#custom-boss-editor').inert=e.busy;
  $('#custom-boss-list').inert=e.busy;
  $('#custom-boss-status-dot').classList.toggle('dirty',e.dirty);
  $('#custom-boss-state').textContent=e.busy?'正在处理…':e.dirty?'有未保存的修改':e.draft?'已保存':'选择模板开始制作';
}
function customBossChanged(){customBossEditor.dirty=true;customBossEditor.resetPending=false;customBossError();customBossControls();updateCustomBossHeading()}
function customBossCanLeave(){if(!customBossEditor.dirty)return true;customBossError('当前 Boss 有未保存的修改。请先保存，或点击底部「撤销本次修改」后再切换。');$('#custom-boss-error').scrollIntoView({block:'center',behavior:'smooth'});return false}
function renderCustomBossSources(selected){
  const rows=customBossEditor.sources.filter(s=>matchesWords(`${s.name} ${s.difficulty} ${s.boss_id} ${s.battle_model||''} ${s.targets.some(t=>t.static_image_id)?'2D 可换图':''}`,$('#custom-boss-search').value));
  $('#custom-boss-source').innerHTML=rows.map(s=>`<option value="${s.boss_id}">${s.targets.some(t=>t.static_image_id)?'[2D 可换图] ':s.battle_model==='3d'?'[3D] ':''}${esc(s.name)} · ${esc(s.difficulty)} · ${s.boss_id}</option>`).join('')||'<option value="">没有匹配的可复制难度</option>';
  if(selected&&rows.some(s=>s.boss_id===selected))$('#custom-boss-source').value=selected;
  customBossControls();
}
function updateCustomBossHeading(){
  const b=customBossEditor.draft;if(!b)return;
  $('#custom-boss-title').textContent=b.name||'未命名 Boss';
  $('#custom-boss-selection-title').textContent=b.name||'未命名关卡';
  $$('[data-selection-name]').forEach(el=>el.textContent=b.name||'未命名关卡');
  $('#custom-boss-identity').textContent=`BOSS ${b.boss_id} · ${new Set(b.targets.map(t=>t.battle_index)).size} 波 · ${b.targets.length} 个部位`;
  $('#custom-boss-opening').textContent=b.enabled?'允许开放':'制作中 · 未开放';
  $('#custom-boss-opening').className=`pill ${b.enabled?'ok':'gold'}`;
}
function updateCustomBossScope(){
  const e=customBossEditor,b=e.draft;if(!b)return;
  if(!b.targets[e.target])e.target=0;
  const t=b.targets[e.target],basic=e.tab==='basic';
  $('#custom-boss-basic').hidden=!basic;$('#custom-boss-part-scope').hidden=basic;
  $('#custom-boss-part-scope').setAttribute('aria-labelledby',`cb-tab-${e.tab}`);
  $$('[data-boss-tab]').forEach(el=>{const on=el.dataset.bossTab===e.tab;el.classList.toggle('active',on);el.setAttribute('aria-selected',String(on));el.tabIndex=on?0:-1});
  const waves=[...new Set(b.targets.map(row=>row.battle_index))];
  $('#custom-boss-waves').innerHTML=waves.map(w=>`<button type="button" data-boss-wave="${w}" class="${t.battle_index===w?'active':''}" aria-pressed="${t.battle_index===w}">第 ${w+1} 波</button>`).join('');
  $('#custom-boss-part').innerHTML=b.targets.map((row,i)=>row.battle_index===t.battle_index?`<option value="${i}">${esc(row.name)} · 部位 ${row.enemy_index+1}</option>`:'').join('');
  $('#custom-boss-part').value=e.target;
  $('#custom-boss-part-title').textContent=t.name||`部位 ${t.enemy_index+1}`;
  $('#custom-boss-part-note').textContent=e.tab==='actions'?'为这个波次的当前部位编排行动。其他波次和部位分别配置。':e.tab==='appearance'?'修改当前波次、当前部位。完整角色图片可在「基础与开放」设置独立排列。':'这些是 Boss 部位自身的基础数值。HP 已包含原编队倍率。';
  $('#custom-boss-part-mode').textContent=e.tab==='actions'?(t.actions==null?'沿用模板行动':`${t.actions.length} 条自定义行动`):`敌人 ${t.stats.enemy_id}`;
  $$('[data-boss-part-panel]').forEach(el=>{
    el.hidden=Number(el.dataset.bossPartPanel)!==e.target;
    el.querySelector('.cb-part-stats').hidden=e.tab!=='stats';
    el.querySelector('.cb-part-appearance').hidden=e.tab!=='appearance';
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
  return `<section class="cb-action-section" data-action-target="${i}"><div class="cb-action-mode"><div class="field"><label for="ca-${i}-mode">行动逻辑模式（当前波次、当前部位）</label><select id="ca-${i}-mode" data-action-mode aria-describedby="ca-${i}-mode-help">${[['original','只用原逻辑'],['custom','只用自定义行动'],['combined','原逻辑＋自定义行动']].map(([v,label])=>`<option value="${v}" ${mode===v?'selected':''}>${label}</option>`).join('')}</select><p class="hint" id="ca-${i}-mode-help">${modeHelp[mode]}</p></div></div><p class="hint">首次 1／间隔 2 表示第 1、3、5……回合施放，间隔 0 只放一次。自定义行动同回合按列表从上到下执行；开场（第 0 回合）也遵循所选模式。开场被动、破坏技能仍沿用模板。死亡、眩晕的部位不执行回合行动。</p><div data-original-list="${i}">${renderCustomBossOriginalSkills(t,i)}</div><div data-action-list ${enabled?'':'hidden'}><div class="custom-boss-buff-picker"><label for="ca-${i}-buff-preset">快速添加玩家 Buff · ${presets.length} 个资源招式分支</label><input type="search" id="ca-${i}-buff-search" data-buff-search placeholder="搜索物攻、魔防、抽卡、暴击、回复、屏障等" aria-label="搜索玩家 Buff"><div class="filterbar"><select id="ca-${i}-buff-preset">${customBossBuffPresetOptions(presets)}</select><button type="button" class="secondary" data-action-command="add-buff" ${presets.length?'':'disabled'}>添加 Buff 行动</button></div><p class="hint">自动整理当前资源中的纯玩家增益、回复和减益解除招式；添加后逐项修改数值。属性、范围等沿用来源，完整效果显示在行动卡内。若想保留 Boss 原有攻击并额外加 Buff，选择「原逻辑＋自定义行动」。</p></div>${rows}<button type="button" class="secondary" data-action-command="add">添加普通／其他行动</button></div></section>`;
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
  if(!e.skillRequests.has(source))e.skillRequests.set(source,api(`/api/custom-boss-skills?source_boss_id=${source}`).then(data=>{e.skills.set(source,data.skills||[]);customBossOriginalActions.set(source,data.original_actions||[]);return data.skills||[]}).finally(()=>e.skillRequests.delete(source)));
  return e.skillRequests.get(source);
}
async function loadCustomBossActionSkills(){
  const draft=customBossEditor.draft;if(!draft)return;
  try{
    await customBossSkillOptions(draft.source_boss_id);
    if(customBossEditor.draft!==draft)return;
    refreshCustomBossOriginalSkills();
  }catch(err){
    if(customBossEditor.draft===draft){
      customBossError(`读取原有招式失败：${errorText(err)}`);
      $$('[data-original-list]').forEach(el=>el.innerHTML='<p class="hint">原有招式读取失败。</p><button type="button" class="secondary sm" data-original-retry>重新读取</button>');
    }
    return;
  }
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
function renderCustomBossAppearance(t,i,source){
  const e=customBossEditor,original=source?.targets.find(s=>s.battle_index===t.battle_index&&s.enemy_index===t.enemy_index),canUpload=!!original?.static_image_id;
  const status=e.appearanceCurrent[e.draft.boss_id]===false?'图片或部位名有待应用的修改。生成更新包后，按包内说明合并资源并重启。':'当前使用已应用的外观；修改图片或部位名后，须重新生成并应用更新包。';
  return `<section class="cb-part-appearance"><div class="cb-entry-note"><b>选关页名称：<span data-selection-name>${esc(e.draft.name)}</span></b><p>下面修改的是战斗部位名。关卡名称、选关封面和图片排列在「基础与开放」分别设置。</p><button type="button" class="secondary sm" data-selection-settings>修改关卡名称与封面</button></div><div class="field"><label for="cb-${i}-name">战斗内的部位名称</label><input id="cb-${i}-name" data-part-name="${i}" maxlength="80" value="${esc(t.name)}"><p class="hint">模板名称：${esc(original?.name||t.name)}。最多 80 字，不支持英文逗号、引号、标签和换行。</p></div><div class="cb-artwork-layout"><div class="cb-artwork-preview" id="cb-${i}-preview">${customBossArtworkPreview(t)}</div><div><h4>部位 PNG</h4>${canUpload?`<div class="field"><label for="cb-${i}-artwork">上传本体或部位图片</label><input id="cb-${i}-artwork" type="file" accept="image/png,.png" data-part-artwork="${i}"></div><button type="button" class="secondary sm" data-part-artwork-reset="${i}" ${t.artwork?'':'disabled'}>恢复模板图片</button><p class="hint">建议透明背景 PNG，单张最多 4MB，边长 64–4096 像素，总像素不超过 800 万；部位图和封面合计最多 16MB。首波本体图默认同时用作选关封面，也可另行上传封面。</p>`:'<p class="hint">这个部位使用 3D 或 Live2D 外观，可修改名称。上传 PNG 请选择标有「2D 可换图」的模板及普通图片部位。</p>'}<p class="hint">完整角色图选择「独立排列」，本体与部位会分开显示。已制作好透明留白的分层部件图选择「模板拼接」。部位数量和实际战斗破坏关系保持模板配置。</p></div></div><div class="cb-entry-note"><b>应用战斗外观</b><p>${status}</p><p>先保存配置，再生成当前 Boss 的外观更新包。封面、排列和部位名在应用资源包、重启并重新下载资源后生效。</p><button type="button" class="secondary" data-boss-export>生成外观资源更新包</button></div></section>`;
}
function customBossArtworkPreview(t){return t.artwork?`<img src="data:image/png;base64,${esc(t.artwork)}" alt="部位PNG预览">`:'<span class="hint">沿用模板图片<br>上传 PNG 后在这里预览</span>'}
function renderCustomBossSelectionPreview(){
  const b=customBossEditor.draft;if(!b)return;
  const artwork=b.cover_artwork||b.targets.find(t=>t.battle_index===0&&t.enemy_index===0)?.artwork;
  $('#custom-boss-cover-preview').innerHTML=artwork?`<img src="data:image/png;base64,${esc(artwork)}" alt="选关封面预览">`:'<span class="hint">沿用模板封面</span>';
  $('#custom-boss-cover-reset').disabled=!b.cover_artwork;
}
async function readCustomBossPNG(file){
  if(!file.size||file.size>4*1024*1024)throw new Error('请选择4MB以内的PNG图片');
  const signature=new Uint8Array(await file.slice(0,8).arrayBuffer());
  if(![137,80,78,71,13,10,26,10].every((v,i)=>signature[i]===v))throw new Error('图片必须为PNG格式');
  const bitmap=await createImageBitmap(file);
  try{if(bitmap.width<64||bitmap.height<64||bitmap.width>4096||bitmap.height>4096||bitmap.width*bitmap.height>8*1024*1024)throw new Error('PNG边长须为64–4096像素，总像素不超过800万')}finally{bitmap.close()}
  return new Promise((resolve,reject)=>{const reader=new FileReader();reader.onload=()=>resolve(reader.result.split(',')[1]);reader.onerror=()=>reject(new Error('图片读取失败'));reader.readAsDataURL(file)});
}
$('#custom-boss-cover').addEventListener('change',async()=>{
  const e=customBossEditor,b=e.draft,input=$('#custom-boss-cover'),file=input.files[0];if(!b||!file||e.busy)return;
  e.busy=true;customBossControls();customBossError();
  try{
    const artwork=await readCustomBossPNG(file);
    if(b.targets.reduce((n,t)=>n+(t.artwork?.length||0)*.75,file.size)>16*1024*1024)throw new Error('封面和部位PNG合计最多16MB');
    if(e.draft!==b)return;b.cover_artwork=artwork;customBossChanged();renderCustomBossSelectionPreview();
  }catch(err){customBossError(errorText(err))}finally{input.value='';e.busy=false;customBossControls()}
});
$('#custom-boss-cover-reset').onclick=()=>{const b=customBossEditor.draft;if(!b||customBossEditor.busy)return;delete b.cover_artwork;customBossChanged();renderCustomBossSelectionPreview()};
$('#custom-boss-layout').onchange=()=>{const b=customBossEditor.draft;if(b){b.artwork_layout=$('#custom-boss-layout').value;customBossChanged()}};
$('#custom-boss-targets').addEventListener('input',event=>{
  const i=event.target.dataset.partName;if(i===undefined)return;
  const t=customBossEditor.draft.targets[Number(i)];t.name=event.target.value;customBossChanged();updateCustomBossScope();
});
$('#custom-boss-targets').addEventListener('change',async event=>{
  const input=event.target,index=input.dataset.partArtwork;if(index===undefined)return;
  const e=customBossEditor,b=e.draft,t=b?.targets[Number(index)],file=input.files[0];if(!t||!file||e.busy)return;
  e.busy=true;customBossControls();customBossError();
  try{
    const artwork=await readCustomBossPNG(file);
    const total=b.targets.reduce((n,row)=>n+(row===t?file.size:(row.artwork?.length||0)*.75),(b.cover_artwork?.length||0)*.75);
    if(total>16*1024*1024)throw new Error('一个Boss的部位PNG合计最多16MB');
    if(e.draft!==b)return;
    t.artwork=artwork;customBossChanged();renderCustomBossSelectionPreview();$(`#cb-${index}-preview`).innerHTML=customBossArtworkPreview(t);$(`[data-part-artwork-reset="${index}"]`).disabled=false;
  }catch(err){customBossError(errorText(err))}finally{input.value='';e.busy=false;customBossControls()}
});
$('#custom-boss-targets').addEventListener('click',async event=>{
  if(event.target.closest('[data-selection-settings]')){customBossEditor.tab='basic';updateCustomBossScope();$('#custom-boss-name').focus();return}
  const reset=event.target.closest('[data-part-artwork-reset]');
  if(reset){const i=Number(reset.dataset.partArtworkReset),t=customBossEditor.draft?.targets[i];if(!t||customBossEditor.busy)return;delete t.artwork;customBossChanged();renderCustomBossSelectionPreview();$(`#cb-${i}-preview`).innerHTML=customBossArtworkPreview(t);reset.disabled=true;return}
  if(!event.target.closest('[data-boss-export]'))return;
  const e=customBossEditor;if(e.busy||e.dirty||!e.draft)return;
  e.busy=true;customBossControls();customBossError();
  try{
    const r=await fetch('/api/custom-bosses/export',{method:'POST',headers:{'Content-Type':'application/json','X-Kairisei-Admin-Action':'apply'},body:JSON.stringify({expected_revision:e.revision,boss_id:e.draft.boss_id})});
    if(!r.ok){const data=await r.json();throw new Error(data.error||'生成外观资源更新包失败')}
    const url=URL.createObjectURL(await r.blob()),a=document.createElement('a');a.href=url;a.download=`自制Boss-${e.draft.boss_id}-外观-v${e.revision}.zip`;a.click();setTimeout(()=>URL.revokeObjectURL(url),60000);
    toast('外观包已生成。按包内说明合并资源、重启，并让客户端重新下载。');
  }catch(err){customBossError(errorText(err))}finally{e.busy=false;customBossControls()}
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
  $('#custom-boss-layout').value=b.artwork_layout||'auto';renderCustomBossSelectionPreview();
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
    return `<div data-boss-part-panel="${i}" hidden>${renderCustomBossAppearance(t,i,source)}<section class="cb-part-stats"><div class="cb-stats-grid">${attribute}${base}</div><details class="cb-resistances"><summary>进阶设置 · 属性减伤与异常抗性</summary><p class="hint">属性伤害倍率 100 表示原倍率，0 表示该属性伤害为零；异常抗性 100 表示完全抵抗。数值仍受原技能和被动影响。</p><div class="cb-stats-grid">${fixed}${rates}${statuses}${dot}</div></details></section>${renderCustomBossActions(t,i)}</div>`;
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
    customBossEditor.revision=data.revision;customBossEditor.bosses.push(data.boss);customBossEditor.appearanceCurrent[data.boss.boss_id]=data.appearance_current;selectCustomBoss(data.boss.boss_id);
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
  const invalid=[...$('#custom-boss-form').querySelectorAll('input[type=number]')].find(el=>!el.disabled&&((!el.value&&!el.dataset.originalField&&!el.dataset.buffField&&!['power','power_rate','hits'].includes(el.dataset.actionField))||!el.checkValidity()));
  if(invalid){const part=invalid.closest('[data-boss-part-panel]');customBossEditor.tab=part?(invalid.dataset.actionField||invalid.dataset.buffField||invalid.dataset.originalField?'actions':'stats'):'basic';if(part)customBossEditor.target=Number(part.dataset.bossPartPanel);updateCustomBossScope();invalid.closest('details')?.setAttribute('open','');invalid.reportValidity();invalid.focus();return}
  if(!b.name.trim()||!b.difficulty.trim()){customBossError('请填写 Boss 名称和难度名称');return}
  const invalidName=b.targets.findIndex(t=>!t.name.trim()||Array.from(t.name).length>80||/[,"<>\r\n\x00]/.test(t.name)&&t.name!==customBossEditor.sources.find(s=>s.boss_id===b.source_boss_id)?.targets.find(s=>s.battle_index===t.battle_index&&s.enemy_index===t.enemy_index)?.name);
  if(invalidName!==-1){customBossEditor.tab='appearance';customBossEditor.target=invalidName;updateCustomBossScope();customBossError('部位名称须为1至80字，不支持英文逗号、引号、标签或换行');$(`#cb-${invalidName}-name`).focus();return}
  if(b.bp_use_half>b.bp_use){customBossError('组队体力不能超过单人体力');return}
  const incomplete=b.targets.findIndex(t=>t.actions?.some(a=>!a.skill_id||!a.function_id));
  if(incomplete!==-1){customBossEditor.tab='actions';customBossEditor.target=incomplete;updateCustomBossScope();customBossError('请为每条行动选择有效招式');const j=b.targets[incomplete].actions.findIndex(a=>!a.skill_id||!a.function_id);$(`#ca-${incomplete}-${j}-skill`).focus();return}
  customBossEditor.busy=true;customBossError();customBossControls();
  const body={expected_revision:customBossEditor.revision,cover_artwork:b.cover_artwork||'',artwork_layout:b.artwork_layout||'auto',...Object.fromEntries(['boss_id','name','difficulty','bp_use','bp_use_half','enabled','continue','targets'].map(k=>[k,b[k]]))};
  try{
    const data=await api('/api/custom-bosses',{method:'PUT',body:JSON.stringify(body)});
    customBossEditor.revision=data.revision;customBossEditor.bosses=customBossEditor.bosses.map(row=>row.boss_id===data.boss.boss_id?data.boss:row);customBossEditor.appearanceCurrent[data.boss.boss_id]=data.appearance_current;selectCustomBoss(data.boss.boss_id,true);
    toast(data.appearance_current===false?'配置已保存。图片和部位名需生成并应用外观更新包。':'Boss 配置已保存，后续新开房使用新数值');await refreshCustomBossCatalogs();
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

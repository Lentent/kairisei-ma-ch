'use strict';
const customCardEditor={config:{cards:[]},revision:0,nextID:98000001,selected:0,dirty:false,rules:{},help:{},targetLabels:{},valueLabels:{},applied:{},occupied:[],presentationNames:{},effect:null};
function loadCustomCardHelp(d){customCardEditor.rules=d.parameter_rules||{};customCardEditor.help=d.effect_help||{};customCardEditor.targetLabels=d.target_labels||{};customCardEditor.valueLabels=d.value_labels||{};if(d.presentation_names)customCardEditor.presentationNames=d.presentation_names;customCardEditor.templateDialogues={...customCardEditor.templateDialogues,...d.template_dialogues}}
function customEffectHelp(code){return customCardEditor.help[code]||{name:code,description:'此效果尚未配置中文说明，请结合来源卡技能说明。',parameters:[]}}
function customTargetLabel(target){return customCardEditor.targetLabels[target]||{SELECT:'沿用技能目标',SELF:'自身',USER_ONE:'己方单体',USER_ALL:'己方全体',FRIEND_ALL:'己方全体',ENEMY_ONE:'敌方单体',ENEMY_ALL:'敌方全体'}[target]||target||'沿用模板目标'}
function customTargetOptionsHTML(target,role=false,attack=false){
  const options=attack?['ENEMY_ONE','ENEMY_ALL']:['SELF','USER_ONE','USER_ALL','ENEMY_ONE','ENEMY_ALL'];
  if(role)options.unshift('SELECT');
  if(target==='FRIEND_ALL'&&options.includes('USER_ALL'))options[options.indexOf('USER_ALL')]='FRIEND_ALL';
  if(target&&!options.includes(target))options.unshift(target);
  return options.map(value=>`<option value="${esc(value)}" ${value===target?'selected':''}>${esc(customTargetLabel(value))}</option>`).join('');
}
function changeCustomSkillTarget(index,target){
  const c=currentCustomCard(),root=c.skills[index][0],functions=new Set();
  c.skills.filter(skill=>skill[0]===root).forEach(skill=>{skill[19]=target;functions.add(Number(skill[49])||Number(skill[0]))});
  const reset=(c.action_sources||[]).some(source=>functions.has(Number(source.function_id)));
  if(reset){c.action_sources=c.action_sources.filter(source=>!functions.has(Number(source.function_id)));toast('目标已修改，技能动作已恢复模板；可重新选择匹配范围的动作')}
  customCardChanged();renderCustomSkills();
}
function customEffectTargetLabel(card,role,functionID=Number(role[0])){
  let label=customTargetLabel(role[9]);
  if(role[9]==='SELECT'){
    const branches=card.skills.map((skill,index)=>({skill,index})).filter(({skill})=>(Number(skill[49])||Number(skill[0]))===functionID);
    const targets=[...new Set(branches.map(({skill})=>skill[19]))];
    label=targets.length===1&&targets[0]&&targets[0]!=='SELECT'?`${customTargetLabel(targets[0])}（沿用技能目标）`:branches.length?`${branches.map(({skill,index})=>`分支 ${index+1}：${customTargetLabel(skill[19])}`).join('；')}（沿用技能目标）`:'沿用技能目标（待确认）';
  }
  return label+(Number(role[10])===1?' · 排除自身':'');
}
function customValueLabel(value){return customCardEditor.valueLabels[value]||value||'空'}
function customParameterHelp(role,p){return customEffectHelp(role[8]).parameters[p]||{name:`参数${p+1}（待确认）`,description:'尚未确认用途与单位，建议保留模板值。'}}
function customParameterValueText(value,unit){const n=Number(value);if(value===''||!Number.isFinite(n))return customValueLabel(value);const suffix={percent:'%',permille:'‰',permyriad:'‱',turn:'回合',count:'次／张',point:'',scaled_growth:'',flag:''}[unit]??'';if(unit==='permille')return `${n}‰ = ${n/10}%`;if(unit==='permyriad')return `${n}‱ = ${n/100}%`;if(unit==='flag')return n===0?'0：关闭／排除':'非0：开启／包含';return `${n}${suffix}`}
function customEffectParameterHTML(role,type,p,i){
  if(!type)return '';const h=customParameterHelp(role,p),value=role[20+p]??'',id=`custom-effect-${i}-${p}`;
  return `<div class="field custom-effect-param"><label ${type==='VALUE'?`for="${id}"`:''}>${esc(h.name)}${h.unit?` <span class="hint">${esc({percent:'百分比',permille:'千分比',permyriad:'万分比',turn:'回合',count:'数量',point:'数值',scaled_growth:'成长系数',flag:'开关'}[h.unit]||'')}</span>`:''}</label>${type==='VALUE'?`<input id="${id}" type="number" min="-100000000" max="100000000" value="${esc(value)}" data-custom-param="${i}:${20+p}" aria-describedby="${id}-help ${id}-value"><span class="hint custom-effect-current" id="${id}-value">当前：${esc(customParameterValueText(value,h.unit))}</span>`:`<div class="custom-effect-fixed">${esc(customValueLabel(value))}<span class="hint"> · 沿用来源卡</span></div>`}<p class="hint" id="${id}-help">${esc(h.description)}</p></div>`;
}
function customSkillPlaceholderOptions(c,skillIndex){
  const root=c.skills[skillIndex][0],options=[];
  c.skills.map((skill,index)=>({skill,index})).filter(x=>x.skill[0]===root).slice(0,5).forEach(({skill,index},branch)=>{
    const fn=Number(skill[49])||Number(skill[0]),group=[...new Set(c.skills.map(r=>Number(r[49])||Number(r[0])))].indexOf(fn)+1;
    c.roles.map((role,index)=>({role,index})).filter(x=>Number(x.role[0])===fn).slice(0,5).forEach(({role,index:roleIndex},effect)=>{
      const h=customEffectHelp(role[8]);
      for(let slot=0;slot<10;slot++){const info=h.description_slots?.[slot];options.push({number:branch*50+effect*10+slot,info,role,roleIndex,branch:index+1,group,effect:effect+1,effectName:h.name})}
    });
  });return options;
}
function customPlaceholderDetail(option){
  if(!option)return '当前技能没有对应的展示位置，可能是占位符写错或对应效果已移除。';
  const source=`技能分支 ${option.branch} → 效果组 ${option.group} → 第 ${option.effect} 项「${option.effectName}」`;
  if(!option.info)return `${source}：此展示位置尚未确认用途，可能为未使用字段；请勿把编号当成原始参数序号。`;
  const params=(option.info.parameters||[]).map(p=>{const h=customParameterHelp(option.role,p);return `${h.name}=${customParameterValueText(option.role[20+p]??'',h.unit)}`}).join('；');
  return `${source} → ${option.info.name}。${option.info.formula}${params?` 当前参数：${params}。`:''}`;
}
function customDescriptionHelpHTML(c,i){
  const options=customSkillPlaceholderOptions(c,i),used=[...new Set((c.skills[i][3]||'').match(/\{\d+\}/g)||[])],find=token=>options.find(o=>o.number===Number(token.slice(1,-1)));
  const preview=(c.skills[i][3]||'').replace(/\{\d+\}/g,token=>{const o=find(token);return o?.info?`〔${o.info.name}〕`:`〔${token}待确认〕`});
  return `<p class="hint">占位符取客户端计算后的展示值，按分支和效果位置编号；不是第几个参数。等级参与计算，百分比数值后需自行写 %。添加、移除或移动效果后请核对下方对应关系。</p><div class="custom-description-preview"><b>含义预览</b><p>${esc(preview).replace(/&lt;br\s*\/?&gt;/gi,'<br>')}</p><span class="hint">此预览显示含义；游戏中的具体数值由卡牌等级计算，整数换算时会截断小数。</span></div>${used.length?`<ul class="custom-placeholder-used">${used.map(token=>{const o=find(token);return `<li><b>${esc(token)}</b> ${esc(customPlaceholderDetail(o))}${o?` <button type="button" class="secondary sm" data-custom-placeholder-jump="${o.roleIndex}">查看对应效果</button>`:''}</li>`}).join('')}</ul>`:'<p class="hint">这段介绍没有数值占位符，可以在下方选择插入。</p>'}<details class="custom-placeholder-options"><summary>查看可用占位符／点击插入</summary>${options.filter(o=>o.info).map(o=>`<div class="custom-placeholder-option"><button type="button" class="secondary sm" data-custom-placeholder-insert="${i}:${o.number}">插入 {${o.number}}</button><span>${esc(customPlaceholderDetail(o))}</span></div>`).join('')||'<p class="hint">当前效果还没有已确认的展示字段，请保留模板占位符并核对来源介绍。</p>'}</details>`;
}
function renderCustomDescriptionHelp(){
  const c=currentCustomCard();if(!c)return;
  c.skills.forEach((_,i)=>{$(`#custom-skill-desc-help-${i}`).innerHTML=customDescriptionHelpHTML(c,i)});
  $$('[data-custom-placeholder-insert]').forEach(el=>el.onclick=()=>{const [i,n]=el.dataset.customPlaceholderInsert.split(':').map(Number),input=$(`#custom-skill-desc-${i}`),token=`{${n}}`,start=input.selectionStart??input.value.length,end=input.selectionEnd??start;if(input.value.length-(end-start)+token.length>500)return toast('技能说明最多500字',true);input.value=input.value.slice(0,start)+token+input.value.slice(end);c.skills[i][3]=input.value;input.focus();input.setSelectionRange(start+token.length,start+token.length);customCardChanged();renderCustomDescriptionHelp()});
  $$('[data-custom-placeholder-jump]').forEach(el=>el.onclick=()=>{$(`#custom-effect-row-${Number(el.dataset.customPlaceholderJump)}`).scrollIntoView({behavior:'smooth',block:'center'})});
}
const currentCustomCard=()=>customCardEditor.config.cards.find(c=>c.card_id===customCardEditor.selected);
function customCardChanged(){customCardEditor.dirty=true;$('#custom-card-status').textContent='有未保存的修改。保存草稿后可生成资源更新包。';updateDraftIndicators()}
async function loadCustomCards(){
  const d=await api('/api/custom-cards'),e=customCardEditor;
  e.config=d.config;e.revision=d.revision;e.nextID=d.next_card_id;loadCustomCardHelp(d);e.applied=d.applied||{};e.occupied=d.occupied_card_ids||[];e.dirty=false;
  if(!currentCustomCard())e.selected=e.config.cards[0]?.card_id||0;
  renderCustomCards();$('#custom-card-status').textContent='草稿已载入。生成更新包并应用到资源后，在游戏中生效。';
}
function renderCustomCardList(){
  const e=customCardEditor,q=$('#custom-card-search').value.trim();$('#custom-card-count').textContent=`${e.config.cards.length} 张`;
  $('#custom-card-list').innerHTML=e.config.cards.filter(c=>matchesWords(`${c.name} ${c.prefix} ${c.card_id}`,q)).map(c=>`<button type="button" class="custom-card-choice ${c.card_id===e.selected?'selected':''}" data-custom-card="${c.card_id}" aria-pressed="${c.card_id===e.selected}"><b>${esc(c.prefix?`【${c.prefix}】${c.name}`:c.name)}</b><span>${c.card_id} · ${cardJobName(c.arthur_type)} · ${c.cost} COST${e.applied[c.card_id]?' · 已应用资源':''}</span></button>`).join('')||'<p class="empty">暂无卡牌，点击“从现有卡复制”开始。</p>';
  $$('[data-custom-card]').forEach(el=>el.onclick=()=>{e.selected=Number(el.dataset.customCard);renderCustomCards()});
}
function customArtworkURL(c,key='artwork'){const raw=c?.[key];if(!raw)return '';return `data:image/${raw.startsWith('/9j/')?'jpeg':'png'};base64,${raw}`}
function renderCustomCardPreview(){
  const c=currentCustomCard();$('#custom-card-preview').innerHTML=c?image(customArtworkURL(c)||`/assets/card/${c.template_card_id}.webp`,c.name):'<span>卡面预览</span>';
  $('#custom-card-frame-preview').innerHTML=c?image(customArtworkURL(c,'card_artwork')||customArtworkURL(c)||`/assets/card/${c.template_card_id}.webp`,`${c.name}普通卡面`):'<span>普通卡面预览</span>';
  $('#custom-card-frame-preview').className=`custom-card-preview custom-card-frame-preview ${c?.card_artwork_mode==='contain'?'contain':'cover'}`;
  $('#custom-card-frame-fit').value=c?.card_artwork_mode||'cover';
  $('#custom-card-icon-preview').innerHTML=c?image(customArtworkURL(c,'icon_artwork')||customArtworkURL(c)||customArtworkURL(c,'card_artwork')||`/assets/card/${c.template_card_id}.webp`,`${c.name}小图`):'<span>小图预览</span>';
  $('#custom-card-frame-info').textContent=c?.card_artwork?`普通卡面已单独上传 ${(c.card_artwork.length*.75/1024).toFixed(0)} KB。`:c?.artwork?'普通卡面从完整立绘自动生成。':'普通卡面沿用模板。';
  $('#custom-card-art-info').textContent=c?.artwork?`立绘已上传 ${(c.artwork.length*.75/1024).toFixed(0)} KB；保存草稿时一并保存。`:'当前沿用模板立绘。';
  $('#custom-card-icon-info').textContent=c?.icon_artwork?`小图已上传 ${(c.icon_artwork.length*.75/1024).toFixed(0)} KB；保存草稿时一并保存。`:c?.artwork||c?.card_artwork?'小图将从完整立绘或普通卡面自动生成。':'当前沿用模板小图。';
}
function renderCustomCards(){
  const c=currentCustomCard(),e=customCardEditor;renderCustomCardList();$('#custom-card-version').textContent=`草稿 v${e.revision}`;
  $('#custom-card-fields').disabled=!c;$('#custom-card-remove').disabled=!c||!!e.applied[c.card_id];$('#custom-card-remove').title=c&&e.applied[c.card_id]?'已应用的卡牌须保留，避免影响玩家库存':'';
  $('#custom-card-title').textContent=c?`${c.name} · ${c.card_id}`:'选择或新增一张卡';$('#custom-card-template-info').textContent=c?`模板卡 ${c.template_card_id} · 等级、稀有度、语音和条件分支沿用模板`:'';
  for(const [id,key] of [['name','name'],['prefix','prefix'],['job','arthur_type'],['attr','attribute'],['cost','cost']])$('#custom-card-'+id).value=c?.[key]??'';
  renderCustomCardPreview();renderCustomCardDialogue();
  $('#custom-card-cutin-info').textContent=c?.cutin_template_card_id?`出牌特写：${customPresentationName(c.cutin_template_card_id)}`:'出牌特写：沿用模板';
  $('#custom-card-cutin-reset').disabled=!c?.cutin_template_card_id;
  $('#custom-card-stats').innerHTML=c?['hp','attack','magic','mind'].map((key,i)=>`<tr><th>${['HP','物理攻击','魔法攻击','回复量'][i]}</th>${['initial','maximum','love_bonus'].map(group=>`<td><input type="number" min="0" max="10000000" data-custom-stat="${group}:${key}" value="${c[group][key]}" aria-label="${['HP','物理攻击','魔法攻击','回复量'][i]} ${group==='initial'?'初始':group==='maximum'?'满级':'忠诚加成'}"></td>`).join('')}</tr>`).join(''):'';
  $$('[data-custom-stat]').forEach(el=>el.oninput=()=>{const [group,key]=el.dataset.customStat.split(':');c[group][key]=Number(el.value);customCardChanged()});
  renderCustomSkills();updateDraftIndicators();
}
function renderCustomSkills(){
  const c=currentCustomCard();if(!c){$('#custom-card-skills').innerHTML='';return}
  const functions=[...new Set(c.skills.map(r=>Number(r[49])||Number(r[0])))];
  const skillFields=c.skills.map((r,i)=>`<div class="custom-skill-text"><b>技能分支 ${i+1}</b><div class="field"><label for="custom-skill-target-${i}">技能目标</label><select id="custom-skill-target-${i}" data-custom-skill-target="${i}">${customTargetOptionsHTML(r[19],false,r[10]==='ATTACK')}</select><p class="hint">同一技能的条件分支共用目标；普通技能与觉醒技能可分别设置。触发条件沿用模板。</p></div><div class="field"><label for="custom-skill-name-${i}">技能名称</label><input id="custom-skill-name-${i}" maxlength="60" value="${esc(r[1])}" data-custom-skill="${i}:1"></div><div class="field"><label for="custom-skill-subtitle-${i}">技能副名（名称下方的小字）</label><input id="custom-skill-subtitle-${i}" maxlength="60" value="${esc(r[2])}" data-custom-skill="${i}:2" aria-describedby="custom-skill-subtitle-help-${i}"><p class="hint" id="custom-skill-subtitle-help-${i}">最多60字，可留空清除。不能包含英文逗号、双引号或换行。</p></div><div class="field"><label for="custom-skill-desc-${i}">技能介绍（下方可查看占位符含义）</label><input id="custom-skill-desc-${i}" maxlength="500" value="${esc(r[3])}" data-custom-skill="${i}:3" aria-describedby="custom-skill-desc-help-${i}"></div><div id="custom-skill-desc-help-${i}" class="custom-description-help"></div></div>`).join('');
  const groups=functions.map((fn,k)=>{
    const indices=c.roles.map((r,i)=>Number(r[0])===fn?i:-1).filter(i=>i>=0);
    const branches=c.skills.map((r,i)=>(Number(r[49])||Number(r[0]))===fn?i+1:0).filter(Boolean).join('、');
    const action=(c.action_sources||[]).find(s=>Number(s.function_id)===fn);
    return `<div class="custom-effect-group"><div class="toolbar"><h3>效果组 ${k+1}</h3><span class="hint">关联技能分支 ${branches} · ${indices.length}/5 项效果</span><span class="spacer"></span><button type="button" class="secondary sm" data-custom-add-effect="${fn}" ${indices.length>=5?'disabled':''}>从现有卡添加效果</button></div><div class="toolbar"><span class="hint">攻击／技能动作：${action?esc(customPresentationName(action.card_id)):'沿用模板'}</span><span class="spacer"></span><button type="button" class="secondary sm" data-custom-action="${fn}">选择动作来源</button><button type="button" class="secondary sm" data-custom-action-reset="${fn}" ${action?'':'disabled'}>恢复模板动作</button></div>${indices.map((i,j)=>{
      const r=c.roles[i],source=c.role_sources[i],rules=customCardEditor.rules[r[8]]||[],h=customEffectHelp(r[8]);
      return `<div class="custom-effect-row" id="custom-effect-row-${i}"><div class="toolbar"><b>${j+1}. ${esc(h.name)}</b><span class="hint">来源卡 ${source.card_id}</span><span class="spacer"></span><button type="button" class="secondary sm" data-custom-move="${i}:-1" ${j===0?'disabled':''} aria-label="上移效果">↑</button><button type="button" class="secondary sm" data-custom-move="${i}:1" ${j===indices.length-1?'disabled':''} aria-label="下移效果">↓</button><button type="button" class="danger sm" data-custom-remove-effect="${i}" ${indices.length<2?'disabled':''}>移除</button></div><div class="field"><label for="custom-effect-target-${i}">效果目标</label><select id="custom-effect-target-${i}" data-custom-effect-target="${i}">${customTargetOptionsHTML(r[9],true,r[8]==='ATTACK_AA')}</select></div><p class="custom-effect-target"><b>目标范围：</b>${esc(customEffectTargetLabel(c,r))}</p><p class="custom-effect-explanation">${esc(h.description)}</p>${h.formula?`<p class="hint custom-effect-formula">${esc(h.formula)}</p>`:''}<div class="custom-effect-params">${rules.map((type,p)=>customEffectParameterHTML(r,type,p,i)).join('')}</div><details class="custom-effect-code"><summary>查看效果编号</summary><span class="hint">${esc(r[8])} · 参数顺序与来源模板一致</span></details></div>`;
    }).join('')}</div>`;
  }).join('');
  $('#custom-card-skills').innerHTML=skillFields+groups;
  renderCustomDescriptionHelp();
  $$('[data-custom-skill-target]').forEach(el=>el.onchange=()=>changeCustomSkillTarget(Number(el.dataset.customSkillTarget),el.value));
  $$('[data-custom-effect-target]').forEach(el=>el.onchange=()=>{c.roles[Number(el.dataset.customEffectTarget)][9]=el.value;customCardChanged();renderCustomSkills()});
  $$('[data-custom-skill]').forEach(el=>el.oninput=()=>{const [i,p]=el.dataset.customSkill.split(':').map(Number);c.skills[i][p]=el.value;customCardChanged();if(p===3)renderCustomDescriptionHelp()});
  $$('[data-custom-param]').forEach(el=>el.oninput=()=>{const [i,p]=el.dataset.customParam.split(':').map(Number);c.roles[i][p]=el.value;const h=customParameterHelp(c.roles[i],p-20);$(`#custom-effect-${i}-${p-20}-value`).textContent=`当前：${customParameterValueText(el.value,h.unit)}`;customCardChanged();renderCustomDescriptionHelp()});
  $$('[data-custom-remove-effect]').forEach(el=>el.onclick=()=>{const i=Number(el.dataset.customRemoveEffect);c.roles.splice(i,1);c.role_sources.splice(i,1);customCardChanged();renderCustomSkills()});
  $$('[data-custom-move]').forEach(el=>el.onclick=()=>{const [i,dir]=el.dataset.customMove.split(':').map(Number),same=c.roles.map((r,k)=>r[0]===c.roles[i][0]?k:-1).filter(k=>k>=0),other=same[same.indexOf(i)+dir];if(other===undefined)return;[c.roles[i],c.roles[other]]=[c.roles[other],c.roles[i]];[c.role_sources[i],c.role_sources[other]]=[c.role_sources[other],c.role_sources[i]];customCardChanged();renderCustomSkills()});
  $$('[data-custom-add-effect]').forEach(el=>el.onclick=()=>chooseCustomEffect(Number(el.dataset.customAddEffect)));
  $$('[data-custom-action]').forEach(el=>el.onclick=()=>chooseCustomAction(Number(el.dataset.customAction)));
  $$('[data-custom-action-reset]').forEach(el=>el.onclick=()=>{c.action_sources=(c.action_sources||[]).filter(s=>Number(s.function_id)!==Number(el.dataset.customActionReset));customCardChanged();renderCustomSkills()});
}
function renderCustomCardDialogue(){
  const c=currentCustomCard(),defaults=customCardEditor.templateDialogues?.[c?.template_card_id];
  for(const kind of ['attack','support']){
    const key=`${kind}_dialogue`,input=$(`#custom-card-${kind}-dialogue`);
    input.value=c?.[key]??defaults?.[kind]??'';
    input.placeholder=defaults?'可留空清除台词':'沿用模板台词；输入后替换';
    $(`#custom-card-${kind}-dialogue-reset`).disabled=c?.[key]==null;
    $(`#custom-card-${kind}-dialogue-state`).textContent=c?.[key]==null?'沿用模板':c[key]===''?'已清除台词':'使用自定义台词';
  }
}
for(const kind of ['attack','support']){
  const key=`${kind}_dialogue`;
  $(`#custom-card-${kind}-dialogue`).oninput=()=>{const c=currentCustomCard();if(c){c[key]=$(`#custom-card-${kind}-dialogue`).value;customCardChanged();$(`#custom-card-${kind}-dialogue-reset`).disabled=false;$(`#custom-card-${kind}-dialogue-state`).textContent=c[key]===''?'已清除台词':'使用自定义台词'}};
  $(`#custom-card-${kind}-dialogue-reset`).onclick=()=>{const c=currentCustomCard();if(c){delete c[key];customCardChanged();renderCustomCardDialogue()}};
}
function customPresentationName(id){return customCardEditor.presentationNames[id]||`卡牌 ${id}`}
$('#custom-card-cutin-pick').onclick=()=>{
  const c=currentCustomCard();if(!c)return;
  openContentPicker(rows=>{if(rows.length!==1)throw new Error('一次选择一张特写来源卡');customCardAction(async()=>{
    const d=await api(`/api/custom-cards/template/${rows[0].reward_type_id}`);
    c.cutin_template_card_id=d.card.template_card_id;customCardEditor.presentationNames[d.card.template_card_id]=d.card.name;customCardChanged();renderCustomCards();
  })},['card']);
};
$('#custom-card-cutin-reset').onclick=()=>{const c=currentCustomCard();if(c){delete c.cutin_template_card_id;customCardChanged();renderCustomCards()}};
function chooseCustomAction(fn){
  const c=currentCustomCard(),to=c.skills.find(r=>(Number(r[49])||Number(r[0]))===fn);
  openContentPicker(rows=>{if(rows.length!==1)throw new Error('一次选择一张动作来源卡');customCardAction(async()=>{
    const d=await api(`/api/custom-cards/template/${rows[0].reward_type_id}`),source=d.card;
    customCardEditor.presentationNames[source.template_card_id]=source.name;
    const groups=[...new Set(source.roles.map(r=>Number(r[0])))].map(sourceFn=>({fn:sourceFn,row:source.roles.find(r=>Number(r[0])===sourceFn),skill:source.skills.find(r=>(Number(r[49])||Number(r[0]))===sourceFn)})).filter(g=>g.skill&&g.skill[10]===to[10]&&g.skill[19]===to[19]&&g.row[1]&&g.row[3]);
    $('#custom-effect-title').textContent='选择攻击／技能动作';
    $('#custom-effect-source').textContent=`来源：${source.name}。选择与当前技能类型、目标范围匹配的动作，2D演出、3D动作和命中特效一起切换。`;
    $('#custom-effect-options').innerHTML=groups.map((g,i)=>`<button class="custom-card-choice" type="button" data-custom-action-option="${g.fn}"><b>${esc(g.skill[1])} · 动作组 ${i+1}</b><span>${esc(g.skill[3])}</span></button>`).join('')||'<p class="empty">这张卡没有匹配的完整动作，请关闭后选择另一张来源卡。</p>';
    $$('[data-custom-action-option]').forEach(el=>el.onclick=()=>{c.action_sources=(c.action_sources||[]).filter(s=>Number(s.function_id)!==fn);c.action_sources.push({function_id:fn,card_id:source.template_card_id,source_function_id:Number(el.dataset.customActionOption)});customCardChanged();renderCustomSkills();$('#custom-effect-modal').classList.remove('open')});
    $('#custom-effect-modal').classList.add('open');
  })},['card']);
}
async function customCardAction(fn){if(policyBusy('custom-cards'))return;state.publishing.add('custom-cards');updatePolicyControls();try{await fn()}catch(e){reportError('custom-cards',e,'操作');toast(e.message,true)}finally{state.publishing.delete('custom-cards');updatePolicyControls()}}
function nextCustomCardID(used){for(let id=98000001;id<=98999999;id++)if(!used.has(id))return id;throw new Error('卡牌ID已用完')}
function customCardUsedIDs(){const e=customCardEditor;return new Set([...e.occupied,...Object.keys(e.applied).map(Number),...e.config.cards.map(c=>c.card_id)])}
function validateCustomCardDesign(d){
  const object=v=>v!==null&&typeof v==='object'&&!Array.isArray(v),integer=(v,min,max)=>Number.isSafeInteger(v)&&v>=min&&v<=max;
  const stats=v=>object(v)&&['hp','attack','magic','mind'].every(k=>integer(v[k],0,10000000));
  const rows=(v,min,max,width)=>Array.isArray(v)&&v.length>=min&&v.length<=max&&v.every(r=>Array.isArray(r)&&r.length>=width&&r.every(p=>typeof p==='string'));
  if(!object(d)||!Array.isArray(d.cards)||d.cards.length>200)throw new Error('设计文件须包含有效cards数组，最多200张');
  const ids=new Set(),e=customCardEditor,occupied=new Set(e.occupied);
  for(const c of d.cards){
    if(!object(c)||!integer(c.card_id,98000001,98999999)||!integer(c.template_card_id,1,2147483647)||typeof c.name!=='string'||!c.name.trim()||typeof c.prefix!=='string'||!integer(c.cost,1,10)||!integer(c.arthur_type,0,4)||!['FIRE','ICE','WIND','LIGHT','DARK'].includes(c.attribute)||!stats(c.initial)||!stats(c.maximum)||!stats(c.love_bonus)||!rows(c.skills,1,40,50)||!rows(c.roles,1,120,32)||!Array.isArray(c.role_sources)||c.role_sources.length!==c.roles.length||c.role_sources.some(s=>!object(s)||!integer(s.card_id,1,2147483647)||!integer(s.index,0,119))||['artwork','icon_artwork','card_artwork'].some(key=>c[key]!==undefined&&typeof c[key]!=='string'))throw new Error('设计文件的卡牌字段、四维或技能结构不完整');
    if(c.card_artwork_mode!==undefined&&!['','cover','contain'].includes(c.card_artwork_mode))throw new Error('普通卡面构图须为铺满卡框或完整显示');
    for(const key of ['attack_dialogue','support_dialogue'])if(c[key]!=null&&(typeof c[key]!=='string'||Array.from(c[key]).length>100||/[\r\n\x00,"]/.test(c[key])||c[key]!==''&&!c[key].trim()))throw new Error('出牌台词最多100字，可留空，不能包含英文逗号、双引号或换行');
    if(ids.has(c.card_id))throw new Error('设计文件包含重复的卡牌ID');
    if(c.cutin_template_card_id!==undefined&&!integer(c.cutin_template_card_id,0,2147483647)||c.action_sources!=null&&(!Array.isArray(c.action_sources)||c.action_sources.some(s=>!object(s)||!integer(s.function_id,1,2147483647)||!integer(s.card_id,1,2147483647)||!integer(s.source_function_id,1,2147483647))))throw new Error('设计文件的演出来源结构无效');
    if(occupied.has(c.card_id)&&e.applied[c.card_id]!==c.template_card_id)throw new Error('设计文件不能覆盖当前资源中的其他卡牌');
    ids.add(c.card_id);
  }
  for(const [id,template] of Object.entries(e.applied))if(!d.cards.some(c=>c.card_id===Number(id)&&c.template_card_id===template))throw new Error(`已发布的卡牌${id}须保留原模板，不能从设计文件中移除`);
}
$('#custom-card-new').onclick=()=>openContentPicker(rows=>{if(customCardEditor.config.cards.length+rows.length>200)throw new Error('最多制作200张卡');customCardAction(async()=>{const cards=[],used=customCardUsedIDs();for(const row of rows){const d=await api(`/api/custom-cards/template/${row.reward_type_id}`),c=d.card;loadCustomCardHelp(d);c.card_id=nextCustomCardID(used);used.add(c.card_id);c.name=`${c.name}（自制）`.slice(0,40);cards.push(c)}customCardEditor.config.cards.push(...cards);if(cards.length)customCardEditor.selected=cards[cards.length-1].card_id;customCardChanged();renderCustomCards()})},['card']);
function chooseCustomEffect(fn){const c=currentCustomCard();openContentPicker(rows=>{if(rows.length!==1)throw new Error('一次选择一张效果来源卡');customCardAction(async()=>{const d=await api(`/api/custom-cards/template/${rows[0].reward_type_id}`);loadCustomCardHelp(d);customCardEditor.effect={cardID:c.card_id,fn,source:d.card};$('#custom-effect-source').textContent=`来源：${d.card.name}。选择一项效果加入当前效果组。`;
  $('#custom-effect-title').textContent='选择技能效果';
  $('#custom-effect-options').innerHTML=d.card.roles.map((r,i)=>{const h=customEffectHelp(r[8]),rules=customCardEditor.rules[r[8]]||[],params=rules.map((type,p)=>type==='VALUE'&&h.parameters[p]?.name!=='模板保留字段'?`${customParameterHelp(r,p).name}：${customParameterValueText(r[20+p]??'',customParameterHelp(r,p).unit)}`:'').filter(Boolean).join(' · ');return `<button class="custom-card-choice" type="button" data-custom-effect-option="${i}"><b>${esc(h.name)}</b><span class="custom-effect-target"><b>目标范围：</b>${esc(customEffectTargetLabel(c,r,fn))}</span><span>${esc(h.description)}</span><span>${esc(params)}</span><span>来源技能：${esc(d.card.skills.find(s=>(Number(s[49])||Number(s[0]))===Number(r[0]))?.[3]||'沿用来源卡说明')}</span></button>`}).join('');
  $$('[data-custom-effect-option]').forEach(el=>el.onclick=()=>{const e=customCardEditor.effect,target=customCardEditor.config.cards.find(c=>c.card_id===e.cardID);if(!target)return;if(target.roles.filter(r=>Number(r[0])===Number(e.fn)).length>=5)return toast('每个效果组最多5项效果，请先移除该组的效果',true);const index=Number(el.dataset.customEffectOption),r=[...e.source.roles[index]];r[0]=String(e.fn);target.roles.push(r);target.role_sources.push({card_id:e.source.template_card_id,index});customCardChanged();renderCustomSkills();$('#custom-effect-modal').classList.remove('open')});$('#custom-effect-modal').classList.add('open')})},['card'])}
$('#custom-effect-close').onclick=()=>$('#custom-effect-modal').classList.remove('open');
$('#custom-card-search').oninput=renderCustomCardList;
for(const [id,key,numeric] of [['name','name',false],['prefix','prefix',false],['job','arthur_type',true],['attr','attribute',false],['cost','cost',true]])$('#custom-card-'+id).oninput=()=>{const c=currentCustomCard();if(!c)return;c[key]=numeric?Number($('#custom-card-'+id).value):$('#custom-card-'+id).value;customCardChanged();renderCustomCardList();$('#custom-card-title').textContent=`${c.name} · ${c.card_id}`};
for(const [id,key] of [['art','artwork'],['icon','icon_artwork'],['frame-art','card_artwork']])$('#custom-card-'+id).onchange=()=>customCardAction(async()=>{const c=currentCustomCard(),input=$('#custom-card-'+id),file=input.files[0];if(!c||!file)return;try{if(!['image/png','image/jpeg'].includes(file.type)||file.size>4*1024*1024)throw new Error('请选择4MB以内的PNG/JPEG');const data=await new Promise((resolve,reject)=>{const reader=new FileReader();reader.onload=()=>resolve(reader.result);reader.onerror=()=>reject(new Error('读取图片失败'));reader.readAsDataURL(file)});c[key]=data.split(',')[1];customCardChanged();renderCustomCardPreview()}finally{input.value=''}});
$('#custom-card-frame-reset').onclick=()=>{const c=currentCustomCard();if(c){delete c.card_artwork;customCardChanged();renderCustomCardPreview()}};
$('#custom-card-frame-fit').onchange=()=>{const c=currentCustomCard();if(c){c.card_artwork_mode=$('#custom-card-frame-fit').value;customCardChanged();renderCustomCardPreview()}};
$('#custom-card-art-reset').onclick=()=>{const c=currentCustomCard();if(c){delete c.artwork;customCardChanged();renderCustomCardPreview()}};
$('#custom-card-icon-reset').onclick=()=>{const c=currentCustomCard();if(c){delete c.icon_artwork;customCardChanged();renderCustomCardPreview()}};
$('#custom-card-remove').onclick=()=>{const c=currentCustomCard();if(!c||customCardEditor.applied[c.card_id])return;if(!confirm(`移除“${c.name}”的未发布草稿？`))return;customCardEditor.config.cards=customCardEditor.config.cards.filter(v=>v.card_id!==c.card_id);customCardEditor.selected=customCardEditor.config.cards[0]?.card_id||0;customCardChanged();renderCustomCards()};
$('#custom-card-save').onclick=()=>customCardAction(async()=>{const e=customCardEditor,d=await api('/api/custom-cards',{method:'PUT',body:JSON.stringify({expected_revision:e.revision,config:e.config})});e.config=d.config;e.revision=d.revision;e.nextID=d.next_card_id;e.applied=d.applied||{};e.occupied=d.occupied_card_ids||[];loadCustomCardHelp(d);e.dirty=false;clearViewAlert('custom-cards');renderCustomCards();$('#custom-card-status').textContent='草稿已保存；可生成资源更新包，应用后在游戏中生效。';toast('自制卡牌草稿已保存')});
$('#custom-card-package').onclick=()=>customCardAction(async()=>{const e=customCardEditor;if(e.dirty)throw new Error('请先保存草稿');const r=await fetch('/api/custom-cards/export',{method:'POST',headers:{'Content-Type':'application/json','X-Kairisei-Admin-Action':'apply'},body:JSON.stringify({expected_revision:e.revision})});if(!r.ok){const d=await r.json();const error=new Error(d.error||'生成失败');error.status=r.status;throw error}const blob=await r.blob(),url=URL.createObjectURL(blob),a=document.createElement('a');a.href=url;a.download=`自制卡牌资源-v${e.revision}.zip`;a.click();setTimeout(()=>URL.revokeObjectURL(url),60000);$('#custom-card-status').textContent='更新包已生成。按包内说明停服备份、覆盖资源、重启；使用CDN时同步资源。应用后再发卡。';toast('资源更新包已生成')});
$('#custom-card-json').onclick=()=>downloadContent(customCardEditor.config,'自制卡牌设计.json');
$('#custom-card-import').onchange=()=>customCardAction(async()=>{const d=await importContent($('#custom-card-import'),32*1024*1024);if(d===null)return;validateCustomCardDesign(d);if(!confirm('导入将替换当前页面草稿，继续？'))return;customCardEditor.config=d;customCardEditor.selected=d.cards[0]?.card_id||0;customCardChanged();renderCustomCards()});

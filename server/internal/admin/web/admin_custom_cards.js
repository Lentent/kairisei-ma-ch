'use strict';
const customCardEditor={config:{cards:[]},revision:0,nextID:98000001,selected:0,dirty:false,rules:{},applied:{},effect:null};
const currentCustomCard=()=>customCardEditor.config.cards.find(c=>c.card_id===customCardEditor.selected);
function customCardChanged(){customCardEditor.dirty=true;$('#custom-card-status').textContent='有未保存的修改。保存草稿后可生成资源更新包。';updateDraftIndicators()}
async function loadCustomCards(){
  const d=await api('/api/custom-cards'),e=customCardEditor;
  e.config=d.config;e.revision=d.revision;e.nextID=d.next_card_id;e.rules=d.parameter_rules;e.applied=d.applied||{};e.dirty=false;
  if(!currentCustomCard())e.selected=e.config.cards[0]?.card_id||0;
  renderCustomCards();$('#custom-card-status').textContent='草稿已载入。生成更新包并应用到资源后，在游戏中生效。';
}
function renderCustomCardList(){
  const e=customCardEditor,q=$('#custom-card-search').value.trim();$('#custom-card-count').textContent=`${e.config.cards.length} 张`;
  $('#custom-card-list').innerHTML=e.config.cards.filter(c=>matchesWords(`${c.name} ${c.prefix} ${c.card_id}`,q)).map(c=>`<button type="button" class="custom-card-choice ${c.card_id===e.selected?'selected':''}" data-custom-card="${c.card_id}" aria-pressed="${c.card_id===e.selected}"><b>${esc(c.prefix?`【${c.prefix}】${c.name}`:c.name)}</b><span>${c.card_id} · ${cardJobName(c.arthur_type)} · ${c.cost} COST${e.applied[c.card_id]?' · 已应用资源':''}</span></button>`).join('')||'<p class="empty">暂无卡牌，点击“从现有卡复制”开始。</p>';
  $$('[data-custom-card]').forEach(el=>el.onclick=()=>{e.selected=Number(el.dataset.customCard);renderCustomCards()});
}
function customArtworkURL(c){if(!c?.artwork)return '';return `data:image/${c.artwork.startsWith('/9j/')?'jpeg':'png'};base64,${c.artwork}`}
function renderCustomCardPreview(){
  const c=currentCustomCard();$('#custom-card-preview').innerHTML=c?image(customArtworkURL(c)||`/assets/card/${c.template_card_id}.webp`,c.name):'<span>卡面预览</span>';
  $('#custom-card-art-info').textContent=c?.artwork?`已上传 ${(c.artwork.length*.75/1024).toFixed(0)} KB；保存草稿时一并保存。`:'当前沿用模板卡面。';
}
function renderCustomCards(){
  const c=currentCustomCard(),e=customCardEditor;renderCustomCardList();$('#custom-card-version').textContent=`草稿 v${e.revision}`;
  $('#custom-card-fields').disabled=!c;$('#custom-card-remove').disabled=!c||!!e.applied[c.card_id];$('#custom-card-remove').title=c&&e.applied[c.card_id]?'已应用的卡牌须保留，避免影响玩家库存':'';
  $('#custom-card-title').textContent=c?`${c.name} · ${c.card_id}`:'选择或新增一张卡';$('#custom-card-template-info').textContent=c?`模板卡 ${c.template_card_id} · 等级、稀有度、语音和条件分支沿用模板`:'';
  for(const [id,key] of [['name','name'],['prefix','prefix'],['job','arthur_type'],['attr','attribute'],['cost','cost']])$('#custom-card-'+id).value=c?.[key]??'';
  renderCustomCardPreview();
  $('#custom-card-stats').innerHTML=c?['hp','attack','magic','mind'].map((key,i)=>`<tr><th>${['HP','物理攻击','魔法攻击','回复量'][i]}</th>${['initial','maximum','love_bonus'].map(group=>`<td><input type="number" min="0" max="10000000" data-custom-stat="${group}:${key}" value="${c[group][key]}" aria-label="${['HP','物理攻击','魔法攻击','回复量'][i]} ${group==='initial'?'初始':group==='maximum'?'满级':'忠诚加成'}"></td>`).join('')}</tr>`).join(''):'';
  $$('[data-custom-stat]').forEach(el=>el.oninput=()=>{const [group,key]=el.dataset.customStat.split(':');c[group][key]=Number(el.value);customCardChanged()});
  renderCustomSkills();updateDraftIndicators();
}
function renderCustomSkills(){
  const c=currentCustomCard();if(!c){$('#custom-card-skills').innerHTML='';return}
  const functions=[...new Set(c.skills.map(r=>Number(r[49])||Number(r[0])))];
  const skillFields=c.skills.map((r,i)=>`<div class="custom-skill-text"><b>技能分支 ${i+1}</b><div class="field"><label for="custom-skill-name-${i}">技能名称</label><input id="custom-skill-name-${i}" maxlength="60" value="${esc(r[1])}" data-custom-skill="${i}:1"></div><div class="field"><label for="custom-skill-desc-${i}">说明（保留 {1} 等数值占位符）</label><input id="custom-skill-desc-${i}" maxlength="500" value="${esc(r[3])}" data-custom-skill="${i}:3"></div></div>`).join('');
  const groups=functions.map((fn,k)=>{
    const indices=c.roles.map((r,i)=>Number(r[0])===fn?i:-1).filter(i=>i>=0);
    const branches=c.skills.map((r,i)=>(Number(r[49])||Number(r[0]))===fn?i+1:0).filter(Boolean).join('、');
    return `<div class="custom-effect-group"><div class="toolbar"><h3>效果组 ${k+1}</h3><span class="hint">关联技能分支 ${branches} · ${indices.length}/5 项效果</span><span class="spacer"></span><button type="button" class="secondary sm" data-custom-add-effect="${fn}" ${indices.length>=5?'disabled':''}>从现有卡添加效果</button></div>${indices.map((i,j)=>{
      const r=c.roles[i],source=c.role_sources[i],rules=customCardEditor.rules[r[8]]||[];
      return `<div class="custom-effect-row"><div class="toolbar"><b>${esc(r[8])}</b><span class="hint">来源卡 ${source.card_id} · 目标 ${esc(r[9])}</span><span class="spacer"></span><button type="button" class="secondary sm" data-custom-move="${i}:-1" ${j===0?'disabled':''} aria-label="上移效果">↑</button><button type="button" class="secondary sm" data-custom-move="${i}:1" ${j===indices.length-1?'disabled':''} aria-label="下移效果">↓</button><button type="button" class="danger sm" data-custom-remove-effect="${i}" ${indices.length<2?'disabled':''}>移除</button></div><div class="custom-effect-params">${rules.map((type,p)=>type==='VALUE'?`<div class="field"><label for="custom-effect-${i}-${p}">参数 ${p+1}</label><input id="custom-effect-${i}-${p}" type="number" min="-100000000" max="100000000" value="${esc(r[20+p])}" data-custom-param="${i}:${20+p}"></div>`:r[20+p]?`<span class="hint">参数 ${p+1}：${esc(r[20+p])}</span>`:'').join('')}</div></div>`;
    }).join('')}</div>`;
  }).join('');
  $('#custom-card-skills').innerHTML=skillFields+groups;
  $$('[data-custom-skill]').forEach(el=>el.oninput=()=>{const [i,p]=el.dataset.customSkill.split(':').map(Number);c.skills[i][p]=el.value;customCardChanged()});
  $$('[data-custom-param]').forEach(el=>el.oninput=()=>{const [i,p]=el.dataset.customParam.split(':').map(Number);c.roles[i][p]=el.value;customCardChanged()});
  $$('[data-custom-remove-effect]').forEach(el=>el.onclick=()=>{const i=Number(el.dataset.customRemoveEffect);c.roles.splice(i,1);c.role_sources.splice(i,1);customCardChanged();renderCustomSkills()});
  $$('[data-custom-move]').forEach(el=>el.onclick=()=>{const [i,dir]=el.dataset.customMove.split(':').map(Number),same=c.roles.map((r,k)=>r[0]===c.roles[i][0]?k:-1).filter(k=>k>=0),other=same[same.indexOf(i)+dir];if(other===undefined)return;[c.roles[i],c.roles[other]]=[c.roles[other],c.roles[i]];[c.role_sources[i],c.role_sources[other]]=[c.role_sources[other],c.role_sources[i]];customCardChanged();renderCustomSkills()});
  $$('[data-custom-add-effect]').forEach(el=>el.onclick=()=>chooseCustomEffect(Number(el.dataset.customAddEffect)));
}
async function customCardAction(fn){if(policyBusy('custom-cards'))return;state.publishing.add('custom-cards');updatePolicyControls();try{await fn()}catch(e){reportError('custom-cards',e,'操作');toast(e.message,true)}finally{state.publishing.delete('custom-cards');updatePolicyControls()}}
$('#custom-card-new').onclick=()=>openContentPicker(rows=>{if(customCardEditor.config.cards.length+rows.length>200)throw new Error('最多制作200张卡');customCardAction(async()=>{const cards=[];for(const row of rows){const d=await api(`/api/custom-cards/template/${row.reward_type_id}`);customCardEditor.rules=d.parameter_rules;cards.push(d.card)}for(const c of cards){while(customCardEditor.config.cards.some(v=>v.card_id===customCardEditor.nextID)||customCardEditor.applied[customCardEditor.nextID])customCardEditor.nextID++;if(customCardEditor.nextID>98999999)throw new Error('卡牌ID已用完');c.card_id=customCardEditor.nextID++;c.name=`${c.name}（自制）`.slice(0,40);customCardEditor.config.cards.push(c);customCardEditor.selected=c.card_id}customCardChanged();renderCustomCards()})},['card']);
function chooseCustomEffect(fn){const c=currentCustomCard();openContentPicker(rows=>{if(rows.length!==1)throw new Error('一次选择一张效果来源卡');customCardAction(async()=>{const d=await api(`/api/custom-cards/template/${rows[0].reward_type_id}`);customCardEditor.rules=d.parameter_rules;customCardEditor.effect={cardID:c.card_id,fn,source:d.card};$('#custom-effect-source').textContent=`来源：${d.card.name}。选择一项效果加入当前效果组。`;
  $('#custom-effect-options').innerHTML=d.card.roles.map((r,i)=>`<button class="custom-card-choice" type="button" data-custom-effect-option="${i}"><b>${esc(r[8])} · 目标 ${esc(r[9])}</b><span>${esc(d.card.skills.find(s=>(Number(s[49])||Number(s[0]))===Number(r[0]))?.[3]||'沿用来源卡演出')}</span></button>`).join('');
  $$('[data-custom-effect-option]').forEach(el=>el.onclick=()=>{const e=customCardEditor.effect,target=customCardEditor.config.cards.find(c=>c.card_id===e.cardID);if(!target)return;if(target.roles.filter(r=>Number(r[0])===Number(e.fn)).length>=5)return toast('每个效果组最多5项效果，请先移除该组的效果',true);const index=Number(el.dataset.customEffectOption),r=[...e.source.roles[index]];r[0]=String(e.fn);target.roles.push(r);target.role_sources.push({card_id:e.source.template_card_id,index});customCardChanged();renderCustomSkills();$('#custom-effect-modal').classList.remove('open')});$('#custom-effect-modal').classList.add('open')})},['card'])}
$('#custom-effect-close').onclick=()=>$('#custom-effect-modal').classList.remove('open');
$('#custom-card-search').oninput=renderCustomCardList;
for(const [id,key,numeric] of [['name','name',false],['prefix','prefix',false],['job','arthur_type',true],['attr','attribute',false],['cost','cost',true]])$('#custom-card-'+id).oninput=()=>{const c=currentCustomCard();if(!c)return;c[key]=numeric?Number($('#custom-card-'+id).value):$('#custom-card-'+id).value;customCardChanged();renderCustomCardList();$('#custom-card-title').textContent=`${c.name} · ${c.card_id}`};
$('#custom-card-art').onchange=()=>customCardAction(async()=>{const c=currentCustomCard(),file=$('#custom-card-art').files[0];if(!c||!file)return;try{if(!['image/png','image/jpeg'].includes(file.type)||file.size>4*1024*1024)throw new Error('请选择4MB以内的PNG/JPEG');const data=await new Promise((resolve,reject)=>{const reader=new FileReader();reader.onload=()=>resolve(reader.result);reader.onerror=()=>reject(new Error('读取图片失败'));reader.readAsDataURL(file)});c.artwork=data.split(',')[1];customCardChanged();renderCustomCardPreview()}finally{$('#custom-card-art').value=''}});
$('#custom-card-art-reset').onclick=()=>{const c=currentCustomCard();if(c){delete c.artwork;customCardChanged();renderCustomCardPreview()}};
$('#custom-card-remove').onclick=()=>{const c=currentCustomCard();if(!c||customCardEditor.applied[c.card_id])return;if(!confirm(`移除“${c.name}”的未发布草稿？`))return;customCardEditor.config.cards=customCardEditor.config.cards.filter(v=>v.card_id!==c.card_id);customCardEditor.selected=customCardEditor.config.cards[0]?.card_id||0;customCardChanged();renderCustomCards()};
$('#custom-card-save').onclick=()=>customCardAction(async()=>{const e=customCardEditor,d=await api('/api/custom-cards',{method:'PUT',body:JSON.stringify({expected_revision:e.revision,config:e.config})});e.config=d.config;e.revision=d.revision;e.nextID=d.next_card_id;e.applied=d.applied||{};e.rules=d.parameter_rules;e.dirty=false;clearViewAlert('custom-cards');renderCustomCards();$('#custom-card-status').textContent='草稿已保存；可生成资源更新包，应用后在游戏中生效。';toast('自制卡牌草稿已保存')});
$('#custom-card-package').onclick=()=>customCardAction(async()=>{const e=customCardEditor;if(e.dirty)throw new Error('请先保存草稿');const r=await fetch('/api/custom-cards/export',{method:'POST',headers:{'Content-Type':'application/json','X-Kairisei-Admin-Action':'apply'},body:JSON.stringify({expected_revision:e.revision})});if(!r.ok){const d=await r.json();const error=new Error(d.error||'生成失败');error.status=r.status;throw error}const blob=await r.blob(),url=URL.createObjectURL(blob),a=document.createElement('a');a.href=url;a.download=`自制卡牌资源-v${e.revision}.zip`;a.click();setTimeout(()=>URL.revokeObjectURL(url),60000);$('#custom-card-status').textContent='更新包已生成。按包内说明停服备份、覆盖资源、重启；使用CDN时同步资源。应用后再发卡。';toast('资源更新包已生成')});
$('#custom-card-json').onclick=()=>downloadContent(customCardEditor.config,'自制卡牌设计.json');
$('#custom-card-import').onchange=()=>customCardAction(async()=>{const d=await importContent($('#custom-card-import'));if(!d)return;if(!Array.isArray(d.cards)||d.cards.length>200||d.cards.some(c=>!Array.isArray(c.skills)||!Array.isArray(c.roles)||!Array.isArray(c.role_sources)))throw new Error('设计文件须包含有效cards数组');if(!confirm('导入将替换当前页面草稿，继续？'))return;customCardEditor.config=d;customCardEditor.selected=d.cards[0]?.card_id||0;customCardEditor.nextID=Math.max(customCardEditor.nextID,...d.cards.map(c=>c.card_id+1));customCardChanged();renderCustomCards()});

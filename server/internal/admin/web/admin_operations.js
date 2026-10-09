'use strict';
const poolEditor = { rows: [], cards: [], selected: new Map(), fames: new Map(), coverPath: '', customRevision: 0, variants: [], strategies: new Map(), mixedByID: new Map(), mixedVariant: 0, row: null, page: 0, selectedPage: 0, source: '', job: 0, dirty: false, preview: null, editSerial: 0 };
const poolCoverPattern = /^gacha-covers\/[0-9a-f]{64}\.(png|jpg)$/;
const localTimeInput = unix => unix ? new Date(unix*1000-new Date(unix*1000).getTimezoneOffset()*60000).toISOString().slice(0,16) : '';
const unixInput = id => $(id).value ? Math.floor(new Date($(id).value).getTime()/1000) : 0;
const describeTime = unix => unix ? new Date(unix*1000).toLocaleString('zh-CN',{hour12:false}) : '不限';
const paymentName = profile => {
  if(profile.pay_type===4){const item=poolEditor.catalog?.find(c=>c.reward_type===8&&c.reward_type_id===profile.pay_typeid);return item?`${item.name}（道具 ${profile.pay_typeid}）`:`道具 ${profile.pay_typeid}`;}
  return ({2:'友情点',3:'水晶（先免费后付费）',6:'付费水晶'}[profile.pay_type] || `类型 ${profile.pay_type}`);
};

// New operator pools need a first publication even when their draft matches the template.
// Existing pools have a pending draft when it differs from the effective config.
function poolHasDraft(row){return !!row&&row.draft.revision>0&&row.draft.payload!=null;}
function poolPendingDraft(row) { return poolHasDraft(row) && ((row.custom&&!row.live?.revision)||JSON.stringify(row.draft.payload)!==JSON.stringify(row.config)); }
// One atomic publication group, independently editable variants, one uploaded cover.
// poolEditor.row identifies the group; activeID identifies the displayed variant.
const poolProfessionName=t=>({1:'佣兵',2:'富豪',3:'盗贼',4:'歌姬',5:'混合职业'}[t]||'普通');
function poolActiveRow(){return poolEditor.variants.find(v=>v.gacha_id===poolEditor.activeID)||poolEditor.row;}
function poolDrawCount(row){return poolEditor.legacyByID?.get(row.gacha_id)?.card_num||poolSavedConfig(row).card_num||row.base.card_num;}
function poolVariantState(c){return {selected:new Map((c.card_ids||[]).map((id,i)=>[id,c.weights[i]])),fames:new Map(Object.entries(c.card_fames||{}).map(([id,f])=>[Number(id),Number(f)])),filters:{}};}
function poolRememberActive(){
  const s=poolEditor.contents?.get(poolEditor.activeID);if(!s)return;
  s.selected=poolEditor.selected;s.fames=poolEditor.fames;
  s.filters={job:poolEditor.job,source:poolEditor.source};
  for(const key of ['search','rarity','attribute','cost','resource','ready','eligible-only']){const el=$(`#pool-${key}`);if(el)s.filters[key]=el.type==='checkbox'?el.checked:el.value;}
}
function selectPoolVariant(id,remember=true){
  if(remember)poolRememberActive();
  const s=poolEditor.contents.get(id);if(!s)return;
  poolEditor.activeID=id;poolEditor.editSerial++;poolEditor.selected=s.selected;poolEditor.fames=s.fames;
  poolEditor.job=s.filters.job||0;poolEditor.source=s.filters.source||'';
  for(const key of ['search','rarity','attribute','cost','resource','ready','eligible-only']){const el=$(`#pool-${key}`);if(el){if(el.type==='checkbox')el.checked=s.filters[key]??(key==='eligible-only');else el.value=s.filters[key]??(el.tagName==='SELECT'?el.options[0]?.value??'':'');}}
  poolEditor.page=poolEditor.selectedPage=0;renderRarityTool.touched=false;
  setupMixedPool();renderPoolCards();renderSelectedCards();renderPoolCopyTargets();renderPoolSplit();renderVariantBar();
  if(poolEditor.strategies?.size)renderStrategies();
  setupLegacyPool();
}
function renderVariantBar(){
  const row=poolActiveRow(),s=poolEditor.strategies?.get(row?.gacha_id);if(!row)return;
  $('#pool-variant-name').innerHTML=`${poolDrawCount(row)} 抽 · ${esc(poolProfessionName(row.base.arthur_type))} <code>${row.gacha_id}</code>${s?`<span class="sub">${esc(s.name)}${s.open?'':' · 已关闭'}</span>`:''}`;
}
// Card count and draft marker of one variant, shown in its 分池 row.
function poolVariantSummary(v){
  const s=poolEditor.contents?.get(v.gacha_id),m=poolEditor.mixedByID?.get(v.gacha_id);if(!s)return '';
  const box=poolEditor.boxByID?.get(v.gacha_id);
  const count=box?'箱池 · 前 10 轮 + 无限循环':m?`${(m.steps.length?m.steps[0].reward_pool:m.reward_pool).length} 项奖励${m.steps.length?` · ${m.steps.length} 个阶段`:''}`:`${s.selected.size} 张卡牌`;
  const fames=m?0:[...s.fames.keys()].filter(id=>s.selected.has(id)).length;
  return `<b>${count}</b>${poolPendingDraft(v)?' <span class="tag warn">草稿</span>':''}${fames?`<span class="sub">${fames} 张自定义名声</span>`:''}`;
}
function renderPoolCopyTargets(){
  const rows=poolEditor.variants.filter(v=>v.gacha_id!==poolEditor.activeID&&!v.base.reward_pool?.length);
  $('#pool-copy-menu').hidden=!rows.length||!!poolEditor.mixed||!!poolEditor.boxByID?.size;$('#pool-copy-menu').open=false;
  $('#pool-copy-targets').innerHTML=rows.map(v=>`<label><input type="checkbox" value="${v.gacha_id}"> ${esc(poolProfessionName(v.base.arthur_type))} · ${v.base.card_num} 抽 · ${v.gacha_id}</label>`).join('')||'<span class="hint">没有其他普通卡牌分池</span>';
}
// A 混合职业 variant can hand each profession its cards. Targets are the profession variants of the same
// draw method: variants created from one source share its template; built-in pools compare draw count and cost.
const poolDrawKey=v=>v.template_id?`t${v.template_id}`:`n${v.base.card_num}:${v.base.pay_type}:${v.base.pay_typeid||0}`;
function poolSplitTargets(){
  const row=poolActiveRow();
  if(!row||row.base.arthur_type!==5||poolEditor.mixedByID.has(row.gacha_id))return [];
  return poolEditor.variants.filter(v=>v.base.arthur_type>=1&&v.base.arthur_type<=4&&!poolEditor.mixedByID.has(v.gacha_id)&&poolDrawKey(v)===poolDrawKey(row));
}
function poolSplitPlan(){
  const cards=new Map(poolEditor.cards.map(c=>[c.reward_type_id,c])),common=$('#pool-split-common').checked,source=[...poolEditor.selected];
  return poolSplitTargets().map(v=>{
    const job=v.base.arthur_type,own=source.filter(([id])=>cards.get(id)?.arthur_type===job),shared=common?source.filter(([id])=>cards.has(id)&&!cards.get(id).arthur_type):[];
    return {v,job,own:own.length,shared:shared.length,selected:new Map([...own,...shared].filter(([id])=>poolCardEligibleFor(cards.get(id),v)))};
  });
}
// A profession with no matching card is skipped: its variant keeps its current cards, never becomes empty.
const poolSplitLabel=p=>`${poolProfessionName(p.job)} · ${p.v.base.card_num} 抽（${p.v.gacha_id}）`;
function renderPoolSplit(){
  const targets=poolSplitTargets(),menu=$('#pool-split-menu');menu.hidden=!targets.length;if(!targets.length){menu.open=false;return}
  const plan=poolSplitPlan(),covered=new Set(plan.map(p=>p.job)),missing=[...new Set(poolEditor.variants.map(v=>v.base.arthur_type))].filter(j=>j>=1&&j<=4&&!covered.has(j)).sort();
  $('#pool-split-targets').innerHTML=plan.map(p=>{const had=poolEditor.contents.get(p.v.gacha_id)?.selected.size||0,skip=!p.selected.size;
    return `<div class="pool-split-row${skip?' skip':''}"><b>${esc(poolProfessionName(p.job))}</b><span>${p.v.base.card_num} 抽 <code>${p.v.gacha_id}</code>${skip?`<small>跳过，保留原配置${had?`（现有 ${num(had)} 张）`:''}</small>`:had?`<small class="replace">现有 ${num(had)} 张将被替换</small>`:''}</span>`+
      `<span class="pool-split-count">${skip?'无匹配卡牌':`${num(p.selected.size)} 张${p.shared?`<small>本职业 ${num(p.own)} ＋ 通用 ${num(p.shared)}</small>`:''}`}</span></div>`}).join('')+
    (missing.length?`<p class="hint">${missing.map(poolProfessionName).join('、')}没有这种抽法，不分配。</p>`:'');
  $('#pool-split-apply').disabled=!plan.some(p=>p.selected.size);
}
function poolGroupRows(row=poolEditor.row) { return row ? poolEditor.rows.filter(r=>r.base.groupid===row.base.groupid) : []; }
function poolGroupPending(row=poolEditor.row) { return poolGroupRows(row).some(poolPendingDraft); }
function poolPublished() { return !!poolEditor.row && !poolGroupPending()&&poolEditor.variants.every(r=>!r.custom||r.live.revision>0); }
function poolSavedConfig(row) { return poolHasDraft(row) ? row.draft.payload : row.config; }
const poolPayName=(type,id)=>type===4?(poolEditor.catalog?.find(e=>e.reward_type===8&&e.reward_type_id===id)?.name||`道具 ${id}`):({2:'友情点',3:'水晶',6:'付费水晶'}[type]||`类型 ${type}`);
function poolVariantLabel(row) { const c=poolSavedConfig(row); return `${poolPayName(c.pay_type||row.base.pay_type,c.pay_type?c.pay_typeid:row.base.pay_typeid)} ×${num(c.price)} · ${c.card_num||row.base.card_num} 抽${c.closed?'（关闭）':''}`; }
function poolCopyable(row) { return !row.deleted && !row.base.reward_pool?.length && !row.base.steps?.length && !row.base.box_rounds?.length && !row.base.unowned_only; }
function updatePoolControls() {
  const busy=policyBusy('pool-editor')||!poolEditor.row;
  const deleted=!!poolEditor.row?.deleted;
  $('#pool-save-preview').disabled=busy||deleted;
  $('#pool-export').disabled=busy;$('#pool-import').disabled=busy||deleted;
  $('#pool-create').disabled=policyBusy('pool-editor')||!poolEditor.rows.length;
  $('#pool-custom-new').disabled=$('#pool-box-new').disabled=policyBusy('pool-editor');
  const variantReason=poolVariantUnavailable();
  $('#pool-variant-new').disabled=busy||deleted||!!variantReason;
  $('#pool-variant-new').title=variantReason||(poolEditor.boxByID?.size?'在当前卡池增加单抽、十连或 50 连抽等抽法':'在当前卡池增加单抽、十连等抽法');
  $('#pool-variant-note').textContent=variantReason|| (poolEditor.dirty?'新增前请先保存草稿并预览，以沿用刚编辑的奖池内容。':poolEditor.boxByID?.size?'可新增 1–11 抽或 50 抽分池，分别配置价格和消耗；全部抽法共用奖励、库存及轮次。':'可新增 1–11 抽分池，分别配置价格和奖励；同一职业的相同抽数只保留一个入口。');
  $('#pool-delete').disabled=$('#pool-restore').disabled=busy;
  $('#pool-cover-upload').disabled=busy||deleted;$('#pool-cover-upload-label').classList.toggle('disabled',busy||deleted);
  $('#pool-cover-reset').disabled=busy||deleted||!poolEditor.coverPath;
  $('#pool-publish').disabled=busy||poolEditor.dirty||!poolEditor.preview?.publishable||poolPublished()||!poolEditor.variants.every(poolHasDraft);
  $('#pool-preview-publish').disabled=$('#pool-publish').disabled;
  $('#pool-discard').disabled=busy||deleted||!(poolEditor.dirty||poolEditor.variants.some(poolHasDraft));
  const step=!poolEditor.row?'':poolEditor.dirty?'edit':poolEditor.preview?.publishable&&!poolPublished()?'publish':poolPublished()&&!poolEditor.preview?'done':'preview';
  const order=['edit','preview','publish'];
  $$('#pool-flow li').forEach(li=>{const i=order.indexOf(li.dataset.flow),at=step==='done'?3:order.indexOf(step);li.classList.toggle('current',i===at);li.classList.toggle('done',i<at)});
  $('#pool-publish').title=poolEditor.dirty?'有未保存的修改：请先保存草稿并预览':!poolEditor.preview?'请先保存草稿并核对预览':!poolEditor.preview.publishable?'预览中含未闭包卡牌，不能发布':poolPublished()?'草稿与已发布版本相同':'';
  $('#pool-preview-reason').textContent=poolEditor.preview&&!poolEditor.preview.publishable?'含资源未闭包的卡牌，移除后重新保存才能发布':poolPublished()&&poolEditor.preview?'与已发布版本相同，无需发布':'';
}
async function runPoolEdit(action, what='保存') {
  if(!poolEditor.row)return;
  return runPoolTask(action,what);
}
async function runPoolTask(action, what) {
  if(policyBusy('pool-editor'))return;
  state.publishing.add('pool-editor');updatePolicyControls();
  try{await action()}catch(e){reportError('pool-editor',e,what)}
  finally{state.publishing.delete('pool-editor');updatePolicyControls()}
}

async function loadPoolEditor(selectID) {
  const [data,policy] = await Promise.all([api('/api/gacha-editor'),api('/api/gacha-policy').catch(()=>null)]);
  poolEditor.openGroups=policy?new Set(policy.publication?.group_ids||[]):null;
  const cards=poolEditor.catalog?.length?poolEditor.catalog:await fetchAllCatalog();
  const previous = selectID ?? poolEditor.row?.gacha_id;
  poolEditor.banners=data.banners||[];poolEditor.rows=data.pools;poolEditor.ruleTemplates=data.rule_templates||[];poolEditor.customRevision=data.custom_revision||0;poolEditor.catalog=cards;poolEditor.cards=cards.filter(c=>c.kind==='card');
  renderPoolSelect();
  selectPool(data.pools.some(row=>row.gacha_id===previous)?previous:Number($('#pool-select').value));
}

function selectPool(id=Number($('#pool-select').value)) {
  const picked=poolEditor.rows.find(row=>row.gacha_id===id);
  if(poolEditor.row?.base.groupid!==picked?.base.groupid)$('#pool-cost').value='';
  poolEditor.editSerial++;
  poolEditor.variants=picked?poolGroupRows(picked):[];
  poolEditor.row=poolEditor.variants[0]||null;
  const row=poolEditor.row;
  if (!row) {poolEditor.preview=null;$('#pool-current').textContent='请先选择一个卡池';updatePoolControls();return;}
  $('#pool-select').value=row.gacha_id;
  $('#pool-current').innerHTML=`正在编辑：${esc(row.config.name)} <code>${poolEditor.variants.map(v=>v.gacha_id).join('、')}</code>${row.custom?' <span class="tag info">后台新建</span>':''}${row.deleted?' <span class="tag bad">已删除</span>':''}${poolOpenBadge(row)}`;
  $('#pool-delete').hidden=!row.custom||row.deleted;$('#pool-restore').hidden=!row.custom||!row.deleted;$('#pool-deleted-note').hidden=!row.deleted;
  const config=poolSavedConfig(row);
  poolEditor.contents=new Map(poolEditor.variants.map(v=>[v.gacha_id,poolVariantState(poolSavedConfig(v))]));
  poolEditor.legacyByID=new Map(poolEditor.variants.filter(v=>v.legacy).map(v=>{const c=poolSavedConfig(v);return [v.gacha_id,{card_num:c.card_num||v.base.card_num,banner_key:c.banner_key||v.base.banner_key,gift_rules:structuredClone(c.gift_rules||v.base.gift_rules||[])}]}));
  const boxSource=poolEditor.variants.find(v=>v.base.box_rounds?.length);
  setPoolBoxRounds(boxSource?poolSavedConfig(boxSource).box_rounds||boxSource.base.box_rounds:null);
  poolEditor.activeID=0;
  poolEditor.coverPath=config.cover_path||'';
  $('#pool-play-limit').value=config.play_count_max||0;
  $('#pool-start').value=localTimeInput(config.start_unix);$('#pool-end').value=localTimeInput(config.end_unix);
  // Schedules were per variant before they became pool-wide; show the first one and let a save unify them.
  const schedules=new Set(poolEditor.variants.map(v=>{const c=poolSavedConfig(v);return `${c.start_unix||0}-${c.end_unix||0}`}));
  $('#pool-schedule-note').hidden=schedules.size<2;
  $('#pool-schedule-note').textContent=schedules.size<2?'':`各分池已保存的排期不一致（${schedules.size} 种）。这里显示第一个分池的排期，保存草稿后全部分池统一使用它。`;
  poolEditor.strategies=new Map(poolEditor.variants.map(v=>{const c=poolSavedConfig(v),type=c.pay_type||v.base.pay_type,item=c.pay_type?c.pay_typeid:v.base.pay_typeid;return [v.gacha_id,{open:!c.closed,name:c.name,pay_type:type,pay_typeid:type===4?item||0:0,price:c.price}]}));
  poolEditor.mixedByID=new Map(poolEditor.variants.filter(v=>v.base.reward_pool?.length).map(v=>{const c=poolSavedConfig(v);return [v.gacha_id,structuredClone({reward_pool:c.reward_pool||v.base.reward_pool,steps:c.steps||v.base.steps||[]})]}));
  for(const [id,mixed] of poolEditor.mixedByID){if(poolEditor.legacyByID.has(id)&&!mixed.steps.length&&mixed.reward_pool.length===1&&mixed.reward_pool[0].reward.type===0)mixed.reward_pool=[];}
  poolEditor.dirty=false; poolEditor.preview=null; $('#pool-publish').disabled=true; $('#pool-rarity-rows').innerHTML=''; renderRarityTool.touched=false;
  $('#pool-preview').textContent='';clearInvalid($('#pool-editor'));$('#pool-payment-search').value='';
  renderPoolVersion();selectPoolVariant(row.gacha_id,false);
  renderStrategies(); renderPoolCover(); renderPoolCards(); renderSelectedCards();updatePoolControls();
  if(schedules.size>1)poolChanged();
}
// Whether 「扭蛋发布」 currently opens this pool; a published pool that is not opened stays invisible to players.
function poolOpenBadge(row){const open=poolEditor.openGroups?.has(row.base.groupid);return poolEditor.openGroups&&!row.deleted?` <button type="button" class="tag-link ${open?'ok':'warn'}" data-jump-gachas title="到「扭蛋发布」${open?'查看':'开启'}此卡池">${open?'扭蛋发布：已开启':'扭蛋发布：未开启'}</button>`:''}
$('#pool-current').addEventListener('click',e=>{if(e.target.closest('[data-jump-gachas]'))switchView('gachas')});
function switchPool(id) {
  if(policyBusy('pool-editor')||poolGroupRows().some(r=>r.gacha_id===id))return;
  if(poolEditor.dirty&&!confirm('切换卡池将放弃未保存的编辑，是否继续？'))return;
  selectPool(id);renderPoolSelect();
}
function renderPoolVersion() {
  const row=poolEditor.row,n=poolEditor.variants.length;
  $('#pool-version').textContent=`${poolEditor.variants.some(poolHasDraft)?'有已保存草稿':'无已保存草稿'} · ${row.live.revision?'已发布 v'+row.live.revision:row.custom?'尚未发布':'包内默认配置'}${n>1?` · ${n} 种抽法`:''}`;
}
function poolCoverURL(path) { return path ? '/'+path : poolEditor.row?.banner_url || ''; }
function renderPoolCover() {
  const path=poolEditor.coverPath;
  $('#pool-cover-preview').innerHTML=image(poolCoverURL(path),poolEditor.row?.config.name||'');
  $('#pool-cover-state').textContent=path?'自定义封面':'包内封面';$('#pool-cover-state').className='tag'+(path?' info':'');
}

// Draw methods: per-variant name, payment and price. Draw counts and special rules come from the pool itself.
function renderStrategies() {
  const search=$('#pool-payment-search'),query=search.value.trim();
  const available=(poolEditor.catalog||[]).filter(c=>c.reward_type===8&&c.resource_state!=='unavailable');
  const matches=c=>matchesWords(`${c.name} ${c.reward_type_id}`,query);
  const items=selected=>available.filter(c=>c.reward_type_id===selected||matches(c));
  const methods=[[3,'水晶（先免费后付费）'],[4,'道具'],[2,'友情点'],[6,'付费水晶']];
  $('#pool-strategies').innerHTML=poolEditor.variants.map(v=>{
    const s=poolEditor.strategies.get(v.gacha_id),b=v.base,max=s.pay_type!==2&&b.pay_type===2?b.card_num:b.card_num_max,stepped=!!poolEditor.mixedByID.get(v.gacha_id)?.steps?.length;
    const active=v.gacha_id===poolEditor.activeID,count=poolDrawCount(v);
    return `<tr data-variant="${v.gacha_id}" class="${s.open?'':'closed'}${active?' current':''}"><td><input type="radio" name="pool-variant-pick" data-pick="${v.gacha_id}" ${active?'checked':''} aria-label="编辑分池 ${v.gacha_id}"></td><td><label class="switch"><input type="checkbox" data-strategy="open" ${s.open?'checked':''} aria-label="开放抽法 ${v.gacha_id}"></label></td><td><b>${count} 抽 · ${poolProfessionName(b.arthur_type)}</b><span class="sub">${max>count?`最多 ${max} 抽 · `:''}<code>${v.gacha_id}</code></span></td>`+
      `<td data-label="显示名称"><input data-strategy="name" maxlength="60" value="${esc(s.name)}" aria-label="抽法 ${v.gacha_id} 显示名称"></td>`+
      `<td data-label="消耗方式"><select data-strategy="pay_type" aria-label="抽法 ${v.gacha_id} 消耗方式">${methods.map(([t,l])=>`<option value="${t}" ${s.pay_type===t?'selected':''}>${l}</option>`).join('')}</select></td>`+
      `<td data-label="消耗道具">${s.pay_type===4?`<select data-strategy="pay_typeid" aria-label="抽法 ${v.gacha_id} 消耗道具"><option value="">请选择消耗道具</option>${items(s.pay_typeid).map(c=>`<option value="${c.reward_type_id}" ${c.reward_type_id===s.pay_typeid?'selected':''}>${query&&!matches(c)?'当前已选 · ':''}${esc(c.name)} · ${c.reward_type_id}</option>`).join('')}</select>`:'<span class="hint">—</span>'}</td>`+
      `<td data-label="价格">${stepped?'<span class="hint">按阶段设置</span>':`<input data-strategy="price" type="number" min="1" max="10000000" step="1" value="${esc(s.price)}" aria-label="抽法 ${v.gacha_id} 价格">`}</td><td class="pool-variant-sum" data-label="奖池与排期" data-summary>${poolVariantSummary(v)}</td></tr>`;
  }).join('');
  search.hidden=!poolEditor.variants.some(v=>poolEditor.strategies.get(v.gacha_id).pay_type===4);
  const hint=$('#pool-variants-hint');
  if(hint){hint.dataset.paymentDefaultHint??=hint.textContent;hint.textContent=query&&!search.hidden?`匹配 ${available.filter(matches).length} 个道具，请展开「消耗道具」下拉框选择；当前已选项会保留，不会自动替换。`:hint.dataset.paymentDefaultHint;}
  renderPoolContract();
}
function renderPoolContract() {
  const open=poolEditor.variants.filter(v=>poolEditor.strategies.get(v.gacha_id).open).length;
  const box=!!poolEditor.boxByID?.size;
  $('#pool-contract').textContent=`${poolEditor.variants.length} 个分池${open<poolEditor.variants.length?`（开放 ${open} 个）`:''} · ${box?'奖励、库存、轮次、封面和排期共用，费用各自配置':'封面、排期与累计限抽共用，其余各自配置'} · 卡池开放由「扭蛋发布」控制`;
  $('#pool-settings-note').textContent=box?'全部抽法共用奖励、箱子库存、轮次、封面和排期；每个抽法分别设置开放、名称、消耗和价格。':'封面、排期和累计限抽由整个卡池共用；每个分池（抽法）有自己的开放、名称、消耗、价格和奖池。';
  const hint=$('#pool-variants-hint');
  hint.dataset.paymentDefaultHint=box?'单抽与连抽共用同一个箱子；点击一行设置该抽法的消耗和价格。':'点击一行编辑该分池的奖池；可只开放其中几个（至少一个）。';
  if(!$('#pool-payment-search').value.trim()||$('#pool-payment-search').hidden)hint.textContent=hint.dataset.paymentDefaultHint;
}
function setPoolBoxRounds(rounds){
  // All entries edit the same array, including replacing a whole round via copy.
  const shared=rounds?structuredClone(rounds):null;
  poolEditor.boxByID=new Map(poolEditor.variants.filter(v=>v.base.box_rounds?.length).map(v=>[v.gacha_id,shared]));
}
$('#pool-strategies').addEventListener('input',e=>{
  const el=e.target.closest('[data-strategy]');if(!el)return;
  const s=poolEditor.strategies.get(Number(el.closest('tr').dataset.variant)),field=el.dataset.strategy;
  if(field==='open'){s.open=el.checked;el.closest('tr').classList.toggle('closed',!s.open)}
  else if(field==='name')s.name=el.value;
  else if(field==='price')s.price=Number(el.value);
  else if(field==='pay_typeid')s.pay_typeid=Number(el.value)||0;
  else {s.pay_type=Number(el.value);if(s.pay_type!==4)s.pay_typeid=0}
  el.classList.remove('invalid');poolChanged();
  if(field==='pay_type')renderStrategies();else if(field==='open')renderPoolContract();
});
$('#pool-payment-search').oninput=()=>renderStrategies();
function poolChanged() { poolRememberActive();const cell=$(`#pool-strategies tr[data-variant="${poolEditor.activeID}"] [data-summary]`),row=poolActiveRow();if(cell&&row)cell.innerHTML=poolVariantSummary(row);poolEditor.editSerial++; poolEditor.dirty=true; poolEditor.preview=null; $('#pool-publish').disabled=true; $('#pool-version').textContent='有未保存的修改'; $('#pool-preview').textContent=''; updatePoolControls(); }
$('#pool-strategies').addEventListener('click',e=>{
  const tr=e.target.closest('tr[data-variant]');if(!tr||e.target.closest('input:not([data-pick]),select,label.switch'))return;
  const id=Number(tr.dataset.variant);if(id===poolEditor.activeID||policyBusy('pool-editor')){$(`#pool-strategies [data-pick="${poolEditor.activeID}"]`).checked=true;return}
  selectPoolVariant(id);$(`#pool-strategies [data-pick="${id}"]`)?.focus();
});
$('#pool-copy-apply').onclick=()=>{
  poolRememberActive();const ids=$$('#pool-copy-targets input:checked').map(el=>Number(el.value));if(!ids.length)return toast('请选择目标分池',true);
  if(!confirm(`将当前分池的卡牌、权重、名声和筛选复制到 ${ids.length} 个分池草稿？目标的消耗和价格各自保留。`))return;
  const source=poolEditor.contents.get(poolEditor.activeID);
  for(const id of ids){const target=poolEditor.contents.get(id),row=poolEditor.variants.find(v=>v.gacha_id===id),cards=new Map(poolEditor.cards.map(c=>[c.reward_type_id,c]));target.selected=new Map([...source.selected].filter(([card])=>poolCardEligibleFor(cards.get(card),row)));target.fames=new Map([...source.fames].filter(([card])=>target.selected.has(card)));target.filters=structuredClone(source.filters);}
  poolChanged();renderStrategies();$('#pool-copy-menu').open=false;toast('已复制到所选分池草稿，可逐个切换核对后保存');
};
$('#pool-split-common').onchange=renderPoolSplit;
$('#pool-split-apply').onclick=()=>{
  poolRememberActive();const all=poolSplitPlan(),plan=all.filter(p=>p.selected.size),skipped=all.filter(p=>!p.selected.size);if(!plan.length)return toast('本分池没有可分配的职业卡牌',true);
  const replacing=plan.filter(p=>poolEditor.contents.get(p.v.gacha_id).selected.size).length;
  if(!confirm(`按职业把卡牌分到 ${plan.length} 个职业分池草稿？\n${plan.map(p=>`${poolSplitLabel(p)}：${p.selected.size} 张`).join('\n')}${replacing?`\n其中 ${replacing} 个分池已有卡牌，会被替换。`:''}${skipped.length?`\n\n无匹配卡牌，跳过并保留原配置：\n${skipped.map(poolSplitLabel).join('\n')}`:''}\n\n权重和名声随卡牌一起分配；各分池的消耗和价格不变。`))return;
  for(const p of plan){const target=poolEditor.contents.get(p.v.gacha_id);target.selected=p.selected;target.fames=new Map([...poolEditor.fames].filter(([id])=>p.selected.has(id)))}
  poolChanged();renderStrategies();$('#pool-split-menu').open=false;
  toast(`已分到 ${plan.length} 个职业分池草稿${skipped.length?`；${skipped.map(poolSplitLabel).join('、')} 无匹配卡牌，已跳过并保留原配置`:''}。可在分池表逐个核对后保存`);
};
// The two pop-up menus share the bar: opening one closes the other.
['#pool-split-menu','#pool-copy-menu'].forEach((id,i,all)=>$(id).addEventListener('toggle',()=>{if(!$(id).open)return;$(all[1-i]).open=false;if(id==='#pool-split-menu')renderPoolSplit()}));
function rewardLabel(r) {return poolEditor.catalog?.find(c=>c.reward_type===r.type&&c.reward_type_id===r.reward_typeid)?.name || `${r.type}:${r.reward_typeid}`;}
// Mixed reward pools keep their rewards and stage payments per variant.
function setupMixedPool() {
  const mixed=poolEditor.mixedByID.has(poolEditor.activeID);
  $('#pool-card-editor').style.display=mixed||poolEditor.boxByID?.has(poolEditor.activeID)?'none':'';$('#pool-mixed-editor').style.display=mixed?'':'none';
  poolEditor.mixed=null;
  if(!mixed)return;
  const variants=poolEditor.variants.filter(v=>poolEditor.mixedByID.has(v.gacha_id));
  $('#pool-mixed-variant').innerHTML=variants.map(v=>`<option value="${v.gacha_id}">${esc(poolEditor.strategies.get(v.gacha_id).name)} · ${v.base.card_num} 抽 · ${v.gacha_id}</option>`).join('');
  $('#pool-mixed-variant').hidden=true;
  selectMixedVariant(poolEditor.activeID);
}
function selectMixedVariant(id) {
  const v=poolEditor.variants.find(r=>r.gacha_id===id),s=poolEditor.strategies.get(id);
  poolEditor.mixedVariant=id;poolEditor.mixed=poolEditor.mixedByID.get(id);poolEditor.mixedPage=0;
  $('#pool-mixed-variant').value=String(id);
  $('#pool-stage').innerHTML=(poolEditor.mixed.steps.length?poolEditor.mixed.steps:[{}]).map((_,i)=>`<option value="${i}">第${i+1}阶段</option>`).join('');
  $('#pool-stage-price-label').textContent=`本阶段价格（${poolPayName(s.pay_type,s.pay_typeid)}）`;
  $('#pool-mixed-search').value='';
  $('#pool-mixed-gifts').textContent=(v.base.gift_rules||[]).map(g=>`第${g.from_play}至${g.to_play||'以后'}次赠送：${g.rewards.map(r=>`${rewardLabel(r)} × ${r.num}`).join('、')}`).join('；')||'无额外赠礼';
  renderMixedPool();
}
$('#pool-mixed-variant').onchange=()=>selectMixedVariant(Number($('#pool-mixed-variant').value));
function renderMixedPool() {
  if(!poolEditor.mixed)return;
  const step=poolEditor.mixed.steps[Number($('#pool-stage').value)],pool=step?.reward_pool||poolEditor.mixed.reward_pool;
  renderStagePayment(step);
  $('#pool-stage-price').value=step?.price||poolEditor.strategies.get(poolEditor.mixedVariant)?.price||'';$('#pool-stage-price').disabled=!step;
  const query=$('#pool-mixed-search').value.trim().toLowerCase(), rows=pool.map((r,i)=>({...r,index:i})).filter(r=>`${rewardLabel(r.reward)} ${r.reward.reward_typeid}`.toLowerCase().includes(query));
  const pages=Math.max(1,Math.ceil(rows.length/100));poolEditor.mixedPage=Math.max(0,Math.min(poolEditor.mixedPage,pages-1));
  const custom=!!legacyMixedConfig();
  const total=pool.reduce((sum,r)=>sum+(Number(r.weight)||0),0),share=w=>total?(Number(w)/total*100).toFixed(3):'0';
  renderStageTabs();$('#pool-mixed-total').textContent=`共 ${num(pool.length)} 项 · 权重合计 ${num(total)}`;
  $('#pool-mixed-page').textContent=`${poolEditor.mixedPage+1} / ${pages} 页 · 共 ${rows.length} 项`;
  $('#pool-mixed-prev').disabled=poolEditor.mixedPage===0;$('#pool-mixed-next').disabled=poolEditor.mixedPage===pages-1;
  $('#pool-mixed-rows').innerHTML=rows.slice(poolEditor.mixedPage*100,(poolEditor.mixedPage+1)*100).map(r=>`<tr><td><b class="pool-card-name">${esc(rewardLabel(r.reward))}</b><span class="sub">ID ${r.reward.reward_typeid}</span></td><td class="num">${custom?`<input type="number" min="1" max="9999" value="${r.reward.num}" data-mixed-num="${r.index}" aria-label="奖励数量"><button class="secondary sm" data-mixed-remove="${r.index}">移除</button>`:r.reward.num}</td><td><input type="number" min="1" max="1000000" value="${r.weight}" data-mixed-weight="${r.index}" aria-label="${esc(rewardLabel(r.reward))}权重"><span class="sub" data-share="${r.index}">约 ${share(r.weight)}%</span></td></tr>`).join('')||emptyRow(3,'没有匹配的奖励');
  $$('#pool-mixed-rows [data-mixed-num]').forEach(input=>input.oninput=()=>{pool[Number(input.dataset.mixedNum)].reward.num=Number(input.value);poolChanged();});
  $$('#pool-mixed-rows [data-mixed-remove]').forEach(button=>button.onclick=()=>{pool.splice(Number(button.dataset.mixedRemove),1);poolChanged();renderMixedPool();});
  $$('#pool-mixed-rows [data-mixed-weight]').forEach(input=>input.oninput=()=>{pool[Number(input.dataset.mixedWeight)].weight=Number(input.value);input.classList.remove('invalid');poolChanged();
    const sum=pool.reduce((a,r)=>a+(Number(r.weight)||0),0);$('#pool-mixed-total').textContent=`共 ${num(pool.length)} 项 · 权重合计 ${num(sum)}`;$$('#pool-mixed-rows [data-share]').forEach(el=>el.textContent=`约 ${sum?(Number(pool[Number(el.dataset.share)].weight)/sum*100).toFixed(3):'0'}%`)});
}
// Stage tabs mirror the hidden stage select; each tab shows the stage's price.
function renderStageTabs(){
  const m=poolEditor.mixed,box=$('#pool-stage-tabs');box.hidden=!m?.steps.length;if(box.hidden){box.innerHTML='';return}
  const current=Number($('#pool-stage').value);
  box.innerHTML=m.steps.map((s,i)=>`<button type="button" data-stage="${i}" class="${i===current?'active':''}" aria-pressed="${i===current}">第 ${i+1} 阶段<small>${num(s.price)}</small></button>`).join('');
}
$('#pool-stage-tabs').onclick=e=>{const b=e.target.closest('[data-stage]');if(!b||b.classList.contains('active'))return;$('#pool-stage').value=b.dataset.stage;poolEditor.mixedPage=0;renderMixedPool()};
function renderStagePayment(step){
  $('#pool-stage-payment').hidden=!step;if(!step)return;
  const type=step.pay_type||0,selected=step.pay_typeid||0,query=$('#pool-stage-item-search').value.trim();
  $('#pool-stage-pay-type').value=type;$('#pool-stage-pay-item').hidden=$('#pool-stage-item-search').hidden=type!==4;
  const items=poolEditor.catalog.filter(c=>c.reward_type===8&&c.resource_state!=='unavailable'&&(c.reward_type_id===selected||matchesWords(`${c.name} ${c.reward_type_id}`,query)));
  $('#pool-stage-pay-item').innerHTML='<option value="0">请选择道具</option>'+items.map(c=>`<option value="${c.reward_type_id}" ${selected===c.reward_type_id?'selected':''}>${esc(c.name)} · ${c.reward_type_id}</option>`).join('');
  const s=poolEditor.strategies.get(poolEditor.mixedVariant);$('#pool-stage-price-label').textContent=`本阶段价格（${poolPayName(type||s.pay_type,type?selected:s.pay_typeid)}）`;
}
$('#pool-stage-pay-type').onchange=()=>{const step=poolEditor.mixed.steps[Number($('#pool-stage').value)];step.pay_type=Number($('#pool-stage-pay-type').value);if(step.pay_type!==4)step.pay_typeid=0;poolChanged();renderStagePayment(step)};
$('#pool-stage-pay-item').onchange=()=>{const step=poolEditor.mixed.steps[Number($('#pool-stage').value)];step.pay_typeid=Number($('#pool-stage-pay-item').value);poolChanged();renderStagePayment(step)};
$('#pool-stage-item-search').oninput=()=>renderStagePayment(poolEditor.mixed.steps[Number($('#pool-stage').value)]);
$('#pool-stage').onchange=()=>{poolEditor.mixedPage=0;renderMixedPool()};
$('#pool-stage-price').oninput=()=>{const i=Number($('#pool-stage').value),price=Number($('#pool-stage-price').value);poolEditor.mixed.steps[i].price=price;if(!i)poolEditor.strategies.get(poolEditor.mixedVariant).price=price;poolChanged();const tab=$(`#pool-stage-tabs [data-stage="${i}"] small`);if(tab)tab.textContent=num(price)};
$('#pool-mixed-search').oninput=()=>{poolEditor.mixedPage=0;renderMixedPool()};
$('#pool-mixed-prev').onclick=()=>{poolEditor.mixedPage--;renderMixedPool()};
$('#pool-mixed-next').onclick=()=>{poolEditor.mixedPage++;renderMixedPool()};
function poolIneligibleReason() { return '不符合当前分池的职业或星级限制'; }
function poolMatches(card) {
  const q=$('#pool-search').value.trim().toLowerCase(), rarity=Number($('#pool-rarity').value);
  if($('#pool-eligible-only').checked && !poolCardEligible(card) && !poolEditor.selected.has(card.reward_type_id)) return false;
  return matchesCardFilters(card,'pool') && cardMatchesJob(card,poolEditor.job) && (!poolEditor.source || (card.source_tags||[]).includes(poolEditor.source)) && matchesWords(`${card.name} ${card.reward_type_id} ${card.pict_id} ${cardFacts(card)}`,q) && (!rarity || card.rarity===rarity) && (!$('#pool-ready').checked || card.resource_state!=='unavailable');
}
// Any existing card may join an operator pool; only the unowned-six-star pool keeps its six-star identity (server rule).
function poolCardEligibleFor(card,row){const job=row?.base?.arthur_type||0;return !!card&&!(row?.base?.unowned_only&&card.rarity!==6)&&(!(job>=1&&job<=4)||!card.arthur_type||card.arthur_type===job);}
function poolCardEligible(card) { return poolCardEligibleFor(card,poolActiveRow()); }
function renderPoolCards() {
  $$('#pool-jobs button').forEach(b=>b.classList.toggle('active',Number(b.dataset.job)===poolEditor.job));
  $$('#pool-sources button').forEach(b=>b.classList.toggle('active',b.dataset.source===poolEditor.source));
  const matches=poolEditor.cards.filter(poolMatches), pages=Math.max(1,Math.ceil(matches.length/48));
  poolEditor.page=Math.max(0,Math.min(poolEditor.page,pages-1));
  $('#pool-card-count').innerHTML=`<b>筛选结果 ${num(matches.length)} 张</b><span>目录 ${num(poolEditor.cards.length)} 张 · ${poolEditor.page+1} / ${pages} 页</span>`;
  const base=poolEditor.row?.base;
  $('#pool-eligibility-hint').textContent=base?.unowned_only?'点击卡牌加入或移出草稿。此池为“必得未入手六星”，只接受6星卡牌。':'点击卡牌加入或移出草稿。任意卡牌都可加入（含非扭蛋来源和新卡）；带保底的卡池仍须符合保底星级。';
  $('#pool-eligible-only-field').hidden=!base?.unowned_only;
  $('#pool-prev').disabled=poolEditor.page===0; $('#pool-next').disabled=poolEditor.page>=pages-1;
  $('#pool-cards').innerHTML=matches.slice(poolEditor.page*48,(poolEditor.page+1)*48).map(card=>{const on=poolEditor.selected.has(card.reward_type_id),eligible=poolCardEligible(card);return `<button class="catalog-card ${on?'selected':''}" data-card="${card.reward_type_id}" aria-pressed="${on}" ${!on&&!eligible?`disabled title="不能加入此卡池：${poolIneligibleReason(card)}（服务器规则）"`:''}><div class="thumb">${image(card.image_url,card.name)}</div><div class="catalog-copy"><b class="name" title="${esc(card.name)}">${esc(card.name)}</b><span class="meta">${catalogMeta(card)}${!on&&!eligible?`<span class="tag why">${poolIneligibleReason(card)}</span>`:''}${card.resource_state==='unavailable'?'<span class="tag bad">资源待补齐</span>':card.image_url?'':'<span class="tag">无缩略图</span>'}${on?`<span class="tag ok">权重 ${esc(poolEditor.selected.get(card.reward_type_id))}</span>`:''}</span><span class="facts-text">${esc(cardSourceDescription(card))}</span></div></button>`}).join('')||'<div class="empty"><b>没有匹配的卡牌</b>调整搜索或筛选条件。</div>';
  $$('#pool-cards [data-card]').forEach(button=>button.onclick=()=>{const id=Number(button.dataset.card);poolEditor.selected.has(id)?poolEditor.selected.delete(id):poolEditor.selected.set(id,1);poolChanged();renderPoolCards();renderSelectedCards()});
}
function renderSelectedCards() {
  const cards=new Map(poolEditor.cards.map(card=>[card.reward_type_id,card]));
  const rows=[...poolEditor.selected].filter(([id])=>!cards.has(id)||poolMatches(cards.get(id)));
  const total=[...poolEditor.selected.values()].reduce((a,b)=>a+(Number(b)||0),0);
  $('#pool-selected-count').textContent=`已选 ${poolEditor.selected.size} 张${rows.length!==poolEditor.selected.size?` · 当前筛选显示 ${rows.length} 张`:''}`;
  poolEditor.selectedPage=renderPager('pool-selected',poolEditor.selectedPage,rows.length,100,page=>{poolEditor.selectedPage=page;renderSelectedCards()});
  $('#pool-selected').innerHTML=rows.slice(poolEditor.selectedPage*100,(poolEditor.selectedPage+1)*100).map(([id,weight])=>`<tr><td class="cell-wrap"><b class="pool-card-name">${esc(cards.get(id)?.name||'未知卡牌')}</b><span class="sub"><code>${id}</code>${cards.get(id)?.rarity?` · ${cards.get(id).rarity} 星`:''}${cards.get(id)?.resource_state==='unavailable'?' <span class="tag bad">资源待补齐</span>':''}</span></td><td><input aria-label="${id} 权重" data-weight="${id}" type="number" min="1" max="1000000" step="1" value="${esc(weight)}"><span class="sub">约 ${total?(Number(weight)/total*100).toFixed(3):'0'}%</span></td><td><input aria-label="${id} 抽出名声" data-fame="${id}" type="number" min="1" max="${cards.get(id)?.fame_max||1}" step="1" value="${poolEditor.fames.get(id)||1}" title="1–${cards.get(id)?.fame_max||1}"><span class="sub">上限 ${cards.get(id)?.fame_max||'—'}</span></td><td><button class="x-button" data-remove="${id}" aria-label="移除 ${id}"><svg class="icon"><use href="#i-close"/></svg></button></td></tr>`).join('') || emptyRow(4,poolEditor.selected.size?'当前筛选下没有已选卡牌':'还没有选择卡牌','在左侧目录点击卡牌加入草稿。');
  $('#pool-selected-hint').textContent='翻页与筛选保留已选卡牌与权重；约占比按全部已选卡的相对权重估算，以保存后的预览为准。';
  renderRarityTool();
  $$('#pool-selected [data-weight]').forEach(input=>input.oninput=()=>{poolEditor.selected.set(Number(input.dataset.weight),Number(input.value));input.classList.remove('invalid');poolChanged()});
  $$('#pool-selected [data-fame]').forEach(input=>input.oninput=()=>{const id=Number(input.dataset.fame),v=Number(input.value);v>1?poolEditor.fames.set(id,v):poolEditor.fames.delete(id);input.classList.remove('invalid');poolChanged()});
  $$('#pool-selected [data-remove]').forEach(button=>button.onclick=()=>{poolEditor.selected.delete(Number(button.dataset.remove));poolEditor.fames.delete(Number(button.dataset.remove));poolChanged();renderPoolCards();renderSelectedCards()});
}
// Per-rarity totals: current share from weights, and a target that is spread evenly over that rarity's cards.
function poolRarityGroups() {
  const cards=new Map(poolEditor.cards.map(c=>[c.reward_type_id,c])),groups=new Map();let total=0;
  for(const [id,w] of poolEditor.selected){const r=cards.get(id)?.rarity||0,g=groups.get(r)||{rarity:r,ids:[],weight:0};g.ids.push(id);g.weight+=Number(w)||0;groups.set(r,g);total+=Number(w)||0}
  return {groups:[...groups.values()].sort((a,b)=>b.rarity-a.rarity),total};
}
function renderRarityTool() {
  const {groups,total}=poolRarityGroups();$('#pool-rarity-tool').hidden=!groups.length;if(!groups.length)return;
  const keep=new Map($$('#pool-rarity-rows [data-rarity]').map(el=>[Number(el.dataset.rarity),el.value]));
  $('#pool-rarity-rows').innerHTML=groups.map(g=>{const pct=total?g.weight/total*100:0,value=keep.has(g.rarity)?keep.get(g.rarity):+pct.toFixed(6);return `<tr><td><span class="stars">${'★'.repeat(Math.min(g.rarity,8))}</span> ${g.rarity||'?'} 星</td><td class="num">${g.ids.length}</td><td class="num">${pct.toFixed(3)}%</td><td><span class="input-unit"><input type="number" min="0" max="100" step="0.0001" data-rarity="${g.rarity}" value="${value}" aria-label="${g.rarity}星目标合计概率"><span>%</span></span></td><td class="num" data-per="${g.rarity}"></td></tr>`}).join('');
  updateRarityTool();
}
function rarityTargets() {return $$('#pool-rarity-rows [data-rarity]').map(el=>({rarity:Number(el.dataset.rarity),pct:el.value===''?NaN:Number(el.value),el}))}
function updateRarityTool() {
  const {groups}=poolRarityGroups(),targets=rarityTargets(),sum=targets.reduce((n,t)=>n+(Number.isFinite(t.pct)?t.pct:0),0),ok=targets.every(t=>Number.isFinite(t.pct)&&t.pct>0&&t.pct<=100)&&Math.abs(sum-100)<0.001;
  for(const t of targets){const g=groups.find(g=>g.rarity===t.rarity);$(`#pool-rarity-rows [data-per="${t.rarity}"]`).textContent=g&&Number.isFinite(t.pct)?`${(t.pct/g.ids.length).toFixed(4)}%`:'—';t.el.classList.toggle('invalid',!(Number.isFinite(t.pct)&&t.pct>0&&t.pct<=100))}
  const touched=renderRarityTool.touched;
  $('#pool-rarity-sum').textContent=!touched?'修改目标合计后点击“平均分配到每张卡”':`目标合计 ${+sum.toFixed(4)}%${ok?'':Math.abs(sum-100)<0.001?'（每个星级须大于 0；不想出现的星级请移除其卡牌）':'（须等于 100%）'}`;$('#pool-rarity-sum').className=ok||!touched?'hint':'state-error';
  if(!touched)targets.forEach(t=>t.el.classList.remove('invalid'));
  $('#pool-rarity-apply').disabled=!ok||!touched;
}
$('#pool-rarity-rows').oninput=()=>{renderRarityTool.touched=true;updateRarityTool()};
$('#pool-rarity-reset').onclick=()=>{$('#pool-rarity-rows').innerHTML='';renderRarityTool.touched=false;renderRarityTool()};
$('#pool-rarity-apply').onclick=()=>{
  const {groups}=poolRarityGroups(),targets=rarityTargets();
  // Per-card weight on a 1,000,000 scale, then scaled up so the largest weight stays within the server limit.
  const raw=new Map(targets.map(t=>{const g=groups.find(g=>g.rarity===t.rarity);return [t.rarity,t.pct*10000/g.ids.length]})),scale=Math.max(1,Math.floor(1000000/Math.max(...raw.values())));
  const lines=targets.map(t=>{const g=groups.find(g=>g.rarity===t.rarity);return `${t.rarity} 星：${g.ids.length} 张，合计 ${t.pct}%（每张约 ${(t.pct/g.ids.length).toFixed(4)}%）`});
  if(!confirm(`按星级平均分配已选的 ${poolEditor.selected.size} 张卡牌的权重？\n${lines.join('\n')}\n\n会覆盖这些卡牌当前的权重，保存草稿后才生效。`))return;
  for(const g of groups){const w=Math.min(1000000,Math.max(1,Math.round(raw.get(g.rarity)*scale)));for(const id of g.ids)poolEditor.selected.set(id,w)}
  $('#pool-rarity-rows').innerHTML='';renderRarityTool.touched=false;poolChanged();renderSelectedCards();renderPoolCards();toast('已按星级分配权重，请保存草稿并预览核对实际概率');
};
function poolVariantConfig(id) {
  const state=poolEditor.contents.get(id),entries=[...state.selected].sort((a,b)=>a[0]-b[0]);
  const shared={start_unix:unixInput('#pool-start'),end_unix:unixInput('#pool-end'),card_ids:entries.map(e=>e[0]),weights:entries.map(e=>e[1])};
  const fames=Object.fromEntries([...state.fames].filter(([id,f])=>f>1&&state.selected.has(id)).sort((a,b)=>a[0]-b[0]));
  if(!poolEditor.mixedByID.has(id)&&Object.keys(fames).length)shared.card_fames=fames;
  if(poolEditor.coverPath)shared.cover_path=poolEditor.coverPath;
  return shared;
}
function poolConfigs() {
  poolRememberActive();
  return poolEditor.variants.map(v=>{
    const s=poolEditor.strategies.get(v.gacha_id),m=poolEditor.mixedByID.get(v.gacha_id);
    const extra={card_num:v.base.card_num,banner_key:v.base.banner_key,...(poolEditor.legacyByID?.get(v.gacha_id)||{}),...(m?{reward_pool:m.steps.length?m.steps[0].reward_pool:m.reward_pool,steps:m.steps}:{})};
    const box=poolEditor.boxByID?.get(v.gacha_id);if(box)extra.box_rounds=box;
    if(!s.open)extra.closed=true;
    return {...extra,...poolVariantConfig(v.gacha_id),pay_type:s.pay_type,pay_typeid:s.pay_type===4?s.pay_typeid:0,gacha_id:v.gacha_id,name:s.name.trim(),price:m?.steps.length?m.steps[0].price:s.price,play_count_max:Number($('#pool-play-limit').value)};
  });
}
function poolProblems(configs) {
  const problems=[];clearInvalid($('#pool-editor'));
  const bad=(el,text)=>{problems.push(text);markInvalid(el,text)};
  const cell=(id,field)=>$(`#pool-strategies tr[data-variant="${id}"] [data-strategy="${field}"]`);
  const label=c=>configs.length>1?`抽法 ${c.gacha_id} 的`:'';
  const cards=new Map(poolEditor.cards.map(c=>[c.reward_type_id,c]));
  for(const c of configs){
    if(!c.name)bad(cell(c.gacha_id,'name'),`${label(c)}显示名称不能为空`);
    if(!poolEditor.mixedByID.get(c.gacha_id)?.steps?.length&&(!Number.isInteger(c.price)||c.price<1||c.price>10000000))bad(cell(c.gacha_id,'price'),`${label(c)}价格须为1–10000000的整数`);
    if(c.pay_type===4&&!c.pay_typeid)bad(cell(c.gacha_id,'pay_typeid'),`${label(c)}消耗道具未选择`);
    if(c.box_rounds?.length){
      const row=poolEditor.variants.find(v=>v.gacha_id===c.gacha_id);
      if(c.card_num!==row.base.card_num||!gachaBoxDrawCountAllowed(c.card_num)||c.play_count_max!==0)problems.push('箱池抽数须与入口一致（1–11 抽或 50 抽），累计次数不限');
      if(c.box_rounds.length!==11)problems.push('箱池须有 11 套模板');
      c.box_rounds.forEach((r,i)=>{if(!gachaBoxRoundReady(r))problems.push(`${i===10?'循环模板':`第 ${i+1} 轮`}须配置 1–${gachaBoxMaxRewards} 项奖励，库存为正整数且合计不超过 ${num(gachaBoxMaxStock)} 份`);});
    }else if(!poolEditor.mixedByID.has(c.gacha_id)){
      if(!c.card_ids.length)problems.push(`${label(c)}至少选择一张卡牌`);
      if(c.weights.some(w=>!Number.isInteger(w)||w<1||w>1000000))problems.push(`${label(c)}权重须为1–1000000的整数`);
      if(Object.entries(c.card_fames||{}).some(([id,f])=>!Number.isInteger(f)||f<1||f>(cards.get(Number(id))?.fame_max||1)))problems.push(`${label(c)}抽出名声超出对应卡牌上限`);
      const row=poolEditor.variants.find(v=>v.gacha_id===c.gacha_id);
      if(c.card_ids.some(id=>!poolCardEligibleFor(cards.get(id),row)))problems.push(`${label(c)}有卡牌不符合职业或星级限制`);
    }
  }
  if(configs.every(c=>c.closed))bad($('#pool-strategies [data-strategy="open"]'),'至少开放一种抽法；要关闭整个卡池请在「扭蛋发布」操作');
  if(configs[0].start_unix&&configs[0].end_unix&&configs[0].end_unix<=configs[0].start_unix)bad($('#pool-end'),'结束时间须晚于开始时间');
  const limit=configs[0].play_count_max;
  if(!Number.isInteger(limit)||limit<0||limit>1000000)bad($('#pool-play-limit'),'整池累计次数须为0–1000000，0表示不限');
  {
    const mixed=[...poolEditor.mixedByID.values()],pools=mixed.flatMap(m=>[m.reward_pool,...m.steps.map(s=>s.reward_pool)]).filter(Boolean);
    if(pools.some(pool=>pool.some(r=>!Number.isInteger(r.weight)||r.weight<1||r.weight>1000000)))problems.push('混合奖励权重须为1–1000000的整数');
    if(mixed.some(m=>m.steps.some(s=>!Number.isInteger(s.price)||s.price<1||s.price>10000000)))problems.push('各阶段价格须为1–10000000的整数');
    if(mixed.some(m=>m.steps.some(s=>s.pay_type===4&&!s.pay_typeid)))problems.push('阶段消耗道具未选择');
  }
  return problems;
}
// Preview of the whole pool: shared facts once, then each draw method's cost and odds.
// Preview of the whole pool, shown both in the dialog and the inline panel: cover and pool-wide facts once,
// then each draw method's cost, schedule and contents, warnings, blocked cards, and per-method odds.
function renderPoolPreview(previews) {
  const list=poolEditor.variants.map(v=>previews[v.gacha_id]).filter(Boolean),c=list[0].config,ready=list.every(p=>p.publishable);
  const blocked=[...new Set(list.flatMap(p=>p.blocked_card_ids||[]))],warnings=[...new Set(list.flatMap(p=>p.warnings||[]))];
  poolEditor.preview={publishable:ready,blocked_card_ids:blocked,config:c,previews};
  const names=new Map(poolEditor.cards.map(card=>[card.reward_type_id,card.name]));
  const when=p=>p.config.start_unix||p.config.end_unix?`${describeTime(p.config.start_unix)} → ${describeTime(p.config.end_unix)}`:'不限';
  const contents=p=>{if(p.config.box_rounds?.length)return '箱池 · 前 10 轮 + 无限循环 · 各轮库存独立配置';const fames=Object.keys(p.config.card_fames||{}).length;return p.config.card_ids?.length?`${num(p.config.card_ids.length)} 张卡牌${fames?` · ${fames} 张卡牌自定义名声`:''}`:`${num((p.config.steps?.[0]?.reward_pool||p.config.reward_pool||[]).length)} 项奖励${p.config.steps?.length?` · ${p.config.steps.length} 个阶段`:''}`};
  const gifts=p=>(p.config.gift_rules||p.base.gift_rules||[]).map(g=>`<p class="hint">第 ${g.from_play} 至 ${g.to_play||'以后'} 次赠送：${g.rewards.map(r=>`${esc(rewardLabel(r))} × ${r.num}`).join('、')}</p>`).join('');
  const odds=p=>(p.stages.length?p.stages:[{draw_count:p.base.card_num,card_ids:p.config.card_ids,odds_scaled:p.odds_scaled}]).map(stage=>`<details><summary>${p.base.card_num} 抽 · ${esc(poolProfessionName(p.base.arthur_type))} <code>${p.config.gacha_id}</code> · ${esc(p.config.steps?.length||p.config.box_rounds?.length?stage.name:paymentName(p.base))} · ${stage.draw_count} 抽 · ${(stage.card_ids||stage.rewards||[]).length} 项 · 展开概率</summary><div class="table-wrap" style="max-height:280px"><table><thead><tr><th>卡牌／奖励</th><th class="num">单次概率</th></tr></thead><tbody>${(stage.rewards||stage.card_ids||[]).map((r,i)=>`<tr><td>${esc(typeof r==='number'?(names.get(r)||r):rewardLabel(r))}${typeof r==='number'?'':` × ${r.num}`}${stage.stocks?` · 库存 ${stage.stocks[i]} 份`:''}${typeof r==='number'&&p.config.card_fames?.[r]>1?` <span class="tag info">名声 ${p.config.card_fames[r]}</span>`:''}</td><td class="num">${(stage.odds_scaled[i]/p.odds_scale).toFixed(5)}%</td></tr>`).join('')}</tbody></table></div></details>`).join('')+gifts(p);
  const html=`<div class="preview-head"><div class="gacha-art preview-cover">${image(poolCoverURL(c.cover_path),c.name)}</div><div><h3>${esc(c.name)} ${ready?'<span class="tag ok">可发布</span>':'<span class="tag bad">不能发布</span>'}</h3><p class="hint">${list.length} 个分池一起保存和发布；开放由「扭蛋发布」控制，并须处于排期内。</p></div></div>`+
    `<div class="facts"><div class="fact"><span>封面</span><b>${c.cover_path?'自定义封面':'包内封面'}</b></div><div class="fact"><span>排期</span><b>${esc(when(list[0]))}</b></div><div class="fact"><span>累计限抽</span><b>${c.play_count_max?`每人 ${num(c.play_count_max)} 次（单抽或连抽都计一次）`:'不限'}</b></div></div>`+
    `<div class="table-wrap"><table class="dense preview-variants"><thead><tr><th>分池</th><th>显示名称</th><th>费用</th><th>内容</th><th>状态</th></tr></thead><tbody>${list.map(p=>`<tr class="${p.config.closed?'closed':''}"><td><b>${p.base.card_num} 抽 · ${esc(poolProfessionName(p.base.arthur_type))}</b><span class="sub"><code>${p.config.gacha_id}</code></span></td><td>${esc(p.config.name)}</td><td>${esc(paymentName(p.base))} × ${num(p.config.price)}${p.base.daily_first_free?' · 每日首次免费':''}</td><td>${contents(p)}</td><td>${p.config.closed?'<span class="tag">关闭</span> ':''}${p.publishable?'<span class="tag ok">可发布</span>':'<span class="tag bad">不能发布</span>'}</td></tr>`).join('')}</tbody></table></div>`+
    `${warnings.length?`<div class="pool-warning">${warnings.map(esc).join('<br>')}</div>`:''}${blocked.length?`<div class="pool-blocked">未闭包卡牌（资源缺失，不能发布）：${blocked.map(id=>`${esc(names.get(id)||id)}（${id}）`).join('、')} <button class="secondary sm" type="button" data-remove-blocked>从各分池草稿移除这 ${blocked.length} 张</button></div>`:''}`+
    `${list.map(odds).join('')}<p class="hint">以上为本地运营配置概率。</p>`;
  $('#pool-preview').innerHTML=$('#pool-preview-modal-body').innerHTML=html;
  updatePoolControls();
}
$('#pool-select').onchange=()=>{if(!$('#pool-select').value)return;if(policyBusy('pool-editor')){$('#pool-select').value=poolEditor.row?.gacha_id||'';return}if(poolEditor.dirty&&!confirm('切换卡池将放弃未保存的编辑，是否继续？')){$('#pool-select').value=poolEditor.row.gacha_id;return}selectPool()};
['#pool-start','#pool-end','#pool-play-limit'].forEach(id=>$(id).oninput=()=>{$(id).classList.remove('invalid');poolChanged()});
['#pool-search','#pool-rarity','#pool-ready','#pool-eligible-only'].forEach(id=>$(id).oninput=()=>{poolEditor.page=0;poolEditor.selectedPage=0;renderPoolCards();renderSelectedCards()});
$$('#pool-jobs button').forEach(b=>b.onclick=()=>{poolEditor.job=Number(b.dataset.job);poolEditor.page=0;poolEditor.selectedPage=0;renderPoolCards();renderSelectedCards()});
$$('#pool-sources button').forEach(b=>b.onclick=()=>{poolEditor.source=b.dataset.source;poolEditor.page=0;poolEditor.selectedPage=0;renderPoolCards();renderSelectedCards()});
$('#pool-prev').onclick=()=>{poolEditor.page--;renderPoolCards()}; $('#pool-next').onclick=()=>{poolEditor.page++;renderPoolCards()};
$('#pool-add-matches').onclick=()=>{const add=poolEditor.cards.filter(card=>poolMatches(card)&&poolCardEligible(card)&&!poolEditor.selected.has(card.reward_type_id));if(!add.length)return toast('当前筛选结果中没有可加入的新卡牌');if(poolEditor.selected.size+add.length>6000)return toast(`加入后共 ${poolEditor.selected.size+add.length} 张，超过每个卡池 6000 张上限；请缩小筛选范围`,true);if(!confirm(`把当前筛选结果中 ${add.length} 张可用卡牌加入草稿（权重 1）？`))return;add.forEach(card=>poolEditor.selected.set(card.reward_type_id,1));poolChanged();renderPoolCards();renderSelectedCards()};
$('#pool-clear').onclick=()=>{if(confirm(`清空当前草稿中的全部 ${poolEditor.selected.size} 张卡牌？`)){poolEditor.selected.clear();poolChanged();renderPoolCards();renderSelectedCards()}};
$('#pool-weight-all').onclick=()=>{const input=$('#pool-batch-weight'),weight=Number(input.value);input.classList.remove('invalid');if(!Number.isInteger(weight)||weight<1||weight>1000000){markInvalid(input);return toast('权重须为 1–1000000 的整数',true)}const ids=new Set(poolEditor.cards.filter(poolMatches).map(card=>card.reward_type_id)),hit=[...poolEditor.selected.keys()].filter(id=>ids.has(id));if(!hit.length)return toast('当前筛选下没有已选卡牌');if(!confirm(`把当前筛选下的 ${hit.length} 张已选卡牌权重设为 ${weight}？`))return;for(const id of hit)poolEditor.selected.set(id,weight);poolChanged();renderSelectedCards();renderPoolCards()};
function setPoolFames(value){const ids=new Set(poolEditor.cards.filter(poolMatches).map(c=>c.reward_type_id)),cards=new Map(poolEditor.cards.map(c=>[c.reward_type_id,c])),hit=[...poolEditor.selected.keys()].filter(id=>ids.has(id));if(!hit.length)return toast('当前筛选下没有已选卡牌');if(!confirm(`把当前筛选下 ${hit.length} 张已选卡牌的抽出名声设为${value==='max'?'各自上限':` ${value}（超过上限的按上限）`}？`))return;for(const id of hit){const f=value==='max'?(cards.get(id)?.fame_max||1):Math.min(value,cards.get(id)?.fame_max||1);f>1?poolEditor.fames.set(id,f):poolEditor.fames.delete(id)}poolChanged();renderSelectedCards()}
$('#pool-fame-all').onclick=()=>{const input=$('#pool-batch-fame'),v=Number(input.value);input.classList.remove('invalid');if(!Number.isInteger(v)||v<1){markInvalid(input);return toast('名声须为正整数',true)}setPoolFames(v)};
$('#pool-fame-max').onclick=()=>setPoolFames('max');
$('#pool-export').onclick=()=>{const url=URL.createObjectURL(new Blob([JSON.stringify({group_id:poolEditor.row.base.groupid,configs:poolConfigs()},null,2)],{type:'application/json'}));const link=document.createElement('a');link.href=url;link.download=`gacha-pool-${poolEditor.row.base.groupid}.json`;link.click();setTimeout(()=>URL.revokeObjectURL(url),1000)};
// Import accepts a whole-pool export ({group_id, configs}) or an older single-variant file of this pool.
$('#pool-import').onchange=event=>runPoolEdit(async()=>{
  try {
    const file=event.target.files[0];if(!file)return;
    if(file.size>8*1024*1024)throw new Error('配置文件超过 8 MiB');
    let data;try{data=JSON.parse(await file.text())}catch{throw new Error('配置文件不是有效的 JSON')}
    const ids=new Set(poolEditor.variants.map(v=>v.gacha_id)),configs=Array.isArray(data?.configs)?data.configs:[data];
    if(!configs.length||configs.some(c=>!ids.has(c?.gacha_id))||new Set(configs.map(c=>c.gacha_id)).size!==configs.length||(Array.isArray(data?.configs)&&(data.group_id!==poolEditor.row.base.groupid||configs.length!==ids.size)))throw new Error('导入身份须与当前卡池的分池一致');
    for(const c of configs){
      if(!Array.isArray(c.card_ids)||!Array.isArray(c.weights)||c.card_ids.length!==c.weights.length||c.card_ids.length>6000||new Set(c.card_ids).size!==c.card_ids.length||c.card_ids.some(id=>!Number.isInteger(id)||id<=0)||c.weights.some(w=>!Number.isInteger(w)||w<1||w>1000000))throw new Error(`分池 ${c.gacha_id} 的卡牌／权重无效`);
      if(typeof c.name!=='string'||!Number.isInteger(c.price)||(c.pay_type!=null&&![0,2,3,4,6].includes(c.pay_type)))throw new Error('名称、价格或消耗方式无效');
      if(c.card_fames!=null&&(typeof c.card_fames!=='object'||Object.entries(c.card_fames).some(([id,f])=>!c.card_ids.includes(Number(id))||!Number.isInteger(f)||f<1)))throw new Error('抽出名声设置无效');
      localTimeInput(c.start_unix);localTimeInput(c.end_unix);
      if(poolEditor.boxByID?.has(c.gacha_id)&&(!Array.isArray(c.box_rounds)||c.box_rounds.length!==11||c.box_rounds.some(r=>!Array.isArray(r.rewards))))throw new Error('箱池导入须包含 11 套奖励模板');
    }
    const first=configs[0],boxConfigs=configs.filter(c=>poolEditor.boxByID?.has(c.gacha_id));
    if(boxConfigs.some(c=>JSON.stringify(c.box_rounds)!==JSON.stringify(boxConfigs[0].box_rounds)))throw new Error('无限池所有抽法共用奖励与轮次，导入的箱池模板必须一致');
    if(boxConfigs.some(c=>c.card_num!==poolEditor.variants.find(v=>v.gacha_id===c.gacha_id).base.card_num))throw new Error('箱池抽数不能通过导入改变，请使用新增分池');
    if(first.cover_path&& !poolCoverPattern.test(first.cover_path))throw new Error('封面路径无效');
    if(!confirm(`以导入内容替换 ${configs.length} 个分池草稿？${boxConfigs.length?'奖励和轮次同步到全部抽法，消耗分别载入':'卡牌、权重和消耗分别载入'}，封面、排期及整池次数使用第一项。`))return;
    poolEditor.coverPath=first.cover_path||'';$('#pool-play-limit').value=first.play_count_max||0;
    $('#pool-start').value=localTimeInput(first.start_unix);$('#pool-end').value=localTimeInput(first.end_unix);$('#pool-schedule-note').hidden=true;
    for(const c of configs){
      const base=poolEditor.variants.find(v=>v.gacha_id===c.gacha_id).base,type=c.pay_type||base.pay_type;
      poolEditor.contents.set(c.gacha_id,poolVariantState(c));
      if(poolEditor.legacyByID.has(c.gacha_id))poolEditor.legacyByID.set(c.gacha_id,{card_num:c.card_num||base.card_num,banner_key:c.banner_key||base.banner_key,gift_rules:structuredClone(c.gift_rules||base.gift_rules||[])});
      poolEditor.strategies.set(c.gacha_id,{open:!c.closed,name:c.name,pay_type:type,pay_typeid:type===4?(c.pay_type?c.pay_typeid:base.pay_typeid)||0:0,price:c.price});
      if(poolEditor.mixedByID.has(c.gacha_id)&&Array.isArray(c.reward_pool))poolEditor.mixedByID.set(c.gacha_id,structuredClone({reward_pool:c.reward_pool,steps:c.steps||[]}));
    }
    if(boxConfigs.length)setPoolBoxRounds(boxConfigs[0].box_rounds);
    selectPoolVariant(poolEditor.activeID,false);renderStrategies();renderPoolCover();poolChanged();toast('已载入草稿，请保存并预览');
  }finally{event.target.value=''}
},'导入');
$('#pool-save-preview').onclick=()=>runPoolEdit(async()=>{
  const rows=poolEditor.variants,serial=poolEditor.editSerial,configs=poolConfigs(),problems=poolProblems(configs);
  if(problems.length){showViewAlert('pool-editor',{title:`不能保存草稿：${problems.length} 处需要修正`,message:problems.join('；')});return}
  const result=await api('/api/gacha-editor/group/draft',{method:'POST',body:JSON.stringify({group_id:rows[0].base.groupid,configs,expected:Object.fromEntries(rows.map(r=>[r.gacha_id,{draft_revision:r.draft.revision}]))})});
  for(const r of rows)r.draft=result.documents[r.gacha_id];
  clearViewAlert('pool-editor');
  if(poolEditor.variants===rows&&poolEditor.editSerial===serial){
    poolEditor.dirty=false;renderPoolVersion();
    renderPoolPreview(result.previews);$('#pool-preview-modal').classList.add('open');
  }
  renderPoolSelect();state.loaded.delete('audit');
  if(!rows.every(r=>result.previews[r.gacha_id].publishable))toast('草稿已保存，但含未闭包卡牌，暂不能发布',true);
});
// Blocked cards cannot be published; remove them from the draft in one step, then save and preview again.
function removeBlockedPoolCards() {
  const ids=poolEditor.preview?.blocked_card_ids||[];if(!ids.length||policyBusy('pool-editor'))return;
  for(const state of poolEditor.contents.values())ids.forEach(id=>{state.selected.delete(id);state.fames.delete(id)});
  $('#pool-preview-modal').classList.remove('open');poolChanged();renderPoolCards();renderSelectedCards();
  toast(`已从草稿移除 ${ids.length} 张未闭包卡牌，请重新保存草稿并预览`);
}
['#pool-preview','#pool-preview-modal-body'].forEach(id=>$(id).addEventListener('click',e=>{if(e.target.closest('[data-remove-blocked]'))removeBlockedPoolCards()}));
$('#pool-publish').onclick=()=>publishPool(true);
$('#pool-preview-publish').onclick=()=>publishPool(false);
$('#pool-preview-close').onclick=()=>$('#pool-preview-modal').classList.remove('open');
function publishPool(ask){
  if(policyBusy('pool-editor')||!poolEditor.row||!poolEditor.preview?.publishable||poolEditor.dirty)return;
  if(poolPublished())return;
  const rows=poolEditor.variants,row=rows[0];
  if(ask&&!confirm(`发布已预览的「${poolEditor.preview.config.name}」？\n${rows.length>1?`${rows.length} 种抽法一起发布；`:''}草稿 v${row.draft.revision} 将替换已发布 v${row.live.revision}，玩家下次请求使用新配置。`))return;
  return runPoolEdit(async()=>{
    const result=await api('/api/gacha-editor/group/publish',{method:'POST',body:JSON.stringify({group_id:row.base.groupid,expected:Object.fromEntries(rows.map(r=>[r.gacha_id,{draft_revision:r.draft.revision,live_revision:r.live.revision,sha256:r.draft.sha256}]))})});
    for(const r of rows){r.live=result.documents[r.gacha_id];r.config=r.draft.payload;r.draft=result.drafts[r.gacha_id]}
    $('#pool-preview-modal').classList.remove('open');
    if(poolEditor.variants===rows){renderPoolVersion();poolEditor.preview=null;$('#pool-preview').innerHTML=`<p class="state-ok">已发布 v${row.live.revision}：${esc(row.config.name)}${rows.length>1?`（${rows.length} 种抽法）`:''}。玩家下次请求使用新配置。</p>`}
    renderPoolSelect();clearViewAlert('pool-editor');state.loaded.delete('audit');state.loaded.delete('gachas');
    if(poolEditor.openGroups&&!poolEditor.openGroups.has(row.base.groupid))toast('已发布配置。此卡池尚未在「扭蛋发布」开启，玩家仍看不到它',true);
    else toast(rows.length>1?`卡池已发布（${rows.length} 种抽法）`:'卡池配置已发布');
  },'发布');
}
// Discard removes the saved body and reloads the published config or template.
$('#pool-discard').onclick=()=>{
  const rows=poolEditor.variants,row=poolEditor.row;if(!row||policyBusy('pool-editor'))return;
  const pending=rows.filter(poolHasDraft),published=row.live.revision>0;
  if(!pending.length&&!poolEditor.dirty)return toast('当前没有需要放弃的草稿');
  if(!confirm(pending.length?`删除此卡池的已保存草稿${poolEditor.dirty?'和未保存的修改':''}？页面将载入${published?`已发布版本 v${row.live.revision}`:'模板默认配置；此卡池仍未发布'}。`:'放弃未保存的修改，恢复为已保存的内容？'))return;
  return runPoolEdit(async()=>{
    if(pending.length){const result=await api('/api/gacha-editor/group/discard',{method:'POST',body:JSON.stringify({group_id:row.base.groupid,expected:Object.fromEntries(rows.map(r=>[r.gacha_id,{draft_revision:r.draft.revision,live_revision:r.live.revision}]))})});for(const r of rows)r.draft=result.documents[r.gacha_id]}
    poolEditor.dirty=false;selectPool(row.gacha_id);renderPoolSelect();state.loaded.delete('audit');clearViewAlert('pool-editor');toast(pending.length?`已删除草稿内容，载入${published?'已发布版本':'模板默认配置（仍未发布）'}`:'已放弃未保存的修改');
  },'放弃草稿');
};

// Cover upload: the server stores the image by content hash; the draft references it until published.
$('#pool-cover-upload').onchange=event=>runPoolEdit(async()=>{
  try{
    const file=event.target.files[0];if(!file)return;
    if(!/^image\/(png|jpeg)$/.test(file.type))throw new Error('封面须为 PNG 或 JPEG 图片');
    if(file.size>2*1024*1024)throw new Error('封面文件超过 2 MiB');
    const data=await new Promise((resolve,reject)=>{const reader=new FileReader();reader.onload=()=>resolve(String(reader.result).split(',')[1]);reader.onerror=()=>reject(new Error('读取封面文件失败'));reader.readAsDataURL(file)});
    const result=await api('/api/gacha-covers',{method:'POST',body:JSON.stringify({data_base64:data})});
    poolEditor.coverPath=result.cover_path;poolChanged();renderPoolCover();
    const ratio=result.width/result.height;
    toast(`封面已上传（${result.width}×${result.height}）${Math.abs(ratio-934/372)>0.3?'，比例与建议的 934×372 不同，游戏内可能被裁切':''}；保存草稿并发布后生效`,Math.abs(ratio-934/372)>0.3);
  }finally{event.target.value=''}
},'上传封面');
$('#pool-cover-reset').onclick=()=>{if(!poolEditor.coverPath)return;poolEditor.coverPath='';poolChanged();renderPoolCover()};

// New pools select complete built-in rules; copying current live content remains a separate option.
function gachaBoxDrawCountAllowed(count){return Number.isInteger(count)&&((count>=1&&count<=11)||count===50)}
function poolVariantUnavailable(){
  if(!poolEditor.row)return '请先选择一个卡池';
  if(poolEditor.row.deleted)return '请先恢复此卡池';
  const box=poolEditor.variants.some(v=>v.base.box_rounds?.length);
  if(poolEditor.variants.some(v=>{const b=v.base;return !!b.box_rounds?.length!==box||b.reward_pool?.length||b.steps?.length||b.daily_first_free||b.unowned_only||b.user_select_max||b.gacha_type||b.category_num===10000||b.card_num!==b.card_num_max||(box?!gachaBoxDrawCountAllowed(b.card_num):b.card_num<1||b.card_num>11)}))return '此池使用特殊抽取规则，不能新增普通抽法。';
  return poolEditor.variants.some(v=>poolVariantCounts(v).length)?'':`此池的 ${box?'1–11 抽及 50 抽':'1–11 抽'}入口已齐全`;
}
function poolVariantCounts(source){
  const used=new Set(poolEditor.variants.filter(v=>source.base.box_rounds?.length||(v.base.arthur_type||0)===(source.base.arthur_type||0)).map(v=>poolSavedConfig(v).card_num||v.base.card_num));
  const counts=Array.from({length:11},(_,i)=>i+1);
  if(source.base.box_rounds?.length)counts.push(50);
  return counts.filter(n=>!used.has(n)&&n>(source.base.guaranteed_count||0));
}
function renderPoolVariantItems(){
  const field=$('#pool-variant-item-field'),select=$('#pool-variant-pay-item'),previous=Number(select.value),query=$('#pool-variant-item-search').value.trim();
  field.hidden=Number($('#pool-variant-pay-type').value)!==4;
  const items=(poolEditor.catalog||[]).filter(c=>c.reward_type===8&&c.resource_state!=='unavailable'&&(c.reward_type_id===previous||matchesWords(`${c.name} ${c.reward_type_id}`,query)));
  select.innerHTML='<option value="">请选择消耗道具</option>'+items.map(c=>`<option value="${c.reward_type_id}">${esc(c.name)} · ${c.reward_type_id}</option>`).join('');
  if(items.some(c=>c.reward_type_id===previous))select.value=previous;
}
function suggestPoolVariantPrice(){
  const source=poolEditor.variants.find(v=>v.gacha_id===Number($('#pool-variant-source').value));if(!source)return;
  const c=poolSavedConfig(source),count=c.card_num||source.base.card_num;
  $('#pool-variant-price').value=Math.max(1,Math.min(10000000,Math.round(c.price*Number($('#pool-variant-count').value)/count)));
}
function renderPoolVariantSource(){
  const source=poolEditor.variants.find(v=>v.gacha_id===Number($('#pool-variant-source').value));if(!source)return;
  const c=poolSavedConfig(source),counts=poolVariantCounts(source);
  $('#pool-variant-count').innerHTML=counts.map(n=>`<option value="${n}">${n===1?'单抽':`${n} 连抽`}</option>`).join('');
  $('#pool-variant-count').value=counts.includes(10)?10:counts[0]||'';
  $('#pool-variant-display-name').value=c.name;
  $('#pool-variant-pay-type').value=c.pay_type||source.base.pay_type;
  $('#pool-variant-item-search').value='';$('#pool-variant-pay-item').innerHTML=`<option value="${c.pay_typeid||source.base.pay_typeid||''}"></option>`;
  renderPoolVariantItems();suggestPoolVariantPrice();
  $('#pool-variant-rule-note').textContent=source.base.box_rounds?.length?'沿用本池已保存的全部奖励和轮次。新抽法与单抽共用箱子库存、轮次、封面和排期；一次连抽只扣所配置费用，抽空自动接下一轮。':'沿用来源分池已保存的卡牌、权重、名声与职业。封面、排期及累计限抽和整个卡池共用；新增分池先保存为草稿。';
  $('#pool-variant-create-note').textContent=`来源：${poolProfessionName(source.base.arthur_type)} · ${poolHasDraft(source)?'已保存草稿':'当前生效配置'}。费用按来源比例预填，请核对后调整；新分池发布前玩家看不到。`;
  $('#pool-variant-apply').disabled=!counts.length;
}
$('#pool-variant-new').onclick=()=>{
  if(policyBusy('pool-editor')||poolVariantUnavailable())return;
  if(poolEditor.dirty)return toast('请先保存草稿并预览，再新增分池；新分池会沿用已保存的内容',true);
  const sources=poolEditor.variants.filter(v=>poolVariantCounts(v).length),active=sources.find(v=>v.gacha_id===poolEditor.activeID)||sources[0];
  $('#pool-variant-group').textContent=`添加到「${poolEditor.row.config.name}」（组 ${poolEditor.row.base.groupid}）`;
  $('#pool-variant-source').innerHTML=sources.map(v=>`<option value="${v.gacha_id}">${esc(poolProfessionName(v.base.arthur_type))} · ${esc(poolVariantLabel(v))} · ${v.gacha_id}</option>`).join('');
  $('#pool-variant-source').value=active.gacha_id;clearInvalid($('#pool-variant-create-modal'));renderPoolVariantSource();
  $('#pool-variant-create-modal').classList.add('open');
};
$('#pool-variant-source').onchange=renderPoolVariantSource;
$('#pool-variant-count').onchange=suggestPoolVariantPrice;
$('#pool-variant-pay-type').onchange=renderPoolVariantItems;
$('#pool-variant-item-search').oninput=renderPoolVariantItems;
$('#pool-variant-cancel').onclick=()=>$('#pool-variant-create-modal').classList.remove('open');
$('#pool-variant-apply').onclick=()=>{
  const name=$('#pool-variant-display-name').value.trim(),price=Number($('#pool-variant-price').value),pay_type=Number($('#pool-variant-pay-type').value),pay_typeid=pay_type===4?Number($('#pool-variant-pay-item').value):0;
  const source_id=Number($('#pool-variant-source').value),card_num=Number($('#pool-variant-count').value);
  clearInvalid($('#pool-variant-create-modal'));
  if(!name||[...name].length>60)return markInvalid($('#pool-variant-display-name'),'显示名称须为 1–60 字');
  if(!Number.isInteger(price)||price<1||price>10000000)return markInvalid($('#pool-variant-price'),'费用须为 1–10000000 的整数');
  if(pay_type===4&&!pay_typeid)return markInvalid($('#pool-variant-pay-item'),'请选择消耗道具');
  if(!source_id||!card_num)return toast('请选择来源和可用的抽数',true);
  const expected=Object.fromEntries(poolEditor.variants.map(v=>[v.gacha_id,{draft_revision:v.draft.revision,live_revision:v.live.revision,sha256:v.draft.sha256||''}]));
  return runPoolTask(async()=>{
    let result;
    try{result=await api('/api/gacha-variants',{method:'POST',body:JSON.stringify({source_id,card_num,name,pay_type,pay_typeid,price,expected_revision:poolEditor.customRevision,expected})})}
    catch(e){if(e.status===409){$('#pool-variant-create-modal').classList.remove('open');await loadPoolEditor();toast('卡池刚被其他操作更新，已重新载入；请核对后再次新增',true);return}throw e}
    $('#pool-variant-create-modal').classList.remove('open');
    await loadPoolEditor(result.gacha_id);selectPoolVariant(result.gacha_id);
    state.loaded.delete('gachas');state.loaded.delete('audit');clearViewAlert('pool-editor');
    toast(`已新增 ${card_num} 抽分池草稿。核对奖励与费用，保存草稿并预览后发布整池`);
  },'新增分池');
};
function renderPoolCreateSummary() {
  const rule=$('#pool-create-mode').value==='rule',selected=Number($('#pool-create-source').value);
  const template=rule?poolEditor.ruleTemplates.find(t=>t.id===selected):null;
  const members=rule?(template?.gacha_ids||[]).map(id=>poolEditor.rows.find(r=>r.gacha_id===id)).filter(Boolean):poolGroupRows(poolEditor.rows.find(r=>r.gacha_id===selected));
  const seen=new Set(),allowed=rule?!!template?.profession_choices:members.length>0&&members.every(r=>{const b=r.base,k=`${b.card_num}:${b.card_num_max}`;if(b.gacha_type||b.arthur_type||b.user_select_max||b.category_num===10000||b.card_num!==b.card_num_max||b.daily_first_free||b.unowned_only||b.reward_pool?.length||seen.has(k))return false;seen.add(k);return true});
  $$('#pool-create-professions input').forEach(el=>{el.disabled=!allowed;if(!allowed)el.checked=false});
  $('#pool-create-profession-note').textContent=allowed?'创建后各职业分池独立编辑，单职业池自动保留对应职业和通用卡。':'此模板不支持职业选择：需普通固定抽数，且每种抽数只有一种消耗方式。';
  if($('#pool-create-mode').value==='rule'){
    const template=poolEditor.ruleTemplates.find(t=>t.id===Number($('#pool-create-source').value));
    $('#pool-create-summary').innerHTML=template?`<b>${esc(template.name)} · ${template.gacha_ids.length} 种抽法</b><span>初始内容与封面：${esc(template.source_name)}</span>${template.summary.map(line=>`<span>${esc(line)}</span>`).join('')}`:'没有可用的规则模板';
    return;
  }
  const source=poolEditor.rows.find(r=>r.gacha_id===Number($('#pool-create-source').value)),rows=poolGroupRows(source);
  $('#pool-create-summary').innerHTML=source?`<span>将复制 <b>${rows.length} 种抽法</b>，各自沿用当前生效的配置：</span>${rows.map(r=>`<span>· ${esc(poolVariantLabel(r))} · ${num(r.config.card_ids.length)} 张卡牌</span>`).join('')}`:'';
}
function renderPoolCreateSources(){
  const rule=$('#pool-create-mode').value==='rule';
  $('#pool-create-source-label').textContent=rule?'规则模板':'复制来源';
  $('#pool-create-note').textContent=rule?'规则模板使用内置默认内容，保留每日免费、阶段和附赠等完整机制。创建后可调整允许编辑的参数，抽数与特殊机制由模板决定。':'复制当前生效内容，不复制每日免费和赠礼；混合奖励、阶段及未入手限定池请使用规则模板。';
  if(rule){
    $('#pool-create-source').innerHTML=poolEditor.ruleTemplates.map(t=>`<option value="${t.id}">${esc(t.name)} — ${esc(t.source_name)}</option>`).join('');
    const current=poolEditor.ruleTemplates.find(t=>t.id===poolEditor.row?.base.groupid);
    if(current)$('#pool-create-source').value=current.id;
  }else{
    const seen=new Set(),sources=poolEditor.rows.filter(r=>{const g=r.base.groupid;if(seen.has(g))return false;seen.add(g);return poolGroupRows(r).every(poolCopyable)});
    const current=sources.find(r=>r.base.groupid===poolEditor.row?.base.groupid);
    $('#pool-create-source').innerHTML=sources.map(r=>`<option value="${r.gacha_id}">${esc(r.config.name)}${r.custom?'（后台新建）':''} · ${poolGroupRows(r).length} 种抽法</option>`).join('');
    if(current)$('#pool-create-source').value=current.gacha_id;
  }
  renderPoolCreateSummary();
}
$('#pool-create').onclick=()=>{
  if(policyBusy('pool-editor')||!poolEditor.rows.length)return;
  $('#pool-create-mode').value=poolEditor.ruleTemplates.length?'rule':'copy';
  $$('#pool-create-professions input').forEach(el=>el.checked=false);
  $('#pool-create-mode option[value="rule"]').disabled=!poolEditor.ruleTemplates.length;
  renderPoolCreateSources();syncPoolCreateMode();
  $('#pool-create-name').value='';clearInvalid($('#pool-create-modal'));renderPoolCreateSummary();
  $('#pool-create-modal').classList.add('open');setTimeout(()=>$('#pool-create-name').focus(),0);
};
// X and Esc route through the dismiss button, so it closes the dialog; nothing is sent.
$('#pool-create-cancel').onclick=()=>$('#pool-create-modal').classList.remove('open');
$('#pool-create-source').onchange=renderPoolCreateSummary;
$('#pool-create-mode').onchange=renderPoolCreateSources;
// The two mode cards drive the hidden mode select the creation logic reads.
function syncPoolCreateMode(){for(const r of $$('input[name="pool-create-mode-choice"]')){r.checked=r.value===$('#pool-create-mode').value;r.disabled=r.value==='rule'&&!poolEditor.ruleTemplates.length}}
$$('input[name="pool-create-mode-choice"]').forEach(r=>r.onchange=()=>{$('#pool-create-mode').value=r.value;renderPoolCreateSources()});
$('#pool-create-name').oninput=()=>$('#pool-create-name').classList.remove('invalid');
$('#pool-create-name').onkeydown=e=>{if(e.key==='Enter'){e.preventDefault();$('#pool-create-apply').click()}};
$('#pool-create-apply').onclick=()=>{
  const name=$('#pool-create-name').value.trim(),source=Number($('#pool-create-source').value);
  clearInvalid($('#pool-create-modal'));
  if(!name||[...name].length>60){markInvalid($('#pool-create-name'),'请填写 1–60 字的卡池名称');$('#pool-create-name').focus();return}
  if(!source)return toast('请选择可用的规则模板或复制来源',true);
  const selection=$('#pool-create-mode').value==='rule'?{rule_template_id:source}:{source_id:source};
  selection.professions=$$('#pool-create-professions input:checked').map(el=>Number(el.value));
  if(poolEditor.dirty&&!confirm('创建后会切换到新卡池，当前未保存的编辑将放弃。是否继续？'))return;
  return runPoolTask(async()=>{
    let result;
    try{result=await api('/api/gacha-pools',{method:'POST',body:JSON.stringify({...selection,name,expected_revision:poolEditor.customRevision})})}
    catch(e){if(e.status===409){await loadPoolEditor();toast('卡池列表刚被其他操作更新，已重新载入；请确认后再次创建',true);return}throw e}
    $('#pool-create-modal').classList.remove('open');
    poolEditor.dirty=false;$('#pool-name-search').value='';$('#pool-config-status').value='';
    await loadPoolEditor(result.gacha_ids[0]);
    state.loaded.delete('gachas');state.loaded.delete('audit');clearViewAlert('pool-editor');
    toast(`已创建「${name}」：${result.gacha_ids.length} 种抽法（${result.gacha_ids.join('、')}），均为草稿。编辑后保存草稿并发布，再到「扭蛋发布」开启`);
  },'新建卡池');
};
function renderPoolTrash(rows){
  $('#pool-trash-summary').textContent=`卡池回收站（${rows.length}）`;
  $('#pool-trash-rows').innerHTML=rows.map(row=>`<tr><td>${esc(row.config.name)}<span class="sub">组 ${row.base.groupid}</span></td><td>${poolGroupRows(row).map(r=>r.gacha_id).join('、')}</td><td>${row.live.revision?'有发布版':'从未发布'}${poolGroupRows(row).some(poolHasDraft)?' · 有草稿':''}</td><td><button class="secondary sm" data-trash-restore="${row.gacha_id}">恢复</button> <button class="danger sm" data-trash-purge="${row.base.groupid}">永久删除…</button></td></tr>`).join('')||emptyRow(4,'回收站为空');
}
$('#pool-trash-rows').onclick=event=>{const restore=event.target.closest('[data-trash-restore]'),purge=event.target.closest('[data-trash-purge]');if(restore)changePoolDeletion('restore',poolEditor.rows.find(r=>r.gacha_id===Number(restore.dataset.trashRestore)));if(purge)openGachaPurge(Number(purge.dataset.trashPurge))};
function changePoolDeletion(action,row=poolEditor.row) {
  if(!row?.custom||policyBusy('pool-editor'))return;
  const ids=poolGroupRows(row).map(r=>r.gacha_id).join('、');
  const question=action==='delete'
    ?`将卡池「${row.config.name}」移入回收站？\n同组抽法（${ids}）会立即对玩家隐藏，也不能在「扭蛋发布」开启。\n抽取次数、草稿和历史记录都会保留，可从回收站恢复。`
    :`恢复卡池「${row.config.name}」（${ids}）？\n恢复后按原有的配置和排期；若「扭蛋发布」中仍开启此卡池，玩家下次打开扭蛋页即可看到。`;
  if(!confirm(question+(poolEditor.dirty?'\n\n当前未保存的编辑将放弃。':'')))return;
  return runPoolTask(async()=>{
    try{await api(`/api/gacha-pools/${row.gacha_id}/${action}`,{method:'POST',body:JSON.stringify({expected_revision:poolEditor.customRevision})})}
    catch(e){if(e.status===409){await loadPoolEditor();toast('卡池列表刚被其他操作更新，已重新载入；请确认后重试',true);return}throw e}
    poolEditor.dirty=false;$('#pool-config-status').value=action==='delete'?'deleted':'';await loadPoolEditor(row.gacha_id);$('#pool-trash-panel').open=action==='delete';
    state.loaded.delete('gachas');state.loaded.delete('audit');clearViewAlert('pool-editor');
    toast(action==='delete'?`已移入回收站「${row.config.name}」`:`已恢复「${row.config.name}」`);
  },action==='delete'?'删除卡池':'恢复卡池');
}
$('#pool-delete').onclick=()=>changePoolDeletion('delete');
$('#pool-restore').onclick=()=>changePoolDeletion('restore');
// A pending reward job keeps the same immutable payload and keys across
// network errors and page reloads. Retry only unacknowledged recipients.

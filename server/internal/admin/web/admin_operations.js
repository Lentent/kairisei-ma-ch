'use strict';
const poolEditor = { rows: [], cards: [], selected: new Map(), row: null, page: 0, selectedPage: 0, source: '', job: 0, dirty: false, preview: null, editSerial: 0 };
const localTimeInput = unix => unix ? new Date(unix*1000-new Date(unix*1000).getTimezoneOffset()*60000).toISOString().slice(0,16) : '';
const unixInput = id => $(id).value ? Math.floor(new Date($(id).value).getTime()/1000) : 0;
const describeTime = unix => unix ? new Date(unix*1000).toLocaleString('zh-CN') : '不限';
const paymentName = profile => {
  if(profile.pay_type===4){const item=poolEditor.catalog?.find(c=>c.reward_type===8&&c.reward_type_id===profile.pay_typeid);return item?`${item.name}（道具 ${profile.pay_typeid}）`:`道具 ${profile.pay_typeid}`;}
  return ({2:'友情点',3:'水晶（先免费后付费）',6:'付费水晶'}[profile.pay_type] || `类型 ${profile.pay_type}`);
};

function updatePoolControls() {
  const busy=policyBusy('pool-editor')||!poolEditor.row;
  $('#pool-save-preview').disabled=busy;
  $('#pool-new').disabled=policyBusy('pool-editor');$('#pool-clone').disabled=busy;
  $('#pool-export').disabled=busy;$('#pool-import').disabled=busy;
  $('#pool-publish').disabled=busy||poolEditor.dirty||!poolEditor.preview?.publishable||
    (!!poolEditor.row?.live.sha256&&poolEditor.row.live.sha256===poolEditor.row.draft.sha256);
}
async function runPoolEdit(action) {
  if(policyBusy('pool-editor')||!poolEditor.row)return;
  state.publishing.add('pool-editor');updatePolicyControls();
  try{await action()}catch(e){toast(e.message,true)}
  finally{state.publishing.delete('pool-editor');updatePolicyControls()}
}

async function loadPoolEditor() {
  const data = await api('/api/gacha-editor');
  let cards=poolEditor.catalog||[];
  if (!cards.length) {
    cards = [];
    for (let offset=0;;) {
      const page = await api(`/api/catalog?limit=200&offset=${offset}`);
      cards.push(...page.entries); offset += page.entries.length;
      if (offset >= page.total) break;
      if (!page.entries.length) throw new Error('卡牌目录分页中断，请刷新重试');
    }
  }
  const previous = poolEditor.row?.gacha_id;
  poolEditor.banners=data.banners||[];poolEditor.rows=data.pools;poolEditor.catalog=cards;poolEditor.cards=cards.filter(c=>c.kind==='card');
  renderPoolSelect();
  selectPool(data.pools.some(row=>row.gacha_id===previous)?previous:Number($('#pool-select').value));
}

function selectPool(id=Number($('#pool-select').value)) {
  if(poolEditor.row?.gacha_id!==id)$('#pool-cost').value='';
  poolEditor.editSerial++;
  poolEditor.row = poolEditor.rows.find(row=>row.gacha_id===id);
  const row=poolEditor.row;
  if (!row) {poolEditor.preview=null;$('#pool-current').textContent='请先选择一个卡池';updatePoolControls();return;}
  $('#pool-select').value=id;$('#pool-current').textContent=`正在编辑：${row.config.name} · ${id} · 分组 ${row.base.groupid}（${poolEditor.rows.filter(r=>r.base.groupid===row.base.groupid).length} 个入口）`;
  const config = row.draft.payload || row.config;
  $('#pool-name').value=config.name; $('#pool-price').value=config.price;
  $('#pool-start').value=localTimeInput(config.start_unix); $('#pool-end').value=localTimeInput(config.end_unix);
  poolEditor.selected=new Map((config.card_ids||[]).map((id,i)=>[id,config.weights[i]]));
  poolEditor.dirty=false; poolEditor.preview=null; $('#pool-publish').disabled=true;
  $('#pool-preview').textContent='编辑后保存草稿并预览，核对费用、概率和排期，再发布。';
  $('#pool-version').textContent=`草稿 v${row.draft.revision} · 已发布 v${row.live.revision}`;
  $('#pool-contract').textContent=`${paymentName(row.base)} · 每次 ${row.base.card_num} 抽 · 最多 ${row.base.card_num_max} 抽${row.base.user_select_max ? ` · 自选 ${row.base.user_select_max} 张` : ''} · 需在「扭蛋发布」启用该组`;
  setPoolPayment(config);
  setupCustomPool(config);setupMixedPool(config); renderPoolCards(); renderSelectedCards();updatePoolControls();
}

function poolPaymentProfile() {
  const pay_type=Number($('#pool-payment').value)||3;
  return {...poolEditor.row?.base,pay_type,pay_typeid:pay_type===4?Number($('#pool-payment-item').value):0};
}
function renderPoolPaymentItems(selected=$('#pool-payment-item').value) {
  const query=$('#pool-payment-search').value;
  const items=(poolEditor.catalog||[]).filter(c=>c.reward_type===8&&c.resource_state!=='unavailable'&&(String(c.reward_type_id)===String(selected)||matchesWords(`${c.name} ${c.reward_type_id}`,query)));
  $('#pool-payment-item').innerHTML='<option value="">请选择消耗道具</option>'+items.map(c=>`<option value="${c.reward_type_id}">${esc(c.name)} · ${c.reward_type_id}</option>`).join('');
  $('#pool-payment-item').value=String(selected||'');
}
function updatePoolPayment() {
  const p=poolPaymentProfile(),name=p.pay_type===4&&!p.pay_typeid?'请选择道具':paymentName(p);
  $('#pool-payment-item-field').hidden=p.pay_type!==4;
  $('#pool-price-label').textContent=`价格（${name}）`;
  $('#pool-stage-price-label').textContent=`本阶段价格（${name}）`;
  const max=p.pay_type!==2&&poolEditor.row.base.pay_type===2?p.card_num:p.card_num_max;
  $('#pool-contract').textContent=`${name} · 每次 ${p.card_num} 抽 · 最多 ${max} 抽${p.user_select_max?` · 自选 ${p.user_select_max} 张`:''} · 开放由「扭蛋发布」统一控制`;
}
function setPoolPayment(config) {
  const p=config.pay_type?config:poolEditor.row.base;
  $('#pool-payment').value=String(p.pay_type||3);
  $('#pool-payment-search').value='';renderPoolPaymentItems(p.pay_typeid);updatePoolPayment();
}
$('#pool-payment-search').oninput=()=>renderPoolPaymentItems();
$('#pool-payment').onchange=$('#pool-payment-item').onchange=()=>{updatePoolPayment();poolChanged()};
function poolChanged() { poolEditor.editSerial++; poolEditor.dirty=true; poolEditor.preview=null; $('#pool-publish').disabled=true; $('#pool-version').textContent='有未保存的修改'; }
function rewardLabel(r) {return poolEditor.catalog?.find(c=>c.reward_type===r.type&&c.reward_type_id===r.reward_typeid)?.name || `${r.type}:${r.reward_typeid}`;}
function setupMixedPool(config) {
  const mixed=!!poolEditor.row?.base.reward_pool?.length;
  poolEditor.mixed=mixed?structuredClone({reward_pool:config.reward_pool||poolEditor.row.base.reward_pool,steps:config.steps||poolEditor.row.base.steps||[]}):null;
  $('#pool-custom-mixed').hidden=!poolEditor.row?.custom;
  if(mixed&&poolEditor.row.custom&&poolEditor.mixed.reward_pool.length===1&&!poolEditor.mixed.reward_pool[0].reward.type)poolEditor.mixed.reward_pool=[];
  $('#pool-card-editor').style.display=mixed?'none':'';$('#pool-mixed-editor').style.display=mixed?'':'none';
  $('#pool-price').disabled=mixed&&!!poolEditor.mixed.steps.length;
  poolEditor.mixedPage=0;
  if(!mixed)return;
  $('#pool-stage').innerHTML=(poolEditor.mixed.steps.length?poolEditor.mixed.steps:[{}]).map((_,i)=>`<option value="${i}">第${i+1}阶段</option>`).join('');
  $('#pool-mixed-search').value='';
  $('#pool-mixed-gifts').hidden=!!poolEditor.row.custom;
  $('#pool-mixed-gifts').textContent=(poolEditor.row.base.gift_rules||[]).map(g=>`第${g.from_play}至${g.to_play||'以后'}次赠送：${g.rewards.map(r=>`${rewardLabel(r)} × ${r.num}`).join('、')}`).join('；')||'无额外赠礼';
  renderMixedPool();
}
function renderMixedPool() {
  if(!poolEditor.mixed)return;
  const step=poolEditor.mixed.steps[Number($('#pool-stage').value)],pool=step?.reward_pool||poolEditor.mixed.reward_pool;
  $('#pool-stage-price').value=step?.price||$('#pool-price').value;$('#pool-stage-price').disabled=!step;
  const query=$('#pool-mixed-search').value.trim().toLowerCase(), rows=pool.map((r,i)=>({...r,index:i})).filter(r=>`${rewardLabel(r.reward)} ${r.reward.reward_typeid}`.toLowerCase().includes(query));
  const pages=Math.max(1,Math.ceil(rows.length/100));poolEditor.mixedPage=Math.max(0,Math.min(poolEditor.mixedPage,pages-1));
  $('#pool-mixed-page').textContent=`${rows.length}项 · ${poolEditor.mixedPage+1}/${pages}页`;
  $('#pool-mixed-prev').disabled=poolEditor.mixedPage===0;$('#pool-mixed-next').disabled=poolEditor.mixedPage===pages-1;
  $('#pool-mixed-rows').innerHTML=rows.slice(poolEditor.mixedPage*100,(poolEditor.mixedPage+1)*100).map(r=>`<tr><td>${esc(rewardLabel(r.reward))}</td><td>${poolEditor.row.custom?`<input type="number" min="1" max="9999" value="${r.reward.num}" data-mixed-num="${r.index}" aria-label="奖励数量">`:r.reward.num}</td><td><input type="number" min="1" max="1000000" value="${r.weight}" data-mixed-weight="${r.index}" aria-label="奖励权重"></td><td>${poolEditor.row.custom?`<button class="secondary" data-mixed-remove="${r.index}">移除</button>`:""}</td></tr>`).join('');
  $$('#pool-mixed-rows [data-mixed-num]').forEach(input=>input.oninput=()=>{pool[Number(input.dataset.mixedNum)].reward.num=Number(input.value);poolChanged()});
  $$('#pool-mixed-rows [data-mixed-remove]').forEach(button=>button.onclick=()=>{pool.splice(Number(button.dataset.mixedRemove),1);renderMixedPool();poolChanged()});
  $$('#pool-mixed-rows [data-mixed-weight]').forEach(input=>input.oninput=()=>{pool[Number(input.dataset.mixedWeight)].weight=Number(input.value);poolChanged()});
}
$('#pool-stage').onchange=()=>{poolEditor.mixedPage=0;renderMixedPool()};
$('#pool-stage-price').oninput=()=>{const i=Number($('#pool-stage').value);poolEditor.mixed.steps[i].price=Number($('#pool-stage-price').value);if(!i)$('#pool-price').value=$('#pool-stage-price').value;poolChanged()};
$('#pool-mixed-search').oninput=()=>{poolEditor.mixedPage=0;renderMixedPool()};
$('#pool-mixed-prev').onclick=()=>{poolEditor.mixedPage--;renderMixedPool()};
$('#pool-mixed-next').onclick=()=>{poolEditor.mixedPage++;renderMixedPool()};
function poolMatches(card) {
  const q=$('#pool-search').value.trim().toLowerCase(), rarity=Number($('#pool-rarity').value);
  return matchesCardFilters(card,'pool') && cardMatchesJob(card,poolEditor.job) && (!poolEditor.source || (card.source_tags||[]).includes(poolEditor.source)) && matchesWords(`${card.name} ${card.reward_type_id} ${card.pict_id} ${cardFacts(card)}`,q) && (!rarity || card.rarity===rarity) && (!$('#pool-ready').checked || card.resource_state!=='unavailable');
}
function poolCardEligible(card) { const base=poolEditor.row?.base; if(base?.unowned_only && card.rarity!==6) return false; return base?.pay_type===2 || card.gacha_eligible || (!card.detail && card.rarity===6 && (base?.banner_key||'').startsWith('lucky_bag_') && base.cardids.includes(card.reward_type_id)); }
function renderPoolCards() {
  $$('#pool-jobs button').forEach(b=>b.classList.toggle('active',Number(b.dataset.job)===poolEditor.job));
  $$('#pool-sources button').forEach(b=>b.classList.toggle('active',b.dataset.source===poolEditor.source));
  const matches=poolEditor.cards.filter(poolMatches), pages=Math.max(1,Math.ceil(matches.length/48));
  poolEditor.page=Math.max(0,Math.min(poolEditor.page,pages-1));
  $('#pool-card-count').textContent=`目录 ${poolEditor.cards.length} 张 · 匹配 ${matches.length} 张 · ${poolEditor.page+1}/${pages} 页`;
  $('#pool-prev').disabled=poolEditor.page===0; $('#pool-next').disabled=poolEditor.page>=pages-1;
  $('#pool-cards').innerHTML=matches.slice(poolEditor.page*48,(poolEditor.page+1)*48).map(card=>`<button class="catalog-card ${poolEditor.selected.has(card.reward_type_id)?'selected':''}" data-card="${card.reward_type_id}" ${!poolEditor.selected.has(card.reward_type_id)&&!poolCardEligible(card)?'disabled title="此卡不是扭蛋来源的初始形态，可在赠礼目录选择"':''}><div class="thumb">${image(card.image_url,card.name)}</div><div class="catalog-copy"><b class="name">${esc(card.name)}</b><span class="sub">${card.reward_type_id} · ${cardStars(card)} · ${cardJobName(card.arthur_type)}</span><span class="sub">${esc(cardSourceDescription(card))}</span><span class="sub">${card.resource_state==='unavailable'?'运行资源待补齐':card.image_url?'资源可用':'资源可用 · 无后台缩略图'}</span></div></button>`).join('');
  $$('#pool-cards [data-card]').forEach(button=>button.onclick=()=>{const id=Number(button.dataset.card);poolEditor.selected.has(id)?poolEditor.selected.delete(id):poolEditor.selected.set(id,1);poolChanged();renderPoolCards();renderSelectedCards()});
}
function renderSelectedCards() {
  const cards=new Map(poolEditor.cards.map(card=>[card.reward_type_id,card]));
  const rows=[...poolEditor.selected].filter(([id])=>!cards.has(id)||poolMatches(cards.get(id)));
  $('#pool-selected-count').textContent=`已选 ${poolEditor.selected.size} 张 · 当前筛选 ${rows.length} 张`;
  poolEditor.selectedPage=renderPager('pool-selected',poolEditor.selectedPage,rows.length,100,page=>{poolEditor.selectedPage=page;renderSelectedCards()});
  $('#pool-selected').innerHTML=rows.slice(poolEditor.selectedPage*100,(poolEditor.selectedPage+1)*100).map(([id,weight])=>`<tr><td>${id}</td><td>${esc(cards.get(id)?.name||'未知卡牌')}</td><td><input aria-label="${id} 权重" data-weight="${id}" type="number" min="1" max="1000000" value="${esc(weight)}" style="width:110px"></td><td><button class="secondary" data-remove="${id}">移除</button></td></tr>`).join('') || '<tr><td colspan="4" class="empty">还没有选择卡牌</td></tr>';
  $('#pool-selected-hint').textContent='翻页保留已选卡牌与权重，导出包含全部已选卡牌。';
  $$('#pool-selected [data-weight]').forEach(input=>input.oninput=()=>{poolEditor.selected.set(Number(input.dataset.weight),Number(input.value));poolChanged()});
  $$('#pool-selected [data-remove]').forEach(button=>button.onclick=()=>{poolEditor.selected.delete(Number(button.dataset.remove));poolChanged();renderPoolCards();renderSelectedCards()});
}
function poolConfig() {
  const entries=[...poolEditor.selected].sort((a,b)=>a[0]-b[0]);
  const extra=poolEditor.mixed ? {reward_pool:poolEditor.mixed.steps?.length?poolEditor.mixed.steps[0].reward_pool:poolEditor.mixed.reward_pool,steps:poolEditor.mixed.steps||[]} : {};
  const payment=poolPaymentProfile();
  return {...extra,...customPoolConfig(),pay_type:payment.pay_type,pay_typeid:payment.pay_typeid,gacha_id:poolEditor.row.gacha_id,name:$('#pool-name').value.trim(),price:poolEditor.mixed?.steps?.length?poolEditor.mixed.steps[0].price:Number($('#pool-price').value),start_unix:unixInput('#pool-start'),end_unix:unixInput('#pool-end'),card_ids:entries.map(e=>e[0]),weights:entries.map(e=>e[1])};
}
function renderPoolPreview(preview) {
  const c=preview.config, b=preview.base, ready=preview.publishable;
  const names=new Map(poolEditor.cards.map(card=>[card.reward_type_id,card.name]));
  const distributions=preview.stages.length?preview.stages:[{draw_count:b.card_num,card_ids:c.card_ids,odds_scaled:preview.odds_scaled}];
  $('#pool-preview').innerHTML=`<h3>${esc(c.name)}</h3><p>${esc(paymentName(b))} × ${c.price} · ${b.card_num} 抽${b.daily_first_free?' · 每日首次免费':''}</p><p>开始：${describeTime(c.start_unix)}<br>结束：${describeTime(c.end_unix)}</p><p>开放由「扭蛋发布」统一控制，并须处于排期内。</p><p>${preview.warnings.map(esc).join('<br>')}</p>${ready?'':'<p>未闭包卡牌：'+preview.blocked_card_ids.join(', ')+'</p>'}${distributions.map(stage=>`<details><summary>${esc(stage.name||'')} · ${esc(paymentName(b))} · ${stage.draw_count} 抽${stage.rarity?` · 稀有度 ${stage.rarity}`:''} · ${(stage.card_ids||stage.rewards||[]).length} 项 · 展开概率</summary><div class="table-wrap" style="max-height:280px"><table><thead><tr><th>卡牌</th><th>单次概率</th></tr></thead><tbody>${(stage.rewards||stage.card_ids||[]).map((r,i)=>`<tr><td>${esc(typeof r==='number'?(names.get(r)||r):rewardLabel(r))}${typeof r==='number'?'':` × ${r.num}`}</td><td>${(stage.odds_scaled[i]/preview.odds_scale).toFixed(5)}%</td></tr>`).join('')}</tbody></table></div></details>`).join('')}<h4>累计抽取赠礼</h4><p>${(b.gift_rules||[]).map(g=>`第${g.from_play}至${g.to_play||'以后'}次：${g.rewards.map(r=>esc(rewardLabel(r))+' × '+r.num).join('、')}`).join('<br>')||'无'}</p><p class="sub">这些是本地运营配置概率。</p>`;
  updatePoolControls();
}
$('#pool-select').onchange=()=>{if(!$('#pool-select').value)return;if(policyBusy('pool-editor')){$('#pool-select').value=poolEditor.row?.gacha_id||'';return}if(poolEditor.dirty&&!confirm('切换卡池将放弃未保存的编辑，是否继续？')){$('#pool-select').value=poolEditor.row.gacha_id;return}selectPool()};
['#pool-name','#pool-price','#pool-start','#pool-end'].forEach(id=>$(id).oninput=poolChanged);
['#pool-search','#pool-rarity','#pool-ready'].forEach(id=>$(id).oninput=()=>{poolEditor.page=0;poolEditor.selectedPage=0;renderPoolCards();renderSelectedCards()});
$$('#pool-jobs button').forEach(b=>b.onclick=()=>{poolEditor.job=Number(b.dataset.job);poolEditor.page=0;poolEditor.selectedPage=0;renderPoolCards();renderSelectedCards()});
$$('#pool-sources button').forEach(b=>b.onclick=()=>{poolEditor.source=b.dataset.source;poolEditor.page=0;poolEditor.selectedPage=0;renderPoolCards();renderSelectedCards()});
$('#pool-prev').onclick=()=>{poolEditor.page--;renderPoolCards()}; $('#pool-next').onclick=()=>{poolEditor.page++;renderPoolCards()};
$('#pool-add-matches').onclick=()=>{poolEditor.cards.filter(card=>poolMatches(card)&&poolCardEligible(card)).forEach(card=>{if(!poolEditor.selected.has(card.reward_type_id))poolEditor.selected.set(card.reward_type_id,1)});poolChanged();renderPoolCards();renderSelectedCards()};
$('#pool-clear').onclick=()=>{if(confirm('清空当前草稿中的全部卡牌？')){poolEditor.selected.clear();poolChanged();renderPoolCards();renderSelectedCards()}};
$('#pool-weight-all').onclick=()=>{const weight=Number($('#pool-batch-weight').value);if(!Number.isInteger(weight)||weight<1||weight>1000000)return toast('权重须为 1–1000000 的整数',true);const ids=new Set(poolEditor.cards.filter(poolMatches).map(card=>card.reward_type_id));for(const id of poolEditor.selected.keys())if(ids.has(id))poolEditor.selected.set(id,weight);poolChanged();renderSelectedCards()};
$('#pool-export').onclick=()=>{const url=URL.createObjectURL(new Blob([JSON.stringify(poolConfig(),null,2)],{type:'application/json'}));const link=document.createElement('a');link.href=url;link.download=`gacha-${poolEditor.row.gacha_id}.json`;link.click();setTimeout(()=>URL.revokeObjectURL(url),1000)};
$('#pool-import').onchange=event=>runPoolEdit(async()=>{
  try {
    const file=event.target.files[0];if(!file)return;
    if(file.size>2*1024*1024)throw new Error('配置文件超过 2 MiB');
    const c=JSON.parse(await file.text());
    if(!Array.isArray(c.card_ids)||!Array.isArray(c.weights)||c.card_ids.length!==c.weights.length||c.card_ids.length>6000||new Set(c.card_ids).size!==c.card_ids.length||c.card_ids.some(id=>!Number.isInteger(id)||id<=0)||c.weights.some(w=>!Number.isInteger(w)||w<1||w>1000000))throw new Error('卡牌／权重列表无效');
    if(c.gacha_id!==poolEditor.row.gacha_id)throw new Error('导入文件的卡池 ID 与当前卡池不同');
    const start=localTimeInput(c.start_unix),end=localTimeInput(c.end_unix),selected=new Map(c.card_ids.map((id,i)=>[id,c.weights[i]]));
    if(!confirm('以导入文件替换当前编辑内容？'))return;
    $('#pool-name').value=c.name;$('#pool-price').value=c.price;$('#pool-start').value=start;$('#pool-end').value=end;
    poolEditor.selected=selected;setPoolPayment(c);setupCustomPool(c);setupMixedPool(c);poolChanged();renderPoolCards();renderSelectedCards();
  }finally{event.target.value=''}
});
$('#pool-save-preview').onclick=()=>runPoolEdit(async()=>{
  const row=poolEditor.row,serial=poolEditor.editSerial,config=poolConfig();
  const result=await api('/api/gacha-editor/draft',{method:'POST',body:JSON.stringify({config,expected_revision:row.draft.revision})});
  row.draft=result.document;
  if(poolEditor.row===row&&poolEditor.editSerial===serial){
    poolEditor.preview=result.preview;poolEditor.dirty=false;$('#pool-version').textContent=`草稿 v${result.document.revision} · 已发布 v${row.live.revision}`;
    renderPoolPreview(result.preview);$('#pool-preview').scrollIntoView({behavior:'smooth',block:'start'});
  }
  state.loaded.delete('audit');toast('草稿已保存；请核对预览。');
});
$('#pool-publish').onclick=()=>{
  if(policyBusy('pool-editor')||!poolEditor.row||!poolEditor.preview?.publishable||poolEditor.dirty)return;
  if(poolEditor.row.live.sha256&&poolEditor.row.live.sha256===poolEditor.row.draft.sha256)return;
  if(!confirm(`发布已预览的「${poolEditor.preview.config.name}」？玩家下次请求将使用新配置。`))return;
  return runPoolEdit(async()=>{
    const row=poolEditor.row;
    const result=await api('/api/gacha-editor/publish',{method:'POST',body:JSON.stringify({config:{gacha_id:row.gacha_id},expected_revision:row.draft.revision,expected_live_revision:row.live.revision,sha256:row.draft.sha256})});
    row.live=result.document;row.config=result.preview.config;
    if(poolEditor.row===row)$('#pool-version').textContent=`草稿 v${row.draft.revision} · 已发布 v${row.live.revision}`;
    state.loaded.delete('audit');state.loaded.delete('gachas');toast('卡池配置已发布');
  });
};

// A pending reward job keeps the same immutable payload and keys across
// network errors and page reloads. Retry only unacknowledged recipients.

// Operator-created pools share draft/preview/publication with built-in pools.
function setupCustomPool(c) {
  const custom=!!poolEditor.row.custom;
  $('#pool-custom-settings').hidden=$('#pool-custom-gifts').hidden=!custom;
  poolEditor.gifts=structuredClone(c.gift_rules||poolEditor.row.base.gift_rules||[]);
  if(!custom)return;
  $('#pool-draw-count').disabled=!!poolEditor.row.base.fixed_draw_count;
  $('#pool-draw-count').value=poolEditor.row.base.fixed_draw_count?poolEditor.row.base.card_num:(c.card_num||poolEditor.row.base.card_num);
  const key=c.banner_key||poolEditor.row.base.banner_key;
  $('#pool-banner').innerHTML=[...new Set([...(poolEditor.banners||[]),key].filter(Boolean))].map(k=>`<option value="${esc(k)}">${esc(k.startsWith('custom_')?'已上传横幅 '+k.slice(7,15):k)}</option>`).join('');
  $('#pool-banner').value=key;$('#pool-banner-upload').value='';showPoolBanner();renderPoolGifts();
}
function customPoolConfig(){return poolEditor.row.custom?{card_num:Number($('#pool-draw-count').value),banner_key:$('#pool-banner').value,gift_rules:structuredClone(poolEditor.gifts||[])}:{}}
function showPoolBanner(){$('#pool-banner-preview').src='/gacha-assets/'+encodeURIComponent($('#pool-banner').value)+'.png'}
$('#pool-banner').onchange=()=>{showPoolBanner();poolChanged()};$('#pool-draw-count').oninput=poolChanged;
$('#pool-banner-upload').onchange=()=>runPoolEdit(async()=>{
  const file=$('#pool-banner-upload').files[0];if(!file)return;
  try{
    if(file.size>4*1024*1024)throw new Error('PNG横幅不能超过4 MiB');
    const content=await new Promise((resolve,reject)=>{const reader=new FileReader();reader.onload=()=>resolve(String(reader.result).split(',')[1]);reader.onerror=()=>reject(new Error('读取图片失败'));reader.readAsDataURL(file)});
    const data=await api('/api/gacha-banner',{method:'POST',body:JSON.stringify({content})});
    if(!Array.from($('#pool-banner').options).some(o=>o.value===data.banner_key))$('#pool-banner').add(new Option('已上传横幅 '+data.banner_key.slice(7,15),data.banner_key));
    $('#pool-banner').value=data.banner_key;showPoolBanner();poolChanged();toast('横幅已上传；保存草稿并发布后生效');
  }finally{$('#pool-banner-upload').value=''}
});
let poolCreateSource=0;
function showCreatePool(copy){
  if(policyBusy('pool-editor'))return;
  if(poolEditor.dirty&&!confirm('创建后将切换到新池，未保存的修改会放弃，继续？'))return;
  poolCreateSource=copy?poolEditor.row.gacha_id:0;
  $('#pool-create-title').textContent=copy?'整组复制为新卡池':'新增卡池';
  $('#pool-create-name').value=copy?(poolEditor.row.config.name+' 副本').slice(0,60):'新卡池';
  $('#pool-create-mode-field').hidden=copy;$('#pool-create-dialog').classList.add('open');
}
$('#pool-new').onclick=()=>showCreatePool(false);$('#pool-clone').onclick=()=>showCreatePool(true);
$('#pool-create-cancel').onclick=()=>$('#pool-create-dialog').classList.remove('open');
$('#pool-create-confirm').onclick=async()=>{
  if(policyBusy('pool-editor'))return;
  state.publishing.add('pool-editor');updatePolicyControls();$('#pool-create-confirm').disabled=$('#pool-create-cancel').disabled=true;
  try{
    const data=await api('/api/gacha-create',{method:'POST',body:JSON.stringify({source_id:poolCreateSource,name:$('#pool-create-name').value.trim(),mode:$('#pool-create-mode').value})});
    poolEditor.dirty=false;$('#pool-create-dialog').classList.remove('open');$('#pool-name-search').value='';$('#pool-config-status').value='';await loadPoolEditor();selectPool(data.gacha_id);
    if(!poolCreateSource&&poolEditor.mixed){poolEditor.mixed.reward_pool=[];renderMixedPool()}
    state.loaded.delete('gachas');state.loaded.delete('audit');toast(`已创建分组 ${data.group_id}，共 ${data.gacha_ids.length} 个入口；分别配置发布后统一开放`);
  }catch(e){toast(e.message,true)}finally{state.publishing.delete('pool-editor');$('#pool-create-confirm').disabled=$('#pool-create-cancel').disabled=false;updatePolicyControls()}
};
function activeCustomRewardPool(){return poolEditor.mixed.steps[Number($('#pool-stage').value)]?.reward_pool||poolEditor.mixed.reward_pool}
$('#pool-reward-add').onclick=()=>{
  const row=poolEditor.row,pool=activeCustomRewardPool();
  openContentPicker(entries=>{if(poolEditor.row!==row)throw new Error('卡池已切换，请重新选择');if(pool.length+entries.length>6000)throw new Error('每阶段最多6000项');pool.push(...entries.map(c=>({reward:contentReward(c),weight:1})));poolChanged();renderMixedPool()},['card','material','item','sphere','buddy']);
};
function refreshCustomStages(index=0){
  const steps=poolEditor.mixed.steps;
  $('#pool-stage').innerHTML=(steps.length?steps:[{}]).map((_,i)=>`<option value="${i}">第${i+1}阶段</option>`).join('');
  $('#pool-stage').value=Math.min(index,Math.max(0,steps.length-1));
  $('#pool-price').disabled=!!steps.length;
  if(steps.length)$('#pool-price').value=steps[0].price;
  poolChanged();renderMixedPool();
}
$('#pool-stage-add').onclick=()=>{
  const m=poolEditor.mixed;if(m.steps.length>=20){toast('最多20个阶段',true);return}
  if(!m.steps.length)m.steps.push({price:Number($('#pool-price').value),reward_pool:structuredClone(m.reward_pool)});
  m.steps.push(structuredClone(m.steps[m.steps.length-1]));refreshCustomStages(m.steps.length-1);
};
$('#pool-stage-delete').onclick=()=>{
  const m=poolEditor.mixed;if(!m.steps.length)return;
  if(!confirm('删除当前阶段？已上线池的累计抽取次数不会重置。'))return;
  const removed=m.steps.splice(Number($('#pool-stage').value),1)[0];
  if(!m.steps.length){m.reward_pool=removed.reward_pool;$('#pool-price').value=removed.price}
  refreshCustomStages();
};
function renderPoolGifts(){
  $('#pool-gift-rows').innerHTML=(poolEditor.gifts||[]).map((g,i)=>`<div class="panel panel-body"><div class="toolbar"><label>开始次数 <input type="number" min="1" value="${g.from_play}" data-gift-from="${i}"></label><label>结束次数（0为不限） <input type="number" min="0" value="${g.to_play}" data-gift-to="${i}"></label><button class="secondary" data-gift-add="${i}">添加赠礼</button><button class="secondary" data-gift-delete="${i}">删除规则</button></div>${g.rewards.map((r,j)=>`<div class="toolbar"><span>${esc(rewardLabel(r))}</span><input aria-label="赠礼数量" type="number" min="1" max="${r.type===8?1:9999}" value="${r.num}" data-gift-num="${i}:${j}"><button class="secondary" data-gift-remove="${i}:${j}">移除</button></div>`).join('')}</div>`).join('')||'<p class="sub">无额外赠礼</p>';
  for(const [attribute,field] of [['from','from_play'],['to','to_play']])$$(`[data-gift-${attribute}]`).forEach(input=>input.oninput=()=>{poolEditor.gifts[Number(input.dataset[attribute==='from'?'giftFrom':'giftTo'])][field]=Number(input.value);poolChanged()});
  $$('[data-gift-num]').forEach(input=>input.oninput=()=>{const[i,j]=input.dataset.giftNum.split(':').map(Number);poolEditor.gifts[i].rewards[j].num=Number(input.value);poolChanged()});
  $$('[data-gift-remove]').forEach(b=>b.onclick=()=>{const[i,j]=b.dataset.giftRemove.split(':').map(Number);poolEditor.gifts[i].rewards.splice(j,1);poolChanged();renderPoolGifts()});
  $$('[data-gift-delete]').forEach(b=>b.onclick=()=>{poolEditor.gifts.splice(Number(b.dataset.giftDelete),1);poolChanged();renderPoolGifts()});
  $$('[data-gift-add]').forEach(b=>b.onclick=()=>{const row=poolEditor.row,g=poolEditor.gifts[Number(b.dataset.giftAdd)];openContentPicker(entries=>{if(row!==poolEditor.row)throw new Error('卡池已切换');if(g.rewards.length+entries.length>120)throw new Error('每条规则最多120项');g.rewards.push(...entries.map(contentReward));poolChanged();renderPoolGifts()},['card','material','item','sphere','buddy'])});
}
$('#pool-gift-add').onclick=()=>{if(poolEditor.gifts.length>=120){toast('最多120条规则',true);return}poolEditor.gifts.push({from_play:1,to_play:1,rewards:[]});poolChanged();renderPoolGifts()};

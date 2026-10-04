'use strict';

// The 131-01 registry stores complete rules. Keep those rules editable in the
// shared workbench while built-in templates retain their fixed contracts.
function setupLegacyPool() {
  const row=poolActiveRow(),config=poolEditor.legacyByID?.get(row?.gacha_id);
  const box=poolEditor.boxByID?.has(row?.gacha_id);
  setupGachaBoxPool();
  $('#pool-custom-settings').hidden=!config;
  $('#pool-custom-gifts').hidden=!config||box;
  $('#pool-custom-mixed').hidden=!config||!poolEditor.mixed;
  if(!config)return;
  $('#pool-draw-count').value=config.card_num;
  $('#pool-draw-count').disabled=!!row.base.fixed_draw_count||box;
  $('#pool-banner').innerHTML=[...new Set([config.banner_key,...(poolEditor.banners||[])])].filter(Boolean).map(key=>`<option value="${esc(key)}" ${key===config.banner_key?'selected':''}>${esc(key)}</option>`).join('');
  $('#pool-banner-preview').src=`/gacha-assets/${encodeURIComponent(config.banner_key)}.png`;
  renderPoolGifts();
}
function legacyMixedConfig(){return poolEditor.legacyByID?.get(poolEditor.mixedVariant);}
$('#pool-draw-count').oninput=()=>{poolEditor.legacyByID.get(poolEditor.activeID).card_num=Number($('#pool-draw-count').value);poolChanged();};
$('#pool-banner').onchange=()=>{poolEditor.legacyByID.get(poolEditor.activeID).banner_key=$('#pool-banner').value;setupLegacyPool();poolChanged();};
$('#pool-banner-upload').onchange=()=>{
  const file=$('#pool-banner-upload').files[0];if(!file)return;
  const id=poolEditor.activeID;
  return runPoolEdit(async()=>{
    if(file.size>4*1024*1024||file.type!=='image/png')throw new Error('请上传4 MiB以内的PNG横幅');
    const data=await new Promise((resolve,reject)=>{const reader=new FileReader();reader.onerror=reject;reader.onload=()=>resolve(String(reader.result).split(',')[1]);reader.readAsDataURL(file);});
    const result=await api('/api/gacha-banner',{method:'POST',body:JSON.stringify({data_base64:data})});
    poolEditor.legacyByID.get(id).banner_key=result.banner_key;
    setupLegacyPool();poolChanged();
  },'上传横幅').finally(()=>$('#pool-banner-upload').value='');
};
$('#pool-reward-add').onclick=()=>openContentPicker(rows=>{
  const config=legacyMixedConfig();if(!config)return;
  const step=poolEditor.mixed.steps[Number($('#pool-stage').value)],pool=step?.reward_pool||poolEditor.mixed.reward_pool;
  const keys=new Set(pool.map(r=>`${r.reward.type}:${r.reward.reward_typeid}`));
  for(const row of rows){const reward=contentReward(row);if(keys.has(`${reward.type}:${reward.reward_typeid}`))continue;pool.push({weight:1,reward});}
  if(poolEditor.mixed.steps.length)poolEditor.mixed.reward_pool=poolEditor.mixed.steps[0].reward_pool;
  poolChanged();renderMixedPool();
},['currency','card','material','item']);
$('#pool-stage-add').onclick=()=>{
  if(!legacyMixedConfig())return;
  const mixed=poolEditor.mixed;if(mixed.steps.length>=20)return toast('最多20个阶段',true);
  if(!mixed.steps.length)mixed.steps.push({price:poolEditor.strategies.get(poolEditor.activeID).price,reward_pool:structuredClone(mixed.reward_pool)});
  mixed.steps.push(structuredClone(mixed.steps.at(-1)));mixed.reward_pool=mixed.steps[0].reward_pool;
  const index=mixed.steps.length-1;selectMixedVariant(poolEditor.activeID);$('#pool-stage').value=index;renderMixedPool();poolChanged();
};
$('#pool-stage-delete').onclick=()=>{
  if(!legacyMixedConfig())return;
  const mixed=poolEditor.mixed,index=Number($('#pool-stage').value);if(mixed.steps.length<=1)return toast('至少保留一个阶段',true);
  mixed.steps.splice(index,1);mixed.reward_pool=mixed.steps[0].reward_pool;selectMixedVariant(poolEditor.activeID);poolChanged();
};
function renderPoolGifts(){
  const config=poolEditor.legacyByID?.get(poolEditor.activeID);if(!config)return;
  $('#pool-gift-rows').innerHTML=config.gift_rules.map((g,i)=>`<div class="panel panel-body"><div class="toolbar"><label>开始次数 <input type="number" min="1" value="${g.from_play}" data-gift-from="${i}"></label><label>结束次数（0为不限） <input type="number" min="0" value="${g.to_play}" data-gift-to="${i}"></label><button class="secondary" data-gift-add="${i}">添加赠礼</button><button class="secondary" data-gift-delete="${i}">删除规则</button></div>${g.rewards.map((r,j)=>`<div class="toolbar"><span>${esc(rewardLabel(r))}</span><input aria-label="赠礼数量" type="number" min="1" max="${r.type===8?1:9999}" value="${r.num}" data-gift-num="${i}:${j}"><button class="secondary" data-gift-remove="${i}:${j}">移除</button></div>`).join('')}</div>`).join('')||'<p class="hint">无额外赠礼</p>';
}
$('#pool-gift-rows').oninput=e=>{
  const gifts=poolEditor.legacyByID.get(poolEditor.activeID).gift_rules,el=e.target;
  if(el.dataset.giftFrom!==undefined)gifts[Number(el.dataset.giftFrom)].from_play=Number(el.value);
  if(el.dataset.giftTo!==undefined)gifts[Number(el.dataset.giftTo)].to_play=Number(el.value);
  if(el.dataset.giftNum!==undefined){const [i,j]=el.dataset.giftNum.split(':').map(Number);gifts[i].rewards[j].num=Number(el.value);}
  poolChanged();
};
$('#pool-gift-rows').onclick=e=>{
  const button=e.target.closest('button');if(!button)return;
  const config=poolEditor.legacyByID.get(poolEditor.activeID),gifts=config.gift_rules;
  if(button.dataset.giftDelete!==undefined){gifts.splice(Number(button.dataset.giftDelete),1);poolChanged();renderPoolGifts();}
  if(button.dataset.giftRemove!==undefined){const [i,j]=button.dataset.giftRemove.split(':').map(Number);gifts[i].rewards.splice(j,1);poolChanged();renderPoolGifts();}
  if(button.dataset.giftAdd!==undefined){const gift=gifts[Number(button.dataset.giftAdd)];openContentPicker(rows=>{for(const row of rows){const reward=contentReward(row);if(reward.type===8)reward.num=1;gift.rewards.push(reward);}poolChanged();renderPoolGifts();},['currency','card','material','item']);}
};
$('#pool-gift-add').onclick=()=>{const gifts=poolEditor.legacyByID.get(poolEditor.activeID).gift_rules;if(gifts.length>=120)return toast('最多120条赠礼规则',true);gifts.push({from_play:1,to_play:1,rewards:[]});poolChanged();renderPoolGifts();};

$('#pool-custom-new').onclick=()=>{
  $('#pool-custom-source').innerHTML='<option value="0">创建空白卡池</option>'+poolEditor.rows.filter(r=>!r.deleted&&r.gacha_id===poolGroupRows(r)[0]?.gacha_id).map(r=>`<option value="${r.gacha_id}">${esc(r.config.name)} · ${r.gacha_id}</option>`).join('');
  $('#pool-custom-name').value='';$('#pool-custom-mode').value='ordinary';$('#pool-custom-create-modal').classList.add('open');
};
$('#pool-custom-cancel').onclick=()=>$('#pool-custom-create-modal').classList.remove('open');
$('#pool-custom-apply').onclick=()=>{
  const name=$('#pool-custom-name').value.trim(),source_id=Number($('#pool-custom-source').value),mode=$('#pool-custom-mode').value;
  if(!name||[...name].length>60)return toast('名称须为1–60字',true);
  if(poolEditor.dirty&&!confirm('创建后会切换到新卡池，当前未保存的编辑将放弃。是否继续？'))return;
  return runPoolTask(async()=>{
    const result=await api('/api/gacha-create',{method:'POST',body:JSON.stringify({name,source_id,mode})});
    $('#pool-custom-create-modal').classList.remove('open');poolEditor.dirty=false;$('#pool-name-search').value='';$('#pool-config-status').value='';
    await loadPoolEditor(result.gacha_id);state.loaded.delete('gachas');state.loaded.delete('audit');toast('已创建自定义卡池，请配置奖励、保存预览、发布后再开放');
  },'新建卡池');
};

function activeBoxRounds(){return poolEditor.boxByID?.get(poolEditor.activeID);}
function setupGachaBoxPool(){
  const rounds=activeBoxRounds();$('#pool-box-editor').hidden=!rounds;
  $('#pool-play-limit').disabled=!!rounds;
  if(!rounds)return;
  $('#pool-play-limit').value=0;
  $('#pool-box-round').innerHTML=rounds.map((_,i)=>`<option value="${i}">${i===10?'第 11 轮起 · 无限循环模板':`第 ${i+1} 轮`}</option>`).join('');
  renderGachaBoxPool();
}
function renderGachaBoxPool(){
  const rounds=activeBoxRounds();if(!rounds)return;
  const rewards=rounds[Number($('#pool-box-round').value)].rewards,total=rewards.reduce((sum,e)=>sum+(Number(e.stock)||0),0);
  $('#pool-box-total').textContent=`共 ${rewards.length} 项 · ${total}/50 份${total===50?'':'（需调整为 50 份）'}`;
  $('#pool-box-rows').innerHTML=rewards.map((e,i)=>`<tr><td>${esc(rewardLabel(e.reward))}<span class="sub">ID ${e.reward.reward_typeid}${e.reward.type===6?` · 名声 ${e.reward.card_fame}`:''}</span></td><td><input type="number" min="1" max="9999" value="${e.reward.num}" data-box-num="${i}" aria-label="单次发放数量"></td><td><input type="number" min="1" max="50" value="${e.stock}" data-box-stock="${i}" aria-label="库存份数"></td><td data-box-odds="${i}">${total?(e.stock/total*100).toFixed(3):'0'}%</td><td><button class="secondary sm" data-box-remove="${i}">移除</button></td></tr>`).join('')||emptyRow(5,'本轮尚未配置奖励');
}
$('#pool-box-round').onchange=renderGachaBoxPool;
$('#pool-box-add').onclick=()=>openContentPicker(rows=>{
  const rounds=activeBoxRounds();if(!rounds)return;
  const rewards=rounds[Number($('#pool-box-round').value)].rewards;
  if(rows.some(row=>![4,6,8,10,12,13,15,19].includes(row.reward_type)))throw new Error('箱池支持金币、免费水晶、体力、卡牌、素材卡、道具、召唤石和传承卡');
  for(const row of rows){if(rewards.length>=50)break;rewards.push({reward:contentReward(row),stock:1});}
  poolChanged();renderGachaBoxPool();
},['currency','card','material','item','sphere','buddy']);
$('#pool-box-rows').oninput=e=>{
  const rounds=activeBoxRounds();if(!rounds)return;
  const rewards=rounds[Number($('#pool-box-round').value)].rewards,el=e.target;
  if(el.dataset.boxNum!==undefined)rewards[Number(el.dataset.boxNum)].reward.num=Number(el.value);
  if(el.dataset.boxStock!==undefined)rewards[Number(el.dataset.boxStock)].stock=Number(el.value);
  poolChanged();
  const total=rewards.reduce((sum,r)=>sum+(Number(r.stock)||0),0);
  $('#pool-box-total').textContent=`共 ${rewards.length} 项 · ${total}/50 份${total===50?'':'（需调整为 50 份）'}`;
  $$('#pool-box-rows [data-box-odds]').forEach(el=>el.textContent=`${total?(rewards[Number(el.dataset.boxOdds)].stock/total*100).toFixed(3):'0'}%`);
};
$('#pool-box-rows').onclick=e=>{const button=e.target.closest('[data-box-remove]');if(!button)return;activeBoxRounds()[Number($('#pool-box-round').value)].rewards.splice(Number(button.dataset.boxRemove),1);poolChanged();renderGachaBoxPool();};
$('#pool-box-copy').onclick=()=>{
  const rounds=activeBoxRounds(),index=Number($('#pool-box-round').value);if(!rounds)return;
  const targets=prompt('复制到哪些轮？填写 1–11，用逗号分隔；11 表示无限循环模板。','');if(targets===null)return;
  const ids=[...new Set(targets.split(/[,，\s]+/).filter(Boolean).map(Number))];
  if(!ids.length||ids.some(id=>!Number.isInteger(id)||id<1||id>11))return toast('请输入 1–11 的轮次',true);
  if(!confirm(`用当前轮奖励覆盖第 ${ids.join('、')} 套模板？`))return;
  for(const id of ids)if(id-1!==index)rounds[id-1]=structuredClone(rounds[index]);
  poolChanged();renderGachaBoxPool();
};

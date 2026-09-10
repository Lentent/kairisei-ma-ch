'use strict';
const runtimeSettings={value:null,revision:0,catalog:[]};
function itemShopDraft(){return $$('#settings-shop tr').map(row=>({lineup_id:Number(row.dataset.id),enabled:row.querySelector('.shop-enabled').checked,price:Number(row.querySelector('.shop-price').value)}))}
function runtimeSettingsDirty(){return runtimeSettings.value!==null&&($('#settings-crystal').checked!==runtimeSettings.value.crystal_purchase_enabled||JSON.stringify(itemShopDraft())!==JSON.stringify(runtimeSettings.value.item_shop||[]))}
function renderRuntimeSettings(){
  $('#settings-crystal').checked=runtimeSettings.value.crystal_purchase_enabled;
  $('#settings-state').textContent=runtimeSettings.value.crystal_purchase_enabled?'已开启':'已关闭';
  $('#settings-version').textContent=`版本 ${runtimeSettings.revision} · 保存后即时生效`;
  const configured=new Map((runtimeSettings.value.item_shop||[]).map(row=>[row.lineup_id,row]));
  $('#settings-shop').innerHTML=runtimeSettings.catalog.map(base=>{const row=configured.get(base.item_shop_lineupid);return `<tr data-id="${row.lineup_id}"><td><input class="shop-enabled" type="checkbox" aria-label="上架${esc(base.lineup_name)}" ${row.enabled?'checked':''}></td><td>${esc(base.lineup_name)}<span class="sub">#${row.lineup_id}</span></td><td>${base.pay_type===1?'金币':'水晶'}</td><td><input class="shop-price" type="number" min="1" max="10000000" step="1" value="${row.price}" aria-label="${esc(base.lineup_name)}单价"></td></tr>`}).join('');
  $('#settings-save').disabled=true;
}
async function loadRuntimeSettings(){
  const data=await api('/api/settings');
  runtimeSettings.value=data.settings;runtimeSettings.revision=data.revision;runtimeSettings.catalog=data.item_shop_catalog||[];renderRuntimeSettings();
}
$('#settings-crystal').onchange=()=>$('#settings-save').disabled=!runtimeSettingsDirty();
$('#settings-shop').oninput=()=>$('#settings-save').disabled=!runtimeSettingsDirty();
$('#settings-shop').onchange=()=>$('#settings-save').disabled=!runtimeSettingsDirty();
$('#settings-save').onclick=async()=>{
  if(policyBusy('settings')||!runtimeSettingsDirty())return;
  const itemShop=itemShopDraft();
  if(itemShop.some(row=>!Number.isInteger(row.price)||row.price<1||row.price>10000000)){toast('单价须为1–10000000之间的整数',true);return}
  state.publishing.add('settings');updatePolicyControls();
  try{
    const data=await api('/api/settings',{method:'PUT',body:JSON.stringify({crystal_purchase_enabled:$('#settings-crystal').checked,item_shop:itemShop,expected_revision:runtimeSettings.revision})});
    runtimeSettings.value=data.settings;runtimeSettings.revision=data.revision;renderRuntimeSettings();
    state.loaded.delete('audit');toast('运营设置已生效');
  }catch(e){toast(e.message,true)}finally{state.publishing.delete('settings');updatePolicyControls()}
};

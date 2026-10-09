'use strict';

const dungeonSchedule={saved:null,draft:null,defaults:null,revision:0,entries:[],page:0,previewConfig:'',previewStale:false,listStale:false,limits:{title:80,description:4000,footer:4000,entry_name:100,entry_note:1000,entries:2000}};
const dungeonScheduleKey=e=>`${e.catalog}:${e.group_id}`;
function normalizeDungeonSchedule(config){
  return {title:String(config?.title??'').trim(),description:String(config?.description??'').trim(),footer:String(config?.footer??'').trim(),entries:(config?.entries||[]).map(e=>({catalog:e.catalog,group_id:Number(e.group_id),name:String(e.name??'').trim(),note:String(e.note??'').trim()})).filter(e=>e.name||e.note).sort((a,b)=>a.catalog.localeCompare(b.catalog)||a.group_id-b.group_id)};
}
const dungeonScheduleJSON=config=>JSON.stringify(normalizeDungeonSchedule(config));
function dungeonScheduleDirty(){return !!dungeonSchedule.saved&&dungeonScheduleJSON(dungeonSchedule.saved)!==dungeonScheduleJSON(dungeonSchedule.draft)}
function dungeonScheduleRows(){
  const overrides=dungeonScheduleOverrides(),rows=new Map(dungeonSchedule.entries.filter(e=>e.published||overrides.has(dungeonScheduleKey(e))).map(e=>[dungeonScheduleKey(e),{...e}]));
  for(const entry of dungeonSchedule.draft?.entries||[])if(!rows.has(dungeonScheduleKey(entry)))rows.set(dungeonScheduleKey(entry),{catalog:entry.catalog,group_id:entry.group_id,name:`Boss 组 ${entry.group_id}`,category:'',schedule:'等待重新发布',status:'未发布',published:false});
  return [...rows.values()];
}
function dungeonScheduleOverrides(){return new Map((dungeonSchedule.draft?.entries||[]).map(e=>[dungeonScheduleKey(e),e]))}
function dungeonScheduleUpdatePreview(html,config){
  $('#dungeon-schedule-frame').srcdoc=html||'';
  dungeonSchedule.previewConfig=dungeonScheduleJSON(config);dungeonSchedule.previewStale=false;
}
function dungeonScheduleChanged(){
  updateDungeonScheduleControls();updateDraftIndicators();
}
function updateDungeonScheduleControls(){
  const ready=!!dungeonSchedule.draft,busy=policyBusy('dungeon-schedule'),dirty=dungeonScheduleDirty();
  $('#dungeon-schedule-fields').disabled=!ready||busy;
  for(const action of ['default','refresh','preview','export'])$('#dungeon-schedule-'+action).disabled=!ready||busy;
  $('#dungeon-schedule-save').disabled=!ready||busy||!dirty;
  $('#dungeon-schedule-version').textContent=`配置 v${dungeonSchedule.revision}`;
  setSaveState('#dungeon-schedule-state',!ready?'正在读取':dirty?'有未保存的修改':'与已保存内容一致',dirty?'dirty':ready?'ok':'');
  const stale=dungeonSchedule.previewStale||ready&&dungeonSchedule.previewConfig!==dungeonScheduleJSON(dungeonSchedule.draft);
  setSaveState('#dungeon-schedule-preview-state',!ready?'读取后显示已保存内容；修改草稿后点击更新预览。':stale?'预览未更新，请点击“更新草稿预览”。':dirty?'当前预览对应未保存草稿。':'当前预览对应已保存内容。',stale?'dirty':'');
  $('#dungeon-schedule-refresh').classList.toggle('schedule-refresh-needed',dungeonSchedule.listStale);
  $('#dungeon-schedule-refresh').title=dungeonSchedule.listStale?'Boss 发布设置已改变，刷新名单会保留当前文字草稿':'';
}
function renderDungeonScheduleFields(){
  const c=dungeonSchedule.draft;
  for(const field of ['title','description','footer'])$('#dungeon-schedule-'+field).value=c[field];
  renderDungeonScheduleRows();dungeonScheduleChanged();
}
function renderDungeonScheduleRows(){
  const q=$('#dungeon-schedule-search').value.trim(),catalog=$('#dungeon-schedule-catalog').value,published=$('#dungeon-schedule-published').value;
  const overrides=dungeonScheduleOverrides(),all=dungeonScheduleRows();
  const rows=all.filter(e=>{const override=overrides.get(dungeonScheduleKey(e));return (catalog==='all'||e.catalog===catalog)&&(published==='all'||(published==='published'?e.published:!e.published))&&matchesWords(`${e.name} ${override?.name||''} ${override?.note||''} ${e.group_id}`,q)});
  renderDungeonScheduleCount(all,rows);
  dungeonSchedule.page=renderPager('dungeon-schedule',dungeonSchedule.page,rows.length,25,page=>{dungeonSchedule.page=page;renderDungeonScheduleRows();$('#dungeon-schedule-rows').scrollIntoView({block:'start'})});
  const visible=rows.slice(dungeonSchedule.page*25,(dungeonSchedule.page+1)*25);
  $('#dungeon-schedule-rows').innerHTML=visible.map(e=>{
    const key=dungeonScheduleKey(e),override=overrides.get(key),id=`dungeon-entry-${e.catalog}-${e.group_id}`;
    return `<article class="dungeon-schedule-entry" data-key="${esc(key)}"><div class="dungeon-schedule-entry-info"><div class="dungeon-schedule-entry-heading"><b>${esc(e.name)}</b><span class="tag ${e.published?'ok':''}">${e.published?'已发布':'未发布'}</span></div><span class="sub">${e.catalog==='past'?'往期 Boss':'活动／素材副本'} · ${e.category?esc(bossKindLabel(e.category))+' · ':''}组 ID <code>${e.group_id}</code></span><span class="sub dungeon-schedule-times">${esc(e.schedule||'沿用当前发布排期')}</span><span class="sub">${esc(e.status||'')}</span></div><div class="dungeon-schedule-entry-edit"><div class="field"><label for="${id}-name">展示名称</label><input id="${id}-name" data-field="name" maxlength="${dungeonSchedule.limits.entry_name}" value="${esc(override?.name||'')}" placeholder="${esc(e.name)}"></div><div class="field"><label for="${id}-note">备注</label><textarea id="${id}-note" data-field="note" maxlength="${dungeonSchedule.limits.entry_note}" rows="2" placeholder="例如：推荐属性、攻略提示">${esc(override?.note||'')}</textarea></div></div></article>`;
  }).join('')||'<div class="empty"><b>没有匹配的 Boss 条目</b>调整筛选条件；发布新 Boss 后可点击“刷新发布名单”。</div>';
}
function renderDungeonScheduleCount(all=dungeonScheduleRows(),filtered){
  const publishedCount=all.filter(e=>e.published).length;
  $('#dungeon-schedule-count').textContent=`已发布 ${num(publishedCount)} 组 · 保留未发布备注 ${num(all.length-publishedCount)} 组${filtered?` · 筛选结果 ${num(filtered.length)} 组`:''}${dungeonSchedule.listStale?' · 发布名单已改变，请刷新':''}`;
}
function applyDungeonScheduleData(data){
  dungeonSchedule.saved=normalizeDungeonSchedule(data.config);dungeonSchedule.draft=structuredClone(dungeonSchedule.saved);
  dungeonSchedule.defaults=normalizeDungeonSchedule(data.defaults);dungeonSchedule.revision=data.revision;dungeonSchedule.entries=data.entries||[];
  if(data.limits)dungeonSchedule.limits={...dungeonSchedule.limits,...data.limits};
  for(const field of ['title','description','footer'])$('#dungeon-schedule-'+field).maxLength=dungeonSchedule.limits[field];
  dungeonSchedule.page=0;dungeonSchedule.listStale=false;
  dungeonScheduleUpdatePreview(data.preview_html,dungeonSchedule.saved);renderDungeonScheduleFields();
}
async function loadDungeonSchedule(){applyDungeonScheduleData(await api('/api/dungeon-schedule'))}
function dungeonSchedulePublicationChanged(){
  dungeonSchedule.listStale=true;dungeonSchedule.previewStale=true;
  if(!dungeonScheduleDirty())state.loaded.delete('dungeon-schedule');
  if(dungeonSchedule.draft){renderDungeonScheduleRows();dungeonScheduleChanged()}
}
async function refreshDungeonScheduleEntries(){
  if(policyBusy('dungeon-schedule')||!dungeonSchedule.draft)return;
  state.loading.add('dungeon-schedule');updatePolicyControls();
  try{
    const data=await api('/api/dungeon-schedule');
    // Refresh metadata without adopting remote edits or discarding any local overrides.
    const sameSaved=dungeonScheduleJSON(data.config)===dungeonScheduleJSON(dungeonSchedule.saved);
    dungeonSchedule.entries=data.entries||[];dungeonSchedule.defaults=normalizeDungeonSchedule(data.defaults);dungeonSchedule.listStale=false;dungeonSchedule.previewStale=true;
    if(sameSaved){dungeonSchedule.revision=data.revision;clearViewAlert('dungeon-schedule')}
    else showViewAlert('dungeon-schedule',{kind:'warn',title:'发布名单已刷新，页面内容有新版本',message:conflictHint+'可先导出当前草稿，再重新载入最新版本。',actions:[{label:'导出当前草稿',onClick:()=>$('#dungeon-schedule-export').click()},{label:'重新载入最新版本',primary:true,onClick:()=>loadView('dungeon-schedule',true)}]});
    renderDungeonScheduleRows();dungeonScheduleChanged();toast('发布名单已刷新，当前文字草稿已保留');
  }catch(e){reportError('dungeon-schedule',e,'刷新发布名单')}
  finally{state.loading.delete('dungeon-schedule');updatePolicyControls()}
}
function validateDungeonSchedule(){
  clearInvalid($('#dungeon-schedule'));
  if(!dungeonSchedule.draft.title.trim()){markInvalid($('#dungeon-schedule-title'),'请填写页面标题');$('#dungeon-schedule-title').focus();showViewAlert('dungeon-schedule',{title:'请填写页面标题',message:'日程表需要一个标题。'});return false}
  for(const el of $('#dungeon-schedule-fields').querySelectorAll('input,textarea'))if(!el.checkValidity()){el.reportValidity();return false}
  const config=dungeonSchedule.draft,limits=dungeonSchedule.limits;
  if(config.entries.length>limits.entries){showViewAlert('dungeon-schedule',{title:'备注条目过多',message:`最多保存 ${limits.entries} 组名称和备注，请清除部分未发布条目的备注。`});return false}
  for(const field of ['title','description','footer'])if([...config[field]].length>limits[field]){markInvalid($('#dungeon-schedule-'+field),`最多 ${limits[field]} 字`);$('#dungeon-schedule-'+field).focus();return false}
  const invalid=config.entries.find(e=>[...e.name].length>limits.entry_name||[...e.note].length>limits.entry_note);
  if(invalid){showViewAlert('dungeon-schedule',{title:'条目内容过长',message:`组 ${invalid.group_id} 的展示名称最多 ${limits.entry_name} 字，备注最多 ${limits.entry_note} 字。`});return false}
  return true;
}
function dungeonScheduleReview(config){
  const before=dungeonSchedule.saved,previous=new Map(before.entries.map(e=>[dungeonScheduleKey(e),e])),next=new Map(config.entries.map(e=>[dungeonScheduleKey(e),e])),names=new Map(dungeonScheduleRows().map(e=>[dungeonScheduleKey(e),e.name]));
  const rows=[];
  for(const key of new Set([...previous.keys(),...next.keys()])){
    const a=previous.get(key),b=next.get(key),label=`${names.get(key)||key}（${key}）`;
    rows.push(...diffRows([[`${label} 展示名称`,a?.name||'',b?.name||''],[`${label} 备注`,a?.note||'',b?.note||'']]));
  }
  return reviewChanges({title:'保存副本日程表',lead:'请核对将显示在游戏日程表中的内容。',sections:[{title:'页面文字',rows:diffRows([['页面标题',before.title,config.title],['页面说明',before.description,config.description],['页尾提示',before.footer,config.footer]])},{title:'Boss 展示名称与备注',rows}],note:'保存后玩家重新打开日程表生效。Boss 名单和排期自动读取当前发布设置。',confirmText:'确认保存日程表'});
}
$('#dungeon-schedule-fields').addEventListener('input',event=>{
  if(!dungeonSchedule.draft)return;
  for(const field of ['title','description','footer'])if(event.target.id==='dungeon-schedule-'+field)dungeonSchedule.draft[field]=event.target.value;
  dungeonScheduleChanged();
});
$('#dungeon-schedule-rows').addEventListener('input',event=>{
  const field=event.target.dataset.field,row=event.target.closest('[data-key]');if(!row||!field||!dungeonSchedule.draft)return;
  const key=row.dataset.key,[catalog,id]=key.split(':'),entries=dungeonSchedule.draft.entries;let entry=entries.find(e=>dungeonScheduleKey(e)===key);
  if(!entry){entry={catalog,group_id:Number(id),name:'',note:''};entries.push(entry)}
  entry[field]=event.target.value;
  if(!entry.name&&!entry.note)dungeonSchedule.draft.entries=entries.filter(e=>e!==entry);
  renderDungeonScheduleCount();dungeonScheduleChanged();
});
for(const id of ['search','catalog','published'])$('#dungeon-schedule-'+id).addEventListener(id==='search'?'input':'change',()=>{dungeonSchedule.page=0;renderDungeonScheduleRows()});
$('#dungeon-schedule-clear').onclick=()=>{$('#dungeon-schedule-search').value='';$('#dungeon-schedule-catalog').value='all';$('#dungeon-schedule-published').value='published';dungeonSchedule.page=0;renderDungeonScheduleRows()};
$('#dungeon-schedule-refresh').onclick=refreshDungeonScheduleEntries;
$('#dungeon-schedule-default').onclick=()=>{
  if(policyBusy('dungeon-schedule')||!dungeonSchedule.defaults)return;
  if(!confirm('将默认标题、说明和备注载入草稿？点击“保存日程表”后才生效。'))return;
  dungeonSchedule.draft=structuredClone(dungeonSchedule.defaults);clearViewAlert('dungeon-schedule');renderDungeonScheduleFields();toast('默认内容已载入草稿，保存后生效');
};
$('#dungeon-schedule-export').onclick=()=>{if(dungeonSchedule.draft)downloadJSON('副本日程表草稿.json',{expected_revision:dungeonSchedule.revision,config:normalizeDungeonSchedule(dungeonSchedule.draft)})};
$('#dungeon-schedule-preview').onclick=async()=>{
  if(policyBusy('dungeon-schedule')||!dungeonSchedule.draft||!validateDungeonSchedule())return;
  const config=normalizeDungeonSchedule(dungeonSchedule.draft);state.loading.add('dungeon-schedule');updatePolicyControls();
  try{const data=await api('/api/dungeon-schedule/preview',{method:'POST',body:JSON.stringify({config})});dungeonScheduleUpdatePreview(data.preview_html,config);dungeonScheduleChanged();toast('草稿预览已更新')}
  catch(e){reportError('dungeon-schedule',e,'预览')}
  finally{state.loading.delete('dungeon-schedule');updatePolicyControls()}
};
$('#dungeon-schedule-save').onclick=async()=>{
  if(policyBusy('dungeon-schedule')||!dungeonScheduleDirty()||!validateDungeonSchedule())return;
  const config=normalizeDungeonSchedule(dungeonSchedule.draft);
  if(!await dungeonScheduleReview(config))return;
  state.publishing.add('dungeon-schedule');updatePolicyControls();
  try{
    const data=await api('/api/dungeon-schedule',{method:'PUT',body:JSON.stringify({expected_revision:dungeonSchedule.revision,config})});
    applyDungeonScheduleData(data);state.loaded.delete('audit');state.loaded.delete('dashboard');clearViewAlert('dungeon-schedule');toast('副本日程表已保存，重新打开游戏日程表即可看到');
  }catch(e){reportError('dungeon-schedule',e,'保存')}
  finally{state.publishing.delete('dungeon-schedule');updatePolicyControls()}
};

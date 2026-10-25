'use strict';
const customStampEditor={config:null,revision:0,dirty:false,selected:0,next:980001,published:{},occupied:{}};
const customStampKinds={text:'纯文字',image:'图片',sound:'图片加声音'};
async function loadCustomStamps(){
  const e=customStampEditor,d=await api('/api/custom-stamps');
  e.config=d.config;e.revision=d.revision;e.dirty=false;e.next=d.next_stamp_id;e.published=d.published||{};e.occupied=d.occupied||{};
  if(!e.config.stamps.some(s=>s.stamp_id===e.selected))e.selected=e.config.stamps[0]?.stamp_id||0;
  renderCustomStamps();$('#custom-stamp-status').textContent='设计已载入。应用资源更新包并重启后，新表情才会进入奖励目录。';
}
function currentCustomStamp(){return customStampEditor.config?.stamps.find(s=>s.stamp_id===customStampEditor.selected)}
function customStampChanged(){customStampEditor.dirty=true;$('#custom-stamp-status').textContent='有未保存的修改，保存后可生成资源更新包。';updateDraftIndicators()}
function renderCustomStampList(){
  const e=customStampEditor,q=$('#custom-stamp-search').value.trim().toLowerCase();
  $('#custom-stamp-count').textContent=`${e.config.stamps.length} 个`;
  $('#custom-stamp-list').innerHTML=e.config.stamps.filter(s=>`${s.name} ${s.stamp_id}`.toLowerCase().includes(q)).map(s=>`<button class="custom-card-choice ${s.stamp_id===e.selected?'active':''}" type="button" data-stamp-id="${s.stamp_id}"><b>${esc(s.name)}</b><span>${s.stamp_id} · ${esc(customStampKinds[s.kind])} · ${e.published[s.stamp_id]?'已应用资源':'草稿'}</span></button>`).join('')||'<p class="empty">暂无表情，点击“新增表情”开始制作。</p>';
  $$('#custom-stamp-list [data-stamp-id]').forEach(b=>b.onclick=()=>{e.selected=Number(b.dataset.stampId);renderCustomStamps()});
}
function renderCustomStampMedia(){
  const s=currentCustomStamp(),image=s?.kind==='image'||s?.kind==='sound',sound=s?.kind==='sound';
  $('#custom-stamp-image-fields').hidden=!image;$('#custom-stamp-sound-fields').hidden=!sound;
  $('#custom-stamp-text-preview').hidden=!s||image;$('#custom-stamp-text-preview').textContent=s?.message||'文字内容预览';
  $('#custom-stamp-preview').innerHTML=s?.artwork?`<img src="data:image/png;base64,${s.artwork}" alt="表情图片预览">`:'<span class="hint">请上传图片</span>';
  const audio=$('#custom-stamp-audio-preview'),url=sound&&s.audio?`data:audio/wav;base64,${s.audio}`:'';
  audio.hidden=!url;audio.volume=(s?.volume??100)/100;
  if(audio.getAttribute('src')!==url){audio.pause();if(url)audio.src=url;else audio.removeAttribute('src');audio.load()}
  $('#custom-stamp-volume').value=s?.volume??100;$('#custom-stamp-volume-value').textContent=`${s?.volume??100}%`;
}
function renderCustomStamps(){
  const e=customStampEditor,s=currentCustomStamp();$('#custom-stamp-version').textContent=`草稿 v${e.revision}`;
  renderCustomStampList();$('#custom-stamp-fields').disabled=!s;$('#custom-stamp-remove').disabled=!s||!!e.published[s?.stamp_id];
  $('#custom-stamp-title').textContent=s?`${s.name} · ${s.stamp_id}`:'选择或新增表情';
  for(const [field,key] of [['name','name'],['type','kind'],['category','category'],['message','message']])$(`#custom-stamp-${field}`).value=s?.[key]??'';
  renderCustomStampMedia();updateDraftIndicators();
}
async function customStampAction(fn){
  if(policyBusy('custom-stamps'))return;state.publishing.add('custom-stamps');$('#custom-stamps').inert=true;
  try{await fn()}catch(e){reportError('custom-stamps',e,'操作');toast(e.message,true)}finally{state.publishing.delete('custom-stamps');$('#custom-stamps').inert=false;updatePolicyControls()}
}
$('#custom-stamp-new').onclick=()=>{
  const e=customStampEditor;if(!e.config)return;if(e.config.stamps.length>=200)return toast('最多制作200个表情',true);
  const used=new Set([...Object.keys(e.occupied).map(Number),...e.config.stamps.map(s=>s.stamp_id)]);while(used.has(e.next)&&e.next<=989999)e.next++;
  if(e.next>989999)return toast('可用表情编号已用完',true);
  const s={stamp_id:e.next++,name:'新表情',kind:'text',category:2,message:'你好！',volume:100};e.config.stamps.push(s);e.selected=s.stamp_id;customStampChanged();renderCustomStamps();$('#custom-stamp-name').focus();$('#custom-stamp-name').select();
};
$('#custom-stamp-remove').onclick=()=>{const e=customStampEditor,s=currentCustomStamp();if(!s||e.published[s.stamp_id])return;if(!confirm(`移除“${s.name}”草稿？`))return;e.config.stamps=e.config.stamps.filter(x=>x!==s);e.selected=e.config.stamps[0]?.stamp_id||0;customStampChanged();renderCustomStamps()};
$('#custom-stamp-search').oninput=renderCustomStampList;
for(const [field,key] of [['name','name'],['category','category'],['message','message']])$(`#custom-stamp-${field}`).oninput=()=>{const s=currentCustomStamp();if(!s)return;s[key]=key==='category'?Number($(`#custom-stamp-${field}`).value):$(`#custom-stamp-${field}`).value;customStampChanged();if(key==='name'){renderCustomStampList();$('#custom-stamp-title').textContent=`${s.name} · ${s.stamp_id}`}if(key==='message')$('#custom-stamp-text-preview').textContent=s.message||'文字内容预览'};
$('#custom-stamp-type').onchange=()=>{const s=currentCustomStamp(),kind=$('#custom-stamp-type').value;if(!s)return;if((kind==='text'&&s.artwork||kind!=='sound'&&s.audio)&&!confirm('切换类型会移除不适用的图片或声音，继续？')){$('#custom-stamp-type').value=s.kind;return}s.kind=kind;s.category=kind==='text'?2:kind==='sound'?7:1;if(kind==='text')delete s.artwork;if(kind!=='sound')delete s.audio;customStampChanged();renderCustomStamps()};
$('#custom-stamp-volume').oninput=()=>{const s=currentCustomStamp();if(s){s.volume=Number($('#custom-stamp-volume').value);customStampChanged();renderCustomStampMedia()}};
$('#custom-stamp-image').onchange=()=>customStampAction(async()=>{
  const s=currentCustomStamp(),input=$('#custom-stamp-image'),file=input.files[0];if(!s||!file)return;
  try{
    if(!['image/png','image/jpeg'].includes(file.type)||file.size>4*1024*1024||!file.size)throw new Error('请选择4MB以内的PNG／JPEG图片');
    const image=await createImageBitmap(file);try{if(image.width<64||image.height<64||image.width>4096||image.height>4096||image.width*image.height>8*1024*1024)throw new Error('图片边长须为64–4096像素，总像素不超过800万')}finally{image.close()}
    s.artwork=await new Promise((resolve,reject)=>{const reader=new FileReader();reader.onload=()=>resolve(reader.result.split(',')[1]);reader.onerror=()=>reject(new Error('图片读取失败'));reader.readAsDataURL(file)});
    customStampChanged();renderCustomStampMedia();toast('图片已导入')
  }finally{input.value=''}
});
$('#custom-stamp-audio').onchange=()=>customStampAction(async()=>{const s=currentCustomStamp(),input=$('#custom-stamp-audio'),file=input.files[0];if(!s||!file)return;try{s.audio=await normalizeCustomVoice(file);customStampChanged();renderCustomStampMedia();toast('声音已导入，可试听后保存')}finally{input.value=''}});
function validateCustomStampDesign(d){
  if(!d||!Array.isArray(d.stamps)||d.stamps.length>200)throw new Error('设计须包含 stamps 数组，最多200项');
  const seen=new Set(),integer=(n,min,max)=>Number.isInteger(n)&&n>=min&&n<=max,text=(s,max,required)=>typeof s==='string'&&(!required||!!s.trim())&&Array.from(s).length<=max&&!/[,"<>\r\n\x00]/.test(s);
  let bytes=0;
  for(const s of d.stamps){
    if(!s||!integer(s.stamp_id,980001,989999)||seen.has(s.stamp_id)||!['text','image','sound'].includes(s.kind)||!integer(s.category,1,7)||!integer(s.volume,0,100)||!text(s.name,40,true)||!text(s.message,100,s.kind==='text'))throw new Error('表情编号、名称、类型、分类、内容或音量无效');seen.add(s.stamp_id);
    for(const key of ['artwork','audio'])if(s[key]!=null&&(typeof s[key]!=='string'||s[key].length>5592408||!/^[A-Za-z0-9+/]*={0,2}$/.test(s[key])))throw new Error('图片或声音数据格式无效');
    if(s.kind==='text'&&(s.artwork||s.audio)||s.kind!=='text'&&!s.artwork||s.kind==='sound'&&!s.audio||s.kind==='image'&&s.audio)throw new Error('请为图片类型上传图片，为带声音类型同时上传图片和声音');
    bytes+=(s.artwork?.length||0)*.75+(s.audio?.length||0)*.75;
  }
  if(bytes>16*1024*1024+300)throw new Error('全部表情图片与转换后的声音合计最多16MB');
}
$('#custom-stamp-save').onclick=()=>customStampAction(async()=>{const e=customStampEditor;validateCustomStampDesign(e.config);const d=await api('/api/custom-stamps',{method:'PUT',body:JSON.stringify({expected_revision:e.revision,config:e.config})});e.config=d.config;e.revision=d.revision;e.next=d.next_stamp_id;e.published=d.published||{};e.occupied=d.occupied||{};e.dirty=false;clearViewAlert('custom-stamps');renderCustomStamps();$('#custom-stamp-status').textContent='草稿已保存，可生成资源更新包。';toast('表情草稿已保存')});
$('#custom-stamp-package').onclick=()=>customStampAction(async()=>{const e=customStampEditor;if(e.dirty)throw new Error('请先保存当前修改');const r=await fetch('/api/custom-stamps/export',{method:'POST',headers:{'Content-Type':'application/json','X-Kairisei-Admin-Action':'apply'},body:JSON.stringify({expected_revision:e.revision})});if(!r.ok){const d=await r.json(),error=new Error(d.error||'生成失败');error.status=r.status;throw error}const url=URL.createObjectURL(await r.blob()),a=document.createElement('a');a.href=url;a.download=`自制表情资源-v${e.revision}.zip`;a.click();setTimeout(()=>URL.revokeObjectURL(url),60000);$('#custom-stamp-status').textContent='更新包已生成。按包内说明停服备份、核对基准、合并资源并重启；使用CDN时同步资源。';toast('表情资源更新包已生成')});
$('#custom-stamp-json').onclick=()=>downloadContent(customStampEditor.config,'自制表情设计.json');
$('#custom-stamp-import').onchange=()=>customStampAction(async()=>{const next=await importContent($('#custom-stamp-import'));if(!next)return;validateCustomStampDesign(next);const e=customStampEditor,ids=new Set(next.stamps.map(s=>s.stamp_id));for(const s of e.config.stamps)if(e.published[s.stamp_id]&&!ids.has(s.stamp_id))next.stamps.push(s);if(next.stamps.length>200)throw new Error('合并已发布设计后超过200项');if(!confirm('导入将替换当前草稿，已发布但未在文件中的表情会保留。继续？'))return;e.config=next;e.selected=next.stamps[0]?.stamp_id||0;customStampChanged();renderCustomStamps()});

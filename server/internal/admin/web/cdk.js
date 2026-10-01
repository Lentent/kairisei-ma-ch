'use strict';
document.querySelector('#redeem-form').addEventListener('submit', async event => {
  event.preventDefault();
  const button=document.querySelector('#submit'),status=document.querySelector('#status'),password=document.querySelector('#password');
  if(button.disabled)return;
  button.disabled=true;status.textContent='正在验证并兑换…';
  try {
    const response=await fetch('/api/cdk/redeem',{method:'POST',headers:{'Content-Type':'application/json','X-Kairisei-CDK':'1'},body:JSON.stringify({username:document.querySelector('#username').value.trim(),password:password.value,code:document.querySelector('#code').value.trim()})});
    const data=await response.json();
    if(!response.ok||data.state!=='PASS')throw new Error(data.error||'兑换未完成，请稍后重试');
    status.textContent=`${data.message}。\n角色 UID：${data.result.user_id} · ${data.result.title}`;
    document.querySelector('#code').value='';
  } catch(error) { status.textContent=error.message; }
  finally { password.value='';button.disabled=false; }
});

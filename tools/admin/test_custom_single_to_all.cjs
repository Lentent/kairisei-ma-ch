'use strict';
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const vm=require('node:vm');
const source=fs.readFileSync(path.join(__dirname,'../../server/internal/admin/web/admin_custom_cards.js'),'utf8');
const elements=new Map();
let card,dirty=0;
const notices=[];
const context=vm.createContext({
  currentCustomCard:()=>card,
  customCardChanged:()=>dirty++,
  renderCustomDescriptionHelp:()=>{},
  customPresentationName:String,
  toast:message=>notices.push(message),
  esc:value=>String(value??'').replaceAll('&','&amp;').replaceAll('<','&lt;').replaceAll('"','&quot;'),
  $:selector=>{if(!elements.has(selector))elements.set(selector,{});return elements.get(selector)},
  $$:()=>[],
});
// Load the actual helpers and renderer without page-level event registrations.
vm.runInContext(source.slice(0,source.indexOf('function customValueLabel')),context);
vm.runInContext(source.slice(source.indexOf('function migrateLegacyCustomSingleToAll'),source.indexOf('const customCardDetailFields')),context);
const run=code=>vm.runInContext(code,context);
run(`customCardEditor.help.TURN_SINGLE_TO_ALL={name:'本回合单体全体化',description:'选定角色后续手牌',template_role:Array(33).fill('')};
customCardEditor.help.TURN_SINGLE_TO_ALL.template_role[8]='TURN_SINGLE_TO_ALL';
customCardEditor.help.TURN_SINGLE_TO_ALL.template_role[9]='SELF';
customCardEditor.help.TURN_SINGLE_TO_ALL.template_role[20]='1';`);
function skill(root,fn,kind='SUPPORT'){
  const row=Array(50).fill('');row[0]=String(root);row[49]=String(fn);row[10]=kind;row[19]='SELF';return row;
}
function reset(kind='SUPPORT'){
  const role=Array(33).fill('');role[0]='101';role[8]='TURN_SINGLE_TO_ALL';role[9]='SELF';role[20]='1';
  card={skills:[skill(11,101,kind),skill(11,102,kind),skill(12,103,kind)],roles:[role],role_sources:[{card_id:0,index:0}],action_sources:[{function_id:101,card_id:5}]};
}
reset();
run('renderCustomSkills()');
assert.match(elements.get('#custom-card-skills').innerHTML,/全体化生效对象/);
assert.match(elements.get('#custom-card-skills').innerHTML,/value="SELF" selected>自身有用/);
assert.match(elements.get('#custom-card-skills').innerHTML,/己方一人有用/);
assert.match(elements.get('#custom-card-skills').innerHTML,/全队有用/);
run("changeCustomSingleToAllScope(0,'USER_ONE')");
assert.equal(card.roles[0][9],'USER_ONE');
assert.equal(card.skills[0][19],'USER_ONE');
assert.equal(card.skills[1][19],'USER_ONE');
assert.equal(card.skills[2][19],'SELF');
assert.equal(card.action_sources.length,0);
assert.match(run('customEffectTargetLabel(currentCustomCard(),currentCustomCard().roles[0])'),/选中的己方一人/);
run("changeCustomSingleToAllScope(0,'USER_ALL')");
assert.equal(card.roles[0][9],'USER_ALL');
run("changeCustomSingleToAllScope(0,'SELF')");
assert.equal(card.roles[0][9],'SELF');
assert.equal(card.roles[0][20],'1');
// Old inert SELF markers still mean the whole team until explicitly edited.
card.roles[0][20]='';
assert.equal(run('customSingleToAllScope(currentCustomCard().roles[0])'),'USER_ALL');
run("changeCustomSingleToAllScope(0,'SELF')");
assert.equal(card.roles[0][20],'1');
assert.equal(run('customSingleToAllScope(currentCustomCard().roles[0])'),'SELF');
// Old per-card switches migrate to team scope, while new templates default SELF.
reset();card.turn_single_to_all=true;card.roles=[];card.role_sources=[];
run('migrateLegacyCustomSingleToAll(currentCustomCard())');
assert.equal(card.roles.length,3);
assert.ok(card.roles.every(row=>row[9]==='USER_ALL'&&row[20]==='1'));
assert.equal(card.turn_single_to_all,undefined);
reset('ATTACK');
run("changeCustomSingleToAllScope(0,'USER_ONE')");
assert.equal(card.roles[0][9],'SELF');
assert.equal(card.skills[0][19],'SELF');
assert.match(notices.at(-1),/请选择|请使用/);
assert.match(elements.get('#custom-card-skills').innerHTML,/value="SELF" selected>自身有用/);
assert.ok(dirty>0);
console.log('PASS: scope controls, ally selection, branch isolation, legacy migration and attack-template validation');

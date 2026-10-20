'use strict';
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const vm=require('node:vm');
const source=fs.readFileSync(path.join(__dirname,'../../server/internal/admin/web/admin_custom_cards.js'),'utf8');
const elements=new Map();
const element=selector=>{if(!elements.has(selector))elements.set(selector,{value:''});return elements.get(selector)};
const context=vm.createContext({$:element,$$:()=>[],esc:String,updateDraftIndicators:()=>{}});
vm.runInContext(source,context);
const run=code=>vm.runInContext(code,context);
const card={card_id:98000001,template_card_id:101,name:'双属性验收',prefix:'',cost:3,arthur_type:1,attribute:'FIRE',initial:{hp:1,attack:0,magic:0,mind:0},maximum:{hp:2,attack:0,magic:0,mind:0},love_bonus:{hp:0,attack:0,magic:0,mind:0},skills:[Array(50).fill('')],roles:[Array(33).fill('')],role_sources:[{card_id:101,index:0}]};
context.card=card;
run('customCardEditor.config.cards=[card];customCardEditor.selected=card.card_id');
const attributes=['FIRE','ICE','WIND','LIGHT','DARK'];
for(let i=0;i<attributes.length;i++)for(let j=i;j<attributes.length;j++){
  const expected=i===j?attributes[i]:`${attributes[i]}_${attributes[j]}`;
  element('#custom-card-attr').value=attributes[j];
  element('#custom-card-attr-secondary').value=i===j?'':attributes[i];
  element('#custom-card-attr-secondary').onchange();
  assert.equal(card.attribute,expected);
  assert.equal(run('customCardEditor.dirty'),true);
  // Both dropdowns round-trip a saved/imported design, including dual templates.
  context.savedAttribute=expected;
  run('renderCustomCardAttributes(...savedAttribute.split("_"))');
  assert.equal(element('#custom-card-attr').value,attributes[i]);
  assert.equal(element('#custom-card-attr-secondary').value,i===j?'':attributes[j]);
  assert.equal(element('#custom-card-attribute-info').hidden,i===j);
  assert.doesNotMatch(element('#custom-card-attr-secondary').innerHTML,new RegExp(`value="${attributes[i]}"`));
  const imported=JSON.parse(JSON.stringify({cards:[card]}));
  context.imported=imported;
  run('validateCustomCardDesign(imported)');
}
// Changing the first attribute to the existing second one falls back to single.
element('#custom-card-attr').value='ICE';
element('#custom-card-attr-secondary').value='ICE';
element('#custom-card-attr').onchange();
assert.equal(card.attribute,'ICE');
assert.equal(element('#custom-card-attr-secondary').value,'');
for(const attribute of ['',null,3,'FIRE_FIRE','FIRE_ICE_WIND','FIRE/ICE','fire','FIRE_UNKNOWN']){
  context.imported={cards:[{...card,attribute}]};
  assert.throws(()=>run('validateCustomCardDesign(imported)'));
}
console.log('Custom card attributes: 5 singles, 10 pairs, dropdown changes and JSON import PASS');

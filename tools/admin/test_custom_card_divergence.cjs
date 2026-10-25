'use strict';
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const vm=require('node:vm');
const source=fs.readFileSync(path.join(__dirname,'../../server/internal/admin/web/admin_custom_cards.js'),'utf8');
const elements=new Map();
function element(selector){
  if(!elements.has(selector)){
    let html='',value='',options=[];
    const select=/-target$|-recipe$/.test(selector);
    elements.set(selector,{
      get innerHTML(){return html},set innerHTML(v){html=v;options=[...v.matchAll(/<option value="([^"]*)"/g)].map(m=>m[1]);if(select)value=options[0]||''},
      get value(){return value},set value(v){value=select&&!options.includes(String(v))?'':String(v)},
    });
  }
  return elements.get(selector);
}
const context=vm.createContext({$:element,$$:()=>[],esc:String,num:String,updateDraftIndicators:()=>{},confirm:()=>true});
vm.runInContext(source,context);
const run=code=>vm.runInContext(code,context);
const base={card_id:98000001,template_card_id:101,name:'乖离验收',prefix:'自制',cost:3,arthur_type:1,attribute:'FIRE',rarity_rank:5,initial:{hp:1,attack:0,magic:0,mind:0},maximum:{hp:2,attack:0,magic:0,mind:0},love_bonus:{hp:0,attack:0,magic:0,mind:0},skills:[Array(50).fill('')],roles:[Array(33).fill('')],role_sources:[{card_id:101,index:0}]};
const evolved={...structuredClone(base),card_id:98000002,template_card_id:102,rarity_rank:6};
const divergent={...structuredClone(evolved),card_id:98000003,template_card_id:103,arthur_type:2,attribute:'ICE'};
const recipes=[
  {from_card_id:101,to_card_id:102,from_rarity:5,to_rarity:6,gold:30000}, // Old API defaults to NORMAL.
  {evolution_type:1,from_card_id:101,to_card_id:103,from_rarity:5,to_rarity:6,gold:30000,material_text:'素材 × 2（每张名声 3）'},
  {evolution_type:1,from_card_id:102,to_card_id:103,from_rarity:6,to_rarity:6,gold:20000,material_text:'素材 × 2（每张名声 3）'},
  {evolution_type:1,from_card_id:103,to_card_id:102,from_rarity:6,to_rarity:6,gold:20000,material_text:'素材 × 2（每张名声 3）'},
];
Object.assign(context,{cards:[base,evolved,divergent],recipes});
run(`customCardEditor.config.cards=cards;customCardEditor.evolutionRecipes=recipes;customCardEditor.selected=cards[0].card_id;
renderCustomCardList=()=>{};renderCustomCards=()=>renderCustomCardEvolution();customCardAction=fn=>fn();toast=()=>{};
renderCustomCardEvolution()`);
assert.match(element('#custom-card-evolution-target').innerHTML,/98000002/);
assert.doesNotMatch(element('#custom-card-evolution-target').innerHTML,/98000003/);
assert.match(element('#custom-card-divergence-target').innerHTML,/98000003/);
assert.equal(element('#custom-card-evolution-recipe').value,'101:102');
assert.equal(element('#custom-card-divergence-recipe').value,'101:103');
element('#custom-card-evolution-target').value='98000002';
element('#custom-card-evolution-target').onchange();
element('#custom-card-divergence-target').value='98000003';
element('#custom-card-divergence-target').onchange();
assert.equal(base.evolution.target_card_id,evolved.card_id);
assert.equal(base.divergence.target_card_id,divergent.card_id);
assert.equal(base.divergence.recipe_from_card_id,101);
assert.match(element('#custom-card-divergence-info').textContent,/每张名声 3/);
run('validateCustomCardDesign(customCardEditor.config)');
// Same-rarity cross-job target, then reverse conversion from the other form.
run('customCardEditor.selected=cards[1].card_id;renderCustomCardEvolution()');
assert.doesNotMatch(element('#custom-card-evolution-target').innerHTML,/98000003/);
element('#custom-card-divergence-target').value='98000003';
element('#custom-card-divergence-target').onchange();
assert.equal(evolved.divergence.recipe_from_card_id,102);
run('customCardEditor.selected=cards[2].card_id;renderCustomCardEvolution()');
element('#custom-card-divergence-target').value='98000002';
element('#custom-card-divergence-target').onchange();
assert.equal(divergent.divergence.recipe_from_card_id,103);
// Saved mismatched recipes must be shown as invalid, never used for creating a form.
evolved.divergence.recipe_from_card_id=101;
evolved.divergence.recipe_to_card_id=102;
run('customCardEditor.selected=cards[1].card_id;renderCustomCardEvolution()');
assert.equal(run('selectedCustomEvolutionRecipe("divergence")'),undefined);
assert.match(element('#custom-card-divergence-info').textContent,/没有匹配/);
element('#custom-card-divergence-recipe').value='102:103';
element('#custom-card-divergence-recipe').onchange();
assert.equal(evolved.divergence.recipe_from_card_id,102);
// Clearing one direction keeps normal evolution and the reverse direction.
run('customCardEditor.selected=cards[0].card_id;renderCustomCardEvolution()');
element('#custom-card-divergence-target').value='';
element('#custom-card-divergence-target').onchange();
assert.equal(base.divergence,undefined);
assert.equal(base.evolution.target_card_id,evolved.card_id);
assert.equal(divergent.divergence.target_card_id,evolved.card_id);
for(const route of [[],{},'bad',{target_card_id:1,recipe_from_card_id:101,recipe_to_card_id:103},{target_card_id:98000003,recipe_from_card_id:0,recipe_to_card_id:103}]){
  context.imported={cards:[{...base,divergence:route}]};
  assert.throws(()=>run('validateCustomCardDesign(imported)'));
}
(async()=>{
  // Creating a GOD form preserves the recipe template's job and attribute.
  context.api=async()=>({card:structuredClone(divergent)});
  await element('#custom-card-divergence-create').onclick();
  const created=run('currentCustomCard()');
  assert.equal(created.arthur_type,2);
  assert.equal(created.attribute,'ICE');
  assert.equal(created.name,base.name);
  assert.equal(created.rarity_rank,6);
  assert.equal(base.divergence.target_card_id,created.card_id);
  // Removing an unpublished target removes both kinds of dangling references.
  base.evolution.target_card_id=created.card_id;
  element('#custom-card-remove').onclick();
  assert.equal(base.evolution,undefined);
  assert.equal(base.divergence,undefined);
  assert.equal(divergent.divergence.target_card_id,evolved.card_id);
  run('validateCustomCardDesign(customCardEditor.config)');
  console.log('PASS: normal/GOD separation, same-rarity cross-job targets, bidirectional routes, fame display, import, form creation and removal');
})().catch(error=>{console.error(error);process.exitCode=1});

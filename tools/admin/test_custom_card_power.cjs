 'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
const elements=new Map();
const context=vm.createContext({esc:String,$:s=>{if(!elements.has(s))elements.set(s,{});return elements.get(s)},$$:()=>[]});
vm.runInContext(fs.readFileSync(path.join(__dirname,'../../server/internal/admin/web/admin_custom_cards.js'),'utf8'),context);
vm.runInContext(`
customCardEditor.help.CUSTOM_POWER_BOOST={name:'本组效果数值提升',description:'提升威力',parameters:[{name:'提升百分比',unit:'percent',description:'80表示提升80%'}],template_role:Array(33).fill('')};
const role=Array(33).fill('');role[0]='101';role[8]='CUSTOM_POWER_BOOST';role[20]='80';
const skill=Array(50).fill('');skill[0]='101';skill[49]='101';
customCardEditor.config.cards=[{card_id:98000001,skills:[skill],roles:[role],role_sources:[{card_id:0,index:0,min_chain:4}]}];
customCardEditor.selected=98000001;
renderCustomDescriptionHelp=()=>{};
renderCustomSkills();
`,context);
const html=elements.get('#custom-card-skills').innerHTML;
assert.match(html,/提升百分比/);assert.match(html,/value="80"/);assert.match(html,/value="4" selected/);
assert.match(html,/当前效果组内的效果/);assert.doesNotMatch(html,/全体化生效对象/);
console.log('Custom power boost percentage editor and 4C condition PASS');

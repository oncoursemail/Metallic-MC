// Lightweight DOM-host testing without browser, network, or dependencies.
// Checks scripted page generation and local-only permission/moderation logic.
const fs=require('fs');const vm=require('vm');const assert=require('assert');
const source=fs.readFileSync(require('path').resolve(__dirname,'../app.js'),'utf8');
function load(page='overview'){
 const handlers={};const els={};const switches=['foundry','nocturne','fieldnote'].map(concept=>element({concept}));const navs=['overview','servers','fleet','moderation','star','legacy','audit'].map(page=>element({page}));
 function element(dataset={}){return {dataset:{...dataset},innerHTML:'',textContent:'',attributes:{},setAttribute(k,v){this.attributes[k]=v},removeAttribute(k){delete this.attributes[k]},classList:{toggle(){}},closest(selector){if(selector==='button,[data-close-modal],[data-close-drawer]')return this;if(selector==='.concept-switch'&&this.dataset.concept)return this;return null},focus(){},getAttribute(k){return this.attributes[k]},hasAttribute(k){return Object.hasOwn(this.attributes,k)}}}
 const doc={querySelector(q){els[q]??=element();return els[q]},querySelectorAll(q){if(q==='.concept-switch')return switches;if(q==='.nav-item')return navs;return []},addEventListener(type,fn){handlers[type]=fn}};
 const ctx={document:doc,location:{hash:'#'+page},history:{replaceState(){}},window:{scrollTo(){},scrollY:0},Date,console,URL,Blob,clearTimeout,setTimeout:(cb)=>0,requestAnimationFrame:(cb)=>cb(),FormData:class{constructor(form){this.data=form.fakeData||{}}get(k){return this.data[k]??null}}};
 vm.runInNewContext(source,ctx,{filename:'app.js'});
 return {html:()=>els['#main'].innerHTML,modal:()=>els['#modal-root'].innerHTML,drawer:()=>els['#drawer-root'].innerHTML,nav:navs,els,handlers,button(dataset){return element(dataset)},click(el){handlers.click({target:el})},formSubmit(id,values){handlers.submit({preventDefault(){},target:{id, fakeData:values}})}};
}
const home=load();assert(home.html().includes('Network topology'));assert(home.html().includes('Central Lobby'));assert(home.html().includes('Illustrative systems only'));
const fleet=load('fleet');assert(fleet.html().includes('Interactive cluster map'));assert(fleet.html().includes('PHYSICAL HOST'));
const servers=load('servers');assert(servers.html().includes('Community Test Realm'));
servers.click(servers.button({openServer:'community-07'}));assert(servers.drawer().includes('Community Test Realm'));assert(servers.drawer().includes('Suspend'));
servers.click(servers.button({action:'suspend',target:'community-07'}));assert(servers.modal().includes('Required'));servers.formSubmit('action-form',{reason:'Confirmed repeated and verified policy violations.'});assert(servers.html().includes('SUSPENDED'));assert(servers.drawer().includes('Unsuspend'));
servers.click(servers.button({action:'archive',target:'community-07'}));servers.formSubmit('action-form',{reason:'Authorized archive of test server.',confirm:'Community Test Realm'});assert(servers.drawer().includes('ARCHIVED'));
const archival=load('servers');archival.click(archival.button({openServer:'community-07'}));
archival.click(archival.button({action:'archive',target:'community-07'}));archival.formSubmit('action-form',{reason:'Administrative retention before migration.',confirm:'Community Test Realm'});
assert(archival.drawer().includes('ARCHIVED'));archival.els['#toast-undo'].onclick();
assert(archival.drawer().includes('RUNNING'));assert(archival.drawer().includes('12 / 40'));
const created=load('servers');created.click(created.button({action:'create'}));
created.formSubmit('create-form',{name:'<img src=x onerror=alert(1)>',software:'Paper',node:'starai'});
assert(!created.html().includes('<img src=x'));assert(created.html().includes('&lt;img src=x'));
assert(!/\bfetch\s*\(|XMLHttpRequest|new\s+WebSocket/.test(source),'Preview must never use real network calls');
const moderation=load('moderation');assert(moderation.html().includes('REPORT QUEUE'));moderation.handlers.change({target:{id:'role-switch',value:'viewer'}});moderation.click(moderation.button({action:'playerban',target:'DemoPlayer_42'}));assert(!moderation.modal().includes('Ban player'));
moderation.handlers.change({target:{id:'role-switch',value:'moderator'}});moderation.click(moderation.button({action:'playerban',target:'DemoPlayer_42'}));assert(moderation.modal().includes('Ban player'));moderation.formSubmit('action-form',{reason:'Reviewed report evidence and approved temporary ban.'});assert(moderation.html().includes('REPORT QUEUE / 2'));
const star=load('star');assert(star.html().includes('Engineering hypotheses'));star.click(star.button({action:'stage-ai',target:'AI-028'}));star.formSubmit('action-form',{reason:'Measure the isolated test against baseline'});assert(star.html().includes('IN STAGING'));
const legacy=load('legacy');assert(legacy.html().includes('Read-only by design'));
const audit=load('audit');assert(audit.html().includes('Audit history'));
console.log('PASS: 7 views, map, interactive server drawer, demo suspend/archive, RBAC, player ban, Star AI staging, and audit view.');

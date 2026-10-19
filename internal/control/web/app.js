'use strict';

const $ = id => document.getElementById(id);
const select = s => document.querySelector(s);
const navLabels = {overview:'Overview',nodes:'Compute nodes',resources:'Resource manager',servers:'Game services',operations:'Operations'};
let latest = { nodes: [] };
let resources = null;
let resourceProfileId = 'mc-lobby';

function setText(id, value) { const el=$(id);if(el) el.textContent=value; }
function humanBytes(value) { if(!Number.isFinite(value)||value<0)return '—';const units=['B','KB','MB','GB','TB'];const n=Math.min(Math.floor(Math.log(Math.max(value,1))/Math.log(1024)),units.length-1);return `${(value/1024**n).toFixed(n===0?0:1)} ${units[n]}`; }
function element(tag, className, text) {const el=document.createElement(tag);if(className)el.className=className;if(text!==undefined)el.textContent=String(text);return el;}
function emptyState(title, subtitle) {const wrap=element('div','empty-state');wrap.append(element('div','empty-icon','⌁'),element('strong','',title),element('p','',subtitle));return wrap;}
function status(isOK,label) {const e=element('span','state-pill '+(isOK?'state-ok':'state-down'));e.append(element('span','tiny-dot'),document.createTextNode(label));return e;}
function row(title,sub,ok,last) {const e=element('div','list-item');const left=element('div','list-details');left.append(element('strong','',title),element('small','',sub));e.append(left,status(ok,last));return e;}
function showPane(pane) { if(!navLabels[pane])return;document.querySelectorAll('[data-pane]').forEach(el=>el.classList.toggle('hidden',el.dataset.pane!==pane));$('resource-profile')?.addEventListener('change',e=>{resourceProfileId=e.target.value;updateResources();});
document.querySelectorAll('[data-nav]').forEach(el=>el.classList.toggle('active',el.dataset.nav===pane));setText('page-label',navLabels[pane]);}
function ui() {
 const nodes=latest.nodes||[];
 const online=nodes.filter(n=>n.online);
 const allServices=nodes.flatMap(n=>(n.instances||[]).map(s=>({...s,nodeId:n.nodeId,nodeOnline:n.online})));
 const healthy=allServices.filter(s=>s.nodeOnline&&s.healthy).length;
 const validAvg=online.length?online.reduce((acc,n)=>acc+n.cpuPercent,0)/online.length:null;
 setText('node-count', `${online.length}`);
 setText('node-meta',`${nodes.length} registered · ${nodes.length-online.length} offline`);
 setText('service-count',`${healthy} / ${allServices.length}`);
 setText('service-meta',`${allServices.length-healthy} unavailable or unreachable`);
 setText('cpu-average',validAvg===null?'—':`${validAvg.toFixed(1)}%`);
 setText('agent-count',`${online.length} / ${nodes.length}`);
 const warning=nodes.length===0?'Awaiting telemetry':nodes.length!==online.length||allServices.some(s=>!s.healthy)||allServices.some(s=>!s.nodeOnline)?'Attention required':'All reporting nodes healthy';
 setText('network-state',warning);
 setText('network-detail',nodes.length===0?'No registered agents yet.': `${online.length} online node${online.length===1?'':'s'} · ${healthy} responding service${healthy===1?'':'s'}`);
 $('network-dot').className='status-dot '+(nodes.length===0?'neutral':warning==='Attention required'?'danger':'success');
 setText('node-table-count',`${nodes.length} NODES`);
 setText('server-table-count',`${allServices.length} SERVICES`);
 const previewNodes=$('overview-nodes');previewNodes.replaceChildren();
 if(!nodes.length)previewNodes.append(emptyState('No connected nodes','Start Metallic Agent on one of your Linux servers to see real telemetry.'));
 else nodes.slice(0,5).forEach(n=>previewNodes.append(row(n.hostname,`${n.nodeId} · CPU ${n.cpuPercent.toFixed(1)}% · MEM ${humanBytes(n.memoryUsed)}`,n.online,n.online?'Online':'Offline')));
 const previewServices=$('overview-services');previewServices.replaceChildren();
 if(!allServices.length)previewServices.append(emptyState('No service probes','Configure METALLIC_SERVICES in your agent environment.'));
 else allServices.slice(0,5).forEach(s=>previewServices.append(row(s.name,`${s.kind||'TCP'} · ${s.nodeId}`,s.nodeOnline&&s.healthy,s.nodeOnline&&s.healthy?'Reachable':'Down')));
 const nodeTable=$('nodes-table');nodeTable.replaceChildren();
 if(!nodes.length)nodeTable.append(emptyState('No nodes registered','The agent sends its first heartbeat immediately after starting.'));
 else nodes.forEach(n=>{const card=element('div','node-card');const top=element('div','node-card-top');const info=element('div','node-card-info');info.append(element('span','node-symbol','▤'),element('div','node-name',n.hostname));top.append(info,status(n.online,n.online?'ONLINE':'OFFLINE'));const grid=element('div','node-properties');[['NODE ID',n.nodeId],['CPU',`${n.cpuPercent.toFixed(1)}%`],['MEMORY',`${humanBytes(n.memoryUsed)} / ${humanBytes(n.memoryTotal)}`],['DISK',`${humanBytes(n.diskUsed)} / ${humanBytes(n.diskTotal)}`],['LOAD (1M)',n.load1.toFixed(2)],['LAST SEEN',new Date(n.lastSeen).toLocaleTimeString()]].forEach(([a,b])=>{const cell=element('div','node-property');cell.append(element('small','',a),element('strong','',b));grid.append(cell)});card.append(top,grid);if(n.history?.length>1){const wrap=element('div','history-wrap');wrap.append(element('span','history-label','CPU TREND · LAST 60 REPORTS'));const svg=document.createElementNS('http://www.w3.org/2000/svg','svg');svg.setAttribute('viewBox','0 0 400 48');svg.setAttribute('preserveAspectRatio','none');svg.setAttribute('aria-label','CPU utilization history');const path=document.createElementNS('http://www.w3.org/2000/svg','path');const points=n.history.map((s,i)=>`${i===0?'M':'L'}${(i/(n.history.length-1)*400).toFixed(2)} ${(48-(Math.min(100,Math.max(0,s.cpuPercent))/100*40+4)).toFixed(2)}`).join(' ');path.setAttribute('d',points);path.setAttribute('stroke','#c2df87');path.setAttribute('stroke-width','2.4');path.setAttribute('fill','none');svg.append(path);wrap.append(svg);card.append(wrap)}nodeTable.append(card)});
 const srvTable=$('servers-table');srvTable.replaceChildren();
 if(!allServices.length)srvTable.append(emptyState('No services discovered','Add probes such as lobby@127.0.0.1:25566 and proxy@127.0.0.1:25565.'));
 else allServices.forEach(s=>{const card=element('div','service-row');const left=element('div','service-name');left.append(element('span','node-symbol','▥'),element('div','service-text',s.name));const details=element('div','service-details');details.append(element('span','',s.nodeId),element('code','',s.address));card.append(left,details,status(s.nodeOnline&&s.healthy,s.nodeOnline&&s.healthy?'REACHABLE':'UNREACHABLE'));srvTable.append(card)});
 const ops=$('operations-list');ops.replaceChildren();
 if(!nodes.length)ops.append(row('No node telemetry','Agent installation required',false,'Setup'));
 nodes.filter(n=>!n.online).forEach(n=>ops.append(row(`Node offline: ${n.hostname}`,'No heartbeat received within 35 seconds',false,'Investigate')));
 allServices.filter(s=>!s.healthy||!s.nodeOnline).forEach(s=>ops.append(row(`Service unreachable: ${s.name}`,`${s.nodeId} · ${s.address}`,false,'Investigate')));
 if(nodes.length && !ops.childElementCount)ops.append(emptyState('No active infrastructure alerts','Every configured probe reports reachable and all registered nodes are online.'));
}
function renderResources() {
 const slot=$('resource-candidates');
 if(!slot)return;
 slot.replaceChildren();
 if(!resources || !resources.preview) {
   slot.append(emptyState('No admission data','Connect to Metallic Control to view live resource eligibility.'));
   setText('resource-state','Waiting for controller');
   return;
 }
 const {profiles,preview}=resources;
 const picker=$('resource-profile');
 const existing=Array.from(picker.options).map(o=>o.value).join(',');
 const offered=(profiles||[]).map(p=>p.id).join(',');
 if(existing!==offered) {
   picker.replaceChildren();
   for(const p of (profiles||[])) {
     const opt=document.createElement('option'); opt.value=p.id; opt.textContent=`${p.project} / ${p.name}`;picker.append(opt);
   }
 }
 picker.value=preview.profile.id;
 setText('resource-project',preview.profile.project);
 setText('resource-class',preview.profile.class.toUpperCase());
 setText('resource-description',preview.profile.description);
 setText('resource-cpu',`${(preview.profile.cpuMilli/1000).toFixed(1)} cores`);
 setText('resource-memory',humanBytes(preview.profile.memoryBytes));
 setText('resource-disk',humanBytes(preview.profile.diskBytes));
 setText('resource-node-count',`${preview.candidates.length} NODES CHECKED`);
 setText('resource-state',preview.recommendedNode?`Candidate available: ${preview.recommendedNode}`:'No eligible placement');
 if(!preview.candidates.length) {
   slot.append(emptyState('No nodes reporting','The planner needs authenticated live node telemetry to provide a placement estimate.'));
   return;
 }
 for(const c of preview.candidates) {
   const card=element('div','candidate-card');
   const heading=element('div','candidate-heading');
   heading.append(element('strong','',c.nodeId),status(c.admitted,c.admitted?'ELIGIBLE':'BLOCKED'));
   const grid=element('div','candidate-metrics');
   for(const [k,v] of [['CPU HEADROOM',`${(c.cpuHeadroomMilli/1000).toFixed(1)} cores`],['RAM HEADROOM',humanBytes(c.memoryHeadroomBytes)],['STORAGE HEADROOM',humanBytes(c.diskHeadroomBytes)]]){
     const div=element('div','candidate-metric');div.append(element('small','',k),element('strong','',v));grid.append(div);
   }
   card.append(heading,grid);
   if(c.reasons.length){const why=element('div','candidate-reasons');why.append(element('span','reason-prefix','CHECKS'));for(const reason of c.reasons)why.append(element('span','candidate-reason',reason));card.append(why)}
   else card.append(element('p','candidate-cleared','Passes the conservative snapshot checks. This is not a scheduled reservation.'));
   slot.append(card);
 }
}
async function updateResources() {
 try {
  const res=await fetch('/api/v1/resources?profile='+encodeURIComponent(resourceProfileId),{cache:'no-store',credentials:'same-origin'});
  if(!res.ok)throw new Error('HTTP '+res.status);
  resources=await res.json();renderResources();
 }catch(err) {resources=null;renderResources();}
}
async function update() { try {const res=await fetch('/api/v1/overview',{cache:'no-store',credentials:'same-origin'});if(res.status===401){location.href='/login';return;}if(!res.ok)throw new Error('HTTP '+res.status);latest=await res.json();setText('api-state','Control API connected');$('api-state').className='mini-indicator connected';setText('updated-at','SYNCED '+new Date().toLocaleTimeString());ui();await updateResources();}catch(err){setText('api-state','API unavailable');$('api-state').className='mini-indicator disconnected';setText('updated-at','Latest data may be stale');}}
document.querySelectorAll('[data-nav]').forEach(e=>e.addEventListener('click',()=>showPane(e.dataset.nav)));
document.querySelectorAll('[data-target]').forEach(e=>e.addEventListener('click',()=>showPane(e.dataset.target)));
update();setInterval(update,10000);

fetch("/api/v1/architect/capabilities",{credentials:"same-origin"}).then(r=>{if(r.ok)document.getElementById("architect-link").hidden=false;}).catch(()=>{});

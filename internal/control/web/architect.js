'use strict';
const byId=id=>document.getElementById(id);
const base='/api/v1/architect/';
let ticketTimer;
let catalogTarget;
function node(tag,cls,text){const e=document.createElement(tag);if(cls)e.className=cls;if(text!==undefined)e.textContent=String(text);return e;}
function notice(message,isError=false){const e=byId('notice');e.textContent=message;e.className='notice'+(isError?' error':'');e.hidden=false;}
async function api(path,options={}){
 const headers={...(options.body?{'Content-Type':'application/json'}:{}),...(options.method&&options.method!=='GET'?{'X-Metallic-Intent':'architect'}:{}),...(options.headers||{})};
 const response=await fetch(base+path,{...options,headers,credentials:'same-origin',cache:'no-store'});
 if(!response.ok){const text=(await response.text()).slice(0,300);throw new Error(response.status===401?'Sign in again before changing account links or host credentials.':text||'The request could not be completed.');}
 return response.status===204?null:response.json();
}
async function action(button,fn){button.disabled=true;try{await fn();}catch(error){notice(error.message,true);}finally{button.disabled=false;}}
function hideTicket(){clearTimeout(ticketTimer);byId('pairing-ticket').value='';byId('pairing-result').hidden=true;}
async function loadDiscord(){
 const status=await api('discord');
 const badge=byId('discord-badge'),connect=byId('discord-connect'),unlink=byId('discord-unlink');
 badge.textContent=status.linked?'Linked':status.configured?'Not linked':'Setup required';badge.className='tag'+(status.linked?' ok':'');
 byId('discord-description').textContent=status.linked?'Your Discord identity is connected to this private administrator account.':'Connect a Discord identity to your Metallic account.';
 byId('discord-detail').textContent=status.linked?status.username+' · '+status.discordId:status.configured?'Optional identity linking. No email, messages, or guild access requested.':'An owner-controlled Discord application must be configured before linking.';
 connect.hidden=status.linked;connect.disabled=!status.configured;unlink.hidden=!status.linked;
}
async function loadHosts(){
 const [hostData,serverData]=await Promise.all([api('hosts'),api('servers')]);
 const list=byId('host-list'),select=byId('allocation-host'),previous=select.value;
 list.replaceChildren();select.replaceChildren(new Option('Choose a paired host',''));
 byId('host-persistence').textContent=hostData.persistent?'Host registrations and allocation plans are saved. Pairing tickets expire when the controller restarts.':'Host storage is temporary. Restarting the controller requires pairing again.';
 const hosts=hostData.hosts||[];
 if(!hosts.length)list.append(node('p','empty','No hosts paired. Create a ticket to prepare your first host.'));
 for(const host of hosts){
  const row=node('div','host-row'),info=node('div'),controls=node('div','row-actions');
  info.append(node('strong','',host.name),node('small','',host.id));
  const expired=Date.parse(host.credential_expires_at)<=Date.now();
  controls.append(node('span','tag',host.revoked?'Revoked':expired?'Credential expired':'Paired · no tunnel'));
  if(!host.revoked){const revoke=node('button','danger','Revoke');revoke.type='button';revoke.addEventListener('click',()=>action(revoke,async()=>{await api('hosts/'+encodeURIComponent(host.id),{method:'DELETE'});notice('Host credential revoked. Route policy now rejects this host.');await loadHosts();}));controls.append(revoke);}
  row.append(info,controls);list.append(row);
  if(!host.revoked&&!expired)select.append(new Option(host.name+' · '+host.id.slice(0,12),host.id));
 }
 if([...select.options].some(o=>o.value===previous))select.value=previous;
 byId('allocate-server').disabled=select.options.length===1;
 const serverList=byId('server-list');serverList.replaceChildren();
 for(const server of serverData.servers||[]){
  const row=node('div','server-row'),info=node('div'),r=server.resources||{};
  info.append(node('strong','',server.id),node('small','',r.cpu_cores+' CPU · '+r.memory_mb+' MiB · '+Math.round(r.disk_mb/1024)+' GiB · port '+server.game_port),node('small','','Host '+server.host_id));
  row.append(info,node('span','tag',server.state==='policy_registered'?'Allocation plan':server.state));serverList.append(row);
 }
}
byId('discord-connect').addEventListener('click',()=>action(byId('discord-connect'),async()=>{
 const result=await api('discord/start',{method:'POST',body:'{}'});const target=new URL(result.authorizationUrl);
 if(target.origin!=='https://discord.com'||target.pathname!=='/oauth2/authorize')throw new Error('Unexpected identity-provider destination.');
 window.location.assign(target.href);
}));
byId('discord-unlink').addEventListener('click',()=>action(byId('discord-unlink'),async()=>{await api('discord',{method:'DELETE'});await loadDiscord();notice('Discord account unlinked.');}));
byId('pair-host').addEventListener('click',()=>action(byId('pair-host'),async()=>{
 hideTicket();const ticket=await api('hosts/pairing',{method:'POST',body:'{}'});
 byId('pairing-ticket').value=ticket.token;byId('pairing-expiry').textContent='Expires '+new Date(ticket.expires_at).toLocaleTimeString()+'. Give this ticket only to your host.';
 byId('pairing-result').hidden=false;ticketTimer=setTimeout(hideTicket,Math.max(0,Math.min(300000,Date.parse(ticket.expires_at)-Date.now())));
 notice('A single-use pairing ticket is ready. This does not start a server.');
}));
byId('copy-ticket').addEventListener('click',()=>action(byId('copy-ticket'),async()=>{if(!byId('pairing-ticket').value)throw new Error('The ticket has expired.');await navigator.clipboard.writeText(byId('pairing-ticket').value);notice('Pairing ticket copied.');}));
byId('hide-ticket').addEventListener('click',hideTicket);
window.addEventListener('pagehide',hideTicket);
byId('refresh-hosts').addEventListener('click',()=>action(byId('refresh-hosts'),loadHosts));
byId('allocation-form').addEventListener('submit',event=>{
 event.preventDefault();action(byId('allocate-server'),async()=>{
  const request={host_id:byId('allocation-host').value,server_id:byId('server-name').value,game_port:Number(byId('server-port').value),resources:{cpu_cores:Number(byId('server-cpu').value),memory_mb:Number(byId('server-memory').value),disk_mb:Number(byId('server-disk').value)*1024,player_slots:Number(byId('server-slots').value)}};
  await api('servers',{method:'POST',body:JSON.stringify(request)});notice('Server allocation plan recorded. Minecraft is not running yet.');await loadHosts();
 });
});
function showPlan(plan){
 const panel=byId('plugin-plan');panel.replaceChildren();panel.hidden=false;
 panel.append(node('span','tag',plan.installable?'Metadata checks passed':'Installation blocked'),node('h3','',(plan.version_name||plan.version_id||'Exact release')+' · '+plan.provider));
 panel.append(node('p','',plan.installable?'This release passed the supported metadata checks. No files have been downloaded or executed.':'Resolve the following requirements before any installation.'));
 const blockers=node('ul');
 for(const blocker of plan.blockers||[])blockers.append(node('li','',blocker.message||blocker.code));
 if(blockers.childElementCount)panel.append(blockers);
 if(plan.artifact){
  const a=plan.artifact,section=node('div','artifact');
  section.append(node('strong','',a.filename),node('p','',Number(a.size).toLocaleString()+' bytes'),node('p','','Source: '+a.url));
  if(a.sha512)section.append(node('code','','SHA-512 '+a.sha512));else if(a.sha256)section.append(node('code','','SHA-256 '+a.sha256));
  panel.append(section);
 }
 if((plan.dependencies||[]).length)panel.append(node('p','small',plan.dependencies.length+' dependency records inspected. Required dependencies remain blocked unless fully resolved.'));
 panel.scrollIntoView({behavior:window.matchMedia('(prefers-reduced-motion: reduce)').matches?'auto':'smooth',block:'nearest'});
}
function showSearch(result,target){
 const results=byId('catalog-results');results.replaceChildren();byId('plugin-plan').hidden=true;
 const hits=result.hits||[];
 if(!hits.length){results.append(node('p','empty','No matching releases found for this catalog and Minecraft version. Try another name or version.'));return;}
 for(const hit of hits){
  const row=node('article','catalog-hit'),info=node('div'),controls=node('div','version-controls');
  info.append(node('h3','',hit.name),node('p','',hit.description||'No catalog description.'),node('p','small',(hit.author?hit.author+' · ':'')+hit.project_id));
  const version=node('input');version.placeholder='Exact version / ID';version.maxLength=128;version.setAttribute('aria-label','Exact release for '+hit.name);
  const inspect=node('button','secondary','Inspect release');inspect.type='button';
  inspect.addEventListener('click',()=>action(inspect,async()=>{
   if(!version.value.trim())throw new Error('Enter the exact Hangar version name or Modrinth version ID to inspect.');
   const plan=await api('catalog/resolve',{method:'POST',body:JSON.stringify({provider:result.provider,project_id:hit.project_id,version_id:version.value.trim(),target})});
   showPlan(plan);
  }));
  controls.append(version,inspect);row.append(info,controls);results.append(row);
 }
}
byId('catalog-form').addEventListener('submit',event=>{
 event.preventDefault();action(byId('catalog-search'),async()=>{
  const target={minecraft_version:byId('minecraft-version').value.trim(),platform:'paper'};
  const query=new URLSearchParams({provider:byId('catalog-provider').value,query:byId('catalog-query').value.trim(),...target});
  const result=await api('catalog/search?'+query);catalogTarget=target;showSearch(result,target);
 });
});
document.querySelectorAll('.rail nav a').forEach(link=>link.addEventListener('click',()=>{document.querySelectorAll('.rail nav a').forEach(a=>a.classList.remove('active'));link.classList.add('active');}));
(async()=>{
 try{
  await api('capabilities');byId('controller-state').textContent='Controller connected';byId('controller-dot').classList.add('ready');byId('pair-host').disabled=false;
  const outcomes=await Promise.allSettled([loadDiscord(),loadHosts()]);
  for(const result of outcomes)if(result.status==='rejected')notice(result.reason.message,true);
 }catch(error){byId('controller-state').textContent='Controller unavailable';notice(error.message,true);}
})();

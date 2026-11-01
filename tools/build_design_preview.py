#!/usr/bin/env python3
"""Generate an explicitly illustrative, standalone HTML UI review artifact.

No production API, credentials, real host identifiers or real hardware telemetry
are embedded in the preview. Do not confuse it with a production deployment.
"""
from pathlib import Path
import html
import json
import re
import sys

ROOT = Path(__file__).resolve().parents[1]
WEB = ROOT / 'internal/control/web'
MIB = 1024**3
profiles = [
    dict(id='mc-lobby',name='Minecraft lobby',project='Metallic MC',**{'class':'realtime'},type='realtime',cpuMilli=2000,memoryBytes=4*MIB,diskBytes=8*MIB,description='Sample Paper lobby; does not indicate verified player capacity.'),
    dict(id='mc-match',name='Minecraft match',project='Metallic MC',**{'class':'realtime'},type='realtime',cpuMilli=3000,memoryBytes=6*MIB,diskBytes=10*MIB,description='Independently managed minigame instance.'),
    dict(id='metallic-world',name='Metallic world',project='Metallic',**{'class':'realtime'},type='realtime',cpuMilli=4000,memoryBytes=8*MIB,diskBytes=12*MIB,description='Example private world simulation resource budget.'),
    dict(id='star-inference',name='AI inference',project='Star AI',**{'class':'standard'},type='standard',cpuMilli=3000,memoryBytes=8*MIB,diskBytes=4*MIB,description='Example model-serving slice; costs vary by model.'),
    dict(id='star-learning',name='Learning',project='Star AI',**{'class':'batch'},type='batch',cpuMilli=6000,memoryBytes=12*MIB,diskBytes=12*MIB,description='Resumable, deferrable practice job on spare capacity.'),
]
nodes = [
    dict(nodeId='DESIGN-SAMPLE-EPYC',hostname='Sample / Compute 01',cpuPercent=23.4,cpuThreads=48,load1=5.2,cpuPressureSome10=.5,memPressureSome10=.04,pressureAvailable=True,ioPressureSome10=.1,ioPressureAvailable=True,legacyWingsChecked=True,legacyWingsObserved=False,memoryTotal=128*MIB,memoryUsed=58*MIB,diskTotal=240*MIB,diskUsed=172*MIB,swapTotal=8*MIB,swapUsed=2*MIB,online=True,lastSeen='2026-10-18T20:00:00Z',history=[dict(cpuPercent=x) for x in [21,24,27,23,24,25,18,17,23,22,23,26,24,23]],instances=[dict(name='Sample / Lobby-01',address='127.0.0.1:25566',kind='tcp',healthy=True)]),
    dict(nodeId='DESIGN-SAMPLE-EDGE',hostname='Sample / Compute 02',cpuPercent=41.2,cpuThreads=8,load1=3.1,cpuPressureSome10=6.5,memPressureSome10=.4,pressureAvailable=True,ioPressureSome10=.6,ioPressureAvailable=True,legacyWingsChecked=True,legacyWingsObserved=True,memoryTotal=16*MIB,memoryUsed=11*MIB,diskTotal=235*MIB,diskUsed=90*MIB,swapTotal=4*MIB,swapUsed=1*MIB,online=True,lastSeen='2026-10-18T20:00:00Z',history=[dict(cpuPercent=x) for x in [44,46,42,40,52,44,42,41,39,43,41,40]],instances=[dict(name='Sample / Proxy-01',address='127.0.0.1:25565',kind='tcp',healthy=True)]),
]

def preview(profile_id):
    p = next((p for p in profiles if p['id']==profile_id), profiles[0])
    cand=[]
    for n in nodes:
        reasons=[]
        cpu_head=max(0,int(n['cpuThreads']*(75-n['cpuPercent'])*10-1000))
        memory_head=max(0,int((n['memoryTotal']-n['memoryUsed'])-max(3*MIB,n['memoryTotal']//10)))
        disk_head=max(0,int((n['diskTotal']-n['diskUsed'])-max(16*MIB,n['diskTotal']//10)))
        if cpu_head < p['cpuMilli']: reasons.append('Not enough measured CPU headroom')
        if memory_head < p['memoryBytes']: reasons.append('Not enough memory after host reserve')
        if disk_head < p['diskBytes']: reasons.append('Not enough free monitored disk')
        if n['cpuPressureSome10'] > (3 if p['type']=='batch' else 15): reasons.append('CPU pressure is elevated')
        cand.append(dict(nodeId=n['nodeId'],admitted=not reasons,reasons=reasons,cpuHeadroomMilli=cpu_head,memoryHeadroomBytes=memory_head,diskHeadroomBytes=disk_head))
    best=next((n['nodeId'] for n in cand if n['admitted']), '')
    return dict(profiles=profiles,preview=dict(stage='illustrative-fixture',profile=p,candidates=cand,recommendedNode=best,explanation='Design fixture: example calculation is not production admission logic.'))

def main():
    target=Path(sys.argv[1] if len(sys.argv)>1 else '/mnt/data/metallic-control-interactive-preview.html')
    src=(WEB/'index.html').read_text()
    css=(WEB/'styles.css').read_text()
    js=(WEB/'app.js').read_text()
    src=src.replace('<link rel="stylesheet" href="/assets/styles.css">','<style>'+css+'</style>')
    src=src.replace('<script defer src="/assets/app.js"></script>','')
    # Intentionally static, auditable and forbidden from talking to actual servers.
    canned={x['id']:preview(x['id']) for x in profiles}
    mocked='''<script>
    'use strict';
    const syntheticOverview = '''+json.dumps({'nodes':nodes,'stage':'illustrative-fixture'})+''';
    const syntheticProfiles = '''+json.dumps(canned)+''';
    window.fetch = async function(resource) {
      const path = String(resource);
      const profile = new URLSearchParams(path.split('?')[1] || '').get('profile') || 'mc-lobby';
      if (path.includes('/api/v1/resources')) return {ok:true,status:200,json:async()=>syntheticProfiles[profile]||syntheticProfiles['mc-lobby']};
      if (path.includes('/api/v1/overview')) return {ok:true,status:200,json:async()=>syntheticOverview};
      return {ok:false,status:404,json:async()=>({})};
    };
    </script>'''
    notice='''<div role="note" aria-live="off" class="fixture-ribbon" style="position:fixed;left:50%;transform:translateX(-50%);bottom:15px;z-index:100;white-space:nowrap;max-width:97vw;background:#1b2727;color:#f7f7f4;border:1px solid #d67c5a;padding:9px 14px;font:10px monospace;letter-spacing:.05em;box-shadow:0 4px 16px #0002">INTERACTIVE DESIGN FIXTURE · NOT LIVE DATA</div>'''
    src=src.replace('</body>', notice+mocked+'<script>'+js+'\nconst previewSignout=document.querySelector(".signout"); if(previewSignout){ previewSignout.closest("form").addEventListener("submit", e => {e.preventDefault(); alert("This is an offline design preview; no signed-in session exists.");}); }'+'</script></body>')
    target.write_text(src,encoding='utf8')
    print('Saved',target,'bytes',target.stat().st_size)

if __name__=='__main__':main()

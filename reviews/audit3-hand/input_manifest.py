#!/usr/bin/env python3
"""Reconstruct hashes of every input from checks.py, without executing binaries."""
import hashlib,json,pathlib,random,sys
R=pathlib.Path.cwd();sys.path.insert(0,str(R/'bench'));import jsongen
r=random.Random(819);out=[]
def add(label,data,tasks,oracle):out.append(dict(case=label,bytes=len(data),sha256=hashlib.sha256(data).hexdigest(),tasks=tasks,oracle=oracle))
names=['letters','filter4','strip','csv','idents','strings','xlat','json']
for n in [0,1,2,3,4,15,16,17,63,64,65,127,128,129,191,192,193,255,256,257,511,512,513,4095,4096,4097]:
 data=bytes(r.choice(b'abc_012#"\\\n, {}[]:\xff\x00') for _ in range(n));add(f'random-{n}',data,names,'base.c')
for seed in range(20):add('valid-'+str(seed),jsongen.document(512+seed*100,seed),['json'],'jsongen.skeleton')
for n in [63,64,65,127,128,129,511,512,513,1023,1024,1025,(4<<20)-1,4<<20,(4<<20)+1]:
 for backslashes in ([1,2,31,32,63,64,65] if n<2000 else [1,64,65]):
  data=b'["'+b'a'*max(0,n-2)+b'\\'*(2*backslashes)+b'\\"{},[]:", {"x": []}]';add(f'valid-boundary-{n}-{backslashes}',data,['json'],'jsongen.skeleton')
for data in [b'a'*(4<<20)+b'#x\n"\\\"hi" abc',b'"'+b'\\'*(4<<20)+b'"#hi\nabc',b'#'+b'a'*(4<<20)+b'\nabc"x"']:
 add('fsm-big-'+hashlib.sha256(data).hexdigest()[:8],data,['strip','idents','strings','csv'],'base.c')
(R/'reviews/audit3-hand/input-manifest.json').write_text(json.dumps(out,indent=2)+'\n')

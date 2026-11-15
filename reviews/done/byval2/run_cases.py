import os,subprocess,json,time
from pathlib import Path
root=Path('/home/exedev/targets/byval-review')
env=os.environ.copy();env.update(CGO_ENABLED='0',GOTOOLCHAIN='local',GOFLAGS='-buildvcs=false');env.pop('FC_CAPREF',None)
report=root/'results.jsonl'
with report.open('w') as reportfile:
 for d in sorted(root.glob('[0-9][0-9]_*')):
  results={}
  for mode in ('stock','fc','capref'):
   e=env.copy();e['GOCACHE']=str(root/'gocache-stock') if mode=='stock' else '/home/exedev/targets/fc-gocache'
   if mode=='capref':e['FC_CAPREF']='1'
   binary=d/('bin-'+mode)
   cmd=['go','build','-o',str(binary),'.'] if mode=='stock' else ['/home/exedev/simd-lang/build/fcx','build','-C',str(d),'-o',str(d/('objs-'+mode)),'-link',str(binary),'.']
   start=time.time();print(d.name,mode,'building',flush=True)
   try:
    p=subprocess.run(cmd,cwd=d,env=e,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=240)
    (d/(mode+'-build.log')).write_bytes(p.stdout)
    result={'build_exit':p.returncode,'seconds':round(time.time()-start,2)}
    if p.returncode==0:
     try:
      r=subprocess.run([str(binary)],stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=10)
      result.update(exit=r.returncode,output=r.stdout.decode(errors='replace'))
     except subprocess.TimeoutExpired:result.update(exit='timeout',output='')
    else:result['error']=p.stdout.decode(errors='replace')[-3000:]
   except subprocess.TimeoutExpired:result={'build_exit':'timeout'}
   results[mode]=result
   print(d.name,mode,json.dumps(result),flush=True)
  record={'case':d.name,'results':results};reportfile.write(json.dumps(record)+'\n');reportfile.flush()
  same=all(results[m].get('build_exit')==0 for m in results) and len({(r.get('exit'),r.get('output')) for r in results.values()})==1
  with (root/'FINDINGS.md').open('a') as f:
   f.write('\n## '+d.name+' — '+('MATCH' if same else 'DIFFERENCE OR BUILD FAILURE')+'\n\n')
   if not same:f.write('```go\n'+(d/'main.go').read_text()+'```\n\n')
   for m,r in results.items(): f.write(m+': `'+str(r).replace('`','')+'`\n\n')

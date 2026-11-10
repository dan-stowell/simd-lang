#!/usr/bin/env python3
"""TIMING CAMPAIGN: run only when parent explicitly starts it, with all other jobs idle.
Does not run automatically as part of any correctness probe.
"""
import argparse,hashlib,json,os,pathlib,random,statistics,subprocess
R=pathlib.Path.cwd(); B=R/'build/audit3-hand'; D=R/'reviews/audit3-hand'
a=argparse.ArgumentParser();a.add_argument('--only',default='xlat,letters,filter4,strip,idents,strings,json');a.add_argument('--rounds',type=int,default=7);a.add_argument('--metadata',default='reviews/audit3-2026-11-10/claimed-results.json');a.add_argument('--mib',type=int,default=256);args=a.parse_args()
meta=json.loads((R/args.metadata).read_text());names=args.only.split(',');kinds={'xlat':'text','letters':'text','filter4':'ints','strip':'code','idents':'code','strings':'code','json':'json'}
def sha(path):
 h=hashlib.sha256()
 with open(path,'rb') as f:
  for b in iter(lambda:f.read(1<<22),b''):h.update(b)
 return h.hexdigest()
paths={}
for kind in set(kinds[n] for n in names):
 for path in sorted((R/'build/suite').glob(f'{kind}{args.mib}-*')):
  if path.is_file() and sha(path)==meta['inputs'][kind]:paths[kind]=path;break
 if kind not in paths:raise SystemExit(f'No matching {kind} fixture; do not silently use another distribution')
subprocess.run(['gcc','-O2','-o',str(B/'timer'),str(R/'tools/bench.c')],check=True)
variants={}
for name in names:
 variants[name]=['hand_'+name,'omp_'+name,'st_'+name]
 if name in ['xlat','letters','filter4','strip','json']:variants[name].append('persistent_'+name)
 if name in ['strip','idents','strings']:variants[name].append('packed_'+name)
 if name=='json':variants[name].append('dual_json')
 if name in ['xlat','letters','filter4','strip','json']:
  exe='block64_'+name
  subprocess.run(['gcc','-O3','-march=native','-fopenmp','-DAUDIT_BLK=(64<<20)','-DK=P_'+name,'-o',str(B/exe),str(D/'hand_blocksize.c')],check=True)
  variants[name].append(exe)
allowed=sorted(os.sched_getaffinity(0));sets={'1':[allowed[min(3,len(allowed)-1)]]}
if len(allowed)>=4:sets['4']=allowed[:4]
if len(allowed)>4:sets['all']=allowed
result={'revision':subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip(),'sets':sets,'rounds':args.rounds,'fixtures':{k:{'path':str(v),'sha256':sha(v),'bytes':v.stat().st_size} for k,v in paths.items()},'samples':{},'output_hashes':{},'sources':{str(p):sha(p) for p in D.glob('*.c')}}
rnd=random.Random(16891)
for name in names:
 path=paths[kinds[name]];cols=[(exe,s) for exe in variants[name] for s in sets]
 want=None
 for exe,s in cols:
  env=dict(os.environ,OMP_NUM_THREADS=str(len(sets[s])),OMP_DYNAMIC='FALSE',OMP_PROC_BIND='FALSE')
  with open(path,'rb') as f:
   p=subprocess.Popen(['taskset','-c',','.join(map(str,sets[s])),str(B/exe)],stdin=f,stdout=subprocess.PIPE,env=env)
   h=hashlib.sha256()
   for b in iter(lambda:p.stdout.read(1<<20),b''):h.update(b)
   if p.wait():raise SystemExit(f'{exe}:{s} failed')
  got=h.hexdigest()
  if want is None:want=got
  if got!=want:raise SystemExit(f'{name}: output mismatch {exe}:{s}')
  result['output_hashes'][name+':'+exe+':'+s]=got
 samples={exe+':'+s:[] for exe,s in cols}
 for _ in range(args.rounds):
  order=cols[:];rnd.shuffle(order)
  for exe,s in order:
   env=dict(os.environ,OMP_NUM_THREADS=str(len(sets[s])),OMP_DYNAMIC='FALSE',OMP_PROC_BIND='FALSE')
   p=subprocess.run(['taskset','-c',','.join(map(str,sets[s])),str(B/'timer'),'1',str(path),str(B/exe)],check=True,capture_output=True,text=True,env=env)
   samples[exe+':'+s].append(float(p.stdout.split()[2]))
 result['samples'][name]=samples
 (D/'control-timings.json').write_text(json.dumps(result,indent=2)+'\n')
 print(name, json.dumps({key:round(path.stat().st_size/statistics.median(v)/1000,3) for key,v in samples.items()}),flush=True)

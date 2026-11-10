#!/usr/bin/env python3
"""Correctness only; no timing. Run from repository root."""
import hashlib, json, os, pathlib, random, subprocess, sys
R=pathlib.Path.cwd(); B=R/'build/audit3-hand'; B.mkdir(parents=True,exist_ok=True)
allowed=sorted(os.sched_getaffinity(0)); sets={t:allowed[:t] for t in (1,4,8)}
sys.path.insert(0,str(R/'bench')); import jsongen
names=['letters','filter4','strip','csv','idents','strings','xlat','json']
programs={p[0].strip():p[3].strip().replace(';','\n')+'\n' for l in (R/'bench/programs.txt').read_text().splitlines() if l and not l.startswith('#') for p in [l.split('|',3)]}
for name in names:
 for typ,src in [('base','base.c'),('omp','base_omp.c'),('hand','hand.c')]:
  subprocess.run(['gcc','-O3','-march=native','-fopenmp','-DK=P_'+name,'-o',str(B/(typ+'_'+name)),str(R/'bench'/src)],check=True)
 (B/(name+'.strand')).write_text(programs[name])
 with open(B/(name+'.strand'),'rb') as inp, open(B/('st_'+name),'wb') as out: subprocess.run([str(R/'build/stage2')],stdin=inp,stdout=out,check=True)
 (B/('st_'+name)).chmod(0o755)
def run(typ,name,data,t):
 (B/'input').write_bytes(data)
 with open(B/'input','rb') as inp: return subprocess.run(['taskset','-c',','.join(map(str,sets[t])),str(B/(typ+'_'+name))],stdin=inp,capture_output=True,env=dict(os.environ,OMP_NUM_THREADS=str(t),OMP_DYNAMIC='FALSE'),check=True).stdout
r=random.Random(819); results={'revision':subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip(),'cpu_sets':sets,'checks':0,'mismatches':[]}
def check(label,name,data,want=None,threads=(1,4,8),types=('omp','hand','st')):
 if want is None: want=run('base',name,data,1)
 for typ in types:
  for t in threads:
   got=run(typ,name,data,t); results['checks']+=1
   if got!=want:
    fn=f'{label}-{name}-{typ}-{t}'; (B/fn).write_bytes(data)
    results['mismatches'].append(dict(case=label,name=name,typ=typ,threads=t,input=str(B/fn),want=want[:128].hex(),got=got[:128].hex(),wantlen=len(want),gotlen=len(got)))
for n in [0,1,2,3,4,15,16,17,63,64,65,127,128,129,191,192,193,255,256,257,511,512,513,4095,4096,4097]:
 data=bytes(r.choice(b'abc_012#"\\\n, {}[]:\xff\x00') for _ in range(n))
 for name in names: check(f'random-{n}',name,data)
# Valid JSON coverage, oracle is parsed tree (generated object keys are unique).
for seed in range(20):
 data=jsongen.document(512+seed*100,seed); check('valid-'+str(seed),'json',data,jsongen.skeleton(data))
# Long valid strings and backslash runs crossing SIMD, worker, and 4MiB boundaries.
for n in [63,64,65,127,128,129,511,512,513,1023,1024,1025,(4<<20)-1,4<<20,(4<<20)+1]:
 for backslashes in ([1,2,31,32,63,64,65] if n<2000 else [1,64,65]):
  # Raw text has 2*k backslashes (k escaped backslash characters), then escaped quote.
  data=b'["'+b'a'*max(0,n-2)+b'\\'*(2*backslashes)+b'\\"{},[]:", {"x": []}]'
  check(f'valid-boundary-{n}-{backslashes}','json',data,jsongen.skeleton(data))
# Other automata block and worker carries, including a long string and long comment.
for data in [b'a'*(4<<20)+b'#x\n"\\\"hi" abc', b'"'+b'\\'*(4<<20)+b'"#hi\nabc', b'#'+b'a'*(4<<20)+b'\nabc"x"']:
 for name in ['strip','idents','strings','csv']: check('fsm-big-'+hashlib.sha256(data).hexdigest()[:8],name,data)
# Oracle gap, not a generated-fixture failure: duplicate member names are accepted by Python but collapse.
dup=b'{"x":[],"x":{}}'; results['duplicate_keys']={'input':dup.decode(),'oracle':jsongen.skeleton(dup).decode(),'scanner':run('base','json',dup,1).decode()}
# Document repetition is not a single JSON document; small reproducer has identical construction.
unit=jsongen.document(1024)
try: json.loads(unit*2); results['repeated_document']='accepted'
except json.JSONDecodeError as e: results['repeated_document']=str(e)
assert all(x['name']=='json' and x['typ']=='hand' and x['case'].startswith('random-') for x in results['mismatches']), 'Unexpected valid-input/task mismatch'
(R/'reviews/audit3-hand/checks.json').write_text(json.dumps(results,indent=2)+'\n')
print(json.dumps({'checks':results['checks'],'mismatches':len(results['mismatches']),'duplicate_keys':results['duplicate_keys'],'repeated_document':results['repeated_document']},indent=2))

#!/usr/bin/env python3
import hashlib,json,os,pathlib,random,subprocess,sys
R=pathlib.Path.cwd(); B=R/'build/audit3-hand'; sys.path.insert(0,str(R/'bench'));import jsongen
allowed=sorted(os.sched_getaffinity(0)); sets={t:allowed[:t] for t in (1,4,8)}
for prefix,names,source in [
 ('persistent',['letters','filter4','strip','idents','strings','csv','xlat','json'],'hand_persistent.c'),
 ('packed',['strip','idents','strings'],'ordinary_packed.c'),
 ('dual',['json'],'hand_json_dual.c')]:
 for name in names:
  subprocess.run(['gcc','-O3','-march=native','-fopenmp','-DK=P_'+name,'-o',str(B/(prefix+'_'+name)),str(R/'reviews/audit3-hand'/source)],check=True)
r=random.Random(497); checks=[]
def run(exe,data,t):
 (B/'input').write_bytes(data)
 with open(B/'input','rb') as f: return subprocess.run(['taskset','-c',','.join(map(str,sets[t])),str(B/exe)],stdin=f,capture_output=True,check=True,env=dict(os.environ,OMP_NUM_THREADS=str(t),OMP_DYNAMIC='FALSE')).stdout
for n in [0,1,63,64,65,255,256,257,4097,(4<<20)+257]:
 data=bytes(r.choice(b'a0#"\\\n {},[]:\xff\x00') for _ in range(n))
 for name in ['letters','filter4','strip','idents','strings','csv','xlat']:
  want=run('base_'+name,data,1)
  for prefix in (['persistent_','packed_'] if name in ['strip','idents','strings'] else ['persistent_']):
   for t in [1,4,8]:
    got=run(prefix+name,data,t); assert got==want,(prefix,name,n,t)
    checks.append([prefix+name,n,t])
for seed in range(20):
 data=jsongen.document(1000+seed*100,seed);want=jsongen.skeleton(data)
 for exe in ['persistent_json','dual_json']:
  for t in [1,4,8]:
   assert run(exe,data,t)==want,(exe,seed,t)
   checks.append([exe,len(data),t])
for n in [63,64,65,127,128,129,1023,1024,1025,(4<<20)-1,4<<20,(4<<20)+1]:
 for k in [1,32,64,65]:
  data=b'["'+b'a'*(n-2)+b'\\'*(2*k)+b'\\"{},[]:", {"x": []}]'; want=jsongen.skeleton(data)
  for exe in ['persistent_json','dual_json']:
   for t in [1,4,8]:
    assert run(exe,data,t)==want,(exe,n,k,t)
    checks.append([exe,len(data),t])
xlat=bytes(range(256))*4; want=xlat.decode('cp037').encode('latin1')
for exe in ['base_xlat','omp_xlat','hand_xlat','st_xlat','persistent_xlat']:
 for t in [1,4,8]: assert run(exe,xlat,t)==want,(exe,t)
# Malformed JSON demonstrates hand's narrower input contract and SIMD/tail discrepancy.
bad=b'\\"{'+b' '*61
malformed={exe:run(exe,bad,1).decode() for exe in ['base_json','omp_json','hand_json','st_json']}
assert malformed=={'base_json':'','omp_json':'','hand_json':'{','st_json':''}
(B/'malformed-json64').write_bytes(bad)
result={'checks':len(checks)+15,'cpu_sets':sets,'variants':sorted(set(x[0] for x in checks)),'all_passed':True,'malformed_json64':malformed,'note':'No timings recorded.'}
(R/'reviews/audit3-hand/variants-checks.json').write_text(json.dumps(result,indent=2)+'\n')
print(json.dumps(result,indent=2))

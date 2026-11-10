#!/usr/bin/env python3
"""Generate audit-only controls; never modifies benchmark implementations."""
from pathlib import Path
R=Path(__file__).resolve().parents[2]; D=R/'reviews/audit3-hand'; source=(R/'bench/hand.c').read_text()
# Keep xlat.h's relative include valid when sources are outside bench/.
source=source.replace('#include "xlat.h"','#include "../../bench/xlat.h"')
source=source.replace('enum { BLK = 4 << 20 };','#ifndef AUDIT_BLK\n#define AUDIT_BLK (4 << 20)\n#endif\nenum { BLK = AUDIT_BLK };')
(D/'hand_blocksize.c').write_text(source)
p=source
# Persistent team, unchanged block size, partitioning, arithmetic, buffers, output syscall order.
a=p.index('#if K == P_xlat\n'); z=p.index('#elif K == P_letters',a)
x=p[a:z].replace('  for (size_t b = 0;', '  #pragma omp parallel\n  {\n  for (size_t b = 0;',1).replace('#pragma omp parallel for schedule(static)','#pragma omp for schedule(static)')
x=x.replace('    for (size_t i = v;', '    #pragma omp single\n    {\n    for (size_t i = v;').replace('    put(out, m); }','    put(out, m); } }\n  }')
p=p[:a]+x+p[z:]
for name,nextname in [('letters','filter4'),('filter4','strip'),('strip','idents'),('json','csv')]:
 a=p.index('#elif K == P_'+name+'\n'); z=p.index('#elif K == P_'+nextname,a)
 x=p[a:z]
 x=x.replace('  for (size_t b = 0;', '  #pragma omp parallel\n  {\n  for (size_t b = 0;',1).replace('    #pragma omp parallel\n','',1)
 # The old parallel-region end was the completion barrier; make it explicit.
 if name in ('letters','filter4'):
  x=x.replace('      cnt[t] = k; }','      cnt[t] = k; }\n    #pragma omp barrier\n    #pragma omp single\n    {')
 else:
  marker='    s0 = start[TT];'
  x=x.replace(marker,'    #pragma omp barrier\n    #pragma omp single\n    {\n'+marker)
 # close single, for-b, outer team; the old final brace closed for-b.
 old='    for (int t = 0; t < TT; t++) put(reg[t], cnt[t]'+(' * 4' if name=='filter4' else '')+'); }'
 x=x.replace(old,old+' }\n  }')
 p=p[:a]+x+p[z:]
(D/'hand_persistent.c').write_text(p)
# A separate task-level control: one local scan produces both possible starting-quote outputs.
# Same original block/team organization; isolates the two-pass algorithm, not persistence.
a=source.index('#elif K == P_json\n'); z=source.index('#elif K == P_csv',a)
dual='''#elif K == P_json
  // AUDIT CONTROL: scan once with starting inq=0, store both quote-parity alternatives.
  size_t (*cnt)[2] = calloc(MT, sizeof *cnt);
  uint8_t *(*reg)[2] = malloc(MT * sizeof *reg);
  uint8_t *flip = calloc(MT, 1); int TT = 1, s0 = 0, e0 = 0;
  for (int t = 0; t < MT; t++) for (int q=0;q<2;q++) reg[t][q]=malloc(BLK+64);
  for (size_t b = 0; b < n; b += BLK) { size_t m = n-b<BLK?n-b:BLK;
    #pragma omp parallel
    { RANGE; size_t k0=0,k1=0,i=lo; uint64_t esc=e0,inq=0;
      #pragma omp single
      TT=T;
      if(t) { size_t j=lo; while(j>0 && in[b+j-1]=='\\\\') j--; esc=(lo-j)&1; if(j==0 && e0) esc^=1; }
      // Ensure every thread read e0 before the last thread overwrites it.
      #pragma omp barrier
      for (;i+64<=hi;i+=64) { __m512i c=_mm512_loadu_si512(in+b+i); J_MASKS(c)
        uint64_t st=_mm512_cmpeq_epi8_mask(c,_mm512_set1_epi8('{'))|_mm512_cmpeq_epi8_mask(c,_mm512_set1_epi8('}'))
          |_mm512_cmpeq_epi8_mask(c,_mm512_set1_epi8('['))|_mm512_cmpeq_epi8_mask(c,_mm512_set1_epi8(']'))
          |_mm512_cmpeq_epi8_mask(c,_mm512_set1_epi8(':'))|_mm512_cmpeq_epi8_mask(c,_mm512_set1_epi8(','));
        uint64_t m0=st&~ins,m1=st&ins;
        _mm512_storeu_si512(reg[t][0]+k0,_mm512_maskz_compress_epi8(m0,c)); k0+=_mm_popcnt_u64(m0);
        _mm512_storeu_si512(reg[t][1]+k1,_mm512_maskz_compress_epi8(m1,c)); k1+=_mm_popcnt_u64(m1);
      }
      unsigned q=inq&1,e=esc;
      for(;i<hi;i++) { uint8_t c=in[b+i];
        if(e)e=0; else if(c=='\\\\')e=1; else q^=c=='"';
        unsigned st=(c=='{')|(c=='}')|(c=='[')|(c==']')|(c==':')|(c==',');
        if(st) { if(q)reg[t][1][k1++]=c; else reg[t][0][k0++]=c; }
      }
      cnt[t][0]=k0;cnt[t][1]=k1;flip[t]=q;
      if(t==T-1)e0=e;
    }
    unsigned q=s0;
    for(int t=0;t<TT;t++) { put(reg[t][q],cnt[t][q]); q^=flip[t]; }
    s0=q;
  }
'''
(D/'hand_json_dual.c').write_text(source[:a]+dual+source[z:])
print('Wrote audit-only hand_blocksize.c, hand_persistent.c, hand_json_dual.c')
# Independent ordinary-C source-shape control: packed transition byte and variable shift,
# no intrinsics, same exact state machine, two passes, work partition, buffers and writes.
import re
p=(R/'bench/base_omp.c').read_text().replace('#include "xlat.h"','#include "../../bench/xlat.h"')
tr=[]
for c in range(256):
 f=[1 if c==35 else 2 if c==34 else 0,0 if c==10 else 1,0 if c==34 else 3 if c==92 else 2,2]
 tr.append(sum(x<<(2*s) for s,x in enumerate(f)))
p=re.sub(r'#define K_strip_step\(s, c\).*?\nstatic uint8_t ISW', '#define K_strip_step(s, c) ((s) = (AUDIT_TR[(uint8_t)(c)] >> (2 * (s))) & 3);\nstatic const uint8_t AUDIT_TR[256] = {'+','.join(map(str,tr))+'};\nstatic uint8_t ISW',p,flags=re.S)
(D/'ordinary_packed.c').write_text(p)

/* Audit-only controls. Source hash/RNG are generated from b591f69:explore/link.c.
 * Do not treat these microbenchmarks as complete linkers. */
#include "prototype_helpers.h"
#include <assert.h>
#include <sys/resource.h>
#include <sys/mman.h>
#include <unistd.h>

#define NONE UINT32_MAX
#define NOINLINE __attribute__((noinline))
typedef struct {
  uint64_t h;
  const char *name;
  uint32_t n, id;
} Entry24;
typedef struct { uint64_t h; uint32_t n, id; } Entry16;
_Static_assert(sizeof(Entry24) == 24 && sizeof(Entry16) == 16, "entry layout");
typedef struct {
  size_t U,D,R,K,C,S,N,B,M,np,up;
  char *names,*unames;
  uint32_t *noff,*ulen,*expect,*roff,*rsym;
  uint64_t *fp,*ufp,*base,*addr,*imp,*holes;
  uint8_t *code,*reference;
  size_t nr_unique,nr_duplicates,nr_cross64;
  unsigned hashbits;
  int missing,check,cold;
  uint8_t *eviction;
  size_t eviction_size;
} Job;
typedef struct {
  int32_t *id;
  uint64_t *h;
  Entry24 *e24;
  Entry16 *e16;
  int32_t *race;
  uint8_t *placed;
} Tables;
typedef struct { const char *name; int shape,pre,exact,race; } Mode;
static const Mode modes[] = {
  {"soa_hash_exact",0,0,1,0}, {"soa_pre_exact",0,1,1,0},
  {"soa_hash_fp",0,0,0,0}, {"soa_pre_fp",0,1,0,0},
  {"aos24_hash_exact",1,0,1,0}, {"aos24_pre_exact",1,1,1,0},
  {"aos24_hash_fp",1,0,0,0}, {"aos24_pre_fp",1,1,0,0},
  {"aos16_hash_exact",2,0,1,0}, {"aos16_pre_exact",2,1,1,0},
  {"aos16_hash_fp",2,0,0,0}, {"aos16_pre_fp",2,1,0,0},
  {"race_scalar",3,1,0,1}, {"race_simd",3,1,0,2}
};
static const uint64_t RK[4] = {1,0x9e3779b97f4a7c15ull,0xc2b2ae3d27d4eb4full,0x165667b19e3779f9ull};
static volatile uint64_t sink;
static void barrier(void) { __asm__ __volatile__("" ::: "memory"); }
static void *alloc(size_t n) { void *p=malloc(n?n:1); if(!p) {perror("malloc");exit(2);} return p; }
/* Touch before timing, and make the stores visible to the compiler. */
static void *touched(size_t n) { void *p=alloc(n); memset(p,0,n); barrier(); return p; }
static uint64_t fingerprint(Job *j,const char *s,size_t n) {
  uint64_t h=hash_str(s,(int)n);
  return j->hashbits==64?h:(h&((1ull<<j->hashbits)-1));
}
static uint64_t checksum(const void *p,size_t n) {
  const unsigned char *s=p; uint64_t h=0xcbf29ce484222325ull;
  for(size_t i=0;i<n;i++) h=(h^s[i])*0x100000001b3ull;
  sink^=h; return h;
}
static double stamp(Job *j) { barrier(); return j->check?0:now(); }
static struct rusage usage(Job *j) { struct rusage r={0}; if(!j->check)getrusage(RUSAGE_SELF,&r); return r; }
static void cool(Job *j) {
  if(!j->cold)return;
  uint64_t z=0;
  for(size_t i=0;i<j->eviction_size;i+=64) z+=++j->eviction[i];
  sink^=z; barrier();
}
static Job make_job(size_t U,size_t D,size_t R,size_t K,size_t C,
                    unsigned bits,int missing,int check,int cold) {
  Job j={.U=U,.D=D,.R=R,.K=K,.C=C,.S=U*D,.N=U*R,.B=U*C,
         .hashbits=bits,.missing=missing,.check=check,.cold=cold};
  j.M=1; while(j.M<2*j.S)j.M*=2;
  j.names=alloc(j.S*96);j.noff=alloc((j.S+1)*4);
  for(size_t i=0;i<j.S;i++){
    j.noff[i]=(uint32_t)j.np; size_t n=24+rnd()%65;
    j.np+=(size_t)sprintf(j.names+j.np,"_ZN%zu",i);
    while(j.np-j.noff[i]<n)j.names[j.np++]='a'+rnd()%26;
  }
  j.noff[j.S]=(uint32_t)j.np;
  j.expect=alloc(j.N*4);j.ulen=alloc(j.N*4);
  for(size_t i=0;i<j.N;i++){
    uint64_t r=rnd();j.expect[i]=(r%8?r>>8:(r>>8)%4096*37)%j.S;
    j.ulen[i]=j.noff[j.expect[i]+1]-j.noff[j.expect[i]];j.up+=j.ulen[i];
  }
  j.roff=alloc(U*K*4);j.rsym=alloc(U*K*4);
  for(size_t u=0;u<U;u++){
    uint32_t o=0,last=NONE;
    for(size_t k=0;k<K;k++){
      o+=8+rnd()%27;if(o+4>C)o=C-4;
      j.roff[u*K+k]=o;j.rsym[u*K+k]=rnd()%R;
      if(o==last)j.nr_duplicates++;else {j.nr_unique++;j.nr_cross64+=(o%64>60);}
      last=o;
    }
  }
  j.code=alloc(j.B+64);
  for(size_t i=0;i<j.B;i+=8){uint64_t r=rnd();memcpy(j.code+i,&r,8);}
  j.unames=alloc(j.up+8);size_t p=0;
  for(size_t i=0;i<j.N;i++){memcpy(j.unames+p,j.names+j.noff[j.expect[i]],j.ulen[i]);p+=j.ulen[i];}
  if(missing){j.unames[0]='!';j.expect[0]=NONE;}
  j.fp=alloc(j.S*8);j.ufp=alloc(j.N*8);
  for(size_t i=0;i<j.S;i++)j.fp[i]=fingerprint(&j,j.names+j.noff[i],j.noff[i+1]-j.noff[i]);
  p=0;for(size_t i=0;i<j.N;i++){j.ufp[i]=fingerprint(&j,j.unames+p,j.ulen[i]);p+=j.ulen[i];}
  j.base=alloc((U+1)*8);j.addr=alloc(j.S*8);j.imp=alloc(j.N*8);
  j.base[0]=0x400000;for(size_t u=0;u<U;u++)j.base[u+1]=j.base[u]+C;
  for(size_t i=0;i<j.S;i++)j.addr[i]=j.base[i/D]+(i%D)*(C/D);
  for(size_t i=0;i<j.N;i++)j.imp[i]=j.expect[i]==NONE?0:j.addr[j.expect[i]];
  j.holes=touched((j.B/64+1)*8);
  for(size_t u=0;u<U;u++)for(size_t k=0;k<K;k++){
    size_t o=u*C+j.roff[u*K+k];for(int b=0;b<4;b++)j.holes[(o+b)>>6]|=1ull<<((o+b)&63);
  }
  j.reference=alloc(j.B);memcpy(j.reference,j.code,j.B);
  /* Independent byte-wise oracle, including last-wins duplicate relocations. */
  for(size_t u=0;u<U;u++)for(size_t k=0;k<K;k++){
    size_t o=j.roff[u*K+k];uint32_t v=(uint32_t)(j.imp[u*R+j.rsym[u*K+k]]-j.base[u]-o);
    for(int b=0;b<4;b++)j.reference[u*C+o+b]=(uint8_t)(v>>(8*b));
  }
  if(cold){j.eviction_size=256ull<<20;j.eviction=touched(j.eviction_size);}
  return j;
}
static Tables tables(Job *j) {
  return (Tables){.id=touched(j->M*4),.h=touched(j->M*8),
    .e24=touched(j->M*sizeof(Entry24)),.e16=touched(j->M*sizeof(Entry16)),
    .race=touched(j->M*16),.placed=touched(j->S)};
}
static inline __attribute__((always_inline)) void build_linear(Job *j,Tables *t,const Mode *m) {
  if(m->shape==0)memset(t->id,255,j->M*4);
  if(m->shape==1)memset(t->e24,255,j->M*sizeof(Entry24));
  if(m->shape==2)memset(t->e16,255,j->M*sizeof(Entry16));
  for(size_t i=0;i<j->S;i++){
    uint32_t n=j->noff[i+1]-j->noff[i];const char *s=j->names+j->noff[i];
    uint64_t h=m->pre?j->fp[i]:fingerprint(j,s,n);size_t k=h&(j->M-1);
    if(m->shape==0){
      while(t->id[k]>=0)k=(k+1)&(j->M-1);
      t->id[k]=(int32_t)i;t->h[k]=h;
    }else if(m->shape==1){
      while(t->e24[k].id!=NONE)k=(k+1)&(j->M-1);
      t->e24[k]=(Entry24){h,s,n,(uint32_t)i};
    }else{
      while(t->e16[k].id!=NONE)k=(k+1)&(j->M-1);
      t->e16[k]=(Entry16){h,n,(uint32_t)i};
    }
  }
}
static inline __attribute__((always_inline)) void lookup_linear(Job *j,Tables *t,const Mode *m,uint32_t *out) {
  const char *s=j->unames;
  for(size_t i=0;i<j->N;i++){
    uint32_t n=(m->exact||!m->pre)?j->ulen[i]:0;
    uint64_t h=m->pre?j->ufp[i]:fingerprint(j,s,n);size_t k=h&(j->M-1);uint32_t id=NONE;
    for(size_t probes=0;probes<j->M;probes++,k=(k+1)&(j->M-1)){
      if(m->shape==0){
        id=(uint32_t)t->id[k];if(id==NONE)break;
        if(t->h[k]==h&&(!m->exact||
          (j->noff[id+1]-j->noff[id]==n&&!memcmp(j->names+j->noff[id],s,n))))break;
      }else if(m->shape==1){
        Entry24 *e=t->e24+k;id=e->id;if(id==NONE)break;
        if(e->h==h&&(!m->exact||(e->n==n&&!memcmp(e->name,s,n))))break;
      }else{
        Entry16 *e=t->e16+k;id=e->id;if(id==NONE)break;
        if(e->h==h&&(!m->exact||(e->n==n&&!memcmp(j->names+j->noff[id],s,n))))break;
      }
      id=NONE;
    }
    out[i]=id;if(m->exact||!m->pre)s+=n;
  }
}
/* Specialize outside the measured loops: shape/exact/prehash are compile-time
 * constants, not per-probe dispatch. Public kernels remain noinline barriers. */
#define LINEAR(N,S,P,E) \
static NOINLINE void build_##N(Job *j,Tables *t) { \
  const Mode m={#N,S,P,E,0};build_linear(j,t,&m); } \
static NOINLINE void lookup_##N(Job *j,Tables *t,uint32_t *out) { \
  const Mode m={#N,S,P,E,0};lookup_linear(j,t,&m,out); }
LINEAR(soa_hash_exact,0,0,1) LINEAR(soa_pre_exact,0,1,1)
LINEAR(soa_hash_fp,0,0,0) LINEAR(soa_pre_fp,0,1,0)
LINEAR(aos24_hash_exact,1,0,1) LINEAR(aos24_pre_exact,1,1,1)
LINEAR(aos24_hash_fp,1,0,0) LINEAR(aos24_pre_fp,1,1,0)
LINEAR(aos16_hash_exact,2,0,1) LINEAR(aos16_pre_exact,2,1,1)
LINEAR(aos16_hash_fp,2,0,0) LINEAR(aos16_pre_fp,2,1,0)
#define BUILDER(N) build_##N
#define LOOKUP(N) lookup_##N
static void (*const builders[])(Job *,Tables *)={
  BUILDER(soa_hash_exact),BUILDER(soa_pre_exact),BUILDER(soa_hash_fp),BUILDER(soa_pre_fp),
  BUILDER(aos24_hash_exact),BUILDER(aos24_pre_exact),BUILDER(aos24_hash_fp),BUILDER(aos24_pre_fp),
  BUILDER(aos16_hash_exact),BUILDER(aos16_pre_exact),BUILDER(aos16_hash_fp),BUILDER(aos16_pre_fp)};
static void (*const lookups[])(Job *,Tables *,uint32_t *)={
  LOOKUP(soa_hash_exact),LOOKUP(soa_pre_exact),LOOKUP(soa_hash_fp),LOOKUP(soa_pre_fp),
  LOOKUP(aos24_hash_exact),LOOKUP(aos24_pre_exact),LOOKUP(aos24_hash_fp),LOOKUP(aos24_pre_fp),
  LOOKUP(aos16_hash_exact),LOOKUP(aos16_pre_exact),LOOKUP(aos16_hash_fp),LOOKUP(aos16_pre_fp)};
static NOINLINE size_t build_race(Job *j,Tables *t) {
  memset(t->race,255,j->M*16);memset(t->placed,0,j->S);size_t left=j->S;
  for(int r=0;r<4&&left;r++){
    int32_t *tab=t->race+r*j->M;
    for(size_t i=0;i<j->S;i++)if(!t->placed[i])tab[(j->fp[i]*RK[r]>>20)&(j->M-1)]=(int32_t)i;
    for(size_t i=0;i<j->S;i++)if(!t->placed[i]&&tab[(j->fp[i]*RK[r]>>20)&(j->M-1)]==(int32_t)i){t->placed[i]=r+1;left--;}
  }
  return left;
}
static NOINLINE void lookup_race_scalar(Job *j,Tables *t,uint32_t *out) {
  for(size_t i=0;i<j->N;i++){
    uint64_t f=j->ufp[i];uint32_t got=NONE;
    for(int r=0;r<4;r++){
      int32_t c=t->race[r*j->M+((f*RK[r]>>20)&(j->M-1))];
      if(c>=0&&j->fp[c]==f){got=(uint32_t)c;break;}
    }
    out[i]=got;
  }
}
static NOINLINE void lookup_race_simd(Job *j,Tables *t,uint32_t *out) {
  __m512i mask=_mm512_set1_epi64(j->M-1);size_t i=0;
  for(;i+8<=j->N;i+=8){
    __m512i f=_mm512_loadu_si512(j->ufp+i);__m256i got=_mm256_set1_epi32(-1);__mmask8 need=255;
    for(int r=0;r<4&&need;r++){
      __m512i k=_mm512_and_si512(_mm512_srli_epi64(_mm512_mullo_epi64(f,_mm512_set1_epi64(RK[r])),20),mask);
      __m256i c=_mm512_mask_i64gather_epi32(_mm256_set1_epi32(-1),need,k,t->race+r*j->M,4);
      __mmask8 ok=need&_mm256_cmpgt_epi32_mask(c,_mm256_set1_epi32(-1));
      __m512i cf=_mm512_mask_i32gather_epi64(_mm512_setzero_si512(),ok,c,j->fp,8);
      ok&=_mm512_cmpeq_epi64_mask(cf,f);got=_mm256_mask_mov_epi32(got,ok,c);need&=~ok;
    }
    _mm256_storeu_si256((void *)(out+i),got);
  }
  for(;i<j->N;i++){
    uint32_t got=NONE;for(int r=0;r<4;r++){
      int32_t c=t->race[r*j->M+((j->ufp[i]*RK[r]>>20)&(j->M-1))];
      if(c>=0&&j->fp[c]==j->ufp[i]){got=(uint32_t)c;break;}
    }out[i]=got;
  }
}
static NOINLINE uint64_t hash_all(Job *j) {
  uint64_t h=0;const char *s=j->unames;
  for(size_t i=0;i<j->N;i++){h^=fingerprint(j,s,j->ulen[i]);s+=j->ulen[i];}
  for(size_t i=0;i<j->S;i++)h^=fingerprint(j,j->names+j->noff[i],j->noff[i+1]-j->noff[i]);
  return h;
}
static NOINLINE void patch_unit(Job *j,size_t u,uint8_t *out) {
  const uint32_t *ro=j->roff+u*j->K,*sy=j->rsym+u*j->K;const uint64_t *im=j->imp+u*j->R;
  for(size_t k=0;k<j->K;k++){uint32_t v=(uint32_t)(im[sy[k]]-j->base[u]-ro[k]);memcpy(out+ro[k],&v,4);}
}
static NOINLINE void reloc_whole(Job *j,uint8_t *out) {
  memcpy(out,j->code,j->B);for(size_t u=0;u<j->U;u++)patch_unit(j,u,out+u*j->C);
}
static NOINLINE void reloc_unit(Job *j,uint8_t *out) {
  for(size_t u=0;u<j->U;u++){memcpy(out+u*j->C,j->code+u*j->C,j->C);patch_unit(j,u,out+u*j->C);}
}
/* Conventional bounded working set: copy/patch 16 KiB chunks. Sites crossing a
 * chunk boundary are patched after the chunk containing their final byte. */
static NOINLINE void reloc_block(Job *j,uint8_t *out) {
  const size_t block=16*1024;
  for(size_t u=0;u<j->U;u++){
    size_t k=0;const uint32_t *ro=j->roff+u*j->K,*sy=j->rsym+u*j->K;const uint64_t *im=j->imp+u*j->R;
    for(size_t start=0;start<j->C;start+=block){
      size_t end=start+block;if(end>j->C)end=j->C;
      memcpy(out+u*j->C+start,j->code+u*j->C+start,end-start);
      while(k<j->K&&ro[k]+4<=end){uint32_t v=(uint32_t)(im[sy[k]]-j->base[u]-ro[k]);memcpy(out+u*j->C+ro[k],&v,4);k++;}
    }
    assert(k==j->K);
  }
}
static NOINLINE size_t values_scalar(Job *j,uint32_t *vals) {
  size_t n=0;
  for(size_t u=0;u<j->U;u++){
    uint32_t last=NONE;for(size_t k=0;k<j->K;k++){
      uint32_t o=j->roff[u*j->K+k],v=(uint32_t)(j->imp[u*j->R+j->rsym[u*j->K+k]]-j->base[u]-o);
      if(o==last)vals[n-1]=v;else vals[n++]=v;last=o;
    }
  }return n;
}
static NOINLINE size_t values_simd(Job *j,uint32_t *vals) {
  size_t n=0;
  for(size_t u=0;u<j->U;u++){
    const uint32_t *ro=j->roff+u*j->K,*sy=j->rsym+u*j->K;const uint64_t *im=j->imp+u*j->R;
    uint32_t last=NONE;size_t k=0;__m512i base=_mm512_set1_epi64(j->base[u]);
    for(;k+8<=j->K;k+=8){
      __m512i a=_mm512_i32gather_epi64(_mm256_loadu_si256((void *)(sy+k)),im,8);
      __m512i p=_mm512_add_epi64(base,_mm512_cvtepu32_epi64(_mm256_loadu_si256((void *)(ro+k))));
      uint32_t tmp[8];_mm256_storeu_si256((void *)tmp,_mm512_cvtepi64_epi32(_mm512_sub_epi64(a,p)));
      for(int z=0;z<8;z++){if(ro[k+z]==last)vals[n-1]=tmp[z];else vals[n++]=tmp[z];last=ro[k+z];}
    }
    for(;k<j->K;k++){
      uint32_t v=(uint32_t)(im[sy[k]]-j->base[u]-ro[k]);if(ro[k]==last)vals[n-1]=v;else vals[n++]=v;last=ro[k];
    }
  }return n;
}
static NOINLINE void expand(Job *j,uint32_t *vals,uint8_t *out) {
  const uint8_t *p=(const uint8_t *)vals;
  for(size_t i=0;i<j->B;i+=64){
    __mmask64 m=j->holes[i>>6];
    _mm512_storeu_si512(out+i,_mm512_mask_expandloadu_epi8(_mm512_loadu_si512(j->code+i),m,p));p+=_mm_popcnt_u64(m);
  }assert((size_t)(p-(uint8_t *)vals)==j->nr_unique*4);
}
static int run_join(Job *j,Tables *t,const Mode *m,uint32_t *out,uint32_t *other) {
  cool(j);struct rusage before=usage(j);double begin=stamp(j);size_t left=0;
  if(m->race)left=build_race(j,t);else builders[m-modes](j,t);
  double built=stamp(j);
  if(m->race==1)lookup_race_scalar(j,t,out);
  else if(m->race==2)lookup_race_simd(j,t,out);
  else lookups[m-modes](j,t,out);
  double end=stamp(j);struct rusage after=usage(j);
  size_t bad=0;for(size_t i=0;i<j->N;i++)bad+=out[i]!=j->expect[i];
  if(m->race){
    lookup_race_scalar(j,t,other);
    if(memcmp(out,other,j->N*4)){fprintf(stderr,"scalar/SIMD race mismatch\n");return 1;}
  }
  printf("{\"kind\":\"join\",\"variant\":\"%s\",\"build_ms\":%.6f,\"lookup_ms\":%.6f,\"ms\":%.6f,"
    "\"minor_faults\":%ld,\"major_faults\":%ld,\"mismatches\":%zu,\"unplaced\":%zu,\"checksum\":\"%llx\"}\n",
    m->name,built-begin,end-built,end-begin,after.ru_minflt-before.ru_minflt,after.ru_majflt-before.ru_majflt,
    bad,left,(unsigned long long)checksum(out,j->N*4));
  /* Truncated fingerprints deliberately test semantic loss, not a performance workload. */
  if(m->exact)return bad!=0;
  if(j->hashbits<64)return 0;
  return bad!=0||left!=0;
}
static int run_reloc(Job *j,int which,uint8_t *out,uint32_t *vals) {
  static const char *names[]={"copy_whole_patch","copy_unit_patch","copy_block_patch","stream_scalar_values","stream_simd_values"};
  cool(j);struct rusage before=usage(j);double begin=stamp(j),middle=begin;size_t n=0;
  if(which==0)reloc_whole(j,out);
  else if(which==1)reloc_unit(j,out);
  else if(which==2)reloc_block(j,out);
  else {n=which==3?values_scalar(j,vals):values_simd(j,vals);middle=stamp(j);expand(j,vals,out);}
  double end=stamp(j);struct rusage after=usage(j);int bad=memcmp(out,j->reference,j->B)!=0;
  printf("{\"kind\":\"relocation\",\"variant\":\"%s\",\"ms\":%.6f,\"values_ms\":%.6f,\"expand_ms\":%.6f,"
    "\"minor_faults\":%ld,\"major_faults\":%ld,\"mismatch\":%d,\"checksum\":\"%llx\"}\n",
    names[which],end-begin,middle-begin,which>=3?end-middle:0,after.ru_minflt-before.ru_minflt,
    after.ru_majflt-before.ru_majflt,bad,(unsigned long long)checksum(out,j->B));
  return bad||(which>=3&&n!=j->nr_unique);
}
int main(int argc,char **argv) {
  size_t U=2000,D=500,R=1500,K=6000,C=128<<10;unsigned bits=64;
  int check=0,cold=0,missing=0,fresh=0;const char *only="all";uint64_t order=1;
  for(int a=1;a<argc;a++){
    if(!strcmp(argv[a],"--small")){U=3;D=137;R=193;K=257;C=4096;}
    else if(!strcmp(argv[a],"--check"))check=1;
    else if(!strcmp(argv[a],"--cold"))cold=1;
    else if(!strcmp(argv[a],"--fresh-output"))fresh=1;
    else if(!strcmp(argv[a],"--missing"))missing=1;
    else if(!strcmp(argv[a],"--hash-bits")&&a+1<argc)bits=atoi(argv[++a]);
    else if(!strcmp(argv[a],"--order")&&a+1<argc)order=strtoull(argv[++a],0,10);
    else if(!strcmp(argv[a],"--only")&&a+1<argc)only=argv[++a];
    else {fprintf(stderr,"bad argument: %s\n",argv[a]);return 2;}
  }
  if(bits<1||bits>64)return 2;
  Job j=make_job(U,D,R,K,C,bits,missing,check,cold);
  printf("{\"kind\":\"job\",\"check_only\":%s,\"units\":%zu,\"definitions\":%zu,\"references\":%zu,"
    "\"image_bytes\":%zu,\"definition_string_bytes\":%zu,\"reference_string_bytes\":%zu,\"table_slots\":%zu,"
    "\"relocations\":%zu,\"unique_sites\":%zu,\"duplicate_sites\":%zu,\"cross_64_sites\":%zu,"
    "\"hash_bits\":%u,\"missing\":%d,\"cold\":%d,\"fresh_output\":%d,\"order\":%llu}\n",
    check?"true":"false",U,j.S,j.N,j.B,j.np,j.up,j.M,U*K,j.nr_unique,j.nr_duplicates,j.nr_cross64,bits,missing,cold,fresh,(unsigned long long)order);
  Tables t=tables(&j);uint32_t *out=touched(j.N*4),*other=touched(j.N*4);
  int fail=0;
  /* Randomize once with an independent RNG; never change the workload seed. */
  int ids[14];for(int i=0;i<14;i++)ids[i]=i;
  uint64_t state=order;
  for(int i=13;i>0;i--){state=mix(state+0x9e3779b97f4a7c15ull);int k=state%(i+1),v=ids[i];ids[i]=ids[k];ids[k]=v;}
  for(int i=0;i<14;i++){const Mode *m=modes+ids[i];if(!strcmp(only,"all")||!strcmp(only,m->name))fail|=run_join(&j,&t,m,out,other);}
  if(!strcmp(only,"all")||!strcmp(only,"hash_only")){
    cool(&j);double begin=stamp(&j);uint64_t h=hash_all(&j);double end=stamp(&j);sink^=h;
    printf("{\"kind\":\"hash\",\"variant\":\"hash_only\",\"ms\":%.6f,\"checksum\":\"%llx\"}\n",end-begin,(unsigned long long)h);
  }
  uint8_t *image=fresh?mmap(0,j.B,PROT_READ|PROT_WRITE,MAP_PRIVATE|MAP_ANONYMOUS,-1,0):touched(j.B);
  if(image==MAP_FAILED){perror("mmap");return 2;}
  uint32_t *vals=touched(U*K*4+64);const char *rnames[]={"copy_whole_patch","copy_unit_patch","copy_block_patch","stream_scalar_values","stream_simd_values"};
  int rid[5]={0,1,2,3,4};
  for(int i=4;i>0;i--){state=mix(state+0x9e3779b97f4a7c15ull);int k=state%(i+1),v=rid[i];rid[i]=rid[k];rid[k]=v;}
  for(int z=0;z<5;z++){int i=rid[z];if(!strcmp(only,"all")||!strcmp(only,rnames[i])){
    if(fresh&&madvise(image,j.B,MADV_DONTNEED)){perror("madvise");return 2;}
    fail|=run_reloc(&j,i,image,vals);
  }}
  /* Allocation lifetimes intentionally extend through all kernels. Leak checks off. */
  return fail;
}

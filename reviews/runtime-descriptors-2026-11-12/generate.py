#!/usr/bin/env python3
"""Generate review-only inputs; argument is an empty output directory."""
import pathlib, random, sys
root = pathlib.Path(sys.argv[1]); root.mkdir(parents=True, exist_ok=True)
(root/'go.mod').write_text('module ptrprobe\n\ngo 1.23\n')
base = ['int', 'string', 'struct{}', 'struct{ X int `json:"x"`; Ω string }',
        'func()', 'func(error) string', 'interface{}', 'interface{ M() int }', 'error', 'func(int, ...string) (map[string]int, error)',
        'interface{ Read([]byte) (int,error) }', 'chan<- []byte', '<-chan func()',
        '*int', '**int', 'map[struct{ X int }]chan<- []byte', '[0]func()',
        'struct{ X int `a:"\\x00"` }']
types = list(base)
for t in base:
    types += ['[]'+t, '[1]'+t, 'chan '+t, 'map[string]'+t, 'func('+t+') '+t]
for n in [0,1,7,8,15,16,31,32,63,64,127,128,255,256]:
    types += [f'[{n}]byte', 'struct{'+ ';'.join(f'F{i} byte' for i in range(n))+'}']
for n in [0,1,7,8,15,16,31,32,63,64,127,128]:
    types += ['func('+','.join(['int']*n)+')']
# Seeded syntax/type-tree fuzzing; descriptor fields straddle varint/count boundaries.
rng = random.Random(140040190)
def tree(depth):
    if depth == 0: return rng.choice(base)
    a = tree(depth-1)
    kind = rng.randrange(9)
    if kind == 0: return '*('+a+')'
    if kind == 1: return '[]('+a+')'
    if kind == 2: return f'[{rng.choice([0,1,2,7,8,15,16,127,128,255,256])}]('+a+')'
    if kind == 3: return 'chan ('+a+')'
    if kind == 4: return '<-chan ('+a+')'
    if kind == 5: return 'chan<- ('+a+')'
    if kind == 6: return 'map[[3]string]('+a+')'
    if kind == 7: return 'struct{ X '+a+'; Y [0]*int `json:"y,omitempty"` }'
    return 'func('+a+', ...string) ('+tree(depth-1)+', error)'
types += [tree(rng.randint(1,4)) for _ in range(512)]
# Every pointer is strongly emitted in p, while q only names the element as a value.
for pkg in ['p','q']:
    d=root/pkg; d.mkdir(exist_ok=True)
    s=f'package {pkg}\nimport "reflect"\n'
    if pkg=='p':
        s+='func Pointers() []reflect.Type { return []reflect.Type{\n'
        s+=''.join(f'reflect.TypeOf((*({t}))(nil)),\n' for t in types)+'} }\n'
    else:
        for i,t in enumerate(types):
            if t.startswith('interface') or t == 'error':
                s+=f'func e{i}() reflect.Type {{ return reflect.TypeOf(func(x {t}){{}}).In(0) }}\n'
            else:
                s+=f'func e{i}() reflect.Type {{ var v {t}; return reflect.TypeOf(v) }}\n'
        s+='func Elements() []reflect.Type { return []reflect.Type{'+','.join(f'e{i}()' for i in range(len(types)))+'} }\n'
    s+='func HiddenPointer() reflect.Type { return reflect.TypeOf((*struct{ x int })(nil)) }\n'
    s+='type U int\nfunc (*U) hidden() {}\nfunc HiddenOnly() reflect.Type { return reflect.TypeOf(struct{U}{}) }\n'
    s+='func Hidden() reflect.Type { return reflect.TypeOf(struct{ x int }{}) }\n'
    s+='type E int\nfunc (*E) hidden() {}\nfunc (E) Value() int {return 17}\n'
    s+='func Embedded() reflect.Type { return reflect.TypeOf(struct{E}{}) }\n'
    s+='func Implements(t reflect.Type) bool {return t.Implements(reflect.TypeOf((*interface{hidden()})(nil)).Elem())}\n'
    (d/'types.go').write_text(s)
(root/'main.go').write_text('''package main
import("fmt";"reflect";"runtime";"sync";"ptrprobe/p";"ptrprobe/q")
func must(b bool) {if !b {panic("identity/method mismatch")}}
func rebuild(t reflect.Type) reflect.Type {
 if t.Name()!="" {return t}
 switch t.Kind() {
 case reflect.Pointer: return reflect.PointerTo(rebuild(t.Elem()))
 case reflect.Array: return reflect.ArrayOf(t.Len(),rebuild(t.Elem()))
 case reflect.Slice: return reflect.SliceOf(rebuild(t.Elem()))
 case reflect.Map: return reflect.MapOf(rebuild(t.Key()),rebuild(t.Elem()))
 case reflect.Chan: return reflect.ChanOf(t.ChanDir(),rebuild(t.Elem()))
 case reflect.Func:
  in:=make([]reflect.Type,t.NumIn());out:=make([]reflect.Type,t.NumOut())
  for i:=range in {in[i]=rebuild(t.In(i))};for i:=range out {out[i]=rebuild(t.Out(i))}
  return reflect.FuncOf(in,out,t.IsVariadic())
 case reflect.Struct:
  fs:=make([]reflect.StructField,t.NumField());for i:=range fs {fs[i]=t.Field(i);fs[i].Type=rebuild(fs[i].Type)}
  return reflect.StructOf(fs)
 };return t
}
func main(){
 es:=q.Elements(); dyn:=make([]reflect.Type,len(es))
 for i,t:=range es {dyn[i]=reflect.PointerTo(t)}
 ps:=p.Pointers()
 for i,t:=range es {
  if dyn[i]!=ps[i] {panic(fmt.Sprint("cross-package index ",i,": ",dyn[i]," != ",ps[i]))};must(dyn[i]==ps[i]);must(reflect.New(t).Type()==ps[i]);must(ps[i].Elem()==t)
  must(rebuild(t)==t);must(reflect.PointerTo(rebuild(t))==ps[i])
  v:=reflect.New(t);must(reflect.TypeOf(v.Interface())==ps[i]);must(v.Elem().CanSet())
  dst:=reflect.New(ps[i]).Elem();dst.Set(v);must(dst.Interface()==v.Interface())
  m:=reflect.MakeMap(reflect.MapOf(ps[i],reflect.TypeOf(0)));m.SetMapIndex(v,reflect.ValueOf(i));must(m.MapIndex(v).Int()==int64(i))
  c:=reflect.MakeChan(reflect.ChanOf(reflect.BothDir,ps[i]),1);c.Send(v);r,ok:=c.Recv();must(ok&&r.Interface()==v.Interface())
  f:=reflect.MakeFunc(reflect.FuncOf([]reflect.Type{ps[i]},[]reflect.Type{ps[i]},false),func(v []reflect.Value) []reflect.Value{return v});must(f.Call([]reflect.Value{v})[0].Type()==ps[i])
 }
 var wg sync.WaitGroup
 for n:=0;n<32;n++ {wg.Add(1);go func(){defer wg.Done();for i,t:=range es {must(reflect.PointerTo(t)==ps[i]);for j:=0;j<8;j++ {t=reflect.PointerTo(t);must(reflect.New(t.Elem()).Type()==t)}}}()};wg.Wait();runtime.GC()
 a,b:=p.Hidden(),q.Hidden();must(a.String()==b.String());must(a!=b);must(reflect.PointerTo(a)!=reflect.PointerTo(b));must(reflect.PointerTo(a)==p.HiddenPointer());must(reflect.PointerTo(b)==q.HiddenPointer())
 must(p.Implements(reflect.PointerTo(p.HiddenOnly())));must(q.Implements(reflect.PointerTo(q.HiddenOnly())))
 for _,t:=range []reflect.Type{p.Embedded(),q.Embedded()} {pt:=reflect.PointerTo(t);must(pt.NumMethod()==1);must(reflect.New(t).MethodByName("Value").Call(nil)[0].Int()==17);must(p.Implements(pt)!=q.Implements(pt))}
 methodProbe()
 fmt.Println("PASS",len(es),"cross-package types; constructors; 32 concurrent readers; private names; methods")
}
''')
(root/'methods.go').write_text('''package main
import "reflect"
type V int
func (V) M() int {return 3}
type P int
func (*P) M() int {return 5}
type G[T any] int
func (*G[T]) M() int {return 7}
type A = struct{P}
type B = struct{*P}
type C = struct{V}
type D = struct{G[[]int]}
type E = struct{P;V}
type F = struct{P; M int}
type I interface{M() int}
type H = struct{I}
type NP *int
func methodProbe(){
 must(reflect.PointerTo(reflect.TypeOf(NP(nil)))==reflect.TypeOf((*NP)(nil)))
 for i,t:=range []reflect.Type{reflect.TypeOf(A{}),reflect.TypeOf(B{}),reflect.TypeOf(C{}),reflect.TypeOf(D{}),reflect.TypeOf(H{})} {
  pt:=reflect.PointerTo(t);must(pt.NumMethod()==1);must(pt.Implements(reflect.TypeOf((*I)(nil)).Elem()))
  v:=reflect.New(t);if i==1 {v.Elem().Field(0).Set(reflect.New(reflect.TypeOf(P(0))))}
  if i==4 {v.Elem().Field(0).Set(reflect.ValueOf(V(0)))}
  want:=[]int64{5,5,3,7,3}[i];must(v.Method(0).Call(nil)[0].Int()==want)
  x:=v.Interface();must(x.(I).M()==int(want));switch x.(type){case I:default:panic("type switch")}
 }
 for _,t:=range []reflect.Type{reflect.TypeOf(E{}),reflect.TypeOf(F{})} {must(reflect.PointerTo(t).NumMethod()==0)}
}
''')
print(len(types), 'element types generated in',root)

#!/usr/bin/env bash
# bt.sh <binary> [args]: run under gdb until it stops, then list the code addresses found on the stack, symbolized
# through the Go function table (for binaries without DWARF or an ELF symbol table).
bin=$1; shift
gdb -batch -ex "run $*" -ex 'info registers rip' -ex 'x/400gx $rsp' "$bin" 2>&1 > /tmp/bt.$$.out
grep -m2 "signal\|exited" /tmp/bt.$$.out
python3 - "$bin" /tmp/bt.$$.out <<'P'
import re,subprocess,sys
txt=open(sys.argv[2]).read()
addrs=[]
m=re.search(r'rip\s+(0x[0-9a-f]+)',txt)
if m: addrs.append(m.group(1))
for l in txt.split('\n'):
    if re.match(r'^0x[0-9a-f]+:',l):
        for w in l.split(':',1)[1].split():
            v=int(w,16)
            if 0x401000<=v<0x2000000: addrs.append(hex(v))
out=subprocess.run(['go','tool','addr2line',sys.argv[1]],input='\n'.join(addrs),capture_output=True,text=True).stdout.split('\n')
last=None
for i,a in enumerate(addrs):
    if 2*i+1>=len(out): break
    f=out[2*i]; 
    if f=='?' : continue
    line=f"{a} {f} {out[2*i+1].replace('/usr/local/go/src/','')}"
    if line!=last: print(line)
    last=line
P
rm -f /tmp/bt.$$.out

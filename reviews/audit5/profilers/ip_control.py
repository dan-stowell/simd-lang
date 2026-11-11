#!/usr/bin/env python3
import json
import probe
p = probe.asm("divide", """ mov $5000000,%ecx
 mov $3,%ebx
 mov $123456,%eax
loop:
 xor %edx,%edx
 div %ebx
after_divide:
 nop
 dec %ecx
 jnz loop
""" + probe.EXIT)
probe.run(["taskset","-c",probe.CPU,p])
probe.sample("divide",p)
(probe.HERE/"ip-control.json").write_text(json.dumps(probe.records,indent=2)+"\n")

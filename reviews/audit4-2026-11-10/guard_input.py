#!/usr/bin/env python3
"""Guarded input allocation in AUDIT COPIES of generated ELFs, not production.

The appended setup copies the same 4096 input bytes to a new allocation followed
by a PROT_NONE page, then enters the unchanged original generated program.
All original stencils and their operand offsets remain byte-identical.
"""
import json
import os
from pathlib import Path
import struct
import subprocess
import sys

R = Path(__file__).resolve().parents[2]
O = Path(__file__).resolve().parent
B = R/"build/audit4/guard"
B.mkdir(exist_ok=True, parents=True)
sys.path.insert(0, str(R/"boot"))
import strand

asm = """.intel_syntax noprefix
.text
.global _start
_start:
push rax
push rcx
push rdx
push rsi
push rdi
push r8
push r9
push r10
push r11
push r12
cmp qword ptr [rbx-8],4096
jne bad
mov eax,9
xor edi,edi
mov esi,8192
mov edx,3
mov r10d,0x22
mov r8,-1
xor r9d,r9d
syscall
cmp rax,-4096
jae bad
mov r12,rax
mov rdi,rax
mov rsi,[rbx-16]
mov ecx,4096
cld
rep movsb
mov eax,10
lea rdi,[r12+4096]
mov esi,4096
xor edx,edx
syscall
test rax,rax
jne bad
mov [rbx-16],r12
pop r12
pop r11
pop r10
pop r9
pop r8
pop rdi
pop rsi
pop rdx
pop rcx
pop rax
jmp finish
bad:
mov edi,77
mov eax,231
syscall
finish:
"""
(B/"setup.S").write_text(asm)
subprocess.run(["as","--64",B/"setup.S","-o",B/"setup.o"], check=True)
subprocess.run(["objcopy","-O","binary","--only-section=.text",B/"setup.o",B/"setup.bin"], check=True)
setup = (B/"setup.bin").read_bytes()
inp = bytes(range(256))*16
(B/"input").write_bytes(inp)
rows = []
for name, src in [
    ("byte_4092", "<s\n4092 [s out4\n"),
    ("byte_4093", "<s\n4093 [s out4\n"),
    ("byte_4094", "<s\n4094 [s out4\n"),
    ("byte_4095", "<s\n4095 [s out4\n"),
    ("dword_4092", "<s\n4092 {s out4\n"),
    ("dword_4095", "<s\n4095 {s out4\n"),
    ("byte_lead", "<s\n's out\n"),
    ("slice_tail", "<s\n4 ,d\n~1 4093 3 0 (s )d end\n'd out\n"),
]:
    original = strand.compile_(src.encode())
    dataoff = 112 + len(strand.tables()["stub"])
    codeoff = dataoff + struct.unpack_from("<I",original,dataoff)[0]
    guarded = bytearray(original)
    start = len(guarded)
    guarded += setup
    jumpoff = len(guarded)
    guarded += b"\xe9" + struct.pack("<i", codeoff-(jumpoff+5))
    # Only the ELF sizes and the initial code-entry displacement change.
    struct.pack_into("<QQ",guarded,96,len(guarded),len(guarded))
    struct.pack_into("<I",guarded,dataoff,start-dataoff)
    assert guarded[codeoff:len(original)] == original[codeoff:]
    row = {"name":name, "source":src, "old_code_offset":codeoff,
           "audit_setup_offset":start, "generated_code_unchanged":True}
    for kind, body in (("original",original),("guarded",guarded)):
        exe = B/(name+"."+kind)
        exe.write_bytes(body)
        exe.chmod(0o755)
        with (B/"input").open("rb") as f:
            p = subprocess.run(["taskset","-c",str(min(os.sched_getaffinity(0))),exe],
                               stdin=f,capture_output=True,timeout=10)
        row[kind]={"status":p.returncode,"output_hex":p.stdout.hex(),
                   "stderr":p.stderr.decode(errors="replace")}
    rows.append(row)
(O/"guard-input.json").write_text(json.dumps({"setup_assembly":asm,"rows":rows},indent=2)+"\n")
for row in rows:
    print(row["name"], row["original"]["status"], row["guarded"]["status"])

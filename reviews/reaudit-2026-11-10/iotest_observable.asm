
/home/exedev/simd-lang/build/reaudit-reproduction/run-rpxf0e1o/iotest_observable:     file format elf64-x86-64


Disassembly of section .init:

Disassembly of section .plt:

Disassembly of section .plt.got:

Disassembly of section .plt.sec:

Disassembly of section .text:

00000000000016d0 <touch>:
    16d0:	f3 0f 1e fa          	endbr64
    16d4:	48 8b 35 0d 2a 00 00 	mov    rsi,QWORD PTR [rip+0x2a0d]        # 40e8 <per>
    16db:	48 85 f6             	test   rsi,rsi
    16de:	74 40                	je     1720 <touch+0x50>
    16e0:	49 89 f8             	mov    r8,rdi
    16e3:	31 c0                	xor    eax,eax
    16e5:	31 d2                	xor    edx,edx
    16e7:	4c 0f af c6          	imul   r8,rsi
    16eb:	4c 03 05 fe 29 00 00 	add    r8,QWORD PTR [rip+0x29fe]        # 40f0 <base>
    16f2:	66 0f 1f 44 00 00    	nop    WORD PTR [rax+rax*1+0x0]
    16f8:	41 0f b6 0c 00       	movzx  ecx,BYTE PTR [r8+rax*1]
    16fd:	48 05 00 10 00 00    	add    rax,0x1000
    1703:	48 01 ca             	add    rdx,rcx
    1706:	48 39 f0             	cmp    rax,rsi
    1709:	72 ed                	jb     16f8 <touch+0x28>
    170b:	48 8d 05 4e 29 00 00 	lea    rax,[rip+0x294e]        # 4060 <sums>
    1712:	48 89 14 f8          	mov    QWORD PTR [rax+rdi*8],rdx
    1716:	31 c0                	xor    eax,eax
    1718:	c3                   	ret
    1719:	0f 1f 80 00 00 00 00 	nop    DWORD PTR [rax+0x0]
    1720:	31 d2                	xor    edx,edx
    1722:	48 8d 05 37 29 00 00 	lea    rax,[rip+0x2937]        # 4060 <sums>
    1729:	48 89 14 f8          	mov    QWORD PTR [rax+rdi*8],rdx
    172d:	31 c0                	xor    eax,eax
    172f:	c3                   	ret

Disassembly of section .fini:

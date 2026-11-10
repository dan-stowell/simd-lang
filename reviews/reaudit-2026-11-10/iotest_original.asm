
/home/exedev/simd-lang/build/reaudit-reproduction/run-rpxf0e1o/iotest_original:     file format elf64-x86-64


Disassembly of section .init:

Disassembly of section .plt:

Disassembly of section .plt.got:

Disassembly of section .plt.sec:

Disassembly of section .text:

00000000000016d0 <touch>:
    16d0:	f3 0f 1e fa          	endbr64
    16d4:	48 8b 15 45 29 00 00 	mov    rdx,QWORD PTR [rip+0x2945]        # 4020 <per>
    16db:	48 85 d2             	test   rdx,rdx
    16de:	74 3b                	je     171b <touch+0x4b>
    16e0:	48 8d 42 ff          	lea    rax,[rdx-0x1]
    16e4:	48 c1 e8 0c          	shr    rax,0xc
    16e8:	83 e0 01             	and    eax,0x1
    16eb:	48 89 c1             	mov    rcx,rax
    16ee:	b8 00 10 00 00       	mov    eax,0x1000
    16f3:	48 39 d0             	cmp    rax,rdx
    16f6:	73 23                	jae    171b <touch+0x4b>
    16f8:	48 85 c9             	test   rcx,rcx
    16fb:	74 13                	je     1710 <touch+0x40>
    16fd:	b8 00 20 00 00       	mov    eax,0x2000
    1702:	48 39 d0             	cmp    rax,rdx
    1705:	73 14                	jae    171b <touch+0x4b>
    1707:	66 0f 1f 84 00 00 00 	nop    WORD PTR [rax+rax*1+0x0]
    170e:	00 00
    1710:	48 05 00 20 00 00    	add    rax,0x2000
    1716:	48 39 d0             	cmp    rax,rdx
    1719:	72 f5                	jb     1710 <touch+0x40>
    171b:	31 c0                	xor    eax,eax
    171d:	c3                   	ret

Disassembly of section .fini:

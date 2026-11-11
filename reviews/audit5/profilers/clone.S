.global _start
.text
_start:

 lea stackend(%rip),%rsi
 mov $0x50f00,%edi
 xor %edx,%edx
 xor %r10d,%r10d
 xor %r8d,%r8d
 mov $56,%eax
 syscall
 test %eax,%eax
 jz worker
parent_wait:
 mov $35,%eax
 lea delay(%rip),%rdi
 xor %esi,%esi
 syscall
 cmpq $1,done(%rip)
 jne parent_wait
 mov $231,%eax
 xor %edi,%edi
 syscall
worker:
 mov $10000000,%ecx
worker_loop:
 vpxor %ymm0,%ymm0,%ymm0
 dec %ecx
 jnz worker_loop
 movq $1,done(%rip)
 mov $60,%eax
 xor %edi,%edi
 syscall
.data
delay: .quad 0,50000000
done: .quad 0
.bss
.lcomm stack,65536
stackend:

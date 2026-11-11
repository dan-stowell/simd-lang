.global _start
.text
_start:

 mov $9,%eax
 xor %edi,%edi
 mov $268435456,%esi
 mov $3,%edx
 mov $0x8022,%r10d
 mov $-1,%r8
 xor %r9d,%r9d
population_syscall:
 syscall
after_population:
 mov $60000000,%ecx
busy_loop:
 dec %ecx
 jnz busy_loop

 mov $60,%eax
 xor %edi,%edi
 syscall

.global _start
.text
_start:
 mov $300000000,%ecx
loop:
 dec %ecx
 jnz loop

 mov $60,%eax
 xor %edi,%edi
 syscall

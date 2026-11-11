.global _start
.text
_start:
 mov $100,%ecx
loop:
 vpxor %ymm0,%ymm0,%ymm0
 dec %ecx
 jnz loop

 mov $60,%eax
 xor %edi,%edi
 syscall

.global _start
.text
_start:
 mov $5000000,%ecx
 mov $3,%ebx
 mov $123456,%eax
loop:
 xor %edx,%edx
 div %ebx
after_divide:
 nop
 dec %ecx
 jnz loop

 mov $60,%eax
 xor %edi,%edi
 syscall

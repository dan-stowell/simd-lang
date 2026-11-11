.global _start
.text
_start:
 int3

 mov $60,%eax
 xor %edi,%edi
 syscall

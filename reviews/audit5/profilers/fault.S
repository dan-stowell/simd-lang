.global _start
.text
_start:
 ud2

 mov $60,%eax
 xor %edi,%edi
 syscall

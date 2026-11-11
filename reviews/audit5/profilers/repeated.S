.global _start
.text
_start:
 lea buf(%rip),%rdi
 mov $100,%ecx
 xor %eax,%eax
 rep stosb

 mov $60,%eax
 xor %edi,%edi
 syscall

.bss
.lcomm buf,100

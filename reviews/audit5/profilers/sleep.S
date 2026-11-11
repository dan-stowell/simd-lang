.global _start
.text
_start:

 mov $35,%eax
 lea delay(%rip),%rdi
 xor %esi,%esi
 syscall
after_sleep:

 mov $60,%eax
 xor %edi,%edi
 syscall

.data
delay: .quad 0,200000000

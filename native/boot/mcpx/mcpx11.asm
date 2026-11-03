# MCPX 1.1 boot ROM, mapped at 0xFFFFFE00, assembled with GNU as and placed by ld

.set FLASH_XCODES,   0xFF000080
.set STACK_TOP,      0x0008F000
.set PRELDR_START,   0xFFFFD400
.set PRELDR_END,     0xFFFFFC80
.set TEA_DELTA,      0x9E3779B9
.set PCI_CONFIG,     0x0CF8
.set MCPX_BRIDGE,    0x80000880
.set CR0_NO_CACHE,   0x9FFFFFFF

.code32
.globl _start
_start:
{load} xor %eax, %eax
mov $0x10, %al
mov %eax, %ds
mov %eax, %es
mov %eax, %ss
mov $FLASH_XCODES, %esi

# Each record is one opcode byte, an address dword and a data dword
fetch:
mov (%esi), %al
mov 1(%esi), %ebx
mov 5(%esi), %ecx
cmp $0x07, %al
jne op_mem_read
{load} mov %ecx, %edx
{load} mov %bl, %al
{load} mov %edx, %ebx
{load} mov %edi, %ecx

op_mem_read:
cmp $0x02, %al
jne op_mem_write
and $0x0FFFFFFF, %ebx
mov (%ebx), %edi
jmp advance

op_mem_write:
cmp $0x03, %al
jne op_and_or
mov %ecx, (%ebx)
jmp advance

op_and_or:
cmp $0x06, %al
jne op_pci_write
{load} and %ebx, %edi
{load} or %ecx, %edi
jmp advance

op_pci_write:
cmp $0x04, %al
jne op_pci_read
cmp $MCPX_BRIDGE, %ebx
jne pci_write_go
and $-3, %ecx
pci_write_go:
{load} mov %ebx, %eax
mov $PCI_CONFIG, %dx
out %eax, (%dx)
add $4, %dl
{load} mov %ecx, %eax
out %eax, (%dx)
jmp advance

op_pci_read:
cmp $0x05, %al
jne op_jne
{load} mov %ebx, %eax
mov $PCI_CONFIG, %dx
out %eax, (%dx)
add $4, %dl
in (%dx), %eax
{load} mov %eax, %edi
jmp advance

op_jne:
cmp $0x08, %al
jne op_jmp
{load} cmp %ebx, %edi
je advance
{load} add %ecx, %esi
jmp advance

op_jmp:
cmp $0x09, %al
jne op_accum
{load} add %ecx, %esi
jmp advance

op_accum:
cmp $0x10, %al
jne op_io_write
{load} and %ebx, %ebp
{load} or %ecx, %ebp
{load} mov %ebp, %edi
jmp advance

op_io_write:
cmp $0x11, %al
jne op_io_read
{load} mov %ebx, %edx
{load} mov %ecx, %eax
out %al, (%dx)
jmp advance

op_io_read:
cmp $0x12, %al
jne op_exit
{load} mov %ebx, %edx
in (%dx), %al
movzbl %al, %edi
jmp advance

op_exit:
cmp $0xEE, %al
jne advance
jmp decrypt

advance:
add $9, %esi
{disp32} jmp fetch

# Clears the fixed range MTRRs, points the default type MSR at ebx and enables caching
decrypt:
{load} xor %ecx, %ecx
mov $0x02, %ch
{load} xor %eax, %eax
{load} xor %edx, %edx
clear_mtrr:
wrmsr
inc %ecx
cmp $0x0F, %cl
jbe clear_mtrr
mov $0xFF, %cl
{load} mov %ebx, %eax
wrmsr
mov %cr0, %eax
and $CR0_NO_CACHE, %eax
mov %eax, %cr0

# Davies Meyer TEA hash over the pre-loader, two blocks of state alternating through the stack frame
mov $PRELDR_START, %ebp
mov $PRELDR_END, %edi
mov $STACK_TOP, %esp
{load} xor %ecx, %ecx
mov %ecx, 0x1C(%esp)
hash_chain:
{load} mov %ebp, %eax
{load} mov %edi, %ebx
mov %edi, 0x14(%esp)
mov %esp, 0x18(%esp)
hash_block:
mov %eax, 0x04(%esp)
mov %ebx, 0x08(%esp)
mov 0(%ebp), %edx
mov 4(%ebp), %esi
mov %edx, 0x0C(%esp)
mov %esi, 0x10(%esp)
mov %ecx, (%esp)
mov $0x20, %cl
tea_round:
addl $TEA_DELTA, (%esp)
{load} mov %ebx, %edx
{load} mov %ebx, %esi
add (%esp), %edx
shl $4, %esi
add 0x04(%esp), %esi
{load} xor %esi, %edx
{load} mov %ebx, %esi
shr $5, %esi
add 0x08(%esp), %esi
{load} xor %esi, %edx
{load} add %edx, %eax
{load} mov %eax, %edx
{load} mov %eax, %esi
add (%esp), %edx
shl $4, %esi
add 0x0C(%esp), %esi
{load} xor %esi, %edx
{load} mov %eax, %esi
shr $5, %esi
add 0x10(%esp), %esi
{load} xor %esi, %edx
{load} add %edx, %ebx
loop tea_round
xchg %eax, 0x14(%esp)
xchg %ebx, 0x18(%esp)
add 0x1C(%esp), %ebp
xorl $8, 0x1C(%esp)
{load} cmp %edi, %ebp
jb hash_block

lea 0x0C(%esp), %esi
mov %ebx, 0x10(%esp)
mov %eax, (%esi)
mov $preldr_digest, %edi
mov $4, %cl
repe cmpsl
jne halt
jmp PRELDR_START

halt:
mov $MCPX_BRIDGE, %eax
mov $PCI_CONFIG, %dx
out %eax, (%dx)
ljmp $0x08, $halt_write

preldr_key:
.byte 0xB4, 0x71, 0xEF, 0x97, 0x06, 0x5F, 0x49, 0x0A
.byte 0xE0, 0xF7, 0x2A, 0xA2
preldr_digest:
.byte 0xC7, 0x40, 0x67, 0xAF, 0x5D, 0x2A, 0xD5, 0xDA
.byte 0x93, 0x32, 0xD4, 0xC5, 0xE8, 0xF4, 0xBF, 0x68

.code16
real_entry:
.byte 0x66
lgdtw %cs:gdt_pointer
.byte 0x66
lidtw %cs:gdt_pointer
mov %cr0, %eax
or $1, %al
mov %eax, %cr0
ljmpl $0x08, $_start
{load} mov %di, %di

gdt:
.quad 0
.word 0xFFFF, 0x0000
.byte 0x00, 0x9B, 0xCF, 0x00
.word 0xFFFF, 0x0000
.byte 0x00, 0x93, 0xCF, 0x00

reset_vector:
jmp real_entry
{load} mov %di, %di
gdt_pointer:
.word 0x18
.long gdt
halt_write:
add $4, %dl
mov $2, %al
out %al, (%dx)

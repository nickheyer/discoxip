# MCPX 1.0 boot ROM, mapped at 0xFFFFFE00, assembled with GNU as and placed by ld

.set FLASH_XCODES,   0xFF000080
.set RC4_STATE,      0x0008F000
.set BLDR_BASE,      0x00090000
.set BLDR_SIZE,      0x6000
.set BLDR_MAGIC,     0x7854794A
.set BLDR_MAGIC_AT,  BLDR_BASE + BLDR_SIZE - 0x1C
.set FLASH_BLDR_END, 0xFFFFFE00 - BLDR_SIZE
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

# RC4 key schedule over the 16 byte key kept in this ROM
mov $0x03020100, %eax
mov $0x40, %ecx
mov $RC4_STATE, %esi
{load} mov %esi, %edx
identity:
mov %eax, (%edx)
add $4, %edx
add $0x04040404, %eax
dec %ecx
jne identity
{load} xor %ecx, %ecx
{load} xor %edi, %edi
mov $bldr_key, %ebp
mov %cl, 0x100(%esi)
mov %cl, 0x101(%esi)
{load} xor %ebx, %ebx
schedule:
{load} xor %edx, %edx
{load} xor %eax, %eax
mov (%edi,%esi,1), %dl
mov 0(%ebp,%ecx,1), %al
{load} add %al, %bl
inc %ecx
{load} add %dl, %bl
inc %edi
mov (%ebx,%esi,1), %al
mov %al, -1(%edi,%esi,1)
cmp $0x10, %ecx
mov %dl, (%ebx,%esi,1)
jne schedule_next
{load} xor %ecx, %ecx
schedule_next:
cmp $0x100, %edi
jb schedule

# Decrypts the 2BL from the top of flash into RAM
{load} xor %ecx, %ecx
{load} xor %edx, %edx
{load} xor %edi, %edi
{load} xor %eax, %eax
mov $RC4_STATE, %esi
mov $BLDR_SIZE, %ebp
mov 0x100(%esi), %cl
mov 0x101(%esi), %dl
stream:
inc %cl
mov (%esi,%ecx,1), %al
{load} add %al, %dl
mov (%esi,%edx,1), %bl
mov %bl, (%esi,%ecx,1)
mov %al, (%esi,%edx,1)
{load} add %bl, %al
mov FLASH_BLDR_END(%edi), %bl
mov (%esi,%eax,1), %al
{load} xor %al, %bl
mov %bl, BLDR_BASE(%edi)
inc %edi
dec %ebp
jne stream

mov BLDR_MAGIC_AT, %eax
cmp $BLDR_MAGIC, %eax
jne halt
mov BLDR_BASE, %eax
jmp *%eax

halt:
mov $MCPX_BRIDGE, %eax
mov $PCI_CONFIG, %dx
out %eax, (%dx)
ljmp $0x08, $halt_write

bldr_key:
.byte 0x27, 0x45, 0xA9, 0x10, 0x39, 0x7E, 0x6A, 0xA6
.byte 0x86, 0xFB, 0x4B, 0x1A, 0x4B, 0xA9, 0x0F, 0xD2
nop
nop
nop

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

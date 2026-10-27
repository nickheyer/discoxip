# Gaps

Every value the tool asserts instead of reading from the executable, the kernel, the flash or the archives, and everything that does not work.

## Rule

Zero hard coded values anywhere in code. Not one, under any circumstances. Everything is determined at build time from the input. Every row below is a violation of this rule until the value is derived.

Buckets

- CLAIM, an assertion about this dashboard, this kernel or the compiler that built them
- LIB, knowledge of XDK, kernel or C runtime routines and objects
- SPEC, a documented hardware or file format fact
- TUNE, a budget, limit, default, naming choice or output path

Verified on 2026-10-26 against sample/5659 with kernels/5713_orig.bin and sample/4304 with kernels/4034_orig.bin. Both builds exit 0.

## Web

pkg/web/static holds index.html and nothing else. No JavaScript file will ever exist in this project. The only JavaScript is the minimal embedded script in index.html.

## Broken

| where | what |
|---|---|
| repository | zero *_test.go files, go test ./... runs nothing |
| pkg/bios/hardware.go:78-109 | the whole console description is typed in, nothing derives it |

## Derivation failures in the samples

Routines the lifter records in render.json scene errors and continues past.

| sample | routine | reason |
|---|---|---|
| 5659 | 0006D774, 000777A8, 0011A2EC, 0011CA15, 0011D068 | more than 4096 blocks |
| 5659 | 000728D2, 00073658, 00129BDD | stack probe reserves a frame of no fixed size |
| 5659 | 0006FA2C | XapiFormatTimeOut reads EDX on entry, which no caller passes |
| 5659 | 000C5F25 | QueryPerformanceCounter reads EAX on entry, which no caller passes |
| 5659 | 0013ECAC | ADC EAX, EDX carry arithmetic after flags |
| 4304 | 000B0350 | more than 4096 blocks |
| 4304 | 00052B0C | XapiFormatTimeOut reads EDX on entry, which no caller passes |
| 4304 kernel | IoFileObjectType | lifted as a routine, 800109ED has no instruction |

Scene content the samples do not resolve.

| sample | scene | mesh |
|---|---|---|
| both | default_memory3 | Memory/Tunnel_01-FACES.xm packed in no extracted archive |
| both | default_music2 | Music/Tunnel_01-FACES.xm, Music/Tunnel_02-FACES.xm packed in no extracted archive |
| both | settings_adoc.xip | present beside the archives, named by no string in xboxdash.xbe, never extracted |
| 5659 | xodash/update.xbe, xodash/xonlinedash.xbe | name no scene archives, ignored |

## Duplicated assumptions

| assumption | places |
|---|---|
| entry stack pointer 0x10000 | pkg/xbe/symexec.go:1506 literal, pkg/xbe/lift.go:123 liftedEntrySP |
| writable section test Flags&0x1 | pkg/xbe/symexec.go:705, pkg/xbe/construct.go:385, pkg/xbe/scene.go:1218, pkg/xbe/xbe.go:405 |
| prologue idiom | pkg/xbe/symexec.go:358-363 with FLD/FLD1/FLDZ/FSTP, pkg/xbe/flow.go:136-151 without them |
| jump table shape JMP [index*4+disp] | pkg/xbe/symexec.go:2359, pkg/xbe/lift.go:181, pkg/xbe/machine.go:654, pkg/xbe/disasm.go:426 which alone requires Disp != 0 |
| thunk hop limit 8 | pkg/xbe/symexec.go:3175, 3204, 3406 |
| executor budget 20000 steps, 64 paths | pkg/xbe/convention.go:317, lookup.go:294, device.go:73, 200, 404, resource.go:162 |
| retry cap 4 | pkg/xbe/convention.go:336, device.go:97, 223, 438 |
| retry cap 6 | pkg/xbe/derive.go:504, 1042, scene.go:1523 |
| retry cap 8 | pkg/xbe/material.go:388, draw.go:458, 651 |
| D3DSURFACE_DESC words | pkg/xbe/symexec.go:3585 constants, pkg/xbe/scene.go:700 |
| maxCodepoint 0x024F | pkg/font/export.go:13 default, pkg/cli/font.go:89 argument |
| "Audio" directory | pkg/web/export.go:874, pkg/cli/extract.go:136 |
| "_meshes.json" | pkg/extract/extract.go:127, pkg/scene/scene.go:249 |

## CLAIM, assertions about the dashboard, the kernel or the compiler

### pkg/xbe/scene.go

| where | value | use |
|---|---|---|
| 159 | XGetDevices, XGetDeviceChanges, XInputOpen, XInputClose, XInputGetState, XInputPoll, XInputGetCapabilities | routines whose direct callers form the input layer |
| 162-176 | caller is unnamed, outside library sections, stores no absolute global | input layer membership |
| 199 | Node, Group, Transform, Level, Inline, Layer, Waver, HUD, Background, Viewpoint, NavigationInfo, Screen, Shape | classes whose draw, tick and hook must summarize |
| 224 | reaches D3DXMatrixPerspectiveFovLH at depth 0 and D3DDevice_Clear at depth 1 | identifies the frame routine |
| 241-258 | Group slot that forwards one virtual slot unchanged, not the draw slot | identifies the tick slot |
| 265 | has callers and reaches Direct3D_CreateDevice | identifies the init routine |
| 279 | pushes a wide string ending in .xap | identifies the main scene load |
| 288-296 | uses IoCreateSymbolicLink, called before init | identifies the drive linking routine |
| 306-316 | tick calls frame, so tick is the loop body | merged frame loop |
| 319-332 | static object advanced by the clock that is not the renderer | identifies the camera |
| 348 | Level, Transform, Background | natives that must summarize |
| 352 | Level.GoTo string callee | identifies the fire event routine |
| 371-375 | ImageTexture texture getter slot callees | image load helpers |
| 390, 396 | reaches D3DDevice_BlockUntilVerticalBlank at depth 1, or symbol D3DDevice_Swap | identifies present |
| 467 | this seeded in ECX | thiscall |
| 502-509 | callee name sub_%X with this equal to the static object | static callees |
| 590 | root type must be Node and vtable nonzero | which classes get scene slots |
| 752-757 | switch with at least 8 targets plus a pointer call | identifies the script dispatcher |
| 773-837 | virtual calls made on more than one value, one result field read as float is number, one not floated is string, none read is release | script value conversions |
| 841-860 | exactly one vtable stored by the dispatcher or its callees | number value class |
| 861-885 | native hands operator_new result to a constructor filling field from operator_new_array | string value class |
| 1000-1049 | Level field named control, tick hands it to exactly one helper, helper binds it into exactly one global | input target global |
| 1098-1147 | tick hook walking a global list it frees through operator_delete, one unlink helper reading one other field | timer list |
| 1180-1226 | exactly one backward jump, last MOV r,imm into a writable section is the head, last LEA r,[base+disp] disp>0 is the next link | list walk shape |
| 1242 | base not ESP or EBP, no index, no segment | object field operands |
| 1269 | MOV and PUSH immediates only | absolute global references |
| 1283-1295 | Level.GoTo, OnDeactivate searched to callee depth 2 | level switch and deactivation |
| 1299-1326 | Inline class, callees to depth 2, callee shared with level switch is the loader, exactly one unloader | archive loader |
| 1329, 1337 | .xip identifies the load step, initialize identifies the post load step | inline load steps |
| 1355-1358 | exactly 2 load steps and 3 post steps, roles Reset, Resolve, Populate, NewScope, Attach by call order | scene load helpers |
| 1377 | join helper called exactly twice | path join |
| 1390-1398 | this counted as one input and seeded in ECX | path join inputs |
| 1429-1434 | first wcschr input is src, a second distinct one is base, the wcscpy destination is out | path join roles |
| 1487 | string arguments are pushed immediates | string callee |
| 1502-1513 | vcall at the world slot on a global | matrix stack fallback |
| 1549-1553 | CALL [reg+disp] | virtual call slot |
| 1572-1589 | backward unconditional jump is the back edge | frame loop body |
| 1602 | exactly one virtual call | loop body dispatch |
| 1616-1643 | LEA r,[ECX+disp] is the children array, CALL [reg+disp] is the draw slot, a direct call is the element accessor, the last 1 byte access is the visible flag | group traversal |
| 1684 | On prefix | script event strings |

### pkg/xbe/system.go

| where | value | use |
|---|---|---|
| 23, 1267 | 4 | navigation directions |
| 100-167 | Translator.SetLanguage, Translator.GetTimeZoneText, Translator.GetTimeZoneCount, Date.getDaysInMonth, MemoryMonitor.selectUp, MemoryMonitor.FormatFreeBlocks, Config.GetXdashVersion, Translator.GetLanguageCode, Config.GetRecoveryKey, SavedGameGrid.selectDown, Config.ToggleNoisyCamera | natives looked up by class and name |
| 179-180 | Config.ForceSetLanguage, ForceSetTimeZone, ForceSetClock | force set natives and output keys |
| 195-219 | DiscDrive class, first scale 4 table in the constructor or direct callees, ends at 0 or a non printable string | disc type table |
| 229 | Text class, the draw slot the draw derivation found, direct non library callees are formatter candidates | clock formatter |
| 248-256 | prefix %d%c is ymd, %02d%c is dmy, contains : and %s is 12 hour, contains : and % is 24 hour, no % with a letter is meridiem | clock format classification |
| 267, 270 | exactly 4 formats and 2 meridiem words | clock formatter validity |
| 318-329 | FLD [abs] then next FSTP [abs] | selection clock global |
| 345-351 | one 1 byte MOV [abs] | camera noise flag |
| 395-413 | and of a constant with an expression mentioning a global, exactly one per native | launch flags |
| 431-446 | run of PUSH imm ending at a pushed string containing %, exactly 4 | dashboard version |
| 456-465 | scale 4 table, 4 byte entries of two UTF-16 units, count equals language count | language codes |
| 478-562 | native or direct callees, XcHMAC key arguments, letters are distinct lookup expressions, 1 byte load from const+(x rem m) | recovery key |
| 588-625 | MOVSD into EDI with a pending EBP local code pointer, MOV [abs],reg holding code, MOV [abs],imm wide string is a name and code address is a native | native record stores |
| 639-643 | exactly mov eax,imm32 ret (B8 .. C3, 6 bytes) | method table accessor |
| 745 | the walk ends at the first record with no name | method record walk |
| translate.go:112 | the section loader is the routine the language setter hands a word of its section name table, in the argument slot the call shows | section loader |
| translate.go:237 | the table readers are unnamed routines indexing memory the loader wrote pointers into the section at | translation table |
| translate.go:300 | a reader reads two words per entry, the entry a scale apart, the first word the key as keys precede values in a section | translation entries |
| translate.go:300 | every entry is confirmed by the reader answering the key with the value, in exactly one placement of the key and buffer among its inputs | translation entries |
| translate.go:367 | a replay places register inputs in the order the executor seeds them and the rest on the stack | replay inputs |
| translate.go:197 | code a replay reaches that no lift covered is lifted from the kernel for its addresses, else from the executable | replay code |
| translate.go:223 | segment relative reads land in a scratch block of words naming their own address, every list in it empty | replay processor block |
| xbe.go:138 | every section counts as referenced once before a replay, as its data is in memory | replay sections |
| 768-782 | MemoryMonitor class, XGetDevices argument, exactly one device type | memory unit type |
| 792-804 | first [disp+index*scale] operand with disp>0 is the table, scale 4, 0 terminator, 64 byte C string names an XBE section | translation sections |
| 805-865 | 3 operand IMUL stride, [scaled+disp] disp>0 base, int16 biases, 4 byte packed dates, key, std name required, dlt name optional | time zone table |
| 664 | the one vtable slot whose mov eax,imm ret accessors return writable tables holding a name and a native store | method table slot |
| 698 | stride is the shortest gap between two name stores past a base, first name and native offsets agree across tables | method record layout |
| 868 | the text native loads exactly one entry word, which is the key | time zone key |
| 898 | unnamed non library routine with an IMUL by the stride and the base as an immediate or displacement | time zone converter candidates |
| 968 | stores through one input whose values load entry words at TIME_ZONE_INFORMATION bias offsets, copies taking the field and an entry word, date helpers taking the field and entry bytes | time zone entry layout |
| 1072-1107 | inlined dates are four entry bytes stored at the SYSTEMTIME words, a helper date is read off the helper storing source bytes into destination words | packed date layout |
| 1239-1248 | scale 2 table, 13 int16 entries | days in month |
| 1259-1280 | scale 4 table, 8 units, 0 entry is an error, 0xFF terminator | device navigation |
| 1306 | native plus direct callees, first SHRD with an immediate | block shift |

### pkg/xbe/draw.go

| where | value | use |
|---|---|---|
| 177-193 | call site passing an immediate naming a wide string and an immediate non library function | registrar call site |
| 204 | at least 8 call sites | registrar |
| 226 | 90 percent majority | argument slot role |
| 226-299 | roles name, ctor and size by majority, descriptor and parent as the pointer slots where one holds the other, table the remaining pointer slot, name ctor size desc required | registrar arguments |
| 1272 | the one vtable slot whose mov eax,imm ret accessor returns every registered field table | field table slot |
| 1394 | unnamed non library routines calling through that slot, the names handed to a call lying a stride apart from the table, the records returned starting past the header | field table walker |
| 1317 | records of three words, the offset word the one no class repeats, the type word the other | field record words |
| 321-322, 1513 | printable wide strings | class and field names |
| 462-476 | first allocation is this, later ones new%d, this in ECX | factory constructor |
| 573-574 | Shape, Appearance, Mesh, MaxMaterial | required node classes |
| 585-588 | accessor call sits in the slot that stores the mesh global | draw slot |
| 591, 605, 626, 686 | one draw slot shared by Shape, Mesh, MaxMaterial, Appearance | draw slot |
| 610-618 | callee shared with the mesh draw is mesh loaded, callee handed a descriptor is kind check | shape draw helpers |
| 620, 1158-1167 | CMP r/m,imm with a registered descriptor | inlined kind check |
| 627-634 | exactly one plain callee other than the wrapper | material lookup |
| 637, 1200-1220 | registry base pushed as a known stack immediate to a library routine | inlined material lookup |
| 644 | buffer vtable longer than the accessor slot | buffer vtable |
| 647, 657 | slot 0 reaches D3DDevice_DrawIndexedVertices within depth 1 | buffer draw |
| 675 | this in ECX | draw paths |
| 703-704 | vcall result handed to SetTexture | texture slot |
| 712 | Node, Shape, Appearance, Material, MaxMaterial, Mesh, Sphere, Texture, ImageTexture | draw chain classes |
| 730, 736 | Transform constructor defaults required, Node is the root | class defaults |
| 821 | exactly one count field | buffer count |
| 825 | shader lookup takes the format field only | buffer format |
| 837 | vertices, stride, indices, primitive, count, format all present | buffer object |
| 845-881 | virtual call on the mesh whose Sphere override stores immediates at the format and stride offsets | load slot and sphere buffer |
| 886-888 | ImageTexture getter returns a this field | texture field |
| 1049-1053 | printable UTF-16, or a 0 word inside a section is the empty string | string constants |
| 1065, 1083 | only slots differing from the parent, exactly one matches | field set hook |
| 1122-1132 | arg0 word 0 is a field offset | hook descriptor |
| 1151 | exactly one Shape slot stores the mesh global | current mesh |
| 1189-1194 | only PUSH EAX between the helper call and the library call | helper result forwarding |
| 1233-1258 | unnamed library routine receiving a vcall result takes the requested name | naming by usage |

### pkg/xbe/derive.go

| where | value | use |
|---|---|---|
| 29-34 | MOV [disp32],imm32 with a data address | static device object |
| 77 | back buffer among the described fields | surface fields |
| 379-387, 463-470 | eq guard between a constant and a vcall result or a load from base fmt | vertex format constants |
| 501, 760 | this in ECX | thiscall seeding |
| 516-528 | world matrix is a vcall result whose argument is a global | world slot and matrix stack |
| 534 | view and projection matrices are pure data globals | transform globals |
| 545 | a matrix handed to several transforms is skipped | transform globals |
| 587-596 | loads a method table and has the most SetRenderState_ callees | render state wrapper |
| 622-651 | last CMP imm before a setter call, JE JNE keep it, JGE JL JG JLE JA JB JAE JBE reset it | complex state index |
| 665, 672 | a symbol carried by 2 or more functions is ambiguous | setter symbols |
| 731-747 | most direct setter calls plus wrapper calls | render state defaults routine |
| 782-791 | mov ecx,[disp+reg] within 7 instructions before SetRenderState_Simple, disp inside a section | method table load |
| 816, 826 | exactly one scaled input and one other input | wrapper inputs |
| 885-888 | array is 0 when the deferred setter is called | dashboard deferred copy |
| 976 | register 0 | node constants at c0 |
| 985 | rows 4 | node load is a matrix |
| 1020 | at most 400 instructions | clock tick candidate |
| 1049 | 8 byte absolute store | clock global |
| 1064-1084 | fmul of a tick term by a float constant under fadd fsub and unary operators | clock scale |
| 1133 | 0x3F800000 | hierarchy alpha starts at 1.0 |
| 1136-1147 | FMUL with an absolute operand | hierarchy alpha multiply |

### pkg/xbe/material.go

| where | value | use |
|---|---|---|
| 95-131 | most frequent (constant base, counter global) among stores of an object pointer at base+counter*k | material registry |
| 174-182 | unnamed non library function reaching D3DXCreateTexture or D3DDevice_CreateTexture at depth 1 and XGSwizzleRect at depth 0 | texture producer |
| 190-203 | function whose results become constructed objects, at least 3 sites | allocator |
| 212-236 | function constructing the most named objects, at least 8, allocator is the most used one | factory |
| 277 | factory is a single execution path | factory replay |
| 293-316 | most common 4 byte pointer field accepted by namesObject, vtable at +0 | material name field |
| 332 | vtable slot 0 | apply method |
| 378, 382 | this in ECX, one stack argument node | apply call |

### pkg/xbe/lookup.go

| where | value | use |
|---|---|---|
| 63-73 | unnamed function directly calling a symbol createsVertexShader accepts | shader lookup |
| 137-146 | exactly one input | fixed function lookup |
| 167-176 | nearest preceding CALL within 24 instructions, stopping at RET or JMP | format accessor |
| 238-241 | first argument register holding the result that the callee reads live | register passing |
| 251-253 | CALL [reg+disp] with no index | virtual call |
| 266-281 | mov obj,[mesh+off], mov vt,[obj+0], call [vt+slot] within 6 instructions | inlined accessor |
| 307 | accessor vcall takes only this | accessor |
| 314-320 | receiver is a load of [in.reg+off] | accessor shape |
| 329-361 | global most often loaded into the mesh register, lowest VA on ties | current mesh global |
| 377-381 | constants compared in the lookup guards | vertex formats |

### pkg/xbe/device.go and library.go

| where | value | use |
|---|---|---|
| device.go:158-176 | first load with disp>0 through a register loaded from the device global | back buffer |
| library.go:184-193 | named, not fastcall, reads ECX on entry | takes a receiver |
| library.go:197-215 | load slot reaches a texture creation through unnamed routines and the texture slot calls no image loader | class creates its own texture |
| library.go:245-253 | first MOV dword [reg+disp],imm in CMiniport::MapRegisters | GPU register base |
| library.go:264-287 | OR reg,imm with bit 31 after a dword load [base+disp], routine plus direct callees only | resource data offset and alias bit |

### pkg/xbe/convention.go

| where | value | use |
|---|---|---|
| 114-124 | unnamed helper takes live in registers then stack slots | calling idiom |
| 128-137 | cdecl symbol reading registers takes registers first | LTCG idiom |
| 138-140 | no live registers or canonical equals stack bytes means signature order, only ECX live means thiscall | calling idiom |
| 142-145 | canonical 4 bytes, no stack, one live register is the only parameter | LTCG register passing |
| 146-167 | unlisted names take registers then stack | calling idiom |
| 267-305 | unique solution, else unique exact, else unique passed in order with registers before stack | optimizer push order |

### pkg/xbe/inlined.go and resource.go

| where | value | use |
|---|---|---|
| inlined.go:354-415 | 4 byte MOV r,[base+index*4] disp 0, array pointer load disp>=8 within 7 instructions, data pointer at +8, JMP, CMP or TEST with Jcc pairs to one target, XOR dest,dest at failure | inlined array element |
| inlined.go:471-480 | only MOV or LEA that write neither dependency registers nor ESP are carried | scheduler interleave |
| inlined.go:559-581 | AND v,type, CMP v,cached, Jcc join, MOV v,[base+d], CMP or TEST, Jcc join, PUSH v, CALL | cached recursion shape |
| inlined.go:590-652 | MOV r,[base+0], TEST v,refMask, INC dword [base+0], AND count,refMask, CMP count,1, TEST v,busyMask, CALL, JMP last+2, DEC v, MOV [base+0] | AddRef and Release shapes |
| inlined.go:676-684 | AND v,typeMask then a load at i+3 and a recursive call | count helper |
| resource.go:449-457 | every push is the flush or a texture bind | inlined push commands |
| resource.go:666-668 | guards reading the device or the allocations are dropped | inlined XDK tests |

### pkg/xbe/symexec.go

| where | value | use |
|---|---|---|
| 289 | unnamed, outside library sections | where inlined array accessors are searched |
| 303-306, 321-328 | replayed as calls with This base+off, temp registers clobbered | inlined accessors and reference count helpers |
| 358-371 | PUSH EBP EBX ESI EDI, PUSH ECX after MOV EBP,ESP, walk over MOV LEA SUB AND XOR FLD FLD1 FLDZ FSTP, SUB or AND ESP marks the frame | prologue |
| 4362 | a prologue push of ecx popped back by an epilogue in reverse order reserves a local | prologue |
| 399-431 | SUB ESP,imm is an argument area when the next CALL arity equals imm/4 plus pushes, scan stops at jump RET POP | frame allocation |
| 452-460 | ADD ESP,imm not a frame size, or POP ECX EDX EAX after a call | caller cleanup |
| 567-586 | FIST or FISTP, no CALL, RET, at most 80 instructions | ftol helper |
| 595, 631-633 | unnamed callees in the entry point section | static object methods |
| 640, 676-688 | ECX preferred and default receiver | static object receiver |
| 649-653 | PUSH imm of a writable address right before a CALL to a callee with RET n | object as stack argument 0 |
| 790-809 | MOV [global],reg from EAX of a constructor call within 10 instructions | global holds that class |
| 846-848 | vtable constant at offset 0 size 4 | learned object vtable |
| 910-917 | first dword is a known function entry | isVtable |
| 1043-1044, 1080-1081 | [reg+disp] with disp inside a section and reg a call result | table indexed by the result |
| 1112-1113, 1300-1301 | absolute access below the image base | null dereference, path dropped |
| 1331 | fs gs es ds cs ss regions never emit stores | segment memory |
| 1524-1531 | unnamed non library entries fold inlined creations and resources | link time inlining |
| 1538-1546 | register inputs only for unnamed non library functions, ECX excluded | register arguments |
| 1690-1693 | nulled paths dropped | never runs through null |
| 1970-1987 | FWAIT FLDCW FNSTCW FNCLEX FNINIT WBINVD INVD are no ops, INT HLT UD2 abandon the path or end as trap | ignored instructions |
| 2005-2008 | MOV FS:[0],ESP | exception registration, pushes reset |
| 2025 | a saving PUSH ECX is not frame marked | PUSH ECX is a local |
| 2321, 2330 | XADD and CMPXCHG single threaded | no races |
| 2359-2363 | JMP [index*4+disp] no base | switch table |
| 2855-2869, 3132-3134 | CALL [reg+disp], reg loaded from [obj+0] within 96 instructions | virtual call |
| 2908-2910, 3427-3437 | single JMP [abs] into the thunk table | import thunk |
| 2911-2920 | EAX constant | stack probe size |
| 2922-2927 | EAX from f2i of st0, EDX symbolic | ftol convention |
| 2929-2944 | operands on the x87 stack, EAX ECX EDX clobbered | math thunks |
| 2996-2998, 3050-3052, 1718-1724 | tail jumped callee takes 4 times its stack inputs | unknown arity |
| 3069-3078 | unnamed code in library sections is always a call | library helpers |
| 3177-3180 | one instruction JMP rel | jump thunk |
| 3338-3341 | stack dropping below the call level | epilogue |
| 3559-3568 | forgetting stops at a frame marked slot | library writes stay in the buffer |
| 3607 | first non absolute pointer argument | surface description buffer |
| 3754-3799 | sub_ and fptr calls take This in ECX, vcall@ prepends ECX | call receivers |
| 3830-3837 | stdcall of unknown arity pops every push since the last call | unknown arity |
| 3852-3853 | sub_ vcall@ fptr calls forget every stack buffer passed | opaque calls |
| 3932-3935 | constant argument inside any section is a pointer | D3DX arguments |
| 4182 | FIST and FISTP round to nearest | control word ignored |

### pkg/xbe lifting, flow, disassembly, construction

| where | value | use |
|---|---|---|
| lift.go:137-145 | IConst, Ptr bases empty stack g%08X import: | register values that hold across blocks |
| lift.go:181-185 | JMP [index*4+disp] | jump table leaders |
| machine.go:67-73, 136-157 | Imm or LEA [abs] passing plausibleEntry, aligned words pointing at plausible code | dynamic leaders |
| machine.go:761 | 0x80000000 to 0xC0000000 | kernel RAM identity map, everything else is a device register |
| machine.go:764 | reg.ESP and reg.EBP bases | stack memory |
| machine.go:793 | cpu_ calls except ins fxrstor frstor fxsave fnsave, and port_out | calls that leave memory unchanged |
| machine.go:853-884 | previous instruction is a boundary or padding, padding is LEA r,[r+0], MOV r,r, XCHG r,r | plausible entry |
| machine.go:887-894 | entry point plus exports in executable sections | kernel roots |
| flow.go:13 | RET HLT INT have no successors | flow |
| flow.go:136-151 | PUSH EBP EBX ESI EDI, PUSH ECX after MOV EBP,ESP, walk over MOV LEA SUB AND XOR | prologue |
| flow.go:177-182 | PUSH of an unwritten register the function POPs | register save |
| flow.go:454-480 | kernel import pops 4 times the pushes since the last call unless cdecl | import cleanup |
| flow.go:482-496 | nearest MOV EAX,imm before the call | stack probe size |
| disasm.go:78-84, 128-155 | linear sweep of every section, undecodable bytes skipped by 1 | decode |
| disasm.go:193 | RET JMP HLT INT stop resynchronizing | resync |
| disasm.go:216-226 | PUSH EBP then any MOV | function entry |
| disasm.go:228-258, 452-474 | aligned word pointing at a decoded instruction after a boundary op, jump table targets excluded, lone JMP is a thunk | function entry |
| disasm.go:309, 378, 384, 393-397 | INT NOP padding, RET INT HLT end flow, JMP to next is padding, JMP into another entry is a tail call | function body |
| disasm.go:426-432 | scale 4, index, disp nonzero, ends at the first entry outside the section | jump table |
| disasm.go:477-483 | RET JMP INT NOP HLT | boundary ops |
| disasm.go:909-922 | AAS through XLATB, segment register moves, ADD [EAX],AL rejected | data decoded as code |
| disasm.go:943-962 | section with signature named functions other than the entry section is library, writable non entry section with none is pure data | section roles |
| construct.go:23 | EAX ECX EDX EBX ESI EDI | argument registers |
| construct.go:26 | XMM0 to XMM3 | float argument registers |
| construct.go:145-206 | MOV [argreg+0],imm passing isVtable | constructor and object register |
| construct.go:216-219 | ESP disp minus depth minus 4, EBP disp minus 8 | stack argument slot |
| construct.go:304-309 | forward JMP followed | factory failure branch |
| construct.go:331-382 | pushed or stored immediate at a printable UTF-16 string marks the object named, MOV [obj+0],vtable before any constructor, MOV [abs],objreg | object construction |
| construct.go:400-403 | MOV r,r reads nothing | padding |
| decompile.go:166-179, 382-385 | last SUB ESP,imm is locals, RET imm/4 is args, CALL [mem] is a vtable call | pseudocode |
| expr.go:417-422 | call result entering pointer arithmetic is an object base | result pointers |
| expr.go:687 | pointer compared with 0 is non null unless base is empty, this, res or a global object | null tests folded |
| sse.go:138-139, 275-282 | LDMXCSR STMXCSR PREFETCH SFENCE EMMS no ops, CVTSS2SI CVTPS2PI round to nearest | SSE control ignored |
| system_insn.go:160-170 | POPFD outside machine mode leaves flags unbound | flags |
| intrinsic.go:31-44 | MOV EDX,imm then JMP, descriptor name after one byte | math thunk |
| intrinsic.go:48-79 | at most 12 instructions, FXCH, two spills, core with FYL2X and FSCALE | pow thunk |
| intrinsic.go:82-112 | at most 400 instructions, rep MOVSD or STOSD, at least 2 jump tables, no calls | memcpy and memset |
| intrinsic.go:116-140 | CMP ECX,[mem] then Jcc then RET | security cookie check |
| intrinsic.go:144-163 | at most 40 instructions, TEST EAX,EAX, XCHG ESP,EAX, CMP EAX,0x1000 | stack probe |
| startup.go:13, 16 | _cinit, atexit | CRT initializer walk |
| startup.go:24-40 | register immediates outside the entry section in _cinit, in pairs | initializer tables |
| startup.go:92 | exactly one path | static initializer replay |
| template.go:20 | KeInitializeInterrupt copies code with rep MOVSD | interrupt template |
| xtlid.go:13 | .XTLID | library identity section |
| xbe.go:345-348 | UTF-16 units with a zero high byte only | material names |
| files.go:25, 32 | printable 0x20 to 0x7E, at least 3 characters | wide strings |
| files.go:72 | no \ / : % in the name | archive names |
| files.go:116-120 | %s prefix and X: drive stripped | directory preference |
| textures.go:64-79 | D3DXCreateTexture arguments at base 1, D3DDevice_CreateTexture at base 0 | produced textures |
| textures.go:80 | buffer bases buf and res | pixel stores |
| kernel.go:57-68, 81-84 | export referenced as [disp32] memory is a variable | kernel exports |

### Other packages

| where | value | use |
|---|---|---|
| pkg/bios/boot.go:126 | Pops 4 | kernel load routine cleanup |
| pkg/bios/boot.go:231 | the one kernel string ending in a backslash and the name of an executable under the launch directory, the digits after Partition | launch partition |
| pkg/bios/boot.go:263 | an executable is a file opening with the image magic | launch directory executables |
| pkg/bios/boot.go:153-166 | PUSH imm of the kernel key right before a CALL | kernel load routine |
| pkg/bios/disk.go:17-21 | Partition, IoCreateDevice, HalDiskCachePartitionCount, fixedPartitions 2 | partition enumeration |
| pkg/bios/disk.go:152 | device object +0x18 | DeviceExtension |
| pkg/bios/disk.go:164-196 | 9 words, sector aligned offset whose sector appears elsewhere, length aligned and different | partition extension layout |
| pkg/bios/image.go:10 | .data | section the flash copy overwrites |
| pkg/web/export.go:216 | fonts/Xbox.xtf, Xbox.xtf | file naming the font version |
| pkg/web/export.go:427 | default_default | entry scene |
| pkg/web/export.go:436-460 | archive_scene split on the last underscore, default is the main scene, inline URL is the capitalized scene plus .xap | scene naming |
| pkg/web/export.go:874, pkg/cli/extract.go:136 | Audio | auxiliary directory |
| pkg/scene/scene.go:114-128 | underscore prefix matching a manifest key | archive of a scene |
| pkg/scene/scene.go:140-156 | suffix :url, exactly one match | cross archive meshes |
| pkg/scene/gltf.go:195-201, 209-216, 294-313, 361, 474, 493-520 | Transform Shape Mesh, translation rotation scaleOrientation scale, geometry appearance material texture url name, transparency diffuseColor shininess emissiveColor | scene vocabulary |
| pkg/xap/ast.go:189-190, 226-227 | Mesh url, Material MaxMaterial name | scene vocabulary |
| pkg/xap/parser.go:339, 348, 453-458, 484 | an uppercase first letter marks a type name | grammar |
| pkg/cli/pack.go:115-126 | .vb is 6, .ib is 5, .xbx is 2, else 0, type 1 never written | archive entry types |
| pkg/mesh/mesh.go:42-51 | Shape Transform DEF material appearance geometry children url | text mesh detection |

## LIB, knowledge of XDK, kernel and C runtime routines

### Signature and name databases

| where | value | use |
|---|---|---|
| pkg/xbe/patterns.go:17 | patterns/dxbx.pat.gz, patterns/d3d8ltcg.pat.gz, patterns/xtlid.xml.gz | embedded Dxbx and D3D8LTCG signatures and the XboxDev identity table |
| pkg/xbe/patterns.go:164, 309-340 | d3d8ltcg, D3D8, D3D8LTCG | library names selecting pattern versions |
| pkg/xbe/patterns.go:458-473 | ??2@YAPAXI@Z operator_new, ??3@YAXPAX@Z operator_delete, ??_U@YAPAXI@Z operator_new_array, ??_V@YAXPAX@Z operator_delete_array, ?swprintf@@YAHPA_WIPB_WZZ _snwprintf | CRT decorated names |
| pkg/xbe/disasm.go:523-903 | kernelExports, 379 ordinals, 371 named | kernel export names by ordinal |
| pkg/xbe/disasm.go:971 | usage | library tag of functions named by shape |
| pkg/xbe/convention.go:91 | d3d8ltcg | excluded from canonical argument sizes |

### Parameter roles and conventions

| where | value | use |
|---|---|---|
| pkg/xbe/convention.go:28-47 | D3DDevice_SetTransform, SetTextureState_TexCoordIndex, SetTextureState_BorderColor, SetTextureState_ColorKeyColor, SetTextureState_BumpEnv, Swap, DrawVertices, DrawIndexedVertices, SetTexture, SetPalette, SetStreamSource, SetIndices, SetVertexShader, GetTransform, SetMaterial, SetViewport, CreateVertexShader, Direct3D_CreateDevice | declared parameter kinds, 18 entries |
| pkg/xbe/symexec.go:3155 | DbgPrint, RtlSprintf, RtlSnprintf, RtlVsprintf, RtlVsnprintf | kernel cdecl exports |
| pkg/xbe/symexec.go:3158 | HalReturnToFirmware, KeBugCheck, KeBugCheckEx, RtlRaiseException, RtlRaiseStatus | kernel exports that never return |
| pkg/xbe/translate.go:25 | RtlEnterCriticalSection, RtlEnterCriticalSectionAndRegion, RtlLeaveCriticalSection, RtlLeaveCriticalSectionAndRegion, KeEnterCriticalRegion, KeLeaveCriticalRegion | answered at once in a single threaded replay, popping what the kernel pops |
| pkg/xbe/symexec.go:3163 | every other export is stdcall | import calls |
| pkg/xbe/symexec.go:3578 | D3DDevice_CreateVertexShader 2, CreatePixelShader 1, CreateVertexBuffer 4, D3DVertexBuffer_Lock 3, D3DXCreateMatrixStack 1 | result through a pointer argument |
| pkg/xbe/symexec.go:3582 | D3DXCreateTexture 7 | result through a pointer argument in image creations |
| pkg/xbe/symexec.go:3588 | D3DXCreateTexture width 1 height 2 format 5, D3DDevice_CreateTexture 0 1 4 | texture creation arguments |
| pkg/xbe/symexec.go:3593, pkg/xbe/derive.go:50 | PixelJar::Get2DSurfaceDesc, D3DSurface_GetDesc, D3DTexture_GetLevelDesc | surface describers |
| pkg/xbe/symexec.go:3625 | ?0D3DXCOLOR | library body run inline |
| pkg/xbe/symexec.go:3629-3641 | D3DX and XG prefixes stripped, Matrix 64, Vec4 Quaternion Plane Color 16, Vec3 12, Vec2 8 | D3DX buffer sizes, Vec3 also applied to D3DXVec3Transform whose output is 16 bytes |
| pkg/xbe/symexec.go:3900-3917 | apiConstantBias 96, D3DDevice_SetVertexShaderConstant prefix | constant register numbering |
| pkg/xbe/symexec.go:4000 | D3DXMatrixDeterminant, D3DXVec3Dot, D3DXVec3Length, D3DXVec3LengthSq, D3DXVec4Dot, D3DXQuaternionDot, D3DXVec2Dot | scalar results, nothing written through arg0 |
| pkg/xbe/symexec.go:4009-4032 | D3DXVec3TransformNormal D3DXVec3TransformCoord D3DXVec3Transform arg2 64, D3DXMatrixInverse arg1 4, D3DXMatrixAffineTransformation arg2 arg4 12 arg3 16, D3DXMatrixRotationAxis D3DXQuaternionRotationAxis arg1 12, D3DXMatrixRotationQuaternion arg1 16 | argument buffer sizes |
| pkg/xbe/symexec.go:2947-2962 | libcmt plus mathIntrinsics | CRT math reads doubles at [esp] and [esp+8] |
| pkg/xbe/symexec.go:2549-2570 | D3DDevice_SetTransform, D3DDevice_GetTransform | rep movs over the transform array |
| pkg/xbe/symexec.go:3726-3729 | ECX, EDX, 8 bytes | fastcall |
| pkg/xbe/library.go:160-166 | ECX EDX fastcall, ECX receiver otherwise | library parameters |
| pkg/xbe/construct.go:98 | _security_check_cookie | leaves every register unchanged |
| pkg/xbe/intrinsic.go:14-19 | sin cos tan sqrt floor ceil exp log log10 atan asin acos sinh cosh tanh fabs fmod pow atan2 | CRT math names and arities |
| pkg/xbe/lookup.go:398-412 | D3DDevice_CreateVertexShader args 0 declaration 1 program, null program is fixed function | shader creation |
| pkg/xbe/derive.go:174, 197-199, 486-511, 579-643, 665-679, 779, 860-863, 931-939, 955-983, 1027-1098 | D3D8, D3DDevice_CreateVertexShader, D3DDevice_SetTransform, D3DDevice_SetRenderState_ prefix, _Simple _Deferred NotInline, D3DDevice_SetVertexShaderConstant prefix with count suffix args 0 register 2 count, KeTickCount | render derivation names |
| pkg/xbe/draw.go:593, 647, 691-700, 789-810, 829 | D3DDevice_SetVertexShader, DrawIndexedVertices args 0 primitive 1 count 2 index data, SetTexture arg 1, SetStreamSource args 1 vertices 2 stride, SetIndices arg 0 | buffer and draw calls |
| pkg/xbe/material.go:180-181 | D3DXCreateTexture, D3DDevice_CreateTexture, XGSwizzleRect | texture producers |
| pkg/xbe/device.go:59, 151, 187, 229, 283, 288, 413-429 | D3DDevice_SetTransform, GetDisplayMode, SetTextureState_TexCoordIndex, TEXCOORDINDEX under D3D8, SetTextureState_Deferred and SetRenderState_Deferred synthesized, constant setter ECX register EDX data | device derivation |
| pkg/xbe/resource.go:12-20 | D3DDevice_CreateVertexBuffer2, D3DVertexBuffer_Lock2, D3DDevice_SetTexture, D3DDevice_SetPalette, D3D::MakeRequestedSpace, D3D_BlockOnTime, D3D::DestroyResource, XMemAlloc, XMemFree | inlined resource code |
| pkg/xbe/resource.go:72-151, 175-201, 355-437, 546-620 | DestroyResource at depth 0 marks a stage setter, status 0 is palette, XMemAlloc takes 2 pushes attrs then size, two allocations header then data, AND imm data mask, last MOV [reg+disp],imm is Common, first MOV r,[abs] is the device, token SHL then ADD, device +0 is the push pointer, data word at +4 | inlined resource shapes |
| pkg/xbe/inlined.go:12-17, 224-263, 443-446 | D3D::ParseDeclarationStream, D3D::ParseProgram, D3DDevice_CreateVertexShader, handle or 1, D3D:: calls dropped, Common masks ref 0xffff type 0x70000 cached 0x50000 busy 0x780000 | inlined shader creation and D3DResource |
| pkg/xbe/library.go:12 | D3DTexture_GetLevelDesc, D3DTexture_GetSurfaceLevel, D3DSurface_GetDesc, D3DResource_Release, XGBytesPerPixelFromFormat, XGIsSwizzledFormat, XGUnswizzleRect | routines lifted for the runtime GPU |
| pkg/xbe/library.go:15 | CMiniport::MapRegisters, PixelJar::FindSurfaceWithinTexture | XDK internals looked up by name |
| pkg/xbe/system.go:507, 775 | XcHMAC args 0 key 1 length, XGetDevices arg 0 | natives |
| pkg/xbe/scene.go:1429 | wcschr, wcscpy | path join |
| pkg/xbe/scene.go:871, 927, 1159 | operator_new_array, operator_new, operator_delete | allocation names |
| pkg/xbe/template.go:20 | KeInitializeInterrupt | interrupt template |
| pkg/bios/disk.go:148 | IoCreateDevice takes 6 arguments | partition creation hook |

### Buffers library routines write

| where | value | use |
|---|---|---|
| pkg/xbe/symexec.go:3524, 3530 | wideBuffer 0x1000, itowBuffer 66 | default write extents |
| pkg/xbe/symexec.go:3535 | ReadFile arg1 count arg2 else 1<<20 and 4 bytes through arg3, GetFileSize 4 through arg1, SetFilePointer 4 through arg2 | outBuffers |
| pkg/xbe/symexec.go:3536 | FindFirstFileA 320 through arg1, FindNextFileA 320 through arg1, GetLocalTime 16, GetSystemTime 16 | outBuffers |
| pkg/xbe/symexec.go:3537 | QueryPerformanceCounter 8, QueryPerformanceFrequency 8, XGetLaunchInfo 4 through arg0 and 3072 through arg1 | outBuffers |
| pkg/xbe/symexec.go:3538 | XcSHAInit 116, XcSHAUpdate 116, XcSHAFinal 116 and 20 through arg1, XcHMAC 20 through arg6 | outBuffers |
| pkg/xbe/symexec.go:3539 | XQueryValue 4 through arg1, arg3 bytes through arg2 else 256, 4 through arg4 | outBuffers |
| pkg/xbe/symexec.go:3540-3542 | wcscpy wcscat swprintf _wcslwr _wcsupr lstrcpyW lstrcatW 0x1000, wcsncpy wcsncat 2 times arg2, _snwprintf 2 times arg1, _itow _ltow 66 through arg1 | outBuffers |
| pkg/xbe/symexec.go:3543 | memcpy memset arg2 else 1<<20, XNetRandom arg1 else 1<<16 | outBuffers |

### Vertex shader microcode and enums

| where | value | use |
|---|---|---|
| pkg/xbe/vsh.go:40-43 | 14 MAC names, 8 ILU names, 16 output names, 13 MAC argument sets | microcode decode |
| pkg/xbe/vsh.go:48-55 | 35 field positions across the 4 instruction words | microcode fields |
| pkg/xbe/vsh.go:58, 102-106 | XVS tag 0x2078, count in the high half, program at +8, 16 byte instructions | XVS header |
| pkg/xbe/vsh.go:67-95 | xyzw mask bit order, swizzle codes, mux 1 r 2 v 3 c, constant bias -96 | operand decode |
| pkg/xbe/vsh.go:121-178 | stored word order 3 0 1 2, output index & 0xF, MUX 0 MAC 1 ILU, ORB 0 rejected, paired ILU writes R1 | output routing |
| pkg/xbe/vsh.go:263-320 | R0 to R11, 11 outputs, MAC and ILU GLSL semantics, RCC and LIT clamps | GLSL translation |
| pkg/xbe/vsh.go:343-346 | R12 is oPos | position mirror |
| pkg/xbe/enums.go:22-66 | 139 render states with the build that introduced or removed each | render state names by XDK build |
| pkg/xbe/enums.go:19 | ZFUNC, FOGENABLE, PSTEXTUREMODES | section starts |
| pkg/xbe/enums.go:99-105, 113-118 | 32 texture stage states, op and arg states first before build 3949 | texture stage names by build |
| pkg/xbe/enums.go:128-130 | view 0, projection 1, world 6 | transform states |
| pkg/xbe/enums.go:134-145 | 20 vertex data types with byte and component counts | declaration decode |
| pkg/xbe/nv2a.go:5-14 | 40 NV097 method words | push buffer state names |
| pkg/xbe/symexec.go:3585, pkg/xbe/scene.go:700 | format 0, width 0x14, height 0x18 | D3DSURFACE_DESC |
| pkg/xbe/device.go:181 | 4 | texture stages |
| pkg/xbe/derive.go:370 | position blendweight normal diffuse specular fog pointsize backdiffuse backspecular texcoord0 to texcoord3 | fixed function registers |
| pkg/scene/layout.go:76-87 | register 0 position 0x32, 2 normal 0x32, 3 diffuse 0x40, 4 specular 0x40, 9 plus i texcoord 0x22 | FVF expansion |

## SPEC, hardware and file format facts

### Console hardware, pkg/bios/hardware.go:78-109

| where | value |
|---|---|
| 80-82 | 64 MB, 2 cycles per instruction, 733333333 Hz |
| 84-87 | interrupt base 0x30, PIT 1193182 Hz, flash mirror 0xFF000000, ROM overlay 0xFFFFFE00 |
| 88-92 | CPUID leaf 0 GenuineIntel max 2, leaf 1 0x68A 0x0383F9FF, leaf 2 0x03020101 0x0C040882 |
| 93 | SMC 0x20 version P01 challenge 52 72 EA F3, EEPROM 0xA8, encoder 0x8A, sensor 0x98 |
| 94 | GPU boot0 0x02A000A2, strap 0, RAM type 0 |
| 96-107 | 11 PCI functions, host, memory, lpc, smbus, usb0, usb1, nic, apu, aci, ide, agp bridge, gpu on bus 1, with vendor 0x10DE ids 02A5 02A6 01B2 01B4 01C2 01C3 01B0 01B1 01BC 01B7 02A0, revisions, classes, BAR sizes, IRQs 11 1 9 4 5 6 14 3 |

### Boot ROM and flash, pkg/bios

| where | value |
|---|---|
| bios.go:15-25 | bank 0x40000, MCPX 0x200, 2BL 0x6000 at 0x90000, preldr block 0x2A00, params 0x80, nonce 0x10, key 16, boot params 36, signature 0x7854794A |
| bios.go:56-57 | MCPX 1.0 sha1 5d270675b54eb8071b480e42d22a3015ac211cef key at 0x1A5, MCPX 1.1 sha1 6c875f17f773aaec51eb434068bb6c657c4343c0 key at 0x19C |
| bios.go:117, 145, 158-166 | 0xE9 marks a preldr, key xor 0x5C, entry record at block minus 8, first 16 bytes zeroed and the entry written |
| bios.go:190-201 | entry low 16 bits, keys pointer at entry minus 8, eeprom cert kernel keys in order |
| bios.go:213-218 | all zero or all 0xFF kernel key means a clear kernel |
| boot.go:16-26 | 2BL load 0x90000, run 0x400000, stack 0x8F000, GDT 0xFFFFFFD8 limit 0x18, x codes at 0x80 of 9 bytes ending at op 0xEE, CS 8, DS 0x10, CR0 0x11, kernel at 0x10000 |
| boot.go:129-132 | EBP and ESP, segments CS DS ES SS FS GS, GDT, CR0 at 2BL entry |
| boot.go:99 | section flags 0x5 |
| disk.go:22, 109-112 | sector 512, counted string length, length plus 1, pointer |
| disk.go:258-323 | ATA and ATAPI identify words, 16 heads, 63 sectors, cylinders capped 16383, LBA capped 0x0FFFFFFF, cycle times 120, feature words 0x001E 0x0017 0x4020 0x4000 0x0020 0x4000 0x0407 0x6001, checksum word 0xA5A5 |
| eeprom.go:13-42 | 256 bytes, hash 0, confounder 0x14, HDD key 0x1C, region 0x2C, factory 0x30 to 0x60, serial 0x34, MAC 0x40, online key 0x48, video standard 0x58, user 0x60 to 0xC0, time zone fields, language 0x90, video 0x94, audio 0x98, parental 0x9C to 0xA4, misc 0xB8, DVD region 0xBC |
| eeprom.go:79, 105-132 | 12 character serial, ones complement section checksum, HMAC SHA1 then RC4 seal |
| fatx.go:15-28 | FATX magic, 0x1000 superblock, 512 sectors, 32 sectors per cluster, root cluster 1, 64 byte entries, 42 byte names, 0xFF free, attributes 0x10 0x20, FAT16 limit 0xFFF0, chain ends 0xFFFF 0xFFFFFFFF, 0x1000 page |
| fatx.go:160-178, 189-190 | entry layout name length 0, attribute 1, name 2, cluster 44, size 48, times 52 56 60, packed time and date from 1980 |
| lzx.go:10-43 | 128 KB window, 32 KB chunk, 6144 growth, LZX extra bits and position base tables, E8 threshold 32768 |
| pe.go:37-82, 94-114 | MZ, PE at 0x3C, optional header 0x10B, fields at 16 28 56 60 92 96 100, 40 byte section headers, export directory words 16 20 28 |
| pe.go:151-156 | characteristics 0x80000000 is writable 0x1, 0x20000000 is executable 0x4 |
| image.go:10 | .data |

### Executable, pkg/xbe

| where | value |
|---|---|
| xbe.go:22-27 | entry xor retail 0xA8FC57AB debug 0x94859D4B sega 0x40B5C16E, thunk xor retail 0x5B6D40B6 debug 0xEFB1F152 sega 0x2290059D |
| xbe.go:32 | XBEH |
| system.go:71-83 | TIME_ZONE_INFORMATION bias 0, standard name 4, standard date 0x44, standard bias 0x54, daylight name 0x58, daylight date 0x98, daylight bias 0xA8, SYSTEMTIME month 2, day of week 4, day 6, hour 8 |
| xbe.go:32-90, 113-117, 153, 188-207, 248-254 | XBEH, 0x178 header, 0x38 section headers, 20 byte TLS, 16 byte library entries with name 8 major +8 minor +10 build +12 |
| xbe.go:175, 178 | sega when entry & 0xF0000000 is 0x40000000, debug when entry xor debug key is below 0x4000000 |
| xbe.go:405, symexec.go:552-556, 705-708, 1283, machine.go:131, 165, disasm.go:296 | flags 0x1 writable, 0x4 executable, bytes past raw size are not constant |
| disasm.go:112-123 | kernel thunk table 0 terminated, ordinal & 0x7FFFFFFF |
| derive.go:609-612, resource.go:24, 243 | push header count at bit 18, method & 0x1FFF, token floor 1<<18, bits above 13 match |
| resource.go:151, library.go:286 | alias bit 0x80000000 |
| vsh.go:376-396 | D3DVSD end 0xFFFFFFFF, type tok>>29, stream tok&0xF, skip bit 28, data type (tok>>16)&0xFF, register tok&0x1F |
| patterns.go:96-242, 289-294 | IDA pat syntax, --- terminator, 32 byte prefix, .. wildcard, ^ references, : names |
| patterns.go:256-269 | FLIRT CRC16 init 0xFFFF poly 0x8408 |
| patterns.go:427, 437-492 | rel32 and abs32 references, MSVC decoration letters for cdecl, fastcall @, stdcall @N |
| symexec.go:666-667, 748-751, 1588, 2987-2993, 3205-3214, 3983-3984, construct.go:29, 92, 289 | RET n cleanup, EAX ECX EDX caller saved, EBP EBX ESI EDI callee saved, XMM0 to XMM7, result in EAX |
| symexec.go:919-923, lift.go:120, system_insn.go:10-12, 4333-4338 | register and segment indices, CMOV to Jcc |
| flow.go:347-351, decompile.go:175-179 | stack arguments at disp 4 and above, RET imm/4 |
| machine.go:99-104, 722, system_insn.go:121-126 | HLT INT STI POPFD end a block, UD2 is INT 6 |
| system.go:444 | reverse push order |

### Archive and asset formats

| where | value |
|---|---|
| pkg/xip/entry.go:8-13 | entry types 0 1 2 4 5 6 |
| pkg/xip/entry.go:56-57 | mesh entry pool in the top byte, index start in 24 bits, size is the triangle count |
| pkg/xip/xip.go:11, 65, 81-82, writer.go:61-71 | XIP0, 16 byte header, 16 byte records, 4 byte name records, 4 byte alignment |
| pkg/texture/format.go:9-14 | R5G6B5 0x05, A8R8G8B8 0x06, X8R8G8B8 0x07, DXT1 0x0C, DXT3 0x0E, DXT5 0x0F, no other format decodes |
| pkg/texture/xpr.go:10, 53, 85, 131-134 | XPR0, resource type 4, format register at 0x18, format bits 8 to 15, mips 16 to 19, log2 width 20 to 23, log2 height 24 to 27 |
| pkg/texture/dxt.go | DXT1 DXT3 DXT5 block decode |
| pkg/buffer/format.go:56-85 | D3DVSDT codes 0x12 0x22 0x32 0x42 0x72 0x40 0x16 0x25 0x45 0x15 0x35 0x11 0x21 0x31 0x41 0x14 0x24 0x34 0x44 |
| pkg/buffer/vb.go:10 | 8 byte pool header, vertex count then format |
| pkg/font/xtf.go:53-72, 83, 88-98, 137 | XTF0 and XTF1, height 0x04, name 0x08, glyph count 0x30, range count 0x34, ranges 0x38, 28 byte glyph records, chunk header of two words |
| pkg/font/lzx.go:8-22, 151-162 | LZX symbols, window 15 to 21 bits, slot counts |
| pkg/font/xtf.go:313 | every window of the format is tried and exactly one must decode every chunk to its stated size |
| pkg/font/xtf.go:249-309 | Unicode block names |
| pkg/xap/parser.go:40-43 | Windows-1252 high half |
| pkg/xap/parser.go:92-101, 208-238, 312-317, 404-409, lexer.go:175 | function var behavior DEF USE children, true TRUE false FALSE, identifier characters - . / ~ |
| pkg/mesh/text.go:15-16 | DEF and url patterns |
| pkg/scene/gltf.go:136, 423-454, 577-580, 631-650 | glTF 2.0 generator discoxip, targets 34962 34963, component types 5126 5123, sampler 9729 9987 10497, magic glTF 2, JSON 0x4E4F534A, BIN 0x004E4942 |
| pkg/scene/layout.go:55-62 | FVF bits XYZ 0x002, normal 0x010, diffuse 0x040, specular 0x080, texcoord count 0xF00 shift 8 |
| pkg/web/export.go:692-707, 739-778, 852-864 | IMA step and index tables, RIFF WAVE fmt data, tag 0x0069, 4 byte channel headers, 8 samples per 4 bytes, 44 byte PCM header |
| pkg/web/export.go:225 | font version is the fourth magic byte |

## TUNE, budgets, defaults, naming and output paths

### Executor budgets and limits

| where | value |
|---|---|
| pkg/xbe/symexec.go:36, 46 | this buffer index -1, 1024 stack buffer elements |
| pkg/xbe/symexec.go:168-174 | 60000 steps, 512 paths, 4000 inline size, 2500 frame steps, 4 loop unrolls, 1<<16 rep unroll, 1<<16 rep compare |
| pkg/xbe/symexec.go:382, 577, 604, 791, 839, 1124, 1132, 1240, 1254, 1650, 1660, 3109, 3175, 3204, 3266, 3406, 3802, 3811, 3859 | alloc scan 40, ftol 80 instructions, 8 static this rounds, look back 10, 6 learn retries, overlap 63, split above 8 bytes, reassembly 64, frame budget 1<<30, trace every 5000 or first 400, vtable scan 96, hops 8, demand scan 400, 8 stack words, pointee extent 1<<30 |
| pkg/xbe/symexec.go:3535, 3543 | 1<<20 and 1<<16 fallback write sizes |
| pkg/xbe/expr.go:308, 769, 951 | decimal below 1024, 512 node compare budget, 1<<21 path nodes |
| pkg/xbe/lift.go:114, 117, 123, 327 | 20000 block steps, 4096 blocks, entry SP 0x10000, re lift 4 times blocks |
| pkg/xbe/scene.go:111, 114, 442-447, 456, 1392-1393, 1407, 1523, 1673 | callee depth 2, 262144 path effects, 4096 or 256 paths, 20000 steps, 4 stack arguments, 256 and 20000 for the join, 0 or 6 retries, look back 5 |
| pkg/xbe/convention.go:317-318, 336, pkg/xbe/lookup.go:167, 257, 294-295, pkg/xbe/device.go:73-74, 97, 200-201, 223, 404-405, 424, 438 | 20000 steps, 64 paths, 4 retries, look back 24 and 6, row count 4 |
| pkg/xbe/derive.go:362, 494-504, 604, 782, 963-965, 1042 | store width 4, 30000 steps, 128 paths, 3 seeded arguments, 6 retries, 256 table entries, look back 7 |
| pkg/xbe/draw.go:256, 368, 478, 484, 676-677, 1182, 1397 | 90 percent, 64 slots, 3 and 2 seeded arguments, 8 retries, look back 3, 4096 paths and 20000 steps for a walker |
| pkg/xbe/material.go:203, 236, 348-352, 355, 388 | 3 sites, 8 objects, highest VA constructor, sort by name, 8 attempts |
| pkg/xbe/resource.go:27, 162-163, 167, 200 | push window 8, 64 paths, 20000 steps, 4 inputs, shift below 24 |
| pkg/xbe/inlined.go:213, 362 | default program 0, look back 7 |
| pkg/xbe/system.go:498, 804, 950 | 2 seeded arguments, 64 byte names, 64 paths and 20000 steps for a symbolic run |
| pkg/xbe/textures.go:27-28 | 50000000 steps, no frame limit |
| pkg/xbe/patterns.go:40, 91, 328, 336, 317-371, 410-411 | 8 weak bytes, 1<<20 and 1<<24 line buffers, 288 bytes read, distance sentinel 1<<30, nearest version, lexical tie break, weak patterns need references |
| pkg/xbe/vsh.go:296, 371 | ARL epsilon 0.001, 64 declaration tokens |
| pkg/xbe/disasm.go:430, 907 | 512 jump table entries, 8 instructions inspected |
| pkg/xbe/construct.go:296 | 96 construction steps |
| pkg/xbe/decompile.go:521, 525 | decimal below 256, strings between 3 and 199 characters |
| pkg/bios/disk.go:136-139, 158 | scratch 0x7FFF0000 0x7FFE0000 0x7FFE1000 0x7FFD0000, 100000 steps |
| pkg/font/xtf.go:100 | 100000 ranges |
| pkg/xbe/translate.go:14-18, 22 | replay stack 0x7FFF0000, return 0x7FFD0000, buffer 0x7FFC0000, processor block 0x7FFA0000 of 0x1000 bytes, 1<<24 blocks |
| pkg/extract/extract.go:17, pkg/cli/xbe.go:186 | 256 KB buffers |

### Environment switches

| where | value |
|---|---|
| pkg/xbe/symexec.go:160-164 | DISCOXIP_TRACE, DISCOXIP_NO_VRESOLVE, DISCOXIP_NO_KEEP |
| pkg/xbe/scene.go:108, pkg/xbe/derive.go:161 | DISCOXIP_SCENE_TRACE, DISCOXIP_STAGE_TRACE |

### Defaults

| where | value |
|---|---|
| pkg/bios/eeprom.go:64-73 | serial 000000000000, MAC 00 50 F2 00 00 01, region 1, video 0x00400100, language 1, DVD region 1, GMT and BST |
| pkg/bios/disk.go:63-64 | DISCOXIP VIRTUAL HARD DISK DISCOXIP000001 1.0, DISCOXIP VIRTUAL DVD-ROM DISCOXIP000002 1.0 |
| pkg/font/export.go:13, pkg/cli/font.go:89 | codepoints up to 0x024F |
| pkg/scene/gltf.go:483-530 | double sided, metallic 0, roughness 0.8, white base color, roughness 1 minus shininess, BLEND below alpha 1 |
| pkg/web/static/index.html:23-24 | three 0.170.0 from cdn.jsdelivr.net |
| pkg/cli/version.go:11-12 | dev, none |

### Symbol and effect vocabulary matched by later code

| where | value |
|---|---|
| pkg/xbe/symexec.go:20, 2408, 2482, 2497-2532, eval.go:264-297 | store, call, fill, copy, port_in, port_out |
| pkg/xbe/symexec.go:672, 974-1008, 1036-1040, 1064-1101, 1305, 1308, 1506-1509, 2538, 2619, 2888, 3098, 4074-4090 | arg%d, reg.*, segbase.*, fs gs, res%d, g%08X, import:*, stack, reg.EBP, sub_%08X, cond.*, vcall@%X, fptr, fpu.* |
| pkg/xbe/symexec.go:303, 837, 2537, 2929-2944, 3983-3984 | clobber.*, learn%d, Collisions_ prefix |
| pkg/xbe/lift.go:56-58, 280-296, 313, 366-371, 408-413, 428-435, 482-489, 730 | jump branch switch return trap halt noreturn jumpind hlt int iret, flags.*, fcmp.*, XMM%d, block keys, xmm%d.%d, mm%d.%d, t%d |
| pkg/xbe/machine.go:323-330, 446, 648 | reg.ESP, machine flag kind, flag kind codes, self |
| pkg/xbe/system_insn.go:16-49, 97 | 33 cpu_ call names, dfstep%d |
| pkg/xbe/eval.go:35-40, 335-339, 483-506 | registers start at 0, rdtsc increments by 1, resolved symbol names |
| pkg/xbe/expr.go:323-353, 371, 489, 753 | dump notation, result pointers on, 10 float operators, %s@%X |
| pkg/xbe/convention.go:14-25, 61-81, 184-218, 325-331, 361-517 | 8 parameter kinds, role priority, role satisfaction, in.* names, role heuristics |
| pkg/xbe/material.go:101, 269, 285, 372 | obj%d, texture:%08X |
| pkg/xbe/lookup.go:40, 299-319, 373 | arg%d, in.*, fmt |
| pkg/xbe/device.go:78-88, 110, 198, 409, 415, 429 | in.state, mat, in.stage, in.value, in.register, data |
| pkg/xbe/library.go:80, 152-178 | vcall@ excluded, empty parameter, stack%d, XMM%d |
| pkg/xbe/kernel.go:111, 177 | kernel tag, vcall@ excluded |
| pkg/xbe/derive.go:475, 672, 920, 936 | view projection, Collisions_ prefix, uppercase without underscores |
| pkg/xbe/draw.go:791-832 | vertices stride indices primitive count format |
| pkg/xbe/inlined.go:339 | array_element |
| pkg/xbe/system.go:273 | ymd dmy time12 time24 order |
| pkg/xbe/decompile.go:53, 195-247, 661 | sub_%08X, arg_%X var_%X ebp_%X, arg%d, loc_%08X, int byte short |

### Output paths and names

| where | value |
|---|---|
| pkg/xbe/xbe.go:368, 402 | image.bin, header pseudo section |
| pkg/bios/boot.go:75-78, 213, disk.go:50-51, pkg/cli/bios.go:130, 136-143 | flash.bin bldr.bin kernel.bin eeprom.bin disk.bin disk.json machine.json boot.json hardware.json, .out, 2bl.bin kernel.exe kerneldata.bin inittbl.bin eeprom.key cert.key kernel.key |
| pkg/extract/extract.go:81, 110-120, 127, 177 | archive_ scene prefix, ~%d and ~archive_%d pools, _meshes.json, .xbx to .png |
| pkg/web/export.go:29-33, 79, 102, 115, 146, 175, 178, 189, 919-924 | data/scenes data/meshes data/textures data/files, manifest.json config.json render.json, proc_%08X.png, sanitized names |
| pkg/cli/build.go:57-61, extract.go:52, 106, scene.go:74, web.go:38 | out, .extracted, web, build, _web |
| pkg/scene/scene.go:274, 307, 316 | .xbx .vb .ib extensions |

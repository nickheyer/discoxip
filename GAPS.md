# Gaps

Every value the tool asserts instead of reading from the executable, the kernel, the flash or the archives, and everything that does not work.

## Rule

Zero hard coded values anywhere in code. Not one, under any circumstances. Everything is determined at build time from the input. Every row below is a violation of this rule until the value is derived.

Buckets

- CLAIM, an assertion about this dashboard, this kernel or the compiler that built them
- LIB, knowledge of XDK, kernel or C runtime routines and objects
- SPEC, a documented hardware or file format fact
- TUNE, a budget, limit, default, naming choice or output path

## Web

pkg/web/static holds index.html and nothing else. No JavaScript file will ever exist in this project. The only JavaScript is the minimal embedded script in index.html, the bridge handing the page's runtime the browser's clock, timers, loaded scripts, fetched files, stored bytes, status text, the keys held, the gamepads and the canvas it draws on. The runtime the page loads is written by Go from the lifted loader, kernel and executable and from the console the boot derived, the page booting the console from the init table through the loader and the kernel to the executable, every device answered from the hardware the kernel's own code programs, the registers the kernel and the executable poll answered with what exits each poll, the disk the build laid out and the console's settings. The graphics engine executes the 3D class's methods the FIFO delivers, translating the vertex programs the executable uploads and the register combiner state it sets into WebGL shaders, drawing into the console's surfaces and scanning out the surface the display controller names every field.

| what the page lacks |
|---|
| the GPU features no sample reaches stop the page with a fault when a program sets them, two sided lighting, depth clamping, polygon stippling, the quincunx convolution kernel, 3D textures, textures carrying border texels, the BRDF, dot product depth replace and constant specular reflection texture programs, the hilo hemisphere dot mappings, exclusive window clips, logic operations and line or point polygon modes |
| the sound processor's voice and signal processors and the network controller's transmitter and receiver have no models, their windows keeping registers as written and answering the registers the kernel and the executable poll with what exits each poll |
| the network controller's PHY has no link partner, so the executable's PhyInitialize call waits out the kernel's 6000 link polls of 500 microsecond stalls, 3 seconds of the real clock, before the dashboard opens its menu |
| the controller carries no memory unit, the daughterboard hub's other ports and the controller's expansion slots standing empty |
| the rumble the dashboard sends the controller reaches no gamepad, the bridge handing over no actuator |
| the second host controller keeps its registers as written, the console wiring no port to it |
| the 4304 dashboard on a v1.0 console takes the A button on its clock screen and then answers no button, pad or stick on its main menu, the controller's reports carrying every press through the daughterboard's hub |
| a surface the executable reads back through the processor holds nothing, the engine drawing surfaces on the page's canvas and never into the console's memory |
| every colour surface is drawn at 8 bits per channel whatever its format names |

## Derivation failures in the samples

Scene content the samples do not resolve.

| sample | scene | mesh |
|---|---|---|
| both | default_memory3 | Memory/Tunnel_01-FACES.xm, named by memory3.xap in default.xip, present in no file of the sample |
| both | default_music2 | Music/Tunnel_01-FACES.xm, Music/Tunnel_02-FACES.xm, named by music2.xap in default.xip, present in no file of the sample |
| both | settings_adoc.xip | an executable image, not an archive, named x:\Settings_adoc.xip, replayed to the first frame of each of its two applications by xbe program, which the build does not run and the web export reads nothing of |
| 5659 | xodash/update.xbe, xodash/xonlinedash.xbe | name no scene archives, ignored |

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

### pkg/xbe/program.go and program_state.go

| where | value | use |
|---|---|---|
| program.go:424-454 | unnamed routine in main's section calling operator_new, allocating, constructing through an unnamed routine, calling vcall@0 then vcall@4 on the object, main first then by address, the vcall@4 call running the application | application launchers |
| program.go:563-591 | first unnamed routine mainXapiStartup calls after _cinit, the pushes since then its words | main |
| program.go:494-541 | ExQueryNonVolatileSetting and its unnamed callees to depth 3, exactly one constant HalReadSMBusValue slave | EEPROM bus address |
| program.go:662-666 | D3DDevice_Swap, D3DDevice_Present or a callee named Flip | present |
| program.go:853-860, 1219-1230 | once the call running the application returned, the first present after an operation whose target is unset or a surface of the back buffer ends a launcher | first frame on the screen |
| program.go:874-909 | no CALL IN OUT INT LOOP, no jump backwards or outside, no store by absolute address | device routine run as it is |
| program.go:919-927 | a registered header's data is the base plus the data word the header held | header binding |
| program.go:1399-1409 | heap block of at least 16 bytes found verbatim in the section loaded when it was allocated | table |
| program_state.go:230-324 | the state dispatcher stores its value at index times 4 past the shadow, hands the simple setter a word at index times 4 past the method table, calls through a pointer at index times 4 past the setter table, compares the index with exactly two bounds, the setter table ends at the first word that is no library routine, the stage dispatcher stores its value at stage times a stride plus state times 4 past the stage shadow | state tables |
| program_state.go:326-365 | a setter stores an input into its shadow at an input scaled by 4 as the index, by the row stride as the row, or at a fixed slot | setter addressing |
| program_state.go:367-407 | the two inputs stored a word apart are the method then the value | simple setter |
| program_state.go:409-474 | the input stored as a word with a constant added is the register, the input loaded or copied from is the data, the input scaling a copy count counts units, every complete path agrees | constant setter |
| program_state.go:476-494 | the furthest load through the first input sizes the definition | pixel shader definition |
| program_state.go:569-603 | a state is set when the run wrote its shadow slot | state snapshot |

### pkg/bios/hardware.go, resolve.go and xcode.go

| where | value | use |
|---|---|---|
| hardware.go:550-621 | MmQueryStatistics stores the page count from a global at offset 4 of its statistics, the routines adding into that global that none of the others calls count the pages when replayed | RAM |
| hardware.go:666-715 | the stall export multiplies a stack slot by a constant or by a global holding one, the slot its microseconds, the bytes its return removes read off its lift, the page spinning the real clock for the microseconds in the export's place | stall |
| hardware.go:3969-4030 | a routine of the executable handing the vector export a fixed level while naming by constant an address inside a device window, itself or in a routine it calls, connects that window to the level's line | device lines |
| hardware.go:667-732 | the routine writing the timer command port programs the timer, its two channel 0 bytes are the divisor, the frequency export returns a constant, the counter export reads one fixed port | timer |
| hardware.go:736-817 | a routine with string output whose replay writes a word with the initialization bit to a port programs a controller, the next words on the port above it the vector base, cascade, ICW4 and mask | interrupt controllers |
| hardware.go:820-893 | the 2BL's write of the page directory register names the directory, the large pages over the range its registers protect are the flash windows, the entry pointing at the directory itself the self map | paging and flash |
| hardware.go:897-1101 | a cpuid test is a masked compare of an answer register, a register write's value is fixed or the register's own reading masked, read in place or through a helper reading only that register | processor |
| hardware.go:1109-1125 | a block in a section of the kernel image is the kernel's, a kernel block in no routine the disassembly knows, itself or through the lifted routine holding it, is data | block stages |
| hardware.go:1243-1284, xcode.go:172-195 | memory the init table writes a constant to and reads back whole expecting it is memory under test | init table accesses |
| page_devices.go:47-89 | memory under test beyond the RAM the kernel counts is the RAM's mirror | tested memory |
| hardware.go:1331-1357 | an address below the RAM size or its mirror at 0x80000000, in the self map window or in a flash window is no device register | device addresses |
| hardware.go:1403-1810 | a branch on a device register's reading is a poll when it is its own target or exactly one target leads back to it through blocks writing no device register, a branch both targets lead back to or reading through a value the loop itself changes loops over something else, the reading and the compare are what the block's flow state fixes, a reading's address is fixed, offset from an inner value, a word of an image table indexed by an inner value or an object field the kernel's stores fix, a table indexed by a value nothing fixes spanning its leading entries whose addresses stay within the reach of the window's constant accesses, an address naming no device address inside a memory window reads no device, a reading nothing fixes the address of is answered to the spinning block on its repeated read of a register with no write between, each block counted by its first lift, the loop exits on the masked compare against a constant, an extension or an or of bits outside the mask dropped, a shift right of the reading folded into the mask and the constant, a bound nothing fixes replaced by the most its parts can hold where the loop exits above it, the executable's blocks walked the same way over the layout the kernel fixed and their polls answered alongside the kernel's | polls |
| hardware.go:1362-1532 | a port a kernel block names by constant is an access, a port it leaves open is resolved through the lift of its routine and the callers, configuration ports are read off the paths of the routines writing the address port | block accesses |
| hardware.go:1536-1988 | a routine is entered with every register its own input, a call loses every register but EBX ESI EDI EBP and the slots the callee removes, paths meeting at a block keep what their values share with the rest lost, two values cut to bits keep the union of the bits, a call block's arguments the merged state leaves lost are read along each predecessor to depth 3, a value loading memory through a fixed, input or lost value is kept, a load of a slot the block stored before it reads that store, a call through a pointer continues at its return removing nothing and forgetting every slot | block flow |
| hardware.go:2002-2026 | a call site is a jump block of the kernel or the executable with a return address to the target, the lift's call effect within 7 bytes before the return address | call sites |
| hardware.go:2031-2068 | a site's arguments are its lift's under each binding of the routine's inputs by its callers, else the block's pushes, the routine's own inputs where nothing calls it | argument tuples |
| hardware.go:2090-2174, 2188-2292 | a calling routine that reads no input is replayed whole, else each constant tuple is replayed with the buffer the call hands over, a byte of a written image unknown unless its snapshot holds a constant built from nothing the buffer carried | configuration space |
| hardware.go:2334-2364 | a routine reads no input when its lift's effects, guards and return mention none | input free |
| hardware.go:2366-2467 | a function writing bus numbers is a bridge with no window past its second register, a window's extent is its furthest access | layout |
| hardware.go:2471-2504 | an access falls in the window of the latest assignment below it, the init table's counting in table order and the later stages' all after them, an assignment a later stage replaced not counting past the init table | windows |
| hardware.go:2551-2747 | the bus ports by the input each routine writes to them, slave to the address port, command to the command port, the write's value to the data port, the port whose reading is written back the status, the port whose reading is kept the data, the remaining fixed write the control, done is the status the init table waits for, the controller is the function whose IO window holds the address port, a part is every address a call fixes or the low byte of a global, a call fixing no part is explored, a command nothing fixes is left unrecorded | bus |
| hardware.go:2780-2806 | a transaction opens at an address port write and closes at a control write, a 2 byte data write is a word | init table and 2BL transactions |
| page_devices.go:168-215 | a 2 byte write of the data port is a word transaction, a read hands over every byte the part holds at the command, a transaction asking for the interrupt raises the line whose handler reaches the status port, the challenge is drawn from the clock at boot | page bus transactions |
| hardware.go:2880-2937 | the kernel's lidt names the table, limit and base stored beside it, a gate patched by one low and one high half holds the patch | interrupt table |
| hardware.go:2941-3200 | a gate whose handler's blocks and callees reach a port or register is a line served, a connection's handler is fixed and its vector a line of a controller or the level handed to the vector export, a context field a handler reaches through is fixed by the connection's routine, its callees to depth 3 or its callers | interrupts |
| resolve.go:62-92 | a routine's lift leaves every callee opaque, a lift out of budget contributes its partial effects | lifts |
| resolve.go:169-186 | a routine takes registers in the order it reads them then stack slots, a named routine stack slots only | bindings |
| resolve.go:305-345 | a routine's inputs resolve through its callers to depth 2, the same binding from several callers kept once | callers |
| resolve.go:399-418 | a global with exactly one constant store and no other store folds to it | globals |
| xcode.go:73-168, page_devices.go:466-490 | the init table runs along every path a tested reading can take, an opcode the ROM has no operation for passed over | init table |

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
| 295 | unnamed, outside library sections | where inlined array accessors are searched |
| 309-314, 329-335 | replayed as calls with This base+off, temp registers clobbered | inlined accessors and reference count helpers |
| 4547 | a prologue push of ecx popped back by an epilogue in reverse order reserves a local | prologue |
| 395-414 | SUB ESP,imm is an argument area when the next CALL arity equals imm/4 plus pushes, scan stops at jump RET POP | frame allocation |
| 452-460 | ADD ESP,imm not a frame size, or POP ECX EDX EAX after a call, a shared add past later pushes and calls counting what they left | caller cleanup |
| 593-612 | FIST or FISTP, no CALL, RET, at most 80 instructions | ftol helper |
| 632-646 | unnamed callees in the entry point section | static object methods |
| 689-728 | ECX preferred and default receiver | static object receiver |
| 649-653 | PUSH imm of a writable address right before a CALL to a callee with RET n | object as stack argument 0 |
| 790-809 | MOV [global],reg from EAX of a constructor call within 10 instructions | global holds that class |
| 873 | vtable constant at offset 0 size 4 | learned object vtable |
| 936-943 | first dword is a known function entry | isVtable |
| 1043-1044, 1080-1081 | [reg+disp] with disp inside a section and reg a call result | table indexed by the result |
| 1138, 1327 | absolute access below the image base | null dereference, path dropped |
| 1355-1361 | fs gs es ds cs ss regions never emit stores | segment memory |
| 1560 | unnamed non library entries fold inlined creations and resources | link time inlining |
| 1604-1619 | register inputs only for unnamed non library functions, ECX excluded | register arguments |
| 1728 | nulled paths dropped | never runs through null |
| 2012-2029 | FWAIT FNCLEX WBINVD INVD are no ops, INT HLT UD2 abandon the path or end as trap | ignored instructions |
| 2045-2048 | MOV FS:[0],ESP | exception registration, pushes reset |
| 2064 | a saving PUSH ECX is not frame marked | PUSH ECX is a local |
| 2361, 2370 | XADD and CMPXCHG single threaded | no races |
| 2938, 3160-3174 | CALL [reg+disp], reg loaded from [obj+0] within 96 instructions | virtual call |
| 2949, 3530-3544 | single JMP [abs] into the thunk table, reached through jump thunks | import thunk |
| 2952-2966 | EAX sizes the frame, a probe past the entry block is an allocation that moves the stack pointer for good | stack probe |
| 2967-2972 | EAX from f2i of st0, EDX symbolic | ftol convention |
| 2993-3007 | operands on the x87 stack, EAX ECX EDX clobbered | math thunks |
| 1748-1755, 3977 | tail jumped callee takes 4 times its stack inputs | unknown arity |
| 3069-3078 | unnamed code in library sections is always a call | library helpers |
| 3234-3260 | one instruction JMP rel | jump thunk |
| 3293 | a tail jump through a pointer the image holds lands where the pointer points at load | callee cleanup |
| 3280-3465 | a body running on into code another routine owns continues there, unless a call that never returns ends it | shared tails |
| 3438 | stack dropping below the call level | epilogue |
| 3691-3707 | forgetting stops at a frame marked slot | library writes stay in the buffer |
| 3723-3754 | first non absolute pointer argument | surface description buffer |
| 3886-3900 | sub_ and fptr calls take This in ECX, vcall@ prepends ECX | call receivers |
| 3914 | stdcall of unknown arity pops every push since the last call | unknown arity |
| 3989, 3998-4004 | sub_ vcall@ fptr calls forget every stack buffer passed | opaque calls |
| 4068 | constant argument inside any section is a pointer | D3DX arguments |

### pkg/xbe lifting, flow, disassembly, construction

| where | value | use |
|---|---|---|
| lift.go:137-145 | IConst, Ptr bases empty stack g%08X import: | register values that hold across blocks |
| machine.go:67-73, 136-157 | Imm or LEA [abs] passing plausibleEntry, aligned words pointing at plausible code | dynamic leaders |
| machine.go:761 | 0x80000000 to 0xC0000000 | kernel RAM identity map, everything else is a device register |
| machine.go:764 | reg.ESP and reg.EBP bases | stack memory |
| machine.go:793 | cpu_ calls except ins fxrstor frstor fxsave fnsave, and port_out | calls that leave memory unchanged |
| machine.go:853-884 | previous instruction is a boundary or padding, padding is LEA r,[r+0], MOV r,r, XCHG r,r | plausible entry |
| machine.go:887-894 | entry point plus exports in executable sections | kernel roots |
| flow.go:13 | RET HLT INT have no successors | flow |
| flow.go:116-140 | PUSH EBP EBX ESI EDI, PUSH ECX after MOV EBP,ESP, walk over MOV LEA SUB AND XOR FLD FLD1 FLDZ FSTP, SUB or AND ESP marks the frame | prologue |
| flow.go:177-182 | PUSH of an unwritten register the function POPs | register save |
| flow.go:454-480 | kernel import pops 4 times the pushes since the last call unless cdecl | import cleanup |
| flow.go:482-496 | nearest MOV EAX,imm before the call | stack probe size |
| disasm.go:79-87, 130-157 | linear sweep of every section, undecodable bytes skipped by 1 | decode |
| disasm.go:195 | RET JMP HLT INT stop resynchronizing | resync |
| disasm.go:220-231 | PUSH EBP then any MOV | function entry |
| disasm.go:234-266, 478-500 | aligned word pointing at a decoded instruction after a boundary op, jump table targets excluded, lone JMP is a thunk | function entry |
| disasm.go:285-297, 981-995 | a routine nothing calls that only a data word, a gap or a thunk from one names must read as code throughout, ports allowed, halts and interrupt returns in the kernel alone, else its bytes are data | data decoded as code |
| disasm.go:335, 404-422 | INT NOP padding, RET INT HLT end flow, JMP to next is padding, JMP into another entry is a tail call | function body |
| disasm.go:161-230, 478-520 | scale 4, index, no base, disp nonzero, entries read outward from the base in both directions until one leaves the section or starts no instruction, the sweep decoding again from entries it passed over misaligned, a negated count selecting the entries below the base | jump table |
| disasm.go:503-509 | RET JMP INT NOP HLT | boundary ops |
| disasm.go:940-979 | AAS through XLATB, LODSB, INC and DEC of ESP, segment register moves and ADD [EAX],AL are data, ports, halts, interrupt returns and loops on ECX are hand written | instruction classes |
| disasm.go:997-1011 | a routine opens with compiled code, the kernel's alone with hand written code | data decoded as code |
| disasm.go:1013-1046 | section with signature named functions other than the entry section is library, writable non entry section with none is pure data | section roles |
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
| jscore.go:148-175 | an interrupt frame keeps the step it cut into and the flag record it left, the return through that frame continuing the step with the record restored when the handler hands back the arithmetic bits it was given, the popped word and address otherwise, nothing kept past the return | interrupt return |
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
| files.go:72 | no : % in the name | archive names |
| files.go:116-120 | %s prefix and X: drive stripped | directory preference |
| textures.go:64-79 | D3DXCreateTexture arguments at base 1, D3DDevice_CreateTexture at base 0 | produced textures |
| textures.go:80 | buffer bases buf and res | pixel stores |
| kernel.go:57-68, 81-84 | export referenced as [disp32] memory, or opening with two pointers into the image, is a variable | kernel exports |

### Other packages

| where | value | use |
|---|---|---|
| pkg/bios/boot.go:126 | Pops 4 | kernel load routine cleanup |
| pkg/bios/boot.go:231 | the one kernel string ending in a backslash and the name of an executable under the launch directory, the digits after Partition | launch partition |
| pkg/bios/boot.go:263 | an executable is a file opening with the image magic | launch directory executables |
| pkg/bios/boot.go:153-166 | PUSH imm of the kernel key right before a CALL | kernel load routine |
| pkg/bios/disk.go:17-21 | Partition, IoCreateDevice, HalDiskCachePartitionCount, fixedPartitions 2 | partition enumeration |
| pkg/bios/disk.go:260-279 | the kernel unlocks the hard disk with the HMAC SHA1 of the console's hard disk key over the identify model then serial strings read back swapped and trimmed of trailing spaces, the 20 byte digest zero padded to 32 | disk password |
| pkg/bios/disk.go:152 | device object +0x18 | DeviceExtension |
| pkg/bios/disk.go:164-196 | 9 words, sector aligned offset whose sector appears elsewhere, length aligned and different | partition extension layout |
| pkg/bios/image.go:10 | .data | section the flash copy overwrites |
| pkg/web/export.go:216 | fonts/Xbox.xtf, Xbox.xtf | file naming the font version |
| pkg/web/export.go:427 | default_default | entry scene |
| pkg/web/export.go:436-460 | archive_scene split on the last underscore, default is the main scene, inline URL is the capitalized scene plus .xap | scene naming |
| pkg/extract/extract.go:21 | Audio | auxiliary directory |
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
| pkg/xbe/disasm.go:549-929 | kernelExports, 379 ordinals, 371 named | kernel export names by ordinal |
| pkg/xbe/disasm.go:1021 | usage | library tag of functions named by shape |
| pkg/xbe/convention.go:91 | d3d8ltcg | excluded from canonical argument sizes |

### Parameter roles and conventions

| where | value | use |
|---|---|---|
| pkg/xbe/convention.go:28-47 | D3DDevice_SetTransform, SetTextureState_TexCoordIndex, SetTextureState_BorderColor, SetTextureState_ColorKeyColor, SetTextureState_BumpEnv, Swap, DrawVertices, DrawIndexedVertices, SetTexture, SetPalette, SetStreamSource, SetIndices, SetVertexShader, GetTransform, SetMaterial, SetViewport, CreateVertexShader, Direct3D_CreateDevice | declared parameter kinds, 18 entries |
| pkg/xbe/symexec.go:3209 | DbgPrint, RtlSprintf, RtlSnprintf, RtlVsprintf, RtlVsnprintf | kernel cdecl exports |
| pkg/xbe/translate.go:24 | RtlEnterCriticalSection, RtlEnterCriticalSectionAndRegion, RtlLeaveCriticalSection, RtlLeaveCriticalSectionAndRegion, KeEnterCriticalRegion, KeLeaveCriticalRegion, KeRaiseIrqlToDpcLevel, KeRaiseIrqlToSynchLevel, KfRaiseIrql, KfLowerIrql | answered at once in a single threaded replay, popping what the kernel pops |
| pkg/xbe/symexec.go:3710 | D3DDevice_CreateVertexShader 2, CreatePixelShader 1, CreateVertexBuffer 4, D3DVertexBuffer_Lock 3, D3DXCreateMatrixStack 1 | result through a pointer argument |
| pkg/xbe/symexec.go:3714 | D3DXCreateTexture 7 | result through a pointer argument in image creations |
| pkg/xbe/symexec.go:3720 | D3DXCreateTexture width 1 height 2 format 5, D3DDevice_CreateTexture 0 1 4 | texture creation arguments |
| pkg/xbe/symexec.go:3725, pkg/xbe/derive.go:50 | PixelJar::Get2DSurfaceDesc, D3DSurface_GetDesc, D3DTexture_GetLevelDesc | surface describers |
| pkg/xbe/symexec.go:3757 | ?0D3DXCOLOR | library body run inline |
| pkg/xbe/symexec.go:3760-3773 | D3DX and XG prefixes stripped, Matrix 64, Vec4 Quaternion Plane Color 16, Vec3 12, Vec2 8 | D3DX buffer sizes, Vec3 also applied to D3DXVec3Transform whose output is 16 bytes |
| pkg/xbe/symexec.go:4036-4055 | apiConstantBias 96, D3DDevice_SetVertexShaderConstant prefix | constant register numbering |
| pkg/xbe/symexec.go:4152 | D3DXMatrixDeterminant, D3DXVec3Dot, D3DXVec3Length, D3DXVec3LengthSq, D3DXVec4Dot, D3DXQuaternionDot, D3DXVec2Dot | scalar results, nothing written through arg0 |
| pkg/xbe/symexec.go:4161-4184 | D3DXVec3TransformNormal D3DXVec3TransformCoord D3DXVec3Transform arg2 64, D3DXMatrixInverse arg1 4, D3DXMatrixAffineTransformation arg2 arg4 12 arg3 16, D3DXMatrixRotationAxis D3DXQuaternionRotationAxis arg1 12, D3DXMatrixRotationQuaternion arg1 16 | argument buffer sizes |
| pkg/xbe/symexec.go:2992-3007 | libcmt plus mathIntrinsics | CRT math reads doubles at [esp] and [esp+8] |
| pkg/xbe/symexec.go:2585-2606 | D3DDevice_SetTransform, D3DDevice_GetTransform | rep movs over the transform array |
| pkg/xbe/symexec.go:3858-3861 | ECX, EDX, 8 bytes | fastcall |
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
| pkg/xbe/program.go:31-38 | mainXapiStartup, _cinit, operator_new, vcall@0 vcall@4, Flip | startup and launcher names |
| pkg/xbe/program.go:42-53 | XGSetTextureHeader args 0 width 1 height 2 levels 3 usage 4 format 6 texture 7 data 8 pitch, D3DResource_Register args 0 resource 1 base | header textures |
| pkg/xbe/program.go:56-65 | D3DDevice_SetTexture args 0 stage 1 texture, D3DDevice_SetRenderTarget arg 0, D3DDevice_SetPixelShader arg 0, D3DDevice_CreatePixelShader args 0 definition 1 out, D3DDevice_Clear args 0 count 1 rects of 4 words 2 flags 3 color 4 z 5 stencil | frame routines |
| pkg/xbe/program.go:68-72 | ExQueryNonVolatileSetting, HalReadSMBusValue, HalWriteSMBusValue | settings and bus |
| pkg/xbe/program.go:76 | D3D | device library section |
| pkg/xbe/program.go:80-96 | RtlAllocateHeap arg 2 size, RtlReAllocateHeap args 2 block 3 size, RtlSizeHeap arg 2 block, RtlFreeHeap 1, GetTickCount advancing 16, XInitDevices XGetDevices XGetDeviceChanges XInputOpen XInputClose XInputGetState XInputPoll XInputSetState 0, XLaunchNewImage XapiBootToDash end the run | XAPI hooks |
| pkg/xbe/program.go:100-124 | XeLoadSection XeUnloadSection arg 0 handle, MmAllocateContiguousMemory MmAllocateContiguousMemoryEx ExAllocatePoolWithTag ExAllocatePool arg 0 size, NtAllocateVirtualMemory args 0 base 2 size, ExQueryPoolBlockSize MmQueryAllocationSize arg 0, KeQueryPerformanceCounter advancing 4096 into EDX EAX, HalReadSMBusValue args 0 slave 1 register 2 word 3 out, HalWriteSMBusValue refused, NtCreateFile NtOpenFile not found, HalReturnToFirmware ends the run, the rest 0 | kernel hooks |
| pkg/xbe/program.go:954-1184 | Direct3DCreate8, Direct3D_CreateDevice args 4 presentation 5 out, D3DDevice_GetDisplayMode arg 0, GetBackBuffer2 GetDepthStencilSurface2 GetRenderTarget2, GetBackBuffer GetDepthStencilSurface GetRenderTarget last arg out, CreateTexture2 args 0 width 1 height 2 depth 3 levels 4 usage 5 format 6 type, CreateTexture args 0 1 2 3 4 and 6 out, CreateCubeTexture args 0 size 1 levels 2 usage 3 format 5 out, CreateVertexBuffer2 CreateIndexBuffer2 arg 0 size, CreateVertexBuffer CreateIndexBuffer args 0 size 4 out, Lock2 arg 0, Lock args 0 1 offset 3 out, D3DTexture_LockRect args 0 1 level 2 rect, D3DCubeTexture_LockRect and D3DDevice::AddRef/24 args 0 1 face 2 level 3 rect, D3DSurface_LockRect args 0 1, D3DTexture_GetLevelDesc and D3DDevice::AddRef/12 args 0 1 2, D3DSurface_GetDesc args 0 1, GetSurfaceLevel2 args 0 1, GetSurfaceLevel args 0 1 2 out, GetCubeMapSurface2 args 0 1 face 2 level, D3DResource_AddRef 2, D3DResource_Release and D3DDevice::AddRef/0 1, D3D_AllocContiguousMemory args 0 size 1 align, CreateVertexShader args 0 declaration 1 program 2 out, SetVertexShader arg 0, SetStreamSource args 1 2, SetIndices arg 0, DrawVertices args 0 1 2, DrawIndexedVertices args 0 1 2 | device hooks |
| pkg/xbe/program_state.go:12-22 | D3DDevice_SetRenderStateNotInline args 0 index 1 value, D3DDevice_SetRenderState_Simple, D3DDevice_SetRenderState_ prefix, D3DDevice_SetTextureStageStateNotInline args 0 stage 1 state 2 value, D3DDevice_SetTextureState_ prefix, D3DDevice_SetVertexShaderConstant prefix, 16 bytes a register, D3D8 | state setters |
| pkg/xbe/program_state.go:445 | ECX EDX then 3 stack words | constant setter inputs |
| pkg/bios/hardware.go:22-33 | KeStallExecutionProcessor, KeQueryPerformanceCounter, KeQueryPerformanceFrequency, KeTimeIncrement, MmQueryStatistics with the page count at offset 4, HalReadSMBusValue args 0 slave 1 command 2 word 3 out, HalWriteSMBusValue args 0 slave 1 command 2 word 3 value, KeInitializeInterrupt args 1 handler 2 context 3 vector, HalGetInterruptVector arg 0 level, HalReadWritePCISpace args 0 bus 1 slot 2 register 3 buffer 4 length 5 write | kernel routines the console derivation reads |
| pkg/bios/hardware.go:316 | HalReadWritePCISpace fills its buffer with the length it is handed on a read | buffer the lift forgets |

### Buffers library routines write

| where | value | use |
|---|---|---|
| pkg/xbe/symexec.go:3631, 3637 | wideBuffer 0x1000, itowBuffer 66 | default write extents |
| pkg/xbe/symexec.go:3641-3651 | ReadFile arg1 count arg2 else 1<<20 and 4 bytes through arg3, GetFileSize 4 through arg1, SetFilePointer 4 through arg2 | outBuffers |
| pkg/xbe/symexec.go:3641-3651 | FindFirstFileA 320 through arg1, FindNextFileA 320 through arg1, GetLocalTime 16, GetSystemTime 16 | outBuffers |
| pkg/xbe/symexec.go:3641-3651 | QueryPerformanceCounter 8, QueryPerformanceFrequency 8, XGetLaunchInfo 4 through arg0 and 3072 through arg1 | outBuffers |
| pkg/xbe/symexec.go:3641-3651 | XcSHAInit 116, XcSHAUpdate 116, XcSHAFinal 116 and 20 through arg1, XcHMAC 20 through arg6 | outBuffers |
| pkg/xbe/symexec.go:3641-3651 | XQueryValue 4 through arg1, arg3 bytes through arg2 else 256, 4 through arg4 | outBuffers |
| pkg/xbe/symexec.go:3641-3651 | wcscpy wcscat swprintf _wcslwr _wcsupr lstrcpyW lstrcatW 0x1000, wcsncpy wcsncat 2 times arg2, _snwprintf 2 times arg1, _itow _ltow 66 through arg1 | outBuffers |
| pkg/xbe/symexec.go:3641-3651 | memcpy memset arg2 else 1<<20, XNetRandom arg1 else 1<<16 | outBuffers |

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
| pkg/xbe/symexec.go:3716-3719, pkg/xbe/scene.go:700 | format 0, width 0x14, height 0x18 | D3DSURFACE_DESC |
| pkg/xbe/device.go:181, pkg/xbe/program.go:395, pkg/xbe/program_state.go:555, 585 | 4 | texture stages |
| pkg/xbe/derive.go:370 | position blendweight normal diffuse specular fog pointsize backdiffuse backspecular texcoord0 to texcoord3 | fixed function registers |
| pkg/scene/layout.go:76-87 | register 0 position 0x32, 2 normal 0x32, 3 diffuse 0x40, 4 specular 0x40, 9 plus i texcoord 0x22 | FVF expansion |

## SPEC, hardware and file format facts

### Console hardware, pkg/bios/hardware.go and xcode.go

| where | value |
|---|---|
| pkg/bios/page_spec.go, kelvinMethods, kelvinUnnamed | the 3D class method numbers and the method kept without a name, from the nouveau and xemu register definitions, 8 lights of 0x80 bytes and back lights of 0x40, 6 specular curve words, 8 point parameters, 4 skinning matrices, texgen planes of 0x40 bytes a stage, 6 cube map faces aligned to 128 bytes |
| pkg/bios/page_shaders.go | vertex program subtoken fields, operations, output registers, outputs in screen space brought back to clip space, the fixed function pipeline as the xemu generator writes it with skinning modes 0 to 6, texgen modes 0x2400 0x2401 0x2402 0x8511 0x8512, light types off infinite local spot, the light context word layout, colour material sources, the rational specular and spot falloff curves, specular switches, fog modes 0x2601 and 0x800 to 0x804 with generation modes 0 to 3 and 6, point parameter sizing, combiner input and output control word fields, final combiner words, texture stage programs 0 to 0x11 with the xemu texture shader semantics, shadow compare functions 0 to 7, depth texture scales 65535 16777215 and 511.9375, the gaussian 3x3 kernel, dot mappings 0 to 4, colour key masks by format |
| pkg/bios/page_graphics.go | vertex array types and sizes, program and constant loading, immediate vertex methods, surface format fields, texture format fields and colour codes with their decoders, depth texture codes 0x2C to 0x31, filter and address codes, convolution filter 7 with its kernel bits, texture control bits for colour keys alpha kill and enable, palette context bit and offset mask, cube map face order, fog colour byte order, light control bits, colour material bits, light mask pairs, skin and execution mode bits, depth clamp bits, clear flag bits, the Z16 clear value in the low 16 bits and the Z24S8 depth above its stencil byte, primitive codes, blend, compare, stencil, cull and face values as OpenGL enumerants, the display start register |
| hardware.go:40-64 | page 4096, large page bit 0x80 at 22 bits, MTRR base 0x200 mask 0x201 type mask 0xFF write protect 5 valid 0x800, timer command port 0x43 channel 0 port 0x40, controller initialization bit 0x10 single bit 0x02 ICW4 bit 0x01, configuration address port 0xCF8 data port 0xCFC enable bit 31, base address registers 0x10 to 0x24, bridge buses 0x18, IO bit 0x1, 256 byte configuration space, 10000000 hundred nanoseconds per second, gate present bit 0x8000 |
| hardware.go:1220 | configuration address bus at bits 16 to 23, device 11 to 15, function 8 to 10, register 0xFC |
| hardware.go:1233-1236 | a memory base address register keeps the bits above 0xF, an IO one above 0x3 |
| hardware.go:1345 | the kernel's RAM mirror at 0x80000000 |
| hardware.go:2386 | the secondary bus in bits 8 to 15 of the bus register |
| hardware.go:2607, 2673 | a bus controller's window is 0x100 ports |
| hardware.go:2924, 2948 | 8 lines per controller, 8 byte gates with the handler halves at 0 and 6 |
| hardware.go:1544 | EBX ESI EDI EBP kept across a call |
| xcode.go:9-20, boot.go:16-26 | x-codes peek 02 poke 03 PCI write 04 PCI read 05 and or 06 chain 07 jne 08 jump 09 out 11 in 12 of 9 bytes, exit 0xEE |
| page_spec.go:4-13 | controller end of interrupt 0x20, specific 0x60, command mask 0xE0, read requests 0x0A, read service 0x0B, operation bit 8, automatic end of interrupt bit 2, 8 lines, the lower command port the master |
| page_spec.go:16-24 | timer channel in bits 6 to 7, access in bits 4 to 5 as low 1 high 2 both 3 and latch 0, ports to 0x43 |
| page_spec.go:26-30 | bus status 0x04 after a refused transaction, control bit 0x10 asking for the interrupt |
| page_spec.go:32-47 | system controller version 1, power 2, AV pack 4, CPU temperature 9, board temperature 0xA, interrupt reason 0x11, scratch 0x1B, challenge 0x1C of 4 bytes, tray 0x40 closed with no disc, power bits reset 1 cycle 0x40 off 0x80 |
| page_spec.go:52-72 | disk ports 0x1F0 to 0x1F7 and 0x3F6, status busy 0x80 ready 0x40 seek 0x10 data 8 error 1, abort 4, slave 0x10, control no interrupt 2 reset 4, 512 byte sectors |
| page_spec.go:75-99 | disk commands identify 0xEC packet identify 0xA1 read DMA 0xC8 write DMA 0xCA read 0x20 write 0x30 read multiple 0xC4 write multiple 0xC5 set multiple 0xC6 set features 0xEF flush 0xE7 standby 0xE0 packet 0xA0 security freeze 0xF5 unlock 0xF2 disable 0xF6 set password 0xF1 reset 8 recalibrate 0x10 initialize device parameters 0x91, packet signature 0x14 0xEB |
| page_spec.go:102-117 | security feature bit 2 of identify words 82 and 85, security status word 128 with supported 1 enabled 2 locked 4 frozen 8 count expired 0x10, 32 byte password at byte 2 of the 512 byte block, 5 unlock tries, 20 byte digest |
| page_spec.go:120-131 | bus master command 0 status 2 table 4, start 1, to memory 8, active 1, failed 2, interrupt 4, table end bit 31, 65536 byte spans |
| page_devices.go:282, 265-278, 306, 305, 366 | the bus master's status latches every interrupt a drive raises, a locked disk aborts the commands reaching its media and the freeze lock, unlock aborts while frozen or disabled or after the tries expire and clears the lock on the password, set password and disable abort while locked or frozen, a security command takes its 512 byte block through the data port |
| page_devices.go:675 | the front end's trap latches into the status until the kernel writes the bit |
| page_spec.go:134-155 | 12 byte packets, test unit ready 0 request sense 3 inquiry 0x12 start stop 0x1B prevent allow 0x1E read capacity 0x25 read 0x28 read TOC 0x43 event status 0x4A mode sense 0x5A read CD 0xBE, sense not ready 2 no medium 0x3A in 18 bytes, interrupt bit 2 input bit 1, sense key at bit 4 of the error, feature bit 1 moving the packet's data by the bus master |
| page_spec.go:158-178 | clock ports 0x70 0x71, seconds 0 minutes 2 hours 4 weekday 6 day 7 month 8 year 9 status A to D century 0x7F, updating 0x80, 24 hour 2, binary 4, power good 0x80, afternoon 0x80 |
| page_spec.go:181-186 | ROM at 0xFFFFFE00 of 0x200 bytes, byte wide init table ports |
| page_spec.go:189-214 | FIFO user windows at 0x800000, 32 channels of 0x10000 bytes with put 0x40 get 0x44 reference 0x48, mode 0x2504, push 0x3200 0x3204, status 0x3214 with empty 0x10, DMA push 0x3220 instance 0x322C put 0x3240 get 0x3244 reference 0x3248, pull 0x3250, access bit 1, channel mask 0x1F, instance mask 0xFFFF shifted 4, instance memory at 0x700000 of 0x100000 bytes |
| page_spec.go:217-243 | memory object class mask 0xFFF with classes 0x3D 0x02 0x03, adjust at bit 20, address mask 0xFFFFF000, old jump 0x20000000 under 0xE0000003 with target 0x1FFFFFFC, flow bits 3 jump 1 call 2 target mask 0xFFFFFFFC, return 0x00020000, method mask 0xE0030003 increasing 0 non increasing 0x40000000, count at bit 18 of 11 bits, subchannel at bit 13 of 3 bits, method address 0x1FFC, 8 subchannels, reference method 0x50 |
| page_spec.go:246-257 | master interrupt status 0x100 with software bit 31, enable 0x40 past each status, runout status 0x2400, engine statuses 0x1100 bit 28, 0x2100 bit 8, 0x400100 bit 12, 0x600100 bit 24, 0x8100 bit 16, 0x9100 bit 20 |
| page_spec.go:259-284 | hash table register 0x2210 with base bits 0x1F0 shifted 4 then 12 and size at bit 16 of 2 bits plus 12, 8 byte entries with instance mask 0xFFFF, engine at bit 16 of 2 bits graphics 1, status bit 31, object class mask 0xFF, handle methods 0x180 to 0x1FC, 3D class 0x97 with semaphore memory 0x1A4 offset 0x1D6C write 0x1D70 and depth clear value 0x1D8C landing in register 0x401A88, pattern class 0x44 whose colour method 0x310 lands in register 0x400B10 |
| page_spec.go:284-296 | video refresh flags 0x00400000 for 60000/1001 Hz and 0x00800000 for 50 Hz, display status 0x600100 with vertical blank bit 1 |
| page_spec.go:299-326 | audio status 0x1000 and enable 0x1004 with global bit 1 and trap bit 0x10, front control 0x1100 with method mode 0xE0 trapped 0xE0 and trap reason 0xF00 requested 0xF00, current voice 0x1110, antecedent 0x1118 with handle 0xFFFF and list at bit 16 of 2 bits, trapped method 0x1300 and parameter 0x1304, trap force 0x1504 with idle 0x8000, voice table 0x202C, idle voice method 0x8000, lists 0x2054 0x2060 0x206C with current and next 4 and 8 past each top |
| page_spec.go:328-353 | 256 voices of 0x80 bytes with state 0x54 holding active bit 21 and paused bit 18, link 0x7C masked 0xFFFF, no voice 0xFFFF, methods at 0x20000 of 0x1000 bytes with antecedent 0x120 on 0x124 off 0x128 release 0x12C pause 0x140 with action bit 18 current voice 0x2F8 lock 0x2FC and voice words 0x300 to 0x37C, handle mask 0xFFFF, 32 sample frames at 48000 Hz |
| page_spec.go:356-371 | envelope counts 0x34 with the amplitude count in the low word and the filter count above, phases at bits 28 and 24 of the state with release 6 and forced release 7, release rates in words 0x6C and 0x18 masked 0xFFF, 16 frames per rate step |
| page_spec.go:374-386 | graphics engine interrupt 0x400100 with error 0x100000, notify source 0x400108 with notification 1, trapped address 0x400704 with channel at bit 20 and subchannel at bit 16, trapped data 0x400708, no operation method 0x100 |
| page_spec.go:389-405 | surface 0x400710 with write at bit 20, read at bit 24 and modulo at bit 28 of 3 bits each, increment 0x40071C with read bit 2, 3D flip methods read 0x120 write 0x124 modulo 0x128 increment 0x12C stall 0x130 |
| page_devices.go:5-8 | disk controller at bus 0 device 9 function 0, its bus master window at register 0x20 |
| page_devices.go:16-17 | IO windows of 0x100 ports, memory windows of 16 MB at most |
| page_devices.go:344 | inquiry of a removable CD-ROM in 36 bytes |
| page_devices.go:386-432 | the clock keeps universal time, a time written into it moves it |
| page_spec.go:494-526, page_nic.go | the network controller at bus 0 device 4 function 0 through register 0x10, its management status 0x180 control 0x190 data 0x194 with in use 0x8000 write 0x400 register bits 0x1F and address at bit 5 as the forcedeth driver documents, the PHY at address 1 and 0xFFFF from any other, control register 0 with self clearing reset 0x8000, status register 1 reading 0x7809 as a PHY able to autonegotiate every speed with no link partner, 32 registers, a transaction completing as the control register is written |
| page_spec.go:528-590, page_usb.go | the host controller at bus 0 device 2 function 0 through register 0x10, the OpenHCI registers revision 0 control 4 command status 8 interrupt status 0xC enable 0x10 disable 0x14 HCCA 0x18 period current 0x1C control head 0x20 control current 0x24 bulk head 0x28 bulk current 0x2C done head 0x30 frame interval 0x34 remaining 0x38 number 0x3C periodic start 0x40 LS threshold 0x44 root hub descriptor A 0x48 B 0x4C status 0x50 and port statuses from 0x54, revision 0x10, 0x64 bytes of registers, periodic 4 isochronous 8 control 0x10 bulk 0x20 enables, state at bits 6 to 7 with operational 2, reset 1 control filled 2 bulk filled 4 commands, overrun 1 done head 2 frame 4 overflow 0x20 root hub 0x40 master bit 31 interrupts, frame interval 0x3FFF resetting to 0x2EDF with the remaining toggle at bit 31, 16 bit frame numbers overflowing at 0x8000, HCCA mask 0xFFFFFF00 and pointer mask 0xFFFFFFF0 |
| page_spec.go:592-658 | 4 root hub ports with descriptor A resetting to 0x01001204 and its 0xFF001F00 written, remote wakeup 0x8000 set by 0x8000 and cleared by bit 31, port connected 1 enabled 2 suspended 4 clear suspend 8 reset 0x10 powered 0x100 low speed 0x200 with changes at bits 16 to 20, a port reset taking 10 ms, 32 interrupt slots, frame number 0x80 and done head 0x84 of the communications area, endpoint function mask 0x7F endpoint at bit 7 direction at bit 11 skip 0x4000 isochronous 0x8000 packet size at bit 16, tail 4 head 8 next 12 with halted 1 and toggle carry 2, transfer rounding 0x40000 direction at bit 19 setup 0 out 1 in 2 toggle at bit 24 from the descriptor when bit 25 error count at bit 26 condition at bit 28, current buffer 4 next 8 end 12 across 4096 byte pages, conditions no error 0 stall 4 not responding 5 underrun 9, 1 ms frames |
| page_usb.go:23-358 | a descriptor moved whole between its buffer and the device a packet at a time, a short packet ending it with the current pointer at the next byte and underrun halting the endpoint unless rounding, a not responding device halting it, a stall halting it, the done queue written to the communications area when the done head interrupt is clear with bit 0 set while other interrupts pend, the frame number written each frame, the control and bulk filled bits cleared when their lists hold no descriptor, a zero length packet closing a data stage that fills its last packet short of the request, the address taking at the status stage, every request the device has no answer for stalling |
| page_spec.go:660-726 | standard requests get status 0 clear feature 1 set feature 3 set address 5 get descriptor 6 get configuration 8 set configuration 9 get interface 10 set interface 11, direction bit 0x80, type bits 0x60 standard 0 class 0x20 vendor 0x40, recipient bits 0x1F with other 3, device 1 and configuration 2 descriptors, 8 byte setup packets, the gamepad's device, configuration and XID descriptors with XID type 0x42, get report 1 set report 9 get capabilities 1, input 0x100 and output 0x200 reports of 20 and 6 bytes on endpoint 2, buttons at 2 analog at 4 thumbsticks at 12, up 1 down 2 left 4 right 8 start 0x10 back 0x20 thumbs 0x40 0x80, A B X Y black white triggers in that order, 255 and 32767 full scale |
| page_spec.go:728-764 | controller port 3 of the root hub or the daughterboard's hub, the daughterboard on root hub port 1 of a console whose southbridge revision marks one, the hub's device, configuration and class descriptors, 4 ports, class descriptor type 0x29, hub recipient 0x20 and port recipient 0x23, change endpoint 1, port features enable 1 suspend 2 reset 4 power 8 and changes from 16, port status connected 1 enabled 2 suspended 4 reset 0x10 powered 0x100 with changes alike, a hub port reset taking 10 ms |
| page_spec.go:782-798, page.go:393-395, page_devices.go:141, 220, 639 | the southbridge at bus 0 device 1 function 0 with its revision in configuration register 8, revision 0xB2 version P01 and a daughterboard for MCPX ROM 1.0 and 0xD4 version P05 without one for MCPX ROM 1.1, the configuration address enable at bit 31 bus at 16 device at 11 function at 8, the graphics processor's boot register 0 reading 0x02A000A2 |

### Boot ROM and flash, pkg/bios

| where | value |
|---|---|
| bios.go:15-26 | bank 0x40000, MCPX 0x200, 2BL 0x6000 at 0x90000, preldr block 0x2A00 running at 0xFFFFD400, params 0x80, nonce 0x10, key 16, boot params 36, signature 0x7854794A |
| bios.go:56-57 | MCPX 1.0 sha1 5d270675b54eb8071b480e42d22a3015ac211cef key at 0x1A5, MCPX 1.1 sha1 6c875f17f773aaec51eb434068bb6c657c4343c0 key at 0x19C |
| bios.go:117, 145, 158-166 | 0xE9 marks a preldr, key xor 0x5C, entry record at block minus 8, first 16 bytes zeroed and the entry written |
| bios.go:190-201 | entry low 16 bits, keys pointer at entry minus 8, eeprom cert kernel keys in order |
| bios.go:213-218 | all zero or all 0xFF kernel key means a clear kernel |
| boot.go:16-26 | 2BL load 0x90000, run 0x400000, stack 0x8F000, GDT 0xFFFFFFD8 limit 0x18, x codes at 0x80 of 9 bytes ending at op 0xEE, CS 8, DS 0x10, CR0 0x11, kernel at 0x10000 |
| boot.go:129-132 | EBP and ESP, segments CS DS ES SS FS GS, GDT, CR0 at 2BL entry |
| boot.go:99 | section flags 0x5 |
| disk.go:22, 109-112 | sector 512, counted string length, length plus 1, pointer |
| disk.go:281-347 | ATA and ATAPI identify words, 16 heads, 63 sectors, cylinders capped 16383, LBA capped 0x0FFFFFFF, cycle times 120, feature words 0x001E 0x0017 0x4020 0x4000 0x0020 0x4000 0x0407 0x6001, security enabled and locked at power on, checksum word 0xA5A5 |
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
| xbe.go:94-108, symexec.go:578-581 | flags 0x1 writable, 0x4 executable, bytes past raw size are not constant |
| disasm.go:113-127 | kernel thunk table 0 terminated, ordinal & 0x7FFFFFFF |
| derive.go:609-612, resource.go:24, 243 | push header count at bit 18, method & 0x1FFF, token floor 1<<18, bits above 13 match |
| resource.go:151, library.go:286 | alias bit 0x80000000 |
| vsh.go:376-396 | D3DVSD end 0xFFFFFFFF, type tok>>29, stream tok&0xF, skip bit 28 with its word count at (tok>>16)&0xF, data type (tok>>16)&0xFF, register tok&0x1F |
| x86.go:4-68 | flags word carry 1 reserved 2 parity 4 zero 0x40 sign 0x80 trap 0x100 interrupt 0x200 direction 0x400 overflow 0x800, CR0 task switched bit 3 write protect bit 16 paging bit 31, CR4 page size extensions bit 4, page entry present 1 writable 2 accessed 0x20 dirty 0x40 large 0x80, large page shift 22, 1024 slots, 8 byte descriptors and gates, selector index mask 0xFFF8, gate type mask 0x1F00 interrupt 0xE00 trap 0xF00 present 0x8000, x87 save area control 0 status 4 tag 8 registers at 28 of 10 bytes, extended save area control 0 status 2 tag 4 registers at 32 lanes at 160 in 16 byte slots of 512, extended precision bias 16383 mantissa 63 bits, status word C0 0x100 C2 0x400 C3 0x4000 |
| jsdefs.go:95-98 | the overflow flag is undefined after the operations the executor has no overflow rule for, so the flags word carries 0 there |
| x86.go:17-22 | device not available vector 7, monitor coprocessor bit 1 |
| symexec.go:2550-2563, jsgen.go:220-230 | a block whose first float, vector or state save instruction comes before anything in it clears the task switched bit traps at entry while the bit is set, a wait only with the monitor bit too |
| jscore.go:137-142, 241 | descriptor base from bits 16 to 31 of the first word and 0 to 7 and 24 to 31 of the second, gate offset from the halves of both words |
| patterns.go:96-242, 289-294 | IDA pat syntax, --- terminator, 32 byte prefix, .. wildcard, ^ references, : names |
| patterns.go:256-269 | FLIRT CRC16 init 0xFFFF poly 0x8408 |
| patterns.go:427, 437-492 | rel32 and abs32 references, MSVC decoration letters for cdecl, fastcall @, stdcall @N |
| symexec.go:2985-2987, 3003-3005, 3334-3337, 3561-3578, construct.go:29, 92, 289 | RET n cleanup, EAX ECX EDX caller saved, EBP EBX ESI EDI callee saved, XMM0 to XMM7, result in EAX |
| symexec.go:1062, 4520-4523, lift.go:120, system_insn.go:10-12 | register and segment indices, CMOV to Jcc |
| flow.go:347-351, decompile.go:175-179 | stack arguments at disp 4 and above, RET imm/4 |
| machine.go:99-104, 722, system_insn.go:121-126 | HLT INT STI POPFD end a block, UD2 is INT 6 |
| system.go:444 | reverse push order |
| program.go:127 | STATUS_OBJECT_NAME_NOT_FOUND 0xC0000034 |
| program.go:131-139 | resource types vertex buffer 0, index buffer 1, texture 4, surface 5 at bit 16, D3DRTYPE texture 3 cube 5 |
| program.go:142-174 | format register 2D 0x20, cube bit 2, format bits 8, mips 16, log2 width 20, log2 height 24, 6 faces, D3DSURFACE_DESC type 4 usage 8 size 0xC, D3DLOCKED_RECT pitch 0 bits 4, presentation parameters width 0 height 4 format 8 flags 0x28 of 0x34 bytes, display mode width 0 height 4 refresh 8 flags 0xC format 0x10, section header name at 0x14, resource data at 4, declaration end 0xFFFFFFFF |

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
| pkg/xbe/symexec.go:36, 49 | this buffer index -1, 1024 stack buffer elements |
| pkg/xbe/symexec.go:171-177 | 60000 steps, 512 paths, 4000 inline size, 2500 frame steps, 4 loop unrolls, 1<<16 rep unroll, 1<<16 rep compare |
| pkg/xbe/symexec.go:391, 593-612, 737-839, 909, 1150, 1158, 1268, 1686, 1696, 3160, 3366, 3914, 3995 | alloc scan 40, ftol 80 instructions, 8 static this rounds, look back 10, 6 learn retries, overlap 63, split above 8 bytes, reassembly 64, frame budget 1<<30, trace every 5000 or first 400, vtable scan 96, demand scan 400, 8 stack words, pointee extent 1<<30 |
| pkg/xbe/symexec.go:3641-3651 | 1<<20 and 1<<16 fallback write sizes |
| pkg/xbe/expr.go:308, 769, 951 | decimal below 1024, 512 node compare budget, 1<<21 path nodes |
| pkg/xbe/lift.go:114, 117, 123, 327 | 20000 block steps, 4096 blocks, entry SP 0x10000, re lift 4 times blocks |
| pkg/xbe/scene.go:111, 114, 442-447, 456, 1392-1393, 1407, 1523, 1673 | callee depth 2, 262144 path effects, 4096 or 256 paths, 4 stack arguments, 256 paths for the join, 0 retries, look back 5 |
| pkg/xbe/lookup.go:167, 257, pkg/xbe/device.go:424 | look back 24 and 6, row count 4 |
| pkg/xbe/derive.go:362, 494-504, 604, 782, 963-965 | store width 4, 30000 steps, 128 paths, 3 seeded arguments, 256 table entries, look back 7 |
| pkg/xbe/draw.go:256, 368, 484, 676-677, 1182, 1391 | 90 percent, 64 slots, 3 and 2 seeded arguments, look back 3, 4096 paths for a walker |
| pkg/xbe/material.go:203, 236, 348-352, 355 | 3 sites, 8 objects, highest VA constructor, sort by name |
| pkg/xbe/resource.go:27, 167, 200 | push window 8, 4 inputs, shift below 24 |
| pkg/xbe/inlined.go:213, 362 | default program 0, look back 7 |
| pkg/xbe/system.go:498, pkg/xbe/translate.go:81 | 2 seeded arguments, 64 byte names |
| pkg/xbe/textures.go:27-28 | 50000000 steps, no frame limit |
| pkg/xbe/patterns.go:40, 91, 328, 336, 317-371, 410-411 | 8 weak bytes, 1<<20 and 1<<24 line buffers, 288 bytes read, distance sentinel 1<<30, nearest version, lexical tie break, weak patterns need references |
| pkg/xbe/vsh.go:296, 371 | ARL epsilon 0.001, 64 declaration tokens |
| pkg/xbe/disasm.go:465 | 512 jump table entries |
| pkg/xbe/construct.go:296 | 96 construction steps |
| pkg/xbe/decompile.go:521, 525 | decimal below 256, strings between 3 and 199 characters |
| pkg/bios/disk.go:136-137 | device object 0x7FFE0000, extension 0x7FFE1000 |
| pkg/font/xtf.go:100 | 100000 ranges |
| pkg/xbe/translate.go:14-17, 21 | replay stack 0x7FFF0000, return 0x7FFD0000, buffer 0x7FFC0000, processor block 0x7FFA0000 of 0x1000 bytes |
| pkg/bios/resolve.go:14-20, 223 | resolve depth 2, split depth 3, 64 open forks, 4 retries, 16 folding passes |
| pkg/xbe/budgets.go:4-13 | 20000 steps, 64 paths, 20000000 explored blocks, 4 library retries, 6 method retries, 8 draw retries, 8 thunk hops, 8 opening instructions |
| pkg/bios/hardware.go:459, 2013, 2569, 3050 | counter step 1<<20 per read, call effect within 7 bytes of the return address, port helper depth 2, context field depth 3 |
| pkg/extract/extract.go:17, pkg/cli/xbe.go:186 | 256 KB buffers |
| pkg/xbe/program.go:19-27, 168-173, 395, pkg/xbe/program_state.go:585 | arena 0x40000000, heap align 16, page align 4096, object 0x20 bytes, first handle 0x10000, 4 lift tries, helper depth 3, 60 Hz, tick step 16, counter step 4096, 4 texture stages |
| pkg/xbe/jscore.go:12, 177, 186, 193-194, 214-215 | 4 KB translation entries, 64 call trail, hardware advanced every 1024 dispatches, 24 stack words of 256, 48 character strings |
| pkg/bios/page_devices.go:12, 21, 126 | 1<<24 init table steps, 1<<16 reads of an unmodeled register before the page stops, 4 timer ticks caught up per advance |
| pkg/bios/page_usb.go:10-15 | 1024 endpoint descriptors a list may hold, 8 transfer descriptors an endpoint moves a frame, 8 frames caught up per advance |
| pkg/bios/page_graphics.go:343 | 4096 vertex buffers kept before they are all dropped, WebGL errors checked every 64 frames |
| pkg/bios/page.go:141, 206-250 | 4 lift retries, the candidates tried for a processor identification answer and a polled register's answer |
| pkg/bios/hardware.go:1379-1381 | 256 blocks walked from a branch's target before it counts as not leading back, poll field depth 3, 64 table entries |
| pkg/web/runtime.go:25-28 | 8192 blocks a script, 65536 blocks between looks at the clock, 12 ms of each frame run, status every 5000 ms |
| pkg/bios/resolve.go:219 | 1048576 folded nodes remembered before the folds forget them all |
| pkg/web/headless.go:16, 56 | 400000000 blocks before a headless run counts as running, 4096 KB node stack |

### Environment switches

| where | value |
|---|---|
| pkg/xbe/symexec.go:163-167 | DISCOXIP_TRACE, DISCOXIP_NO_VRESOLVE, DISCOXIP_NO_KEEP |
| pkg/xbe/scene.go:108, pkg/xbe/derive.go:161 | DISCOXIP_SCENE_TRACE, DISCOXIP_STAGE_TRACE |
| pkg/xbe/program.go:357 | DISCOXIP_PROGRAM_TRACE |
| pkg/bios/hardware.go:408, 1371, 1721, 2019, 2135, 2219, 2649 | DISCOXIP_HARDWARE_TRACE |

### Defaults

| where | value |
|---|---|
| pkg/bios/eeprom.go:64-73 | serial 000000000000, MAC 00 50 F2 00 00 01, region 1, video 0x00400100, language 1, DVD region 1, GMT and BST |
| pkg/bios/disk.go:63-64 | DISCOXIP VIRTUAL HARD DISK DISCOXIP000001 1.0, DISCOXIP VIRTUAL DVD-ROM DISCOXIP000002 1.0 |
| pkg/bios/page.go:331 | the tray closed with no disc |
| pkg/bios/page_devices.go:41-43 | model registers never written read 0 |
| pkg/bios/page_devices.go:51 | the ROM stays visible over the flash |
| pkg/bios/page_devices.go:181-186, 196, 202 | temperatures 0, system controller commands with no role 0, every bus part the boot addresses answers reads with 0 and takes writes |
| pkg/bios/page_devices.go:67-72 | windows never written read 0 |
| pkg/bios/page_spec.go:735-741 | the daughterboard's hub carries the identity of the QEMU hub xemu attaches, vendor 0x0409 product 0x55AA |
| pkg/bios/page_spec.go:766-780 | the keys standing in for the controller, arrows for the pad, Enter and A for A, Backspace and B for B, X and Y, Space for start, Escape for back, Q and E for white and black, Z and C for the triggers, V and N for the thumb clicks, I J K L for the left stick, the numpad's 8 4 5 6 for the right, and the browser's standard gamepad mapping's 16 buttons and 4 axes in its order |
| pkg/font/export.go:9 | codepoints up to 0x024F |
| pkg/scene/gltf.go:483-530 | double sided, metallic 0, roughness 0.8, white base color, roughness 1 minus shininess, BLEND below alpha 1 |
| pkg/cli/version.go:11-12 | dev, none |

### Symbol and effect vocabulary matched by later code

| where | value |
|---|---|
| pkg/xbe/symexec.go:20, 2408, 2482, 2497-2532, eval.go:264-297 | store, call, fill, copy, port_in, port_out |
| pkg/xbe/symexec.go:698, 1062, 1334, 1542, 2579, 2660, 2929, 3027, 3149, 4249 | arg%d, reg.*, segbase.*, fs gs, res%d, g%08X, import:*, stack, reg.EBP, sub_%08X, cond.*, vcall@%X, fptr, fpu.* |
| pkg/xbe/symexec.go:314, 863, 2578, 2985 | clobber.*, learn%d, Collisions_ prefix |
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
| pkg/xbe/program.go:56-65, 80-96, 100-124 | draw clear, alloc one realloc size tick zero leave, load unload contiguous allocated virtual counter bus buswrite notfound |
| pkg/xbe/program_state.go:115-135, 569-603 | in, reg., stack:, state%d |
| pkg/bios/hardware.go:16-18, 1605, 1849, 1866 | init bldr kernel, lost.reg.*, lost.join |
| pkg/bios/resolve.go:189-201 | reg.*, stack:%d |

### Output paths and names

| where | value |
|---|---|
| pkg/xbe/xbe.go:368, 402 | image.bin, header pseudo section |
| pkg/bios/boot.go:75-78, 213, disk.go:50-51, pkg/cli/bios.go:130, 136-143 | flash.bin bldr.bin kernel.bin eeprom.bin disk.bin disk.json machine.json boot.json hardware.json, .out, 2bl.bin kernel.exe kerneldata.bin inittbl.bin eeprom.key cert.key kernel.key |
| pkg/extract/extract.go:22, 86, 115-125, 182 | archive_ scene prefix, ~%d and ~archive_%d pools, _meshes.json, .xbx to .png |
| pkg/web/export.go:29-33, 79, 102, 115, 146, 175, 178, 189, 919-924 | data/scenes data/meshes data/textures data/files, manifest.json config.json render.json, proc_%08X.png, sanitized names |
| pkg/web/runtime.go:16-18, 87, pkg/bios/page.go:44-48, 307-323, pkg/web/headless.go:51 | runtime.js, data, blocks, boot-%d.js, exe-%d.js, mcpx.bin, disk, run%d, headless.mjs |
| pkg/bios/page_devices.go:25-26 | eeprom and sector: keys of the stored bytes |
| pkg/cli/build.go:57-61, extract.go:52, 106, scene.go:74, web.go:38 | out, .extracted, web, build, _web |
| pkg/scene/scene.go:274, 307, 316 | .xbx .vb .ib extensions |

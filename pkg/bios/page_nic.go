package bios

// The network controller, its management interface moving the kernel's PHY reads and writes between the data
// register and the PHY's registers at once, the control register never in use, the PHY's reset bit clearing
// itself, its status that of a PHY with no link partner, every other register kept as the generic model keeps it
func (e *emitter) writeNIC() {
	e.js("// ---- the network controller ----")
	e.js("function nicModel(w) {")
	e.js("const inner = registerModel(w);")
	e.js("const n = shared(windowKey(w) + \":nic\", () => ({ control: 0, data: 0, phy: new Uint32Array(%d) }));", phyRegisters)
	e.js("function phyRead(address, register) { if (address !== %d) return 0x%X; return register === %d ? 0x%X : n.phy[register]; }", miiPHYAddress, miiNoPart, phyStatus, phyStatusNoLink)
	e.js("function phyWrite(address, register, value) { if (address !== %d) return; n.phy[register] = register === %d ? value & ~0x%X : value; }", miiPHYAddress, phyControl, phyControlReset)
	e.js("function transaction(v) { const register = v & 0x%X, address = (v >>> %d) & 0x%X; if ((v & 0x%X) !== 0) phyWrite(address, register, n.data & 0xFFFF); else n.data = phyRead(address, register); n.control = (v & ~0x%X) >>> 0; }", miiRegisterMask, miiAddressShift, miiAddressMask, miiWrite, miiInUse)
	e.js("return {")
	e.js("read(off, size) { switch (off & ~3) { case 0x%X: return 0; case 0x%X: return n.control; case 0x%X: return n.data; } return inner.read(off, size); },", miiStatus, miiControl, miiData)
	e.js("write(off, size, v) { switch (off & ~3) { case 0x%X: return; case 0x%X: transaction(v >>> 0); return; case 0x%X: n.data = v & 0xFFFF; return; } inner.write(off, size, v); },", miiStatus, miiControl, miiData)
	e.js("};")
	e.js("}")
}

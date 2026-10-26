package bios

import (
	"crypto/hmac"
	"crypto/rc4"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
)

// The sections of the serial EEPROM as the kernel lays them out
const (
	eepromSize         = 256
	eepromHash         = 0x00
	eepromConfounder   = 0x14
	eepromHDDKey       = 0x1C
	eepromGameRegion   = 0x2C
	eepromEncrypted    = 0x14
	eepromEncryptedEnd = 0x30
	eepromFactory      = 0x30
	eepromSerial       = 0x34
	eepromMAC          = 0x40
	eepromOnlineKey    = 0x48
	eepromVideoStd     = 0x58
	eepromFactoryEnd   = 0x60
	eepromUser         = 0x60
	eepromTZBias       = 0x64
	eepromTZStdName    = 0x68
	eepromTZDltName    = 0x6C
	eepromTZStdDate    = 0x78
	eepromTZDltDate    = 0x7C
	eepromTZStdBias    = 0x88
	eepromTZDltBias    = 0x8C
	eepromLanguage     = 0x90
	eepromVideoFlags   = 0x94
	eepromAudioFlags   = 0x98
	eepromPCGames      = 0x9C
	eepromPCPassword   = 0xA0
	eepromPCMovies     = 0xA4
	eepromMisc         = 0xB8
	eepromDVDRegion    = 0xBC
	eepromUserEnd      = 0xC0
)

// Console is the configuration the EEPROM holds for one console
type Console struct {
	Serial        string
	MAC           [6]byte
	HDDKey        [16]byte
	OnlineKey     [16]byte
	GameRegion    uint32
	VideoStandard uint32
	Language      uint32
	VideoFlags    uint32
	AudioFlags    uint32
	DVDRegion     uint32
	TimeZoneBias  int32
	StdName       string
	DltName       string
}

// The console the build configures, a North American NTSC unit set to English
func DefaultConsole() Console {
	return Console{
		Serial:        "000000000000",
		MAC:           [6]byte{0x00, 0x50, 0xF2, 0x00, 0x00, 0x01},
		GameRegion:    1,
		VideoStandard: 0x00400100,
		Language:      1,
		DVDRegion:     1,
		StdName:       "GMT",
		DltName:       "BST",
	}
}

// EEPROM writes the console's configuration as the 256 bytes the kernel reads, the first section sealed with the
// EEPROM key the 2BL carries
func (b *Bank) EEPROM(c Console) ([]byte, error) {
	if len(c.Serial) != 12 {
		return nil, fmt.Errorf("bios: a console serial number has 12 characters, got %q", c.Serial)
	}
	e := make([]byte, eepromSize)
	copy(e[eepromHDDKey:], c.HDDKey[:])
	binary.LittleEndian.PutUint32(e[eepromGameRegion:], c.GameRegion)
	copy(e[eepromSerial:], c.Serial)
	copy(e[eepromMAC:], c.MAC[:])
	copy(e[eepromOnlineKey:], c.OnlineKey[:])
	binary.LittleEndian.PutUint32(e[eepromVideoStd:], c.VideoStandard)
	binary.LittleEndian.PutUint32(e[eepromTZBias:], uint32(c.TimeZoneBias))
	copy(e[eepromTZStdName:eepromTZStdName+4], c.StdName)
	copy(e[eepromTZDltName:eepromTZDltName+4], c.DltName)
	binary.LittleEndian.PutUint32(e[eepromLanguage:], c.Language)
	binary.LittleEndian.PutUint32(e[eepromVideoFlags:], c.VideoFlags)
	binary.LittleEndian.PutUint32(e[eepromAudioFlags:], c.AudioFlags)
	binary.LittleEndian.PutUint32(e[eepromDVDRegion:], c.DVDRegion)
	eepromChecksum(e[eepromFactory:eepromFactoryEnd])
	eepromChecksum(e[eepromUser:eepromUserEnd])
	if err := sealEEPROM(e, b.Keys.EEPROM[:]); err != nil {
		return nil, err
	}
	return e, nil
}

// Writes a section's checksum into its first word, the word that makes the section's words sum to all ones
func eepromChecksum(section []byte) {
	var high, low uint32
	for off := 4; off < len(section); off += 4 {
		w := binary.LittleEndian.Uint32(section[off:])
		low += w
		if low < w {
			high++
		}
	}
	binary.LittleEndian.PutUint32(section, ^(high + low))
}

// Seals the key section: its digest under the EEPROM key leads it, and the section is enciphered with the key the
// digest derives
func sealEEPROM(e []byte, key []byte) error {
	plain := e[eepromEncrypted:eepromEncryptedEnd]
	mac := hmac.New(sha1.New, key)
	mac.Write(plain)
	digest := mac.Sum(nil)
	copy(e[eepromHash:], digest)
	mac = hmac.New(sha1.New, key)
	mac.Write(digest)
	c, err := rc4.NewCipher(mac.Sum(nil))
	if err != nil {
		return err
	}
	c.XORKeyStream(plain, plain)
	return nil
}

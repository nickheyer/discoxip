package bios

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/json"
	"strings"
	"testing"
)

// The disk's password is the HMAC of the key over the model and serial as the kernel reads them back, zero padded
func TestDiskPassword(t *testing.T) {
	hdd, _ := DefaultDrives()
	identify := ataIdentify(hdd, 1<<24)
	var key [16]byte
	copy(key[:], "0123456789abcdef")
	mac := hmac.New(sha1.New, key[:])
	mac.Write([]byte("DISCOXIP VIRTUAL HARD DISK"))
	mac.Write([]byte("DISCOXIP000001"))
	want := append(mac.Sum(nil), make([]byte, ataPasswordBytes-ataDigestBytes)...)
	if got := diskPassword(key, identify); !bytes.Equal(got, want) {
		t.Fatalf("password %x, want %x", got, want)
	}
	if identify[ataSecurityWord] != ataSecuritySupported|ataSecurityEnabled|ataSecurityLocked {
		t.Fatalf("security word %#x", identify[ataSecurityWord])
	}
	if identify[ataFeatureWord]&ataSecurityFeature == 0 || identify[ataFeatureEnabledWord]&ataSecurityFeature == 0 {
		t.Fatalf("feature words %#x %#x lack the security feature", identify[ataFeatureWord], identify[ataFeatureEnabledWord])
	}
	disk, _, err := BuildDisk([]Partition{{Number: 1, Start: 0, Length: 64}}, 2, "", hdd, Identity{Model: "D", Serial: "S", Firmware: "1"}, key)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(disk)
	if err != nil {
		t.Fatal(err)
	}
	if len(disk.Password) != ataPasswordBytes || !strings.Contains(string(encoded), `"password":[`) {
		t.Fatalf("the password is not written as byte values: %s", encoded)
	}
	for i, b := range want {
		if disk.Password[i] != int(b) {
			t.Fatalf("password byte %d is %d, want %d", i, disk.Password[i], b)
		}
	}
}

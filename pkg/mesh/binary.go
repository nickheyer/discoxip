package mesh

// BinaryMesh holds parsed binary XM data (4-byte packed records).
type BinaryMesh struct {
	Records []BinaryRecord
	RawData []byte
}

// BinaryRecord is a single 4-byte record from a binary XM file.
type BinaryRecord struct {
	Data [3]byte
	Tag  byte
}

// ParseBinary parses 4-byte records from binary XM data.
func ParseBinary(data []byte) *BinaryMesh {
	bm := &BinaryMesh{RawData: data}

	// Parse 4-byte records
	for i := 0; i+3 < len(data); i += 4 {
		bm.Records = append(bm.Records, BinaryRecord{
			Data: [3]byte{data[i], data[i+1], data[i+2]},
			Tag:  data[i+3],
		})
	}

	return bm
}

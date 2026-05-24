package sqlitevec

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestFloat32VectorToBytesUsesLittleEndianFloat32Bits(t *testing.T) {
	vector := []float32{0, 1.5, -2.25, float32(math.Inf(1))}

	got := float32VectorToBytes(vector)

	if len(got) != len(vector)*4 {
		t.Fatalf("expected %d bytes, got %d", len(vector)*4, len(got))
	}

	for i, value := range vector {
		gotBits := binary.LittleEndian.Uint32(got[i*4:])
		wantBits := math.Float32bits(value)
		if gotBits != wantBits {
			t.Fatalf("index %d: expected bits %08x, got %08x", i, wantBits, gotBits)
		}
	}
}

func TestFloat32VectorToBytesHandlesEmptyVector(t *testing.T) {
	got := float32VectorToBytes(nil)
	if len(got) != 0 {
		t.Fatalf("expected empty byte slice, got %d bytes", len(got))
	}
}

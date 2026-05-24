package sqlitevec

import (
	"encoding/binary"
	"math"
)

func float32VectorToBytes(vector []float32) []byte {
	buf := make([]byte, len(vector)*4)
	for i, v := range vector {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

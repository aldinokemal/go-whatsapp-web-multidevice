package media

import (
	"encoding/binary"
	"math"
)

func PCMFloat32ToInt16LE(pcm []float32) []byte {
	out := make([]byte, len(pcm)*2)
	for i, s := range pcm {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(floatToInt16(s)))
	}
	return out
}

func PCMInt16LEToFloat32(b []byte) []float32 {
	out := make([]float32, len(b)/2)
	for i := range out {
		v := int16(binary.LittleEndian.Uint16(b[i*2:]))
		out[i] = float32(v) / 32768.0
	}
	return out
}

func floatToInt16(s float32) int16 {
	switch {
	case math.IsNaN(float64(s)):
		return 0
	case s >= 1:
		return math.MaxInt16
	case s <= -1:
		return math.MinInt16
	}
	return int16(s * 32767)
}

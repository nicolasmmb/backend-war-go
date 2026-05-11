package indexer

import (
	"testing"

	"backend/internal/ivf"
)

const benchPairCount = 1 << 14
const (
	benchDistSeed       = uint64(0x9e3779b97f4a7c15)
	benchFloatBitsShift = 16
	benchFloatBitsMask  = 0x7fff
	benchFloatCenter    = 16384
	benchFloatScale     = 16384.0
)

var (
	benchDistInputsA    [benchPairCount][ivf.Dim]float32
	benchDistInputsB    [benchPairCount][ivf.Dim]float32
	benchmarkDistSqSink float32
)

func init() {
	state := benchDistSeed
	for i := range benchPairCount {
		for j := range ivf.Dim {
			state = state*lcgMul + lcgInc
			benchDistInputsA[i][j] = float32(int32((state>>benchFloatBitsShift)&benchFloatBitsMask)-benchFloatCenter) / benchFloatScale

			state = state*lcgMul + lcgInc
			benchDistInputsB[i][j] = float32(int32((state>>benchFloatBitsShift)&benchFloatBitsMask)-benchFloatCenter) / benchFloatScale
		}
	}
}

// Referencia escalar para comparar throughput com a variante unrolled.
func BenchmarkDistSq(b *testing.B) {
	b.ReportAllocs()
	var acc float32
	idx := 0

	for i := 0; i < b.N; i++ {
		acc += distSq(&benchDistInputsA[idx], &benchDistInputsB[idx])
		idx++
		if idx == len(benchDistInputsA) {
			idx = 0
		}
	}

	benchmarkDistSqSink = acc
}

func BenchmarkDistSqAVX2(b *testing.B) {
	b.ReportAllocs()
	var acc float32
	idx := 0

	for b.Loop() {
		acc += distSqAVX2(&benchDistInputsA[idx], &benchDistInputsB[idx])
		idx++
		if idx == len(benchDistInputsA) {
			idx = 0
		}
	}

	benchmarkDistSqSink = acc
}

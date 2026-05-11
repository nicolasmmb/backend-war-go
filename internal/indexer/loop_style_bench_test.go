package indexer

import (
	"testing"

	"backend/internal/ivf"
)

const (
	loopBenchPairCount = 1 << 14
	loopFloatBitsShift = 16
	loopFloatBitsMask  = 0x7fff
	loopFloatCenter    = 16384
	loopFloatScale     = 16384.0
)

var (
	loopBenchInputsA [loopBenchPairCount][ivf.Dim]float32
	loopBenchInputsB [loopBenchPairCount][ivf.Dim]float32
	loopBenchSink    float32
)

func init() {
	state := uint64(0xd1b54a32d192ed03)
	for i := range loopBenchPairCount {
		for j := range ivf.Dim {
			state = state*lcgMul + lcgInc
			loopBenchInputsA[i][j] = float32(int32((state>>loopFloatBitsShift)&loopFloatBitsMask)-loopFloatCenter) / loopFloatScale

			state = state*lcgMul + lcgInc
			loopBenchInputsB[i][j] = float32(int32((state>>loopFloatBitsShift)&loopFloatBitsMask)-loopFloatCenter) / loopFloatScale
		}
	}
}

func BenchmarkLoopStyle(b *testing.B) {
	b.Run("classic_jpp", func(b *testing.B) {
		b.ReportAllocs()
		var acc float32
		idx := 0
		for i := 0; i < b.N; i++ {
			a := &loopBenchInputsA[idx]
			c := &loopBenchInputsB[idx]

			var sum float32
			for j := 0; j < ivf.Dim; j++ {
				d := a[j] - c[j]
				sum += d * d
			}
			acc += sum

			idx++
			if idx == loopBenchPairCount {
				idx = 0
			}
		}
		loopBenchSink = acc
	})

	b.Run("range_int", func(b *testing.B) {
		b.ReportAllocs()
		var acc float32
		idx := 0
		for i := 0; i < b.N; i++ {
			a := &loopBenchInputsA[idx]
			c := &loopBenchInputsB[idx]

			var sum float32
			for j := range ivf.Dim {
				d := a[j] - c[j]
				sum += d * d
			}
			acc += sum

			idx++
			if idx == loopBenchPairCount {
				idx = 0
			}
		}
		loopBenchSink = acc
	})
}

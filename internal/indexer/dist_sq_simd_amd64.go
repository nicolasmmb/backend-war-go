//go:build goexperiment.simd && amd64

package indexer

import (
	"backend/internal/ivf"
	"simd/archsimd"
)

// distSqSIMD calcula distancia L2^2 da dimensao fixa usando intrinsics de AVX.
// Retorna ok=false quando o host nao possui AVX habilitado.
func distSqSIMD(a, b *[ivf.Dim]float32) (sum float32, ok bool) {
	if !archsimd.X86.AVX() {
		return 0, false
	}

	// Dim=14 => processa 8 + 4 + 2 (tail com zero-padding por SlicePart).
	d8 := archsimd.LoadFloat32x8Slice(a[:8]).Sub(archsimd.LoadFloat32x8Slice(b[:8]))
	s8 := d8.Mul(d8)

	d4 := archsimd.LoadFloat32x4Slice(a[8:12]).Sub(archsimd.LoadFloat32x4Slice(b[8:12]))
	s4 := d4.Mul(d4)

	dTail := archsimd.LoadFloat32x4SlicePart(a[12:]).Sub(archsimd.LoadFloat32x4SlicePart(b[12:]))
	sTail := dTail.Mul(dTail)

	var lanes8 [8]float32
	var lanes4 [4]float32
	var lanesTail [4]float32
	s8.Store(&lanes8)
	s4.Store(&lanes4)
	sTail.Store(&lanesTail)

	sum = lanes8[0] + lanes8[1] + lanes8[2] + lanes8[3] + lanes8[4] + lanes8[5] + lanes8[6] + lanes8[7]
	sum += lanes4[0] + lanes4[1] + lanes4[2] + lanes4[3]
	sum += lanesTail[0] + lanesTail[1] // somente os 2 valores reais da cauda
	return sum, true
}

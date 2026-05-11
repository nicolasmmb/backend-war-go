//go:build !goexperiment.simd || !amd64

package indexer

import "backend/internal/ivf"

// distSqSIMD retorna ok=false quando o caminho SIMD nao esta disponivel neste build/arquitetura.
func distSqSIMD(_ *[ivf.Dim]float32, _ *[ivf.Dim]float32) (sum float32, ok bool) {
	return 0, false
}

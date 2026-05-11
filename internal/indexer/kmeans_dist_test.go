package indexer

import (
	"backend/internal/ivf"
	"math"
	"testing"
)

func TestDistSqParity(t *testing.T) {
	state := uint64(0x9e3779b97f4a7c15)

	next := func() float32 {
		state = state*6364136223846793005 + 1442695040888963407
		// [-1,1]
		v := float32((state>>40)&0xffff) / 32767.5
		return v - 1.0
	}

	for i := 0; i < 5000; i++ {
		var a, b [ivf.Dim]float32
		for j := 0; j < ivf.Dim; j++ {
			a[j] = next()
			b[j] = next()
		}

		want := distSq(&a, &b)
		got := distSqAVX2(&a, &b)

		diff := math.Abs(float64(got - want))
		if diff > 1e-4 {
			t.Fatalf("distSq mismatch at sample %d: got=%f want=%f diff=%g", i, got, want, diff)
		}
	}
}

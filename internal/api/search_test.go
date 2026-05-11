package api

import (
	"math"
	"slices"
	"testing"

	"backend/internal/indexer"
	"backend/internal/ivf"
)

func makeSyntheticDataset(t *testing.T) *ivf.Dataset {
	t.Helper()
	vectors := make([][ivf.Dim]float32, 0, 15)
	labels := make([]uint8, 0, 15)
	assignments := make([]uint16, 0, 15)

	for i := range 5 {
		var v [ivf.Dim]float32
		v[0] = float32(i) / ivf.FixScale
		vectors = append(vectors, v)
		labels = append(labels, 1)
		assignments = append(assignments, 0)
	}
	for i := range 10 {
		var v [ivf.Dim]float32
		v[0] = float32(1000+i) / ivf.FixScale
		vectors = append(vectors, v)
		labels = append(labels, 0)
		assignments = append(assignments, 1)
	}

	centroids := make([][ivf.Dim]float32, 2)
	centroids[0][0] = 0
	centroids[1][0] = 0.1

	layout, err := indexer.BuildLayout(vectors, labels, centroids, assignments)
	if err != nil {
		t.Fatalf("build layout failed: %v", err)
	}

	kPad := testAlignKPad(layout.K)
	centroidsSOA := make([]float32, ivf.Dim*kPad)
	for c := 0; c < layout.K; c++ {
		for j := range ivf.Dim {
			centroidsSOA[j*kPad+c] = layout.Centroids[c*ivf.Dim+j]
		}
	}
	for c := layout.K; c < kPad; c++ {
		for j := range ivf.Dim {
			centroidsSOA[j*kPad+c] = float32(math.Inf(1))
		}
	}

	return &ivf.Dataset{
		N:            layout.N,
		K:            layout.K,
		KPad:         kPad,
		Scale:        ivf.FixScale,
		Centroids:    layout.Centroids,
		CentroidsSOA: centroidsSOA,
		BBoxMin:      layout.BBoxMin,
		BBoxMax:      layout.BBoxMax,
		Offsets:      layout.Offsets,
		BlockOffsets: layout.BlockOffsets,
		Blocks:       layout.Blocks,
		Labels:       layout.Labels,
		OrigIDs:      layout.OrigIDs,
	}
}

// Sanidade basica da quantizacao, essencial para comparacao em dominio int16.
func TestQuantizeI16(t *testing.T) {
	if quantizeI16(0) != 0 {
		t.Fatalf("expected 0")
	}
	if quantizeI16(1) != 10000 {
		t.Fatalf("expected 10000")
	}
	if quantizeI16(-1) != -10000 {
		t.Fatalf("expected -10000")
	}
	if quantizeI16(2) != 10000 {
		t.Fatalf("expected clamp")
	}
}

// Cenario simples para validar voto de fraude no top-5 em clusters controlados.
func TestSearchFraudCount(t *testing.T) {
	ds := makeSyntheticDataset(t)
	var q [ivf.Dim]float32
	if got := SearchFraudCount(&q, ds, 1); got != 5 {
		t.Fatalf("expected 5 got %d", got)
	}

	var q2 [ivf.Dim]float32
	q2[0] = 0.1
	if got := SearchFraudCount(&q2, ds, 1); got != 0 {
		t.Fatalf("expected 0 got %d", got)
	}
}

// Paridade entre implementacao otimizada e versao escalar de referencia.
func TestScanClusterMatchesScalar(t *testing.T) {
	ds := makeSyntheticDataset(t)
	state := uint64(0x517cc1b727220a95)

	for range 200 {
		var q [ivf.Dim]float32
		var qI16 [ivf.Dim]int16
		for j := range ivf.Dim {
			q[j] = testLCGFloatUnit(&state)
			qI16[j] = quantizeI16(q[j])
		}

		for c := range ds.K {
			var topFast [topKNeighbors]topNeighbor
			var topSlow [topKNeighbors]topNeighbor
			for i := range topFast {
				topFast[i] = sentinelNeighbor
				topSlow[i] = sentinelNeighbor
			}
			worstFast := 0
			worstSlow := 0

			scanCluster(c, &qI16, ds, &topFast, &worstFast)
			scanClusterScalar(c, &qI16, ds, &topSlow, &worstSlow)

			if worstFast != worstSlow {
				t.Fatalf("worst idx mismatch: fast=%d slow=%d", worstFast, worstSlow)
			}
			for i := range topFast {
				if topFast[i] != topSlow[i] {
					t.Fatalf("top[%d] mismatch: fast=%+v slow=%+v", i, topFast[i], topSlow[i])
				}
			}
		}
	}
}

// Lower bound vetorizado precisa ser bit-a-bit igual ao caminho escalar.
func TestBboxLowerBoundMatchesScalar(t *testing.T) {
	ds := makeSyntheticDataset(t)
	state := uint64(0x6a09e667f3bcc909)

	for range 500 {
		var q [ivf.Dim]int16
		for j := range ivf.Dim {
			testLCGStep(&state)
			q[j] = int16(int32(state%20001) - 10000)
		}
		for c := range ds.K {
			gotFast := bboxLowerBound(&q, ds, c)
			gotSlow := bboxLowerBoundScalar(&q, ds, c)
			if gotFast != gotSlow {
				t.Fatalf("cluster=%d mismatch fast=%d slow=%d", c, gotFast, gotSlow)
			}
		}
	}
}

// Insercao incremental top-5 deve preservar semantica da implementacao legada.
func TestTryInsertTop5MatchesLegacy(t *testing.T) {
	state := uint64(0xbb67ae8584caa73b)

	for range 200 {
		var topFast [topKNeighbors]topNeighbor
		var topLegacy [topKNeighbors]topNeighbor
		for i := range topFast {
			topFast[i] = sentinelNeighbor
			topLegacy[i] = sentinelNeighbor
		}
		worstFast := 0
		worstLegacy := 0

		for range 400 {
			testLCGStep(&state)
			d := int64((state >> testFloatBitsShift) & testDistanceMask)
			testLCGStep(&state)
			label := uint8(state & 1)
			testLCGStep(&state)
			origID := uint32(state & testOrigIDMask)

			tryInsertTop5(&topFast, &worstFast, d, label, origID)
			tryInsertTop5Legacy(&topLegacy, &worstLegacy, d, label, origID)
		}

		fastNorm := normalizeTop5(topFast)
		legacyNorm := normalizeTop5(topLegacy)
		if fastNorm != legacyNorm {
			t.Fatalf("top5 mismatch:\nfast=%+v\nlegacy=%+v", topFast, topLegacy)
		}

		fastWorst := topFast[worstFast]
		legacyWorst := topLegacy[worstLegacy]
		if fastWorst.dist != legacyWorst.dist || fastWorst.origID != legacyWorst.origID {
			t.Fatalf("worst mismatch: fast=%+v legacy=%+v", fastWorst, legacyWorst)
		}
	}
}

// Confere paridade da selecao top-N em todos os buckets de pool.
func TestTopNCentroidsMatchesAllocAcrossBuckets(t *testing.T) {
	state := uint64(0x3c6ef372fe94f82b)
	var dists [maxK]float32
	for i := range maxK {
		testLCGStep(&state)
		dists[i] = float32((state >> 32) & testOrigIDMask)
	}

	ns := []int{1, 8, 9, 16, 17, 64, 65, 512, 513, 2048, 4096}
	for _, n := range ns {
		got, scratch := topNCentroids(dists[:], n)
		want := topNCentroidsAlloc(dists[:], n)
		for i := range n {
			if got[i] != want[i] {
				t.Fatalf("n=%d idx=%d mismatch got=%d want=%d", n, i, got[i], want[i])
			}
		}
		releaseTopNCentroidsScratch(scratch)
	}
}

func makeCentroidDistDatasetForTest(k int, seed uint64) *ivf.Dataset {
	kPad := testAlignKPad(k)
	soa := make([]float32, ivf.Dim*kPad)
	state := seed
	for c := 0; c < k; c++ {
		for j := range ivf.Dim {
			soa[j*kPad+c] = testLCGFloatUnit(&state)
		}
	}
	for c := k; c < kPad; c++ {
		for j := range ivf.Dim {
			soa[j*kPad+c] = float32(math.Inf(1))
		}
	}
	return &ivf.Dataset{K: k, KPad: kPad, CentroidsSOA: soa}
}

func TestFillCentroidDistsMatchesScalar(t *testing.T) {
	sizes := []int{1, 2, 7, 8, 9, 15, 63, 64, 65, 510}
	state := uint64(0x510e527fade682d1)

	for _, k := range sizes {
		ds := makeCentroidDistDatasetForTest(k, state^uint64(k))
		var q [ivf.Dim]float32
		for j := range ivf.Dim {
			q[j] = testLCGFloatUnit(&state)
		}

		got := make([]float32, k)
		want := make([]float32, k)
		fillCentroidDists(&q, ds, got)
		fillCentroidDistsScalar(&q, ds, want)

		for i := range k {
			if got[i] != want[i] {
				t.Fatalf("k=%d idx=%d mismatch got=%.9f want=%.9f", k, i, got[i], want[i])
			}
		}
	}
}

// Garante que as podas antecipadas do scan novo nao alteram o resultado final.
func TestScanClusterMatchesLegacy(t *testing.T) {
	ds := makeSyntheticDataset(t)
	state := uint64(0x94d049bb133111eb)

	for range 300 {
		var q [ivf.Dim]float32
		var qI16 [ivf.Dim]int16
		for j := range ivf.Dim {
			q[j] = testLCGFloatUnit(&state)
			qI16[j] = quantizeI16(q[j])
		}

		for c := range ds.K {
			var topFast [topKNeighbors]topNeighbor
			var topLegacy [topKNeighbors]topNeighbor
			for i := range topFast {
				topFast[i] = sentinelNeighbor
				topLegacy[i] = sentinelNeighbor
			}
			worstFast := 0
			worstLegacy := 0

			scanCluster(c, &qI16, ds, &topFast, &worstFast)
			scanClusterLegacy(c, &qI16, ds, &topLegacy, &worstLegacy)

			if worstFast != worstLegacy {
				t.Fatalf("cluster=%d worst mismatch fast=%d legacy=%d", c, worstFast, worstLegacy)
			}
			for i := range topFast {
				if topFast[i] != topLegacy[i] {
					t.Fatalf("cluster=%d top[%d] mismatch fast=%+v legacy=%+v", c, i, topFast[i], topLegacy[i])
				}
			}
		}
	}
}

func normalizeTop5(in [topKNeighbors]topNeighbor) [topKNeighbors]topNeighbor {
	out := in
	slices.SortFunc(out[:], func(a, b topNeighbor) int {
		if a.dist != b.dist {
			if a.dist < b.dist {
				return -1
			}
			return 1
		}
		if a.origID != b.origID {
			if a.origID < b.origID {
				return -1
			}
			return 1
		}
		if a.label < b.label {
			return -1
		}
		if a.label > b.label {
			return 1
		}
		return 0
	})
	return out
}

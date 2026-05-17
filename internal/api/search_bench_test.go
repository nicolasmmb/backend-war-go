package api

import (
	"fmt"
	"math"
	"testing"

	"backend/internal/indexer"
	"backend/internal/ivf"
)

const (
	benchCentroidsK  = 512
	benchClusterSize = 4096
	benchInsertCount = 1 << 14
)

var (
	benchCentroidDataset  *ivf.Dataset
	benchScanDataset      *ivf.Dataset
	benchQueryF32         [ivf.Dim]float32
	benchQueryI16         [ivf.Dim]int16
	benchTopNDists        [maxK]float32
	benchInsertCandidates [benchInsertCount]topNeighbor
	benchDistSink         float32
	benchScanSink         uint32
	benchBboxSink         int64
	benchInsertSink       uint32
	benchTopNSink         int
)

func init() {
	benchCentroidDataset = makeCentroidBenchDataset()
	benchScanDataset = makeScanBenchDataset()

	state := uint64(0x243f6a8885a308d3)
	for j := range ivf.Dim {
		benchQueryF32[j] = testLCGFloatUnit(&state)
		benchQueryI16[j] = quantizeI16(benchQueryF32[j])
	}
	for i := range maxK {
		testLCGStep(&state)
		benchTopNDists[i] = float32((state >> 32) & testOrigIDMask)
	}
	for i := range len(benchInsertCandidates) {
		testLCGStep(&state)
		dist := int64((state >> testFloatBitsShift) & testDistanceMask)
		testLCGStep(&state)
		label := uint8(state & 1)
		testLCGStep(&state)
		origID := uint32(state & testOrigIDMask)
		benchInsertCandidates[i] = topNeighbor{dist: dist, label: label, origID: origID}
	}
}

// Dataset sintetico para medir apenas custo de distancia a centroides (sem custo de scan por cluster).
func makeCentroidBenchDataset() *ivf.Dataset {
	k := benchCentroidsK
	kPad := testAlignKPad(k)
	centroidsSOA := make([]float32, ivf.Dim*kPad)

	state := uint64(0x9e3779b97f4a7c15)
	for c := range k {
		for j := range ivf.Dim {
			centroidsSOA[j*kPad+c] = testLCGFloatUnit(&state)
		}
	}
	for c := k; c < kPad; c++ {
		for j := range ivf.Dim {
			centroidsSOA[j*kPad+c] = float32(math.Inf(1))
		}
	}

	return &ivf.Dataset{
		K:            k,
		KPad:         kPad,
		CentroidsSOA: centroidsSOA,
	}
}

// Dataset sintetico com um cluster denso para estressar scanCluster e bboxLowerBound.
func makeScanBenchDataset() *ivf.Dataset {
	vectors := make([][ivf.Dim]float32, benchClusterSize)
	labels := make([]uint8, benchClusterSize)
	assignments := make([]uint16, benchClusterSize)
	centroids := make([][ivf.Dim]float32, 1)

	state := uint64(0x13198a2e03707344)
	for i := range benchClusterSize {
		for j := range ivf.Dim {
			vectors[i][j] = testLCGFloatUnit(&state)
		}
		labels[i] = uint8(i & 1)
		assignments[i] = 0
	}

	layout, err := indexer.BuildLayout(vectors, labels, centroids, assignments)
	if err != nil {
		panic(err)
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

func BenchmarkCentroidDistScalar(b *testing.B) {
	b.ReportAllocs()
	ds := benchCentroidDataset
	var acc float32
	idx := 0

	for b.Loop() {
		acc += centroidDistScalar(&benchQueryF32, ds, idx)
		idx++
		if idx == ds.K {
			idx = 0
		}
	}

	benchDistSink = acc
}

func BenchmarkCentroidDistAVX2(b *testing.B) {
	b.ReportAllocs()
	ds := benchCentroidDataset
	var acc float32
	idx := 0

	for b.Loop() {
		acc += centroidDist(&benchQueryF32, ds, idx)
		idx++
		if idx == ds.K {
			idx = 0
		}
	}

	benchDistSink = acc
}

func BenchmarkCentroidDistsScalar(b *testing.B) {
	b.ReportAllocs()
	ds := benchCentroidDataset
	out := make([]float32, ds.K)
	var acc float32
	idx := 0

	for b.Loop() {
		fillCentroidDistsScalar(&benchQueryF32, ds, out)
		acc += out[idx]
		idx++
		if idx == ds.K {
			idx = 0
		}
	}

	benchDistSink = acc
}

func BenchmarkCentroidDistsBatch8(b *testing.B) {
	b.ReportAllocs()
	ds := benchCentroidDataset
	out := make([]float32, ds.K)
	var acc float32
	idx := 0

	for i := 0; i < b.N; i++ {
		fillCentroidDists(&benchQueryF32, ds, out)
		acc += out[idx]
		idx++
		if idx == ds.K {
			idx = 0
		}
	}

	benchDistSink = acc
}

func BenchmarkScanClusterScalar(b *testing.B) {
	b.ReportAllocs()
	ds := benchScanDataset
	var top5 [topKNeighbors]topNeighbor
	var worstIdx int

	for i := 0; i < b.N; i++ {
		for j := range top5 {
			top5[j] = sentinelNeighbor
		}
		worstIdx = 0
		scanClusterScalar(0, &benchQueryI16, ds, &top5, &worstIdx)
		benchScanSink += uint32(top5[0].label)
	}
}

func BenchmarkScanClusterAVX2(b *testing.B) {
	b.ReportAllocs()
	ds := benchScanDataset
	var top5 [topKNeighbors]topNeighbor
	var worstIdx int

	for i := 0; i < b.N; i++ {
		for j := range top5 {
			top5[j] = sentinelNeighbor
		}
		worstIdx = 0
		scanCluster(0, &benchQueryI16, ds, &top5, &worstIdx)
		benchScanSink += uint32(top5[0].label)
	}
}

func BenchmarkBboxLowerBoundScalar(b *testing.B) {
	b.ReportAllocs()
	ds := benchScanDataset
	var acc int64

	for i := 0; i < b.N; i++ {
		acc += bboxLowerBoundScalar(&benchQueryI16, ds, 0)
	}

	benchBboxSink = acc
}

func BenchmarkBboxLowerBoundAVX2(b *testing.B) {
	b.ReportAllocs()
	ds := benchScanDataset
	var acc int64

	for i := 0; i < b.N; i++ {
		acc += bboxLowerBound(&benchQueryI16, ds, 0)
	}

	benchBboxSink = acc
}

func BenchmarkTopNCentroids(b *testing.B) {
	b.ReportAllocs()
	var acc int
	for i := 0; i < b.N; i++ {
		ids, scratch := topNCentroids(benchTopNDists[:], defaultNProbe)
		acc += ids[0]
		releaseTopNCentroidsScratch(scratch)
	}
	benchTopNSink = acc
}

func BenchmarkTopNCentroidsPooledByN(b *testing.B) {
	ns := []int{8, 64, 512, 2048}
	for _, n := range ns {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			var acc int
			for i := 0; i < b.N; i++ {
				ids, scratch := topNCentroids(benchTopNDists[:], n)
				acc += ids[0]
				releaseTopNCentroidsScratch(scratch)
			}
			benchTopNSink = acc
		})
	}
}

func BenchmarkTryInsertTop5(b *testing.B) {
	b.ReportAllocs()
	var top5 [topKNeighbors]topNeighbor
	var worstIdx int
	idx := 0
	for i := 0; i < b.N; i++ {
		if i&63 == 0 {
			for j := range top5 {
				top5[j] = sentinelNeighbor
			}
			worstIdx = 0
		}
		c := benchInsertCandidates[idx]
		tryInsertTop5(&top5, &worstIdx, c.dist, c.label, c.origID)
		idx++
		if idx == len(benchInsertCandidates) {
			idx = 0
		}
	}
	benchInsertSink = top5[0].origID
}

package indexer

import (
	"backend/internal/ivf"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
)

const kmeansInitSample = 65_536
const (
	// Parametros do LCG 64-bit (Numerical Recipes) para geracao deterministica barata.
	lcgMul uint64 = 6364136223846793005
	lcgInc uint64 = 1442695040888963407

	// nextN usa os bits altos (melhor qualidade estatistica no LCG) antes do modulo.
	lcgNextNShift = 33
	// nextF64 usa 53 bits de mantissa para gerar float64 uniforme em [0,1).
	lcgNextF64Shift = 11
	lcgNextF64Denom = float64(uint64(1) << 53)

	// Sinais/sentinelas operacionais do treino.
	distSentinel         = float32(1e30)
	maxAssignWorkers     = 16
	maxTrainK            = 4096
	unassignedAssignment = uint16(0xffff)
	maxAssignmentID      = int(^uint16(0))
	earlyStopScale       = 1000 // changed/len < 1/1000 => <0.1%
)

// distSq calcula distancia euclidiana ao quadrado no espaco vetorial de 14 dimensoes.
func distSq(a, b *[ivf.Dim]float32) float32 {
	var s float32
	for j := range ivf.Dim {
		d := a[j] - b[j]
		s += d * d
	}
	return s
}

func distSqAVX2(a, b *[ivf.Dim]float32) float32 {
	if sum, ok := distSqSIMD(a, b); ok {
		return sum
	}

	// Unroll manual aumenta chance de gerar codigo vetorizado no backend do compilador.
	// Trade-off: manutencao fica acoplada a Dim fixa (14).
	d0 := a[0] - b[0]
	d1 := a[1] - b[1]
	d2 := a[2] - b[2]
	d3 := a[3] - b[3]
	d4 := a[4] - b[4]
	d5 := a[5] - b[5]
	d6 := a[6] - b[6]
	d7 := a[7] - b[7]
	d8 := a[8] - b[8]
	d9 := a[9] - b[9]
	d10 := a[10] - b[10]
	d11 := a[11] - b[11]
	d12 := a[12] - b[12]
	d13 := a[13] - b[13]

	return d0*d0 + d1*d1 + d2*d2 + d3*d3 + d4*d4 + d5*d5 + d6*d6 + d7*d7 + d8*d8 + d9*d9 + d10*d10 + d11*d11 + d12*d12 + d13*d13

}

// nearestCentroid encontra o centroide mais proximo para um vetor durante a etapa de atribuicao.
func nearestCentroid(v *[ivf.Dim]float32, centroids [][ivf.Dim]float32) uint16 {
	best := uint16(0)
	bestDist := distSentinel
	for i := range centroids {
		d := distSqAVX2(v, &centroids[i])
		if d < bestDist {
			bestDist = d
			best = uint16(i)
		}
	}
	return best
}

type lcg struct {
	s uint64
}

// newLCG cria um gerador pseudo-aleatorio deterministico para treino reproduzivel.
func newLCG(seed uint64) *lcg {
	return &lcg{s: seed}
}

// nextU64 avanca o estado do LCG e retorna um inteiro pseudo-aleatorio.
func (r *lcg) nextU64() uint64 {
	r.s = r.s*lcgMul + lcgInc
	return r.s
}

// nextN retorna inteiro pseudo-aleatorio no intervalo [0, n).
func (r *lcg) nextN(n int) int {
	if n <= 0 {
		return 0
	}
	return int((r.nextU64() >> lcgNextNShift) % uint64(n))
}

// nextF64 retorna um float em [0,1) para sorteio ponderado do kmeans++.
func (r *lcg) nextF64() float64 {
	return float64(r.nextU64()>>lcgNextF64Shift) / lcgNextF64Denom
}

// kmeansPPInit inicializa centroides com kmeans++ para convergir melhor do que escolha totalmente aleatoria.
func kmeansPPInit(vectors [][ivf.Dim]float32, k int, seed uint64) [][ivf.Dim]float32 {
	// Passo 0: prepara amostra deterministica do dataset para baratear fase de inicializacao.
	n := len(vectors)
	rng := newLCG(seed)
	sampleSize := min(n, kmeansInitSample)
	sample := make([]int, sampleSize)
	for i := range sample {
		sample[i] = rng.nextN(n)
	}

	// Passo 1: escolhe primeiro centroide aleatoriamente dentro da amostra.
	centroids := make([][ivf.Dim]float32, 0, k)
	centroids = append(centroids, vectors[sample[rng.nextN(sampleSize)]])
	minDists := make([]float32, sampleSize)
	for i := range minDists {
		minDists[i] = distSentinel
	}

	// Passo 2: escolhe centroides seguintes com probabilidade proporcional a distancia^2 (kmeans++).
	for len(centroids) < k {
		last := centroids[len(centroids)-1]
		var total float64
		// Atualiza menor distancia de cada ponto para qualquer centroide ja escolhido.
		for i, idx := range sample {
			d := distSqAVX2(&vectors[idx], &last)
			if d < minDists[i] {
				minDists[i] = d
			}
			total += float64(minDists[i])
		}
		if total <= 0 {
			// Caso degenerado: todos os pontos da amostra colapsaram; escolhe fallback aleatorio.
			centroids = append(centroids, vectors[sample[rng.nextN(sampleSize)]])
			continue
		}

		// Sorteio ponderado pelo cumulativo das distancias.
		r := rng.nextF64() * total
		cum := 0.0
		chosen := sampleSize - 1
		for i, d := range minDists {
			cum += float64(d)
			if cum >= r {
				chosen = i
				break
			}
		}
		centroids = append(centroids, vectors[sample[chosen]])
	}

	return centroids
}

// assignParallel distribui a atribuicao de centroides entre workers para reduzir tempo da iteracao.
func assignParallel(vectors [][ivf.Dim]float32, centroids [][ivf.Dim]float32, assignments []uint16) int {
	workers := min(max(runtime.GOMAXPROCS(0), 1), maxAssignWorkers)
	chunk := (len(vectors) + workers - 1) / workers
	var changed int64
	var wg sync.WaitGroup

	for start := 0; start < len(vectors); start += chunk {
		end := min(start+chunk, len(vectors))
		wg.Add(1)
		go func(s, e int) {
			defer wg.Done()
			local := int64(0)
			for i := s; i < e; i++ {
				nb := nearestCentroid(&vectors[i], centroids)
				if nb != assignments[i] {
					assignments[i] = nb
					local++
				}
			}
			// Contador atomico evita lock global em loop quente de atribuicao.
			atomic.AddInt64(&changed, local)
		}(start, end)
	}
	wg.Wait()
	return int(changed)
}

// updateCentroids recalcula centroides como media dos vetores atualmente atribuídos a cada cluster.
func updateCentroids(vectors [][ivf.Dim]float32, assignments []uint16, centroids [][ivf.Dim]float32) {
	k := len(centroids)
	sums := make([][ivf.Dim]float64, k)
	counts := make([]uint32, k)
	for i := range vectors {
		c := int(assignments[i])
		counts[c]++
		for j := range ivf.Dim {
			sums[c][j] += float64(vectors[i][j])
		}
	}
	for c := range k {
		if counts[c] == 0 {
			continue
		}
		inv := 1.0 / float64(counts[c])
		for j := 0; j < ivf.Dim; j++ {
			centroids[c][j] = float32(sums[c][j] * inv)
		}
	}
}

// Train executa kmeans com validacoes, inicializacao kmeans++ e early-stop para custo previsivel.
func Train(vectors [][ivf.Dim]float32, k, iters int, seed uint64) ([][ivf.Dim]float32, []uint16, error) {
	// Passo 0: valida pre-condicoes para evitar treino invalido/corrompido.
	if len(vectors) == 0 {
		return nil, nil, fmt.Errorf("empty vectors")
	}
	if k <= 0 {
		return nil, nil, fmt.Errorf("k must be > 0")
	}
	if k > maxTrainK {
		return nil, nil, fmt.Errorf("k too large: %d (max %d)", k, maxTrainK)
	}
	if k > maxAssignmentID {
		return nil, nil, fmt.Errorf("k exceeds uint16")
	}
	if iters <= 0 {
		return nil, nil, fmt.Errorf("iters must be > 0")
	}

	// Passo 1: inicializa centroides com kmeans++ (melhor convergencia que random puro).
	centroids := kmeansPPInit(vectors, k, seed)

	// Passo 2: inicializa assignments com sentinela para forcar atribuicao na primeira iteracao.
	assignments := make([]uint16, len(vectors))
	for i := range assignments {
		assignments[i] = unassignedAssignment
	}

	// Passo 3: loop principal do kmeans.
	for range iters {
		// 3.1 reatribui cada vetor ao centroide mais proximo.
		changed := assignParallel(vectors, centroids, assignments)
		// 3.2 recalcula centroides pela media dos vetores do cluster.
		updateCentroids(vectors, assignments, centroids)
		// Early-stop quando <0.1% muda de cluster: reduz tempo com perda minima de qualidade.
		if changed*earlyStopScale < len(vectors) {
			break
		}
	}

	return centroids, assignments, nil
}

package api

import (
	"backend/internal/ivf"
	"math"
	"sync"
)

const (
	// maxK limita estruturas estaticas e protege contra indices corrompidos gigantes.
	maxK            = 4096
	bitsetWordShift = 6
	bitsetWordBits  = 64
	bitsetWordMask  = bitsetWordBits - 1
	bitsetWords     = (maxK + bitsetWordMask) / bitsetWordBits

	defaultNProbe = 8
	minNProbe     = 1

	topKNeighbors = 5
	maxFraudCount = topKNeighbors

	// Faixa ambigua de voto que exige varredura completa para melhorar recall.
	fastRefineMinFraudCount = 2
	fastRefineMaxFraudCount = 3

	poolSize8    = 8
	poolSize16   = 16
	poolSize32   = 32
	poolSize64   = 64
	poolSize128  = 128
	poolSize256  = 256
	poolSize512  = 512
	poolSize1024 = 1024
	poolSize2048 = 2048
	poolSize4096 = 4096
)

// topNeighbor guarda candidato de vizinho e metadados para desempate deterministico.
type topNeighbor struct {
	dist   int64
	label  uint8
	origID uint32
}

// sentinelNeighbor inicializa top-5 com distancia maxima para permitir insercao incremental.
var sentinelNeighbor = topNeighbor{dist: math.MaxInt64, label: 0, origID: math.MaxUint32}

// topNCentroidsScratch evita alocacoes recorrentes ao selecionar probes mais proximos.
type topNCentroidsScratch struct {
	ids  []int
	ds   []float32
	pool *sync.Pool
}

func newTopNCentroidsPool(size int) sync.Pool {
	return sync.Pool{
		New: func() any {
			return &topNCentroidsScratch{
				ids: make([]int, size),
				ds:  make([]float32, size),
			}
		},
	}
}

// Pools bucketizados amortizam custo de slices de diferentes tamanhos de nprobe.
var (
	topNCentroidsPool8    = newTopNCentroidsPool(poolSize8)
	topNCentroidsPool16   = newTopNCentroidsPool(poolSize16)
	topNCentroidsPool32   = newTopNCentroidsPool(poolSize32)
	topNCentroidsPool64   = newTopNCentroidsPool(poolSize64)
	topNCentroidsPool128  = newTopNCentroidsPool(poolSize128)
	topNCentroidsPool256  = newTopNCentroidsPool(poolSize256)
	topNCentroidsPool512  = newTopNCentroidsPool(poolSize512)
	topNCentroidsPool1024 = newTopNCentroidsPool(poolSize1024)
	topNCentroidsPool2048 = newTopNCentroidsPool(poolSize2048)
	topNCentroidsPool4096 = newTopNCentroidsPool(poolSize4096)
)

func topNCentroidsPoolFor(n int) (*sync.Pool, int) {
	switch {
	case n <= poolSize8:
		return &topNCentroidsPool8, poolSize8
	case n <= poolSize16:
		return &topNCentroidsPool16, poolSize16
	case n <= poolSize32:
		return &topNCentroidsPool32, poolSize32
	case n <= poolSize64:
		return &topNCentroidsPool64, poolSize64
	case n <= poolSize128:
		return &topNCentroidsPool128, poolSize128
	case n <= poolSize256:
		return &topNCentroidsPool256, poolSize256
	case n <= poolSize512:
		return &topNCentroidsPool512, poolSize512
	case n <= poolSize1024:
		return &topNCentroidsPool1024, poolSize1024
	case n <= poolSize2048:
		return &topNCentroidsPool2048, poolSize2048
	case n <= poolSize4096:
		return &topNCentroidsPool4096, poolSize4096
	default:
		return nil, n
	}
}

func acquireTopNCentroidsScratch(n int) *topNCentroidsScratch {
	pool, size := topNCentroidsPoolFor(n)
	if pool == nil {
		// Fallback sem pool para tamanhos fora dos buckets previstos.
		return &topNCentroidsScratch{
			ids: make([]int, n),
			ds:  make([]float32, n),
		}
	}
	s := pool.Get().(*topNCentroidsScratch)
	s.pool = pool
	s.ids = s.ids[:size]
	s.ds = s.ds[:size]
	return s
}

func releaseTopNCentroidsScratch(s *topNCentroidsScratch) {
	if s == nil || s.pool == nil {
		return
	}
	pool := s.pool
	s.pool = nil
	pool.Put(s)
}

// SearchFraudCount executa busca IVF + refinamento e retorna quantos dos top-5 vizinhos sao fraude.
func SearchFraudCount(query *[ivf.Dim]float32, ds *ivf.Dataset, nprobe int) uint8 {
	// Passo 0: se nao existe dataset valido, nao ha como estimar fraude.
	if ds == nil || ds.K == 0 {
		return 0
	}

	// Passo 1: limita K ao teto suportado pelas estruturas estaticas do hot path.
	k := min(ds.K, maxK)

	// Passo 2: calcula distancia da query para todos os centroides.
	// Esse vetor guia quais clusters serao sondados primeiro.
	var cdists [maxK]float32
	fillCentroidDists(query, ds, cdists[:k])

	// Passo 3: saneia nprobe para o intervalo valido [1, k].
	// nprobe define quantos clusters mais proximos serao avaliados no caminho rapido.
	if nprobe <= 0 {
		nprobe = defaultNProbe
	}
	if nprobe > k {
		nprobe = k
	}
	if nprobe < minNProbe {
		nprobe = minNProbe
	}

	// Passo 4: seleciona os nprobe centroides mais proximos (fast probes).
	// topNScratch vem de pool para evitar alocacao por request.
	fastProbes, topNScratch := topNCentroids(cdists[:k], nprobe)

	// Passo 5: inicializa top-k global de vizinhos (top-5), do pior para o melhor.
	// O sentinel garante que qualquer candidato real entra na primeira comparacao.
	var top5 [topKNeighbors]topNeighbor
	for i := range top5 {
		top5[i] = sentinelNeighbor
	}
	worstIdx := 0

	// Passo 6: prepara estruturas de scan.
	// scanned evita varrer cluster repetido e qI16 coloca query no mesmo dominio quantizado do indice.
	var scanned [bitsetWords]uint64
	var qI16 [ivf.Dim]int16
	for i := range ivf.Dim {
		qI16[i] = quantizeI16(query[i])
	}

	// Passo 7: caminho rapido.
	// Varre apenas os clusters selecionados por nprobe e atualiza top-5 global.
	for _, c := range fastProbes {
		if bitsetGet(&scanned, c) {
			continue
		}
		bitsetSet(&scanned, c)
		scanCluster(c, &qI16, ds, &top5, &worstIdx)
	}

	// Passo 8: voto rapido.
	// Soma quantos labels de fraude apareceram no top-5 atual.
	fastCount := uint8(0)
	for _, n := range top5 {
		fastCount += n.label
	}

	// Heuristica: 0/1/4/5 tende a ser decisao "confiante"; 2/3 aciona refinamento global.
	if fastCount != fastRefineMinFraudCount && fastCount != fastRefineMaxFraudCount {
		releaseTopNCentroidsScratch(topNScratch)
		if fastCount > maxFraudCount {
			return maxFraudCount
		}
		return fastCount
	}

	// Passo 9: refinamento global (apenas se necessario).
	// Para cada cluster restante, aplica lower bound por bounding box.
	// So varre cluster completo quando ainda existe chance de melhorar o pior vizinho atual.
	for c := range k {
		if bitsetGet(&scanned, c) {
			continue
		}
		// Bounding-box lower bound poda clusters incapazes de melhorar o pior vizinho atual.
		lb := bboxLowerBound(&qI16, ds, c)
		if lb <= top5[worstIdx].dist {
			scanCluster(c, &qI16, ds, &top5, &worstIdx)
		}
	}

	// Passo 10: voto final apos refinamento.
	count := uint8(0)
	for _, n := range top5 {
		count += n.label
	}

	// Passo 11: clamp defensivo e limpeza do scratch pool.
	if count > maxFraudCount {
		releaseTopNCentroidsScratch(topNScratch)
		return maxFraudCount
	}
	releaseTopNCentroidsScratch(topNScratch)
	return count
}

// quantizeI16 aplica a mesma quantizacao do indexador para comparar query e vetores no mesmo dominio.
func quantizeI16(x float32) int16 {
	if x > 1 {
		x = 1
	} else if x < -1 {
		x = -1
	}
	s := x * ivf.FixScale
	if s >= 0 {
		s += 0.5
	} else {
		s -= 0.5
	}
	return int16(s)
}

// centroidDist calcula distancia da query para um centroide em layout SoA, usado para escolher probes.
func centroidDist(query *[ivf.Dim]float32, ds *ivf.Dataset, c int) float32 {
	// Dimensao fixa (14) com unroll manual para facilitar vetorizar no backend do compilador.
	kp := ds.KPad
	soa := ds.CentroidsSOA
	base := c

	q0 := query[0]
	q1 := query[1]
	q2 := query[2]
	q3 := query[3]
	q4 := query[4]
	q5 := query[5]
	q6 := query[6]
	q7 := query[7]
	q8 := query[8]
	q9 := query[9]
	q10 := query[10]
	q11 := query[11]
	q12 := query[12]
	q13 := query[13]

	d0 := q0 - soa[base]
	d1 := q1 - soa[kp+base]
	d2 := q2 - soa[2*kp+base]
	d3 := q3 - soa[3*kp+base]
	d4 := q4 - soa[4*kp+base]
	d5 := q5 - soa[5*kp+base]
	d6 := q6 - soa[6*kp+base]
	d7 := q7 - soa[7*kp+base]
	d8 := q8 - soa[8*kp+base]
	d9 := q9 - soa[9*kp+base]
	d10 := q10 - soa[10*kp+base]
	d11 := q11 - soa[11*kp+base]
	d12 := q12 - soa[12*kp+base]
	d13 := q13 - soa[13*kp+base]

	return d0*d0 + d1*d1 + d2*d2 + d3*d3 + d4*d4 + d5*d5 + d6*d6 + d7*d7 + d8*d8 + d9*d9 + d10*d10 + d11*d11 + d12*d12 + d13*d13
}

// centroidDistScalar mantem a implementacao com loop para benchmark A/B.
func centroidDistScalar(query *[ivf.Dim]float32, ds *ivf.Dataset, c int) float32 {
	kp := ds.KPad
	base := c
	var sum float32
	for j := range ivf.Dim {
		d := query[j] - ds.CentroidsSOA[j*kp+base]
		sum += d * d
	}
	return sum
}

// fillCentroidDists calcula distancias para todos os centroides em blocos de 8 para reduzir overhead do loop hot-path.
func fillCentroidDists(query *[ivf.Dim]float32, ds *ivf.Dataset, out []float32) {
	k := len(out)
	if k == 0 {
		return
	}
	kp := ds.KPad
	soa := ds.CentroidsSOA

	i := 0
	// Processa 8 centroides por iteracao para reduzir overhead de indice/branch no loop interno.
	for ; i+7 < k; i += 8 {
		var s0, s1, s2, s3, s4, s5, s6, s7 float32
		for j := range ivf.Dim {
			qj := query[j]
			off := j*kp + i

			d0 := qj - soa[off]
			d1 := qj - soa[off+1]
			d2 := qj - soa[off+2]
			d3 := qj - soa[off+3]
			d4 := qj - soa[off+4]
			d5 := qj - soa[off+5]
			d6 := qj - soa[off+6]
			d7 := qj - soa[off+7]

			s0 += d0 * d0
			s1 += d1 * d1
			s2 += d2 * d2
			s3 += d3 * d3
			s4 += d4 * d4
			s5 += d5 * d5
			s6 += d6 * d6
			s7 += d7 * d7
		}
		out[i] = s0
		out[i+1] = s1
		out[i+2] = s2
		out[i+3] = s3
		out[i+4] = s4
		out[i+5] = s5
		out[i+6] = s6
		out[i+7] = s7
	}
	for ; i < k; i++ {
		out[i] = centroidDist(query, ds, i)
	}
}

// fillCentroidDistsScalar mantem caminho de referencia para benchmark e validacao de paridade.
func fillCentroidDistsScalar(query *[ivf.Dim]float32, ds *ivf.Dataset, out []float32) {
	for i := range len(out) {
		out[i] = centroidDistScalar(query, ds, i)
	}
}

// topNCentroids seleciona os N menores valores de distancia com scratch reutilizavel para reduzir GC.
func topNCentroids(dists []float32, n int) ([]int, *topNCentroidsScratch) {
	scratch := acquireTopNCentroidsScratch(n)
	ids := scratch.ids[:n]
	ds := scratch.ds[:n]
	for i := range n {
		ds[i] = float32(math.Inf(1))
	}
	for i, d := range dists {
		// Vetor ds fica ordenado de menor para maior distancia.
		if d >= ds[n-1] {
			continue
		}
		pos := n - 1
		for pos > 0 && d < ds[pos-1] {
			pos--
		}
		for j := n - 1; j > pos; j-- {
			ds[j] = ds[j-1]
			ids[j] = ids[j-1]
		}
		ds[pos] = d
		ids[pos] = i
	}
	return ids, scratch
}

// topNCentroidsAlloc mantem a versao com alocacao para benchmark A/B.
func topNCentroidsAlloc(dists []float32, n int) []int {
	ids := make([]int, n)
	ds := make([]float32, n)
	for i := range n {
		ds[i] = float32(math.Inf(1))
	}
	for i, d := range dists {
		if d >= ds[n-1] {
			continue
		}
		pos := n - 1
		for pos > 0 && d < ds[pos-1] {
			pos--
		}
		for j := n - 1; j > pos; j-- {
			ds[j] = ds[j-1]
			ids[j] = ids[j-1]
		}
		ds[pos] = d
		ids[pos] = i
	}
	return ids
}

// scanCluster percorre um cluster quantizado e tenta atualizar os 5 melhores candidatos globais.
func scanCluster(c int, q *[ivf.Dim]int16, ds *ivf.Dataset, top5 *[topKNeighbors]topNeighbor, worstIdx *int) {
	// Passo 0: resolve fatia [start,end) do cluster no vetor global e sai cedo se vazio.
	start := int(ds.Offsets[c])
	end := int(ds.Offsets[c+1])
	if start >= end {
		return
	}

	// Passo 1: converte limites para representacao em blocos compactados.
	blockStart := int(ds.BlockOffsets[c])
	blockEnd := int(ds.BlockOffsets[c+1])
	clusterSize := end - start
	blocks := ds.Blocks

	// Passo 2: carrega query quantizada em registradores locais para reduzir loads repetidos.
	q0 := int32(q[0])
	q1 := int32(q[1])
	q2 := int32(q[2])
	q3 := int32(q[3])
	q4 := int32(q[4])
	q5 := int32(q[5])
	q6 := int32(q[6])
	q7 := int32(q[7])
	q8 := int32(q[8])
	q9 := int32(q[9])
	q10 := int32(q[10])
	q11 := int32(q[11])
	q12 := int32(q[12])
	q13 := int32(q[13])

	// Passo 3: varre bloco a bloco do cluster.
	for blockLocal, blockIdx := 0, blockStart; blockIdx < blockEnd; blockLocal, blockIdx = blockLocal+1, blockIdx+1 {
		blockBase := blockIdx * ivf.BlockStride
		lanesInBlock := min(clusterSize-blockLocal*ivf.BlockSize, ivf.BlockSize)

		// top5[0] guarda o pior candidato atual; usar esse limite acelera poda.
		worst := top5[0].dist

		// Passo 4: varre cada lane (vetor) do bloco atual.
		for lane := range lanesInBlock {
			var dist int64
			base := blockBase + lane

			// Passo 4.1: acumula distancia em estagios e poda cedo quando parcial ja pior que "worst".
			// Acumulacao em estagios: poda cedo quando parcial ja ultrapassa o pior atual.
			diff0 := q0 - int32(blocks[base])
			diff1 := q1 - int32(blocks[base+ivf.BlockSize])
			diff2 := q2 - int32(blocks[base+2*ivf.BlockSize])
			diff3 := q3 - int32(blocks[base+3*ivf.BlockSize])
			dist = int64(diff0*diff0) + int64(diff1*diff1) + int64(diff2*diff2) + int64(diff3*diff3)
			if dist > worst {
				continue
			}

			diff4 := q4 - int32(blocks[base+4*ivf.BlockSize])
			diff5 := q5 - int32(blocks[base+5*ivf.BlockSize])
			diff6 := q6 - int32(blocks[base+6*ivf.BlockSize])
			diff7 := q7 - int32(blocks[base+7*ivf.BlockSize])
			dist += int64(diff4*diff4) + int64(diff5*diff5) + int64(diff6*diff6) + int64(diff7*diff7)
			if dist > worst {
				continue
			}

			diff8 := q8 - int32(blocks[base+8*ivf.BlockSize])
			diff9 := q9 - int32(blocks[base+9*ivf.BlockSize])
			diff10 := q10 - int32(blocks[base+10*ivf.BlockSize])
			diff11 := q11 - int32(blocks[base+11*ivf.BlockSize])
			dist += int64(diff8*diff8) + int64(diff9*diff9) + int64(diff10*diff10) + int64(diff11*diff11)
			if dist > worst {
				continue
			}

			diff12 := q12 - int32(blocks[base+12*ivf.BlockSize])
			diff13 := q13 - int32(blocks[base+13*ivf.BlockSize])
			dist += int64(diff12*diff12) + int64(diff13*diff13)
			if dist > worst {
				continue
			}

			// Passo 4.2: candidata vetor no top-5 global e atualiza pior limite para a proxima lane.
			global := start + blockLocal*ivf.BlockSize + lane
			tryInsertTop5(top5, worstIdx, dist, ds.Labels[global], ds.OrigIDs[global])
			worst = top5[0].dist
		}
	}
}

// scanClusterLegacy mantem a versao anterior para benchmark A/B.
func scanClusterLegacy(c int, q *[ivf.Dim]int16, ds *ivf.Dataset, top5 *[topKNeighbors]topNeighbor, worstIdx *int) {
	start := int(ds.Offsets[c])
	end := int(ds.Offsets[c+1])
	if start >= end {
		return
	}
	blockStart := int(ds.BlockOffsets[c])
	blockEnd := int(ds.BlockOffsets[c+1])
	clusterSize := end - start
	blocks := ds.Blocks

	q0 := int32(q[0])
	q1 := int32(q[1])
	q2 := int32(q[2])
	q3 := int32(q[3])
	q4 := int32(q[4])
	q5 := int32(q[5])
	q6 := int32(q[6])
	q7 := int32(q[7])
	q8 := int32(q[8])
	q9 := int32(q[9])
	q10 := int32(q[10])
	q11 := int32(q[11])
	q12 := int32(q[12])
	q13 := int32(q[13])

	for blockLocal, blockIdx := 0, blockStart; blockIdx < blockEnd; blockLocal, blockIdx = blockLocal+1, blockIdx+1 {
		blockBase := blockIdx * ivf.BlockStride
		lanesInBlock := min(clusterSize-blockLocal*ivf.BlockSize, ivf.BlockSize)
		for lane := range lanesInBlock {
			worst := top5[*worstIdx].dist
			base := blockBase + lane
			var dist int64

			diff0 := q0 - int32(blocks[base])
			diff1 := q1 - int32(blocks[base+ivf.BlockSize])
			diff2 := q2 - int32(blocks[base+2*ivf.BlockSize])
			diff3 := q3 - int32(blocks[base+3*ivf.BlockSize])
			dist = int64(diff0*diff0) + int64(diff1*diff1) + int64(diff2*diff2) + int64(diff3*diff3)
			if dist > worst {
				continue
			}

			diff4 := q4 - int32(blocks[base+4*ivf.BlockSize])
			diff5 := q5 - int32(blocks[base+5*ivf.BlockSize])
			diff6 := q6 - int32(blocks[base+6*ivf.BlockSize])
			diff7 := q7 - int32(blocks[base+7*ivf.BlockSize])
			dist += int64(diff4*diff4) + int64(diff5*diff5) + int64(diff6*diff6) + int64(diff7*diff7)
			if dist > worst {
				continue
			}

			diff8 := q8 - int32(blocks[base+8*ivf.BlockSize])
			diff9 := q9 - int32(blocks[base+9*ivf.BlockSize])
			diff10 := q10 - int32(blocks[base+10*ivf.BlockSize])
			diff11 := q11 - int32(blocks[base+11*ivf.BlockSize])
			dist += int64(diff8*diff8) + int64(diff9*diff9) + int64(diff10*diff10) + int64(diff11*diff11)
			if dist > worst {
				continue
			}

			diff12 := q12 - int32(blocks[base+12*ivf.BlockSize])
			diff13 := q13 - int32(blocks[base+13*ivf.BlockSize])
			dist += int64(diff12*diff12) + int64(diff13*diff13)

			global := start + blockLocal*ivf.BlockSize + lane
			tryInsertTop5(top5, worstIdx, dist, ds.Labels[global], ds.OrigIDs[global])
		}
	}
}

// scanClusterScalar mantem a implementacao com loop para benchmark A/B.
func scanClusterScalar(c int, q *[ivf.Dim]int16, ds *ivf.Dataset, top5 *[topKNeighbors]topNeighbor, worstIdx *int) {
	start := int(ds.Offsets[c])
	end := int(ds.Offsets[c+1])
	if start >= end {
		return
	}
	blockStart := int(ds.BlockOffsets[c])
	blockEnd := int(ds.BlockOffsets[c+1])
	clusterSize := end - start

	for blockLocal, blockIdx := 0, blockStart; blockIdx < blockEnd; blockLocal, blockIdx = blockLocal+1, blockIdx+1 {
		blockBase := blockIdx * ivf.BlockStride
		lanesInBlock := min(clusterSize-blockLocal*ivf.BlockSize, ivf.BlockSize)
		for lane := range lanesInBlock {
			var dist int64
			worst := top5[*worstIdx].dist
			for j := range ivf.Dim {
				qv := int32(q[j])
				v := int32(ds.Blocks[blockBase+j*ivf.BlockSize+lane])
				diff := qv - v
				dist += int64(diff * diff)
				if dist > worst {
					break
				}
			}
			global := start + blockLocal*ivf.BlockSize + lane
			tryInsertTop5(top5, worstIdx, dist, ds.Labels[global], ds.OrigIDs[global])
		}
	}
}

// tryInsertTop5 insere candidato melhor e recalcula o pior slot atual para manter poda eficiente.
func tryInsertTop5(top5 *[topKNeighbors]topNeighbor, worstIdx *int, d int64, label uint8, origID uint32) {
	candidate := topNeighbor{dist: d, label: label, origID: origID}
	better := candidate.dist < top5[0].dist || (candidate.dist == top5[0].dist && candidate.origID < top5[0].origID)
	if !better {
		return
	}
	// Mantem top5 ordenado por "pior para melhor" para que o pior fique sempre em top5[0].
	top5[0] = candidate
	for i := 0; i < topKNeighbors-1; i++ {
		a := top5[i]
		b := top5[i+1]
		aIsBetter := a.dist < b.dist || (a.dist == b.dist && a.origID < b.origID)
		if !aIsBetter {
			break
		}
		top5[i], top5[i+1] = top5[i+1], top5[i]
	}
	*worstIdx = 0
}

// tryInsertTop5Legacy mantem a versao anterior para benchmark e validacao de paridade.
func tryInsertTop5Legacy(top5 *[topKNeighbors]topNeighbor, worstIdx *int, d int64, label uint8, origID uint32) {
	worst := top5[*worstIdx]
	better := d < worst.dist || (d == worst.dist && origID < worst.origID)
	if !better {
		return
	}
	top5[*worstIdx] = topNeighbor{dist: d, label: label, origID: origID}

	wi := 0
	for i := 1; i < topKNeighbors; i++ {
		a := top5[i]
		b := top5[wi]
		if a.dist > b.dist || (a.dist == b.dist && a.origID > b.origID) {
			wi = i
		}
	}
	*worstIdx = wi
}

// bboxLowerBound calcula limite inferior por bounding box para decidir se um cluster merece varredura completa.
func bboxLowerBound(q *[ivf.Dim]int16, ds *ivf.Dataset, c int) int64 {
	base := c * ivf.Dim
	mins := ds.BBoxMin
	maxs := ds.BBoxMax

	q0 := int32(q[0])
	q1 := int32(q[1])
	q2 := int32(q[2])
	q3 := int32(q[3])
	q4 := int32(q[4])
	q5 := int32(q[5])
	q6 := int32(q[6])
	q7 := int32(q[7])
	q8 := int32(q[8])
	q9 := int32(q[9])
	q10 := int32(q[10])
	q11 := int32(q[11])
	q12 := int32(q[12])
	q13 := int32(q[13])

	var d0 int32
	lo0 := int32(mins[base])
	hi0 := int32(maxs[base])
	if q0 < lo0 {
		d0 = lo0 - q0
	} else if q0 > hi0 {
		d0 = q0 - hi0
	}
	var d1 int32
	lo1 := int32(mins[base+1])
	hi1 := int32(maxs[base+1])
	if q1 < lo1 {
		d1 = lo1 - q1
	} else if q1 > hi1 {
		d1 = q1 - hi1
	}
	var d2 int32
	lo2 := int32(mins[base+2])
	hi2 := int32(maxs[base+2])
	if q2 < lo2 {
		d2 = lo2 - q2
	} else if q2 > hi2 {
		d2 = q2 - hi2
	}
	var d3 int32
	lo3 := int32(mins[base+3])
	hi3 := int32(maxs[base+3])
	if q3 < lo3 {
		d3 = lo3 - q3
	} else if q3 > hi3 {
		d3 = q3 - hi3
	}
	var d4 int32
	lo4 := int32(mins[base+4])
	hi4 := int32(maxs[base+4])
	if q4 < lo4 {
		d4 = lo4 - q4
	} else if q4 > hi4 {
		d4 = q4 - hi4
	}
	var d5 int32
	lo5 := int32(mins[base+5])
	hi5 := int32(maxs[base+5])
	if q5 < lo5 {
		d5 = lo5 - q5
	} else if q5 > hi5 {
		d5 = q5 - hi5
	}
	var d6 int32
	lo6 := int32(mins[base+6])
	hi6 := int32(maxs[base+6])
	if q6 < lo6 {
		d6 = lo6 - q6
	} else if q6 > hi6 {
		d6 = q6 - hi6
	}
	var d7 int32
	lo7 := int32(mins[base+7])
	hi7 := int32(maxs[base+7])
	if q7 < lo7 {
		d7 = lo7 - q7
	} else if q7 > hi7 {
		d7 = q7 - hi7
	}
	var d8 int32
	lo8 := int32(mins[base+8])
	hi8 := int32(maxs[base+8])
	if q8 < lo8 {
		d8 = lo8 - q8
	} else if q8 > hi8 {
		d8 = q8 - hi8
	}
	var d9 int32
	lo9 := int32(mins[base+9])
	hi9 := int32(maxs[base+9])
	if q9 < lo9 {
		d9 = lo9 - q9
	} else if q9 > hi9 {
		d9 = q9 - hi9
	}
	var d10 int32
	lo10 := int32(mins[base+10])
	hi10 := int32(maxs[base+10])
	if q10 < lo10 {
		d10 = lo10 - q10
	} else if q10 > hi10 {
		d10 = q10 - hi10
	}
	var d11 int32
	lo11 := int32(mins[base+11])
	hi11 := int32(maxs[base+11])
	if q11 < lo11 {
		d11 = lo11 - q11
	} else if q11 > hi11 {
		d11 = q11 - hi11
	}
	var d12 int32
	lo12 := int32(mins[base+12])
	hi12 := int32(maxs[base+12])
	if q12 < lo12 {
		d12 = lo12 - q12
	} else if q12 > hi12 {
		d12 = q12 - hi12
	}
	var d13 int32
	lo13 := int32(mins[base+13])
	hi13 := int32(maxs[base+13])
	if q13 < lo13 {
		d13 = lo13 - q13
	} else if q13 > hi13 {
		d13 = q13 - hi13
	}

	return int64(d0*d0) + int64(d1*d1) + int64(d2*d2) + int64(d3*d3) +
		int64(d4*d4) + int64(d5*d5) + int64(d6*d6) + int64(d7*d7) +
		int64(d8*d8) + int64(d9*d9) + int64(d10*d10) + int64(d11*d11) +
		int64(d12*d12) + int64(d13*d13)
}

// bboxLowerBoundScalar mantem implementacao com loop para benchmark A/B.
func bboxLowerBoundScalar(q *[ivf.Dim]int16, ds *ivf.Dataset, c int) int64 {
	base := c * ivf.Dim
	var sum int64
	for j := range ivf.Dim {
		qv := int32(q[j])
		lo := int32(ds.BBoxMin[base+j])
		hi := int32(ds.BBoxMax[base+j])
		var diff int32
		if qv < lo {
			diff = lo - qv
		} else if qv > hi {
			diff = qv - hi
		}
		sum += int64(diff) * int64(diff)
	}
	return sum
}

// bitsetSet marca cluster ja processado para evitar scan duplicado.
func bitsetSet(bs *[bitsetWords]uint64, i int) {
	bs[i>>bitsetWordShift] |= uint64(1) << (i & bitsetWordMask)
}

// bitsetGet consulta se cluster ja foi processado.
func bitsetGet(bs *[bitsetWords]uint64, i int) bool {
	return (bs[i>>bitsetWordShift]>>(i&bitsetWordMask))&1 == 1
}

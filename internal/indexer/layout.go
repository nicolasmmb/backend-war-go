package indexer

import (
	"fmt"

	"backend/internal/ivf"
)

type Layout struct {
	// N/K repetem metadados do dataset original para validação e leitura no runtime.
	N int
	K int
	// Centroids permanece em AoS para persistencia simples; API converte para SoA ao carregar.
	Centroids []float32
	// BBox auxilia poda por lower-bound antes de varrer cluster inteiro.
	BBoxMin []int16
	BBoxMax []int16
	// Offsets indicam recorte [start,end) de vetores por cluster.
	Offsets []uint32
	// BlockOffsets mapeia cluster -> faixa de blocos compactados.
	BlockOffsets []uint32
	// Blocks guarda vetores quantizados em layout bloco/feature/lane.
	Blocks []int16
	// Labels/OrigIDs preservam classe e id original para voto + desempate deterministico.
	Labels  []uint8
	OrigIDs []uint32
}

// quantize limita feature em [-1,1] e converte para int16 mantendo escala fixa do formato IVF.
func quantize(x float32) int16 {
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

// BuildLayout reorganiza vetores por cluster e monta buffers compactos usados na busca IVF online.
func BuildLayout(vectors [][ivf.Dim]float32, labelsIn []uint8, centroidsIn [][ivf.Dim]float32, assignments []uint16) (*Layout, error) {
	// Passo 0: valida consistencia basica das entradas.
	n := len(vectors)
	k := len(centroidsIn)
	if len(labelsIn) != n {
		return nil, fmt.Errorf("labels length mismatch")
	}
	if len(assignments) != n {
		return nil, fmt.Errorf("assignments length mismatch")
	}

	// Passo 1: conta quantos vetores cada cluster recebeu.
	counts := make([]uint32, k)
	for _, a := range assignments {
		counts[int(a)]++
	}

	// Passo 2: monta offsets cumulativos para mapear cluster -> faixa [start,end) em Labels/OrigIDs.
	offsets := make([]uint32, k+1)
	for c := 0; c < k; c++ {
		offsets[c+1] = offsets[c] + counts[c]
	}
	if int(offsets[k]) != n {
		return nil, fmt.Errorf("offset sum mismatch")
	}

	// Passo 3: calcula quantidade de blocos por cluster e offsets desses blocos.
	blockOffsets := make([]uint32, k+1)
	for c := 0; c < k; c++ {
		// Cada cluster vira ceil(count/BlockSize) blocos para manter stride fixo no scan.
		blocksC := (int(counts[c]) + ivf.BlockSize - 1) / ivf.BlockSize
		blockOffsets[c+1] = blockOffsets[c] + uint32(blocksC)
	}
	totalBlocks := int(blockOffsets[k])

	// Passo 4: aloca buffers finais do layout.
	blocks := make([]int16, totalBlocks*ivf.BlockStride)
	for i := range blocks {
		blocks[i] = ivf.PadValue
	}
	labels := make([]uint8, n)
	origIDs := make([]uint32, n)

	// Passo 5: inicia bbox de cada cluster/dimensao com extremos opostos.
	bboxMin := make([]int16, k*ivf.Dim)
	bboxMax := make([]int16, k*ivf.Dim)
	for i := range bboxMin {
		bboxMin[i] = int16(0x7fff)
		bboxMax[i] = int16(-0x8000)
	}

	writePos := make([]uint32, k)
	copy(writePos, offsets[:k])

	// Passo 6: escreve vetores por cluster no layout em blocos e atualiza bbox.
	for i, v := range vectors {
		c := int(assignments[i])
		pos := int(writePos[c])
		writePos[c]++
		labels[pos] = labelsIn[i]
		origIDs[pos] = uint32(i)

		posInCluster := pos - int(offsets[c])
		blockLocal := posInCluster / ivf.BlockSize
		lane := posInCluster % ivf.BlockSize
		blockGlobal := int(blockOffsets[c]) + blockLocal
		blockBase := blockGlobal * ivf.BlockStride

		for j := 0; j < ivf.Dim; j++ {
			q := quantize(v[j])
			// SoA intra-bloco: mesma feature fica contigua para todas as lanes.
			blocks[blockBase+j*ivf.BlockSize+lane] = q
			bi := c*ivf.Dim + j
			if q < bboxMin[bi] {
				bboxMin[bi] = q
			}
			if q > bboxMax[bi] {
				bboxMax[bi] = q
			}
		}
	}

	// Passo 7: normaliza bbox de clusters vazios para evitar lixo numérico.
	for c := 0; c < k; c++ {
		if counts[c] != 0 {
			continue
		}
		for j := 0; j < ivf.Dim; j++ {
			bboxMin[c*ivf.Dim+j] = 0
			bboxMax[c*ivf.Dim+j] = 0
		}
	}

	// Passo 8: flatten dos centroides para buffer AoS persistido no arquivo.
	centroids := make([]float32, k*ivf.Dim)
	for c := 0; c < k; c++ {
		for j := 0; j < ivf.Dim; j++ {
			centroids[c*ivf.Dim+j] = centroidsIn[c][j]
		}
	}

	// Passo 9: retorna layout final pronto para serializacao.
	return &Layout{
		N:            n,
		K:            k,
		Centroids:    centroids,
		BBoxMin:      bboxMin,
		BBoxMax:      bboxMax,
		Offsets:      offsets,
		BlockOffsets: blockOffsets,
		Blocks:       blocks,
		Labels:       labels,
		OrigIDs:      origIDs,
	}, nil
}

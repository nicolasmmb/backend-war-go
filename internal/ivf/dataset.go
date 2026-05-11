package ivf

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"syscall"
	"unsafe"
)

type Dataset struct {
	// Cabecalho logico do indice.
	N     int
	K     int
	KPad  int
	Scale float32
	// Estruturas principais usadas na busca.
	Centroids    []float32
	CentroidsSOA []float32
	BBoxMin      []int16
	BBoxMax      []int16
	Offsets      []uint32
	BlockOffsets []uint32
	Blocks       []int16
	Labels       []uint8
	OrigIDs      []uint32

	// backing/mmapRef mantem referencia viva do buffer base para slices reinterpretados.
	backing []byte
	mmapRef []byte
}

// Load abre e valida o index RIVF; usa mmap para reduzir overhead de leitura quando habilitado.
func Load(path string, useMMap bool) (*Dataset, error) {
	// Passo 0: abre arquivo e valida tamanho minimo.
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() == 0 {
		return nil, fmt.Errorf("empty index file")
	}

	var data []byte
	var mmapRef []byte
	// Passo 1: tenta mmap quando habilitado; se falhar, faz fallback para leitura tradicional.
	if useMMap {
		mapped, mmapErr := syscall.Mmap(int(f.Fd()), 0, int(st.Size()), syscall.PROT_READ, syscall.MAP_PRIVATE)
		if mmapErr == nil {
			data = mapped
			mmapRef = mapped
		} else {
			// Fallback garante robustez em ambientes sem suporte/limite de mmap.
			data, err = os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("mmap failed: %v; read fallback failed: %w", mmapErr, err)
			}
		}
	} else {
		data, err = os.ReadFile(path)
		if err != nil {
			return nil, err
		}
	}

	// Passo 2: parseia estrutura binaria para views tipadas.
	ds, err := parse(data)
	if err != nil {
		if len(mmapRef) > 0 {
			_ = syscall.Munmap(mmapRef)
		}
		return nil, err
	}

	// Passo 3: preserva referencias do backing para manter slices validos ao longo da vida do dataset.
	ds.backing = data
	ds.mmapRef = mmapRef
	return ds, nil
}

// Close libera o mapeamento de memoria quando o dataset foi carregado via mmap.
func (d *Dataset) Close() error {
	if len(d.mmapRef) == 0 {
		return nil
	}
	err := syscall.Munmap(d.mmapRef)
	d.mmapRef = nil
	return err
}

// parse interpreta o buffer binario no layout RIVF v3 e monta visoes tipadas sem copias desnecessarias.
func parse(bytes []byte) (*Dataset, error) {
	// Passo 0: valida header fixo (tamanho, magic, versao, dimensao).
	if len(bytes) < HeaderSize {
		return nil, fmt.Errorf("index smaller than header")
	}
	if string(bytes[0:4]) != Magic {
		return nil, fmt.Errorf("bad magic")
	}
	version := binary.LittleEndian.Uint32(bytes[4:8])
	if version != Version {
		return nil, fmt.Errorf("bad version: %d", version)
	}
	n := int(binary.LittleEndian.Uint32(bytes[8:12]))
	k := int(binary.LittleEndian.Uint32(bytes[12:16]))
	dim := int(binary.LittleEndian.Uint32(bytes[16:20]))
	if dim != Dim {
		return nil, fmt.Errorf("dim mismatch: got %d want %d", dim, Dim)
	}
	scale := math.Float32frombits(binary.LittleEndian.Uint32(bytes[20:24]))

	// Passo 1: inicia cursor apos header e le blocos na ordem canonica do formato.
	off := HeaderSize

	// 1.1 centroides (float32).
	centroidsBytes := k * Dim * 4
	centroidsRaw, err := take(bytes, &off, centroidsBytes)
	if err != nil {
		return nil, err
	}
	centroids, err := bytesToF32(centroidsRaw, k*Dim)
	if err != nil {
		return nil, err
	}

	// 1.2 bounding boxes min/max (int16).
	bboxBytes := k * Dim * 2
	bboxMinRaw, err := take(bytes, &off, bboxBytes)
	if err != nil {
		return nil, err
	}
	bboxMin, err := bytesToI16(bboxMinRaw, k*Dim)
	if err != nil {
		return nil, err
	}

	bboxMaxRaw, err := take(bytes, &off, bboxBytes)
	if err != nil {
		return nil, err
	}
	bboxMax, err := bytesToI16(bboxMaxRaw, k*Dim)
	if err != nil {
		return nil, err
	}

	// 1.3 offsets por cluster e offsets de blocos.
	offsetsBytes := (k + 1) * 4
	offsetsRaw, err := take(bytes, &off, offsetsBytes)
	if err != nil {
		return nil, err
	}
	offsets, err := bytesToU32(offsetsRaw, k+1)
	if err != nil {
		return nil, err
	}

	blockOffsetsRaw, err := take(bytes, &off, offsetsBytes)
	if err != nil {
		return nil, err
	}
	blockOffsets, err := bytesToU32(blockOffsetsRaw, k+1)
	if err != nil {
		return nil, err
	}
	if len(blockOffsets) == 0 {
		return nil, fmt.Errorf("invalid block offsets")
	}

	// 1.4 blocos quantizados, labels e ids originais.
	totalBlocks := int(blockOffsets[k])
	blocksCount := totalBlocks * BlockStride
	blocksBytes := blocksCount * 2
	blocksRaw, err := take(bytes, &off, blocksBytes)
	if err != nil {
		return nil, err
	}
	blocks, err := bytesToI16(blocksRaw, blocksCount)
	if err != nil {
		return nil, err
	}

	labels, err := take(bytes, &off, n)
	if err != nil {
		return nil, fmt.Errorf("index truncated at labels: %w", err)
	}

	origIDsBytes := n * 4
	origIDsRaw, err := take(bytes, &off, origIDsBytes)
	if err != nil {
		return nil, err
	}
	origIDs, err := bytesToU32(origIDsRaw, n)
	if err != nil {
		return nil, err
	}

	// Passo 2: validacoes finais de integridade do arquivo.
	if off != len(bytes) {
		return nil, fmt.Errorf("unexpected trailing data")
	}
	if int(offsets[k]) != n {
		return nil, fmt.Errorf("offsets[k] != n")
	}

	// Passo 3: precomputa centroides em SoA com padding para acelerar distancias no hot path.
	kPad := alignToBlockSize(k)
	// SoA + padding em multiplo de 8 simplifica loops unrolled no caminho de consulta.
	centroidsSOA := make([]float32, Dim*kPad)
	for c := 0; c < k; c++ {
		for j := 0; j < Dim; j++ {
			centroidsSOA[j*kPad+c] = centroids[c*Dim+j]
		}
	}
	for c := k; c < kPad; c++ {
		for j := 0; j < Dim; j++ {
			centroidsSOA[j*kPad+c] = float32(math.Inf(1))
		}
	}

	// Passo 4: retorna dataset com slices apontando para o buffer parseado.
	return &Dataset{
		N:            n,
		K:            k,
		KPad:         kPad,
		Scale:        scale,
		Centroids:    centroids,
		CentroidsSOA: centroidsSOA,
		BBoxMin:      bboxMin,
		BBoxMax:      bboxMax,
		Offsets:      offsets,
		BlockOffsets: blockOffsets,
		Blocks:       blocks,
		Labels:       labels,
		OrigIDs:      origIDs,
	}, nil
}

func alignToBlockSize(n int) int {
	return (n + BlockSize - 1) &^ (BlockSize - 1)
}

// bytesToF32 converte uma janela de bytes little-endian para []float32 com fallback seguro para desalinhamento.
func bytesToF32(b []byte, n int) ([]float32, error) {
	if len(b) != n*4 {
		return nil, fmt.Errorf("float32 slice size mismatch")
	}
	if n == 0 {
		return []float32{}, nil
	}
	base := unsafe.Pointer(unsafe.SliceData(b))
	if uintptr(base)%4 == 0 {
		return unsafe.Slice((*float32)(base), n), nil
	}
	out := make([]float32, n)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out, nil
}

// bytesToI16 converte uma janela de bytes little-endian para []int16 com fallback seguro para desalinhamento.
func bytesToI16(b []byte, n int) ([]int16, error) {
	if len(b) != n*2 {
		return nil, fmt.Errorf("int16 slice size mismatch")
	}
	if n == 0 {
		return []int16{}, nil
	}
	base := unsafe.Pointer(unsafe.SliceData(b))
	if uintptr(base)%2 == 0 {
		return unsafe.Slice((*int16)(base), n), nil
	}
	out := make([]int16, n)
	for i := range out {
		out[i] = int16(binary.LittleEndian.Uint16(b[i*2:]))
	}
	return out, nil
}

// bytesToU32 converte uma janela de bytes little-endian para []uint32 com fallback seguro para desalinhamento.
func bytesToU32(b []byte, n int) ([]uint32, error) {
	if len(b) != n*4 {
		return nil, fmt.Errorf("uint32 slice size mismatch")
	}
	if n == 0 {
		return []uint32{}, nil
	}
	base := unsafe.Pointer(unsafe.SliceData(b))
	if uintptr(base)%4 == 0 {
		return unsafe.Slice((*uint32)(base), n), nil
	}
	out := make([]uint32, n)
	for i := range out {
		out[i] = binary.LittleEndian.Uint32(b[i*4:])
	}
	return out, nil
}

// take avanca um cursor de leitura sobre o buffer garantindo limite para evitar acesso fora do arquivo.
func take(data []byte, off *int, size int) ([]byte, error) {
	if size < 0 || *off < 0 || *off+size > len(data) {
		return nil, fmt.Errorf("index truncated")
	}
	start := *off
	*off += size
	return data[start:*off], nil
}

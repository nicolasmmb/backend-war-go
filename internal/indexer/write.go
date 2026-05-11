package indexer

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"math"
	"os"

	"backend/internal/ivf"
)

const (
	indexWriteBufferSize = 1 << 20

	// Campo reservado no header para extensoes futuras mantendo compatibilidade.
	indexHeaderReservedBytes = 8

	bytesPerU16 = 2
	bytesPerU32 = 4
	bytesPerF32 = 4
)

// WriteIndex serializa o Layout para o formato binario IVF v3 lido pela API em runtime.
func WriteIndex(path string, layout *Layout) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriterSize(f, indexWriteBufferSize)
	defer w.Flush()

	if _, err := w.Write([]byte(ivf.Magic)); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, ivf.Version); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(layout.N)); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(layout.K)); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(ivf.Dim)); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, math.Float32bits(ivf.FixScale)); err != nil {
		return err
	}
	// Reserva futura para extensoes de header sem quebrar alinhamento.
	if _, err := w.Write(make([]byte, indexHeaderReservedBytes)); err != nil {
		return err
	}

	for _, v := range layout.Centroids {
		if err := binary.Write(w, binary.LittleEndian, math.Float32bits(v)); err != nil {
			return err
		}
	}
	for _, v := range layout.BBoxMin {
		if err := binary.Write(w, binary.LittleEndian, uint16(v)); err != nil {
			return err
		}
	}
	for _, v := range layout.BBoxMax {
		if err := binary.Write(w, binary.LittleEndian, uint16(v)); err != nil {
			return err
		}
	}
	for _, v := range layout.Offsets {
		if err := binary.Write(w, binary.LittleEndian, v); err != nil {
			return err
		}
	}
	for _, v := range layout.BlockOffsets {
		if err := binary.Write(w, binary.LittleEndian, v); err != nil {
			return err
		}
	}
	for _, v := range layout.Blocks {
		if err := binary.Write(w, binary.LittleEndian, uint16(v)); err != nil {
			return err
		}
	}
	if _, err := w.Write(layout.Labels); err != nil {
		return err
	}
	for _, v := range layout.OrigIDs {
		if err := binary.Write(w, binary.LittleEndian, v); err != nil {
			return err
		}
	}

	if err := w.Flush(); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	return nil
}

// EstimateSizeBytes estima tamanho final do arquivo para telemetria e planejamento de memoria/disco.
func EstimateSizeBytes(layout *Layout) int64 {
	if layout == nil {
		return 0
	}
	total := 0
	total += ivf.HeaderSize
	total += len(layout.Centroids) * bytesPerF32
	total += len(layout.BBoxMin) * bytesPerU16
	total += len(layout.BBoxMax) * bytesPerU16
	total += len(layout.Offsets) * bytesPerU32
	total += len(layout.BlockOffsets) * bytesPerU32
	total += len(layout.Blocks) * bytesPerU16
	total += len(layout.Labels)
	total += len(layout.OrigIDs) * bytesPerU32
	return int64(total)
}

// ValidateLayout checa consistencia estrutural antes de gravar o arquivo para evitar indice corrompido.
func ValidateLayout(layout *Layout) error {
	if layout == nil {
		return fmt.Errorf("layout is nil")
	}
	if len(layout.Offsets) != layout.K+1 {
		return fmt.Errorf("offsets length mismatch")
	}
	if len(layout.BlockOffsets) != layout.K+1 {
		return fmt.Errorf("block offsets length mismatch")
	}
	if len(layout.Labels) != layout.N {
		return fmt.Errorf("labels length mismatch")
	}
	if len(layout.OrigIDs) != layout.N {
		return fmt.Errorf("orig ids length mismatch")
	}
	if int(layout.Offsets[layout.K]) != layout.N {
		return fmt.Errorf("offsets[k] != n")
	}
	expectedBlocks := int(layout.BlockOffsets[layout.K]) * ivf.BlockStride
	if len(layout.Blocks) != expectedBlocks {
		return fmt.Errorf("blocks length mismatch")
	}
	return nil
}

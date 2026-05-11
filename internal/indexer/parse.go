package indexer

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"

	"backend/internal/ivf"
)

const (
	indexerIOBufferSize      = 1 << 20
	estimatedReferenceCount  = 3_100_000
	referenceFraudLabel      = "fraud"
	referenceFraudLabelValue = uint8(1)
	referenceOkLabelValue    = uint8(0)
)

type referenceEntry struct {
	Vector [ivf.Dim]float32 `json:"vector"`
	Label  string           `json:"label"`
}

// LoadReferences faz parse streaming do references.json.gz para evitar carregar JSON bruto inteiro em memoria.
func LoadReferences(path string) ([][ivf.Dim]float32, []uint8, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	gz, err := gzip.NewReader(bufio.NewReaderSize(f, indexerIOBufferSize))
	if err != nil {
		return nil, nil, err
	}
	defer gz.Close()

	dec := json.NewDecoder(bufio.NewReaderSize(gz, indexerIOBufferSize))
	tok, err := dec.Token()
	if err != nil {
		return nil, nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '[' {
		return nil, nil, fmt.Errorf("expected top-level array")
	}

	// Capacidade inicial baseada na ordem de grandeza do dataset oficial para reduzir realocacoes.
	vectors := make([][ivf.Dim]float32, 0, estimatedReferenceCount)
	labels := make([]uint8, 0, estimatedReferenceCount)
	for dec.More() {
		var entry referenceEntry
		if err := dec.Decode(&entry); err != nil {
			return nil, nil, err
		}
		vectors = append(vectors, entry.Vector)
		// O indice trabalha com binario (0/1) para permitir voto rapido no top-k.
		if entry.Label == referenceFraudLabel {
			labels = append(labels, referenceFraudLabelValue)
		} else {
			labels = append(labels, referenceOkLabelValue)
		}
	}
	if _, err := dec.Token(); err != nil {
		return nil, nil, err
	}
	return vectors, labels, nil
}

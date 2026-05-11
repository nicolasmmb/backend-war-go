package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"backend/internal/indexer"

	"github.com/nicolasmmb/envx"
)

const (
	buildIndexMinArgs         = 3
	buildIndexInputPathArg    = 1
	buildIndexOutputPathArg   = 2
	buildIndexUsageExitCode   = 2
	buildIndexFailureExitCode = 1
	bytesPerMiB               = 1024 * 1024
	minValidIVFK              = 1
	minValidIVFIterations     = 1
)

type Config struct {
	IVF_K     int    `default:"512"`                  // IVF_K define quantos centroides (clusters) serao treinados.
	IVF_ITERS int    `default:"10"`                   // IVF_ITERS define quantas iteracoes de refinamento do kmeans serao executadas.
	IVF_SEED  uint64 `default:"16045690984503098046"` // IVF_SEED define a semente pseudoaleatoria usada no treinamento.
}

// main executa o pipeline offline: carregar dataset, treinar IVF e escrever index.bin.
func main() {
	if len(os.Args) < buildIndexMinArgs {
		fmt.Fprintf(os.Stderr, "usage: %s <references.json.gz> <index.bin>\n", os.Args[0])
		os.Exit(buildIndexUsageExitCode)
	}

	inPath := os.Args[buildIndexInputPathArg]
	outPath := os.Args[buildIndexOutputPathArg]
	cfg, err := envx.LoadFromEnv[Config](
		// Invalidar cedo evita treinar por minutos com parametros impossiveis.
		envx.WithValidator(func(cfg *Config) error {
			if cfg.IVF_K < minValidIVFK {
				return errors.New("IVF_K must be > 0")
			}
			if cfg.IVF_ITERS < minValidIVFIterations {
				return errors.New("IVF_ITERS must be > 0")
			}
			return nil
		}),
	)
	if err != nil {
		log.Fatalf("failed to load build-index config: %v", err)
	}

	t0 := time.Now()
	fmt.Fprintf(os.Stderr, "loading references from %s\n", inPath)
	// Leitura streaming para suportar dataset grande sem materializar JSON comprimido inteiro.
	vectors, labels, err := indexer.LoadReferences(inPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load references failed: %v\n", err)
		os.Exit(buildIndexFailureExitCode)
	}
	fmt.Fprintf(os.Stderr, "  loaded %d vectors in %v\n", len(vectors), time.Since(t0))

	t1 := time.Now()
	fmt.Fprintf(os.Stderr, "training kmeans (k=%d, iters=%d)\n", cfg.IVF_K, cfg.IVF_ITERS)
	centroids, assignments, err := indexer.Train(vectors, cfg.IVF_K, cfg.IVF_ITERS, cfg.IVF_SEED)
	if err != nil {
		fmt.Fprintf(os.Stderr, "train failed: %v\n", err)
		os.Exit(buildIndexFailureExitCode)
	}
	fmt.Fprintf(os.Stderr, "  kmeans done in %v\n", time.Since(t1))

	t2 := time.Now()
	fmt.Fprintln(os.Stderr, "building index layout")
	// Layout reorganiza por cluster e bloco para tornar o hot path da API cache-friendly.
	layout, err := indexer.BuildLayout(vectors, labels, centroids, assignments)
	if err != nil {
		fmt.Fprintf(os.Stderr, "layout failed: %v\n", err)
		os.Exit(buildIndexFailureExitCode)
	}
	if err := indexer.ValidateLayout(layout); err != nil {
		fmt.Fprintf(os.Stderr, "layout invalid: %v\n", err)
		os.Exit(buildIndexFailureExitCode)
	}
	fmt.Fprintf(os.Stderr, "  layout built in %v (estimated %.1f MB)\n", time.Since(t2), float64(indexer.EstimateSizeBytes(layout))/bytesPerMiB)

	t3 := time.Now()
	fmt.Fprintf(os.Stderr, "writing %s\n", outPath)

	if err := indexer.WriteIndex(outPath, layout); err != nil {
		fmt.Fprintf(os.Stderr, "write failed: %v\n", err)
		os.Exit(buildIndexFailureExitCode)
	}
	st, _ := os.Stat(outPath)
	fmt.Fprintf(os.Stderr, "  wrote %.1f MB in %v\n", float64(st.Size())/bytesPerMiB, time.Since(t3))

	fmt.Fprintf(os.Stderr, "total: %v\n", time.Since(t0))
}

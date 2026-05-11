package main

import (
	"errors"
	"log"
	"os"
	"runtime"

	"backend/internal/api"

	"github.com/nicolasmmb/envx"
)

const (
	defaultMaxProcs = 1
	minValidPort    = 1
	maxValidPort    = 65535
	minValidNProbe  = 1
)

// main carrega configuracao tipada via envx e inicia a API de fraude.
func main() {
	// A competicao roda sob budget estrito de CPU; fixar 1 P garante previsibilidade.
	runtime.GOMAXPROCS(defaultMaxProcs)

	cfg, err := envx.LoadFromEnv[Config](
		// Valida cedo para falhar de forma explicita em erro de configuracao.
		envx.WithValidator(func(cfg *Config) error {
			if cfg.PORT < minValidPort || cfg.PORT > maxValidPort {
				return errors.New("PORT must be between 1 and 65535")
			}
			if cfg.NPROBE < minValidNProbe {
				return errors.New("NPROBE must be > 0")
			}
			if cfg.INDEX_PATH == "" {
				return errors.New("INDEX_PATH cannot be empty")
			}
			return nil
		}),
	)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	envx.Print(cfg)

	runCfg := api.RunConfig{
		Port:      cfg.PORT,
		UDSPath:   cfg.UDS_PATH,
		UDSMode:   os.FileMode(cfg.UDS_MODE),
		IndexPath: cfg.INDEX_PATH,
		UseMMap:   cfg.USE_MMAP,
		NProbe:    cfg.NPROBE,
	}

	// A API encapsula carga do indice + ciclo HTTP; main apenas orquestra o bootstrap.
	if err := api.Run(runCfg); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}

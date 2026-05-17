package api

import (
	"backend/internal/application/fraudscore"
	"backend/internal/ivf"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const (
	maxBodyBytes      = 64 * 1024
	bodyReadLimitByte = maxBodyBytes + 1

	defaultSocketDirMode = 0o755

	serverReadHeaderTimeout = 2 * time.Second
	serverIdleTimeout       = 30 * time.Second
)

var ErrNilEvaluator = errors.New("nil fraudscore evaluator")

// fraudResponses mapeia contagem de fraudes no top-5 para payload pronto, evitando marshal por request.
var fraudResponses = [6][]byte{
	[]byte(`{"approved":true,"fraud_score":0.0}`),
	[]byte(`{"approved":true,"fraud_score":0.2}`),
	[]byte(`{"approved":true,"fraud_score":0.4}`),
	[]byte(`{"approved":false,"fraud_score":0.6}`),
	[]byte(`{"approved":false,"fraud_score":0.8}`),
	[]byte(`{"approved":false,"fraud_score":1.0}`),
}

// requestBodyBufferPool reduz alocacoes por request durante decode de JSON.
var requestBodyBufferPool = sync.Pool{
	New: func() any {
		return bytes.NewBuffer(make([]byte, 0, bodyReadLimitByte))
	},
}

// RunConfig encapsula parametros de runtime vindos de env/main.
type RunConfig struct {
	Port      int
	UDSPath   string
	UDSMode   os.FileMode
	IndexPath string
	UseMMap   bool
	NProbe    int
}

// Server atua como adapter HTTP e delega regra de negocio para o caso de uso.
type Server struct {
	evaluator fraudscore.Evaluator
}

// New cria handler HTTP e injeta dependencia de aplicacao.
func New(evaluator fraudscore.Evaluator) (*Server, error) {
	if evaluator == nil {
		return nil, ErrNilEvaluator
	}
	return &Server{evaluator: evaluator}, nil
}

// Run inicializa dataset e sobe servidor HTTP em TCP ou Unix socket conforme configuracao.
func Run(cfg RunConfig) error {
	ds, err := ivf.Load(cfg.IndexPath, cfg.UseMMap)
	if err != nil {
		return err
	}
	defer ds.Close()

	useCase, err := newFraudScoreUseCase(ds, cfg.NProbe)
	if err != nil {
		return err
	}

	h, err := New(useCase)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: serverReadHeaderTimeout,
		IdleTimeout:       serverIdleTimeout,
	}

	if cfg.UDSPath != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.UDSPath), defaultSocketDirMode); err != nil {
			return err
		}
		// Remove socket antigo para permitir restart limpo sem "address already in use".
		_ = os.Remove(cfg.UDSPath)
		ln, err := net.Listen("unix", cfg.UDSPath)
		if err != nil {
			return err
		}
		defer ln.Close()
		if cfg.UDSMode != 0 {
			if err := os.Chmod(cfg.UDSPath, cfg.UDSMode); err != nil {
				return err
			}
		}
		return srv.Serve(ln)
	}

	srv.Addr = fmt.Sprintf(":%d", cfg.Port)
	return srv.ListenAndServe()
}

// ServeHTTP roteia somente os endpoints da rinha, evitando overhead de roteador generico.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Path == "/ready" {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method == http.MethodPost && r.URL.Path == "/fraud-score" {
		s.handleFraudScore(w, r)
		return
	}
	w.WriteHeader(http.StatusNotFound)
}

// handleFraudScore valida body e executa o caso de uso para montar resposta deterministica.
func (s *Server) handleFraudScore(w http.ResponseWriter, r *http.Request) {
	// Passo 0: garante fechamento do body assim que o handler termina.
	defer r.Body.Close()

	// Passo 1: decode do JSON com limite de tamanho; qualquer erro retorna status apropriado.
	var req FraudRequest
	status := decodeFraudRequest(r.Body, &req)
	if status != 0 {
		w.WriteHeader(status)
		return
	}

	// Passo 2: adapta DTO para dominio e executa caso de uso antifraude.
	domainReq := mapToDomainFraudRequest(&req)
	out, err := s.evaluator.Evaluate(r.Context(), fraudscore.Input{
		Request: domainReq,
	})
	if err != nil {
		if errors.Is(err, fraudscore.ErrInvalidRequest) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Passo 3: mapeia resultado de dominio para payload pre-serializado.
	count := min(out.Assessment.FraudCount, maxFraudCount)
	resp := fraudResponses[count]

	// Passo 4: devolve resposta pre-serializada para minimizar overhead por request.
	// Conteudo fixo com Content-Length evita chunked encoding e simplifica cliente de benchmark.
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(resp)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resp)
}

// decodeFraudRequest valida limite de tamanho e faz parse JSON com buffer reutilizavel.
func decodeFraudRequest(body io.Reader, out *FraudRequest) int {
	// Passo 0: obtem buffer reutilizavel do pool e limpa estado anterior.
	buf := requestBodyBufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer requestBodyBufferPool.Put(buf)

	// Passo 1: copia no maximo (maxBodyBytes + 1) para detectar overflow sem ler stream inteira.
	if _, err := io.CopyN(buf, body, bodyReadLimitByte); err != nil && err != io.EOF {
		return http.StatusBadRequest
	}

	// Passo 2: se excedeu limite, retorna 413.
	if buf.Len() > maxBodyBytes {
		return http.StatusRequestEntityTooLarge
	}

	// Passo 3: parseia JSON para struct alvo.
	if err := json.Unmarshal(buf.Bytes(), out); err != nil {
		return http.StatusBadRequest
	}

	// Passo 4: sucesso sinalizado por 0.
	return 0
}

// decodeFraudRequestLegacy mantem a versao anterior para benchmark A/B.
func decodeFraudRequestLegacy(body io.Reader, out *FraudRequest) int {
	raw, err := io.ReadAll(io.LimitReader(body, bodyReadLimitByte))
	if err != nil {
		return http.StatusBadRequest
	}
	if len(raw) > maxBodyBytes {
		return http.StatusRequestEntityTooLarge
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return http.StatusBadRequest
	}
	return 0
}

package fraudscore

import (
	"backend/internal/domain/fraud"
	"context"
	"errors"
	"fmt"
)

var (
	ErrInvalidRequest = errors.New("invalid fraud request")
	ErrNilVectorizer  = errors.New("nil vectorizer")
	ErrNilSearcher    = errors.New("nil searcher")
)

// Input representa o comando de aplicacao para calcular score de fraude.
type Input struct {
	Request fraud.Request
}

// Output representa a resposta de aplicacao do score de fraude.
type Output struct {
	Assessment fraud.Assessment
}

// Vectorizer define a porta de extração de features do comando de fraude.
type Vectorizer interface {
	Vectorize(req *fraud.Request) (fraud.FeatureVector, error)
}

// NeighborSearcher define a porta de busca de vizinhos similares no indice.
type NeighborSearcher interface {
	FraudCount(query *fraud.FeatureVector) uint8
}

// UseCase orquestra o fluxo de aplicacao para responder score de fraude.
type UseCase struct {
	vectorizer Vectorizer
	searcher   NeighborSearcher
}

// Evaluator define a porta de aplicacao consumida por adapters (HTTP, CLI, etc.).
type Evaluator interface {
	Evaluate(ctx context.Context, in Input) (Output, error)
}

func NewUseCase(vectorizer Vectorizer, searcher NeighborSearcher) (*UseCase, error) {
	if vectorizer == nil {
		return nil, ErrNilVectorizer
	}
	if searcher == nil {
		return nil, ErrNilSearcher
	}
	return &UseCase{
		vectorizer: vectorizer,
		searcher:   searcher,
	}, nil
}

// Evaluate executa o fluxo de aplicacao do score antifraude.
func (u *UseCase) Evaluate(ctx context.Context, in Input) (Output, error) {
	if err := ctx.Err(); err != nil {
		return Output{}, err
	}
	if err := in.Request.Validate(); err != nil {
		return Output{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	query, err := u.vectorizer.Vectorize(&in.Request)
	if err != nil {
		return Output{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	return Output{
		Assessment: fraud.NewAssessment(u.searcher.FraudCount(&query)),
	}, nil
}

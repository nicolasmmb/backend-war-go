package fraudscore

import (
	"backend/internal/domain/fraud"
	"context"
	"errors"
	"math"
	"testing"
)

type stubVectorizer struct {
	out fraud.FeatureVector
	err error
}

func (s stubVectorizer) Vectorize(*fraud.Request) (fraud.FeatureVector, error) {
	return s.out, s.err
}

type stubSearcher struct {
	count uint8
}

func (s stubSearcher) FraudCount(*fraud.FeatureVector) uint8 {
	return s.count
}

func mustUseCase(t *testing.T) *UseCase {
	t.Helper()
	uc, err := NewUseCase(stubVectorizer{}, stubSearcher{})
	if err != nil {
		t.Fatalf("unexpected ctor error: %v", err)
	}
	return uc
}

func TestNewUseCaseNilDeps(t *testing.T) {
	if _, err := NewUseCase(nil, stubSearcher{}); !errors.Is(err, ErrNilVectorizer) {
		t.Fatalf("expected ErrNilVectorizer, got %v", err)
	}
	if _, err := NewUseCase(stubVectorizer{}, nil); !errors.Is(err, ErrNilSearcher) {
		t.Fatalf("expected ErrNilSearcher, got %v", err)
	}
}

func TestEvaluateInvalidRequest(t *testing.T) {
	uc := mustUseCase(t)

	_, err := uc.Evaluate(context.Background(), Input{
		Request: fraud.Request{},
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestEvaluateVectorizerError(t *testing.T) {
	uc, err := NewUseCase(
		stubVectorizer{err: errors.New("bad timestamp")},
		stubSearcher{},
	)
	if err != nil {
		t.Fatalf("unexpected ctor error: %v", err)
	}

	_, err = uc.Evaluate(context.Background(), Input{
		Request: fraud.Request{
			Transaction: fraud.Transaction{RequestedAt: "2026-03-14T05:15:12Z"},
		},
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestEvaluateAssessment(t *testing.T) {
	uc, err := NewUseCase(
		stubVectorizer{out: fraud.FeatureVector{}},
		stubSearcher{count: 4},
	)
	if err != nil {
		t.Fatalf("unexpected ctor error: %v", err)
	}

	res, err := uc.Evaluate(context.Background(), Input{
		Request: fraud.Request{
			Transaction: fraud.Transaction{RequestedAt: "2026-03-14T05:15:12Z"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Assessment.FraudCount != 4 {
		t.Fatalf("fraud count mismatch: got %d want 4", res.Assessment.FraudCount)
	}
	if res.Assessment.Approved {
		t.Fatalf("expected approved=false")
	}
	if math.Abs(float64(res.Assessment.FraudScore-0.8)) > 1e-6 {
		t.Fatalf("fraud score mismatch: got %.1f want 0.8", res.Assessment.FraudScore)
	}
}

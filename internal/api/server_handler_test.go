package api

import (
	"backend/internal/application/fraudscore"
	"backend/internal/domain/fraud"
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type stubEvaluator struct {
	out fraudscore.Output
	err error
}

func (s stubEvaluator) Evaluate(context.Context, fraudscore.Input) (fraudscore.Output, error) {
	return s.out, s.err
}

func mustServer(t *testing.T, evaluator fraudscore.Evaluator) *Server {
	t.Helper()
	s, err := New(evaluator)
	if err != nil {
		t.Fatalf("unexpected New error: %v", err)
	}
	return s
}

func TestNewNilEvaluator(t *testing.T) {
	_, err := New(nil)
	if !errors.Is(err, ErrNilEvaluator) {
		t.Fatalf("expected ErrNilEvaluator, got %v", err)
	}
}

func TestHandleFraudScoreOK(t *testing.T) {
	s := mustServer(t, stubEvaluator{
		out: fraudscore.Output{
			Assessment: fraud.NewAssessment(4),
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/fraud-score", bytes.NewReader(benchFraudRequestBody))
	w := httptest.NewRecorder()

	s.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status mismatch: got=%d want=%d", w.Code, http.StatusOK)
	}
	if got := w.Body.String(); got != `{"approved":false,"fraud_score":0.8}` {
		t.Fatalf("response mismatch: got=%s", got)
	}
}

func TestHandleFraudScoreErrors(t *testing.T) {
	tests := []struct {
		name     string
		body     []byte
		evalErr  error
		wantCode int
	}{
		{
			name:     "bad json",
			body:     []byte(`{"id":`),
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "invalid request",
			body:     benchFraudRequestBody,
			evalErr:  fraudscore.ErrInvalidRequest,
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "internal error",
			body:     benchFraudRequestBody,
			evalErr:  errors.New("db unavailable"),
			wantCode: http.StatusInternalServerError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := mustServer(t, stubEvaluator{err: tc.evalErr})

			req := httptest.NewRequest(http.MethodPost, "/fraud-score", bytes.NewReader(tc.body))
			w := httptest.NewRecorder()

			s.ServeHTTP(w, req)
			if w.Code != tc.wantCode {
				t.Fatalf("status mismatch: got=%d want=%d", w.Code, tc.wantCode)
			}
		})
	}
}

package api

import (
	"encoding/json"
	"math"
	"testing"
)

func mustParseReq(t *testing.T, body string) *FraudRequest {
	t.Helper()
	var req FraudRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	return &req
}

func approx(a, b, tol float32) bool {
	return float32(math.Abs(float64(a-b))) <= tol
}

// Caso realista "legit" para verificar normalizacao e sentinelas de ultima transacao ausente.
func TestVectorizeLegitExample(t *testing.T) {
	req := mustParseReq(t, `{
		"id":"tx-1329056812",
		"transaction":{"amount":41.12,"installments":2,"requested_at":"2026-03-11T18:45:53Z"},
		"customer":{"avg_amount":82.24,"tx_count_24h":3,"known_merchants":["MERC-003","MERC-016"]},
		"merchant":{"id":"MERC-016","mcc":"5411","avg_amount":60.25},
		"terminal":{"is_online":false,"card_present":true,"km_from_home":29.23},
		"last_transaction":null
	}`)
	v, err := Vectorize(req)
	if err != nil {
		t.Fatalf("vectorize failed: %v", err)
	}

	if !approx(v[0], 0.0041, 1e-3) {
		t.Fatalf("v0=%f", v[0])
	}
	if !approx(v[1], 0.1667, 1e-3) {
		t.Fatalf("v1=%f", v[1])
	}
	if !approx(v[2], 0.05, 1e-3) {
		t.Fatalf("v2=%f", v[2])
	}
	if !approx(v[3], 0.7826, 1e-3) {
		t.Fatalf("v3=%f", v[3])
	}
	if !approx(v[4], 0.3333, 1e-3) {
		t.Fatalf("v4=%f", v[4])
	}
	if v[5] != -1 || v[6] != -1 {
		t.Fatalf("expected -1 for no last tx, got %f/%f", v[5], v[6])
	}
}

// Caso "fraud" para cobrir features de risco alto (MCC, merchant desconhecido, valores extremos).
func TestVectorizeFraudExample(t *testing.T) {
	req := mustParseReq(t, `{
		"id":"tx-3330991687",
		"transaction":{"amount":9505.97,"installments":10,"requested_at":"2026-03-14T05:15:12Z"},
		"customer":{"avg_amount":81.28,"tx_count_24h":20,"known_merchants":["MERC-008","MERC-007","MERC-005"]},
		"merchant":{"id":"MERC-068","mcc":"7802","avg_amount":54.86},
		"terminal":{"is_online":false,"card_present":true,"km_from_home":952.27},
		"last_transaction":null
	}`)
	v, err := Vectorize(req)
	if err != nil {
		t.Fatalf("vectorize failed: %v", err)
	}

	if !approx(v[0], 0.9506, 1e-3) {
		t.Fatalf("v0=%f", v[0])
	}
	if !approx(v[12], 0.75, 1e-3) {
		t.Fatalf("v12=%f", v[12])
	}
	if v[11] != 1.0 {
		t.Fatalf("expected unseen merchant flag 1.0, got %f", v[11])
	}
}

// Parsing temporal invalido precisa falhar cedo para evitar feature lixo no indice.
func TestVectorizeInvalidTimestamp(t *testing.T) {
	req := &FraudRequest{}
	req.Transaction.RequestedAt = "invalid"
	if _, err := Vectorize(req); err == nil {
		t.Fatal("expected error")
	}
}

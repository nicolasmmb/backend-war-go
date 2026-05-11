package api

import (
	"bytes"
	"encoding/json"
	"testing"
)

var benchFraudRequestBody = []byte(`{
	"id":"tx-1329056812",
	"transaction":{"amount":41.12,"installments":2,"requested_at":"2026-03-11T18:45:53Z"},
	"customer":{"avg_amount":82.24,"tx_count_24h":3,"known_merchants":["MERC-003","MERC-016"]},
	"merchant":{"id":"MERC-016","mcc":"5411","avg_amount":60.25},
	"terminal":{"is_online":false,"card_present":true,"km_from_home":29.23},
	"last_transaction":null
}`)

// Garante que o caminho otimizado preserva exatamente semantica/erros da versao legada.
func TestDecodeFraudRequestMatchesLegacy(t *testing.T) {
	cases := []struct {
		name string
		body []byte
	}{
		{name: "valid", body: benchFraudRequestBody},
		{name: "invalid json", body: []byte(`{"id":`)},
		{name: "too large", body: bytes.Repeat([]byte("a"), maxBodyBytes+1)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotReq FraudRequest
			var wantReq FraudRequest

			gotStatus := decodeFraudRequest(bytes.NewReader(tc.body), &gotReq)
			wantStatus := decodeFraudRequestLegacy(bytes.NewReader(tc.body), &wantReq)

			if gotStatus != wantStatus {
				t.Fatalf("status mismatch: got=%d want=%d", gotStatus, wantStatus)
			}
			if gotStatus == 0 {
				gotJSON, err := json.Marshal(gotReq)
				if err != nil {
					t.Fatalf("marshal got req: %v", err)
				}
				wantJSON, err := json.Marshal(wantReq)
				if err != nil {
					t.Fatalf("marshal want req: %v", err)
				}
				if !bytes.Equal(gotJSON, wantJSON) {
					t.Fatalf("decoded request mismatch:\ngot=%s\nwant=%s", string(gotJSON), string(wantJSON))
				}
			}
		})
	}
}

// Benchmarks A/B para medir ganho de alocacao do decoder com pool.
func BenchmarkDecodeFraudRequestLegacy(b *testing.B) {
	b.ReportAllocs()
	var req FraudRequest
	var rdr bytes.Reader
	sum := 0
	for i := 0; i < b.N; i++ {
		rdr.Reset(benchFraudRequestBody)
		req = FraudRequest{}
		status := decodeFraudRequestLegacy(&rdr, &req)
		if status != 0 {
			b.Fatalf("unexpected status: %d", status)
		}
		sum += int(req.Customer.TxCount24h)
	}
	if sum == 0 {
		b.Fatal("unexpected zero sum")
	}
}

func BenchmarkDecodeFraudRequestPooled(b *testing.B) {
	b.ReportAllocs()
	var req FraudRequest
	var rdr bytes.Reader
	sum := 0
	for i := 0; i < b.N; i++ {
		rdr.Reset(benchFraudRequestBody)
		req = FraudRequest{}
		status := decodeFraudRequest(&rdr, &req)
		if status != 0 {
			b.Fatalf("unexpected status: %d", status)
		}
		sum += int(req.Customer.TxCount24h)
	}
	if sum == 0 {
		b.Fatal("unexpected zero sum")
	}
}

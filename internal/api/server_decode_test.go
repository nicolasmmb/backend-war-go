package api

import (
	"bytes"
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

func TestDecodeFraudRequest(t *testing.T) {
	cases := []struct {
		name string
		body []byte
		ok   bool
	}{
		{name: "valid", body: benchFraudRequestBody, ok: true},
		{name: "invalid json", body: []byte(`{"id":`)},
		{name: "wrong type", body: []byte(`{"transaction":{"requested_at":123}}`)},
		{name: "too large", body: bytes.Repeat([]byte("a"), maxBodyBytes+1)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotReq FraudRequest

			gotStatus := decodeFraudRequest(bytes.NewReader(tc.body), &gotReq)
			if tc.ok {
				if gotStatus != 0 {
					t.Fatalf("status=%d", gotStatus)
				}
				if gotReq.Transaction.RequestedAt == "" {
					t.Fatal("expected requested_at parsed")
				}
				if len(gotReq.Customer.KnownMerchants) == 0 {
					t.Fatal("expected known_merchants parsed")
				}
				return
			}
			if gotStatus == 0 {
				t.Fatal("expected bad status")
			}
		})
	}
}

func BenchmarkDecodeFraudRequest(b *testing.B) {
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

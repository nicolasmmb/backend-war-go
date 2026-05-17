package api

import (
	"testing"

	"backend/internal/ivf"
)

var (
	benchVectorizeSink [ivf.Dim]float32
	benchVectorizeErr  error
)

func testVectorizeInputs() []*FraudRequest {
	return []*FraudRequest{
		{
			ID: "tx-nolast",
			Transaction: TransactionData{
				Amount:       41.12,
				Installments: 2,
				RequestedAt:  "2026-03-11T18:45:53Z",
			},
			Customer: CustomerData{
				AvgAmount:      82.24,
				TxCount24h:     3,
				KnownMerchants: []string{"MERC-003", "MERC-016"},
			},
			Merchant: MerchantData{
				ID:        "MERC-016",
				MCC:       "5411",
				AvgAmount: 60.25,
			},
			Terminal: TerminalData{
				IsOnline:    false,
				CardPresent: true,
				KmFromHome:  29.23,
			},
			LastTx: nil,
		},
		{
			ID: "tx-last",
			Transaction: TransactionData{
				Amount:       9505.97,
				Installments: 10,
				RequestedAt:  "2026-03-14T05:15:12Z",
			},
			Customer: CustomerData{
				AvgAmount:      81.28,
				TxCount24h:     20,
				KnownMerchants: []string{"MERC-008", "MERC-007", "MERC-005"},
			},
			Merchant: MerchantData{
				ID:        "MERC-068",
				MCC:       "7802",
				AvgAmount: 54.86,
			},
			Terminal: TerminalData{
				IsOnline:    false,
				CardPresent: true,
				KmFromHome:  952.27,
			},
			LastTx: &LastTxData{
				Timestamp:     "2026-03-14T02:05:13Z",
				KmFromCurrent: 245.91,
			},
		},
	}
}

func benchmarkVectorizeFunc(b *testing.B, name string, req *FraudRequest) {
	b.Run(name, func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			out, err := Vectorize(req)
			benchVectorizeSink = out
			benchVectorizeErr = err
		}
		if benchVectorizeErr != nil {
			b.Fatalf("unexpected error: %v", benchVectorizeErr)
		}
	})
}

func BenchmarkVectorize(b *testing.B) {
	in := testVectorizeInputs()
	benchmarkVectorizeFunc(b, "no_last_tx", in[0])
	benchmarkVectorizeFunc(b, "with_last_tx", in[1])
}

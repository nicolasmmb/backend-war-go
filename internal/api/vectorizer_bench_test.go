package api

import (
	"fmt"
	"math"
	"testing"

	"backend/internal/ivf"
)

var (
	benchVectorizeSink [ivf.Dim]float32
	benchVectorizeErr  error
)

// Caminho legado mantido apenas para benchmark/paridade do pipeline de features.
func vectorizeLegacy(req *FraudRequest) ([ivf.Dim]float32, error) {
	var out [ivf.Dim]float32

	y, m, d, hour, min, sec, ok := parseISODateTime(req.Transaction.RequestedAt)
	if !ok {
		return out, fmt.Errorf("invalid requested_at")
	}
	weekday := weekdayMon0(y, m, d)

	minsLast := float32(-1.0)
	kmLast := float32(-1.0)
	if req.LastTx != nil {
		ly, lm, ld, lh, lmin, lsec, lok := parseISODateTime(req.LastTx.Timestamp)
		if !lok {
			return out, fmt.Errorf("invalid last_transaction.timestamp")
		}
		mins := minutesBetweenAbsLegacy(y, m, d, hour, min, sec, ly, lm, ld, lh, lmin, lsec)
		minsLast = clamp(float32(mins) / maxMinutes)
		kmLast = clamp(req.LastTx.KmFromCurrent / maxKM)
	}

	amountVsAvg := float32(1.0)
	if req.Customer.AvgAmount > 0 {
		amountVsAvg = clamp(req.Transaction.Amount / req.Customer.AvgAmount / amountVsAvgRatio)
	}

	out = [ivf.Dim]float32{
		clamp(req.Transaction.Amount / maxAmount),
		clamp(float32(req.Transaction.Installments) / maxInstallments),
		amountVsAvg,
		float32(minIntLegacy(hour, 23)) / 23.0,
		float32(weekday) / 6.0,
		minsLast,
		kmLast,
		clamp(req.Terminal.KmFromHome / maxKM),
		clamp(float32(req.Customer.TxCount24h) / maxTxCount24h),
		boolToFloat(req.Terminal.IsOnline),
		boolToFloat(req.Terminal.CardPresent),
		boolToFloat(!merchantIDInKnown(req)),
		mccRiskLegacy(req.Merchant.MCC),
		clamp(req.Merchant.AvgAmount / maxMerchantAvgAmount),
	}
	return out, nil
}

func mccRiskLegacy(mcc string) float32 {
	switch mcc {
	case "5411":
		return 0.15
	case "5812":
		return 0.30
	case "5912":
		return 0.20
	case "5944":
		return 0.45
	case "7801":
		return 0.80
	case "7802":
		return 0.75
	case "7995":
		return 0.85
	case "4511":
		return 0.35
	case "5311":
		return 0.25
	case "5999":
		return 0.50
	default:
		return 0.5
	}
}

func minutesBetweenAbsLegacy(y1, m1, d1, h1, min1, sec1, y2, m2, d2, h2, min2, sec2 int) int64 {
	a := epochSeconds(y1, m1, d1, h1, min1, sec1)
	b := epochSeconds(y2, m2, d2, h2, min2, sec2)
	if a > b {
		return (a - b) / 60
	}
	return (b - a) / 60
}

func minIntLegacy(a, b int) int {
	if a < b {
		return a
	}
	return b
}

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

// Paridade numerica rigorosa entre implementacao nova e legada.
func TestVectorizeMatchesLegacy(t *testing.T) {
	reqTimes := []string{
		"2026-03-11T18:45:53Z",
		"2026-03-14T05:15:12Z",
		"2026-12-31T23:59:59Z",
	}
	lastTimes := []*LastTxData{
		nil,
		{Timestamp: "2026-03-11T18:45:00Z", KmFromCurrent: 2.5},
		{Timestamp: "2026-03-10T10:00:00Z", KmFromCurrent: 999.9},
	}
	mccs := []string{"5411", "5812", "5912", "5944", "7801", "7802", "7995", "4511", "5311", "5999", "0000", "abcd", ""}

	caseID := 0
	for _, reqAt := range reqTimes {
		for _, lastTx := range lastTimes {
			for _, mcc := range mccs {
				caseID++
				merchantID := fmt.Sprintf("MERC-%03d", caseID%97)
				known := []string{fmt.Sprintf("MERC-%03d", (caseID+5)%97), fmt.Sprintf("MERC-%03d", (caseID+9)%97)}
				if caseID%2 == 0 {
					known = append(known, merchantID)
				}

				req := &FraudRequest{
					ID: fmt.Sprintf("tx-%d", caseID),
					Transaction: TransactionData{
						Amount:       float32((caseID*137)%15000) * 0.73,
						Installments: uint32(caseID % 16),
						RequestedAt:  reqAt,
					},
					Customer: CustomerData{
						AvgAmount:      float32((caseID*97)%2000) - 200,
						TxCount24h:     uint32(caseID % 40),
						KnownMerchants: known,
					},
					Merchant: MerchantData{
						ID:        merchantID,
						MCC:       mcc,
						AvgAmount: float32((caseID * 41) % 20000),
					},
					Terminal: TerminalData{
						IsOnline:    caseID%2 == 0,
						CardPresent: caseID%3 == 0,
						KmFromHome:  float32((caseID * 19) % 2000),
					},
					LastTx: lastTx,
				}

				got, gErr := Vectorize(req)
				want, wErr := vectorizeLegacy(req)
				if (gErr != nil) != (wErr != nil) {
					t.Fatalf("case=%d error mismatch: got=%v want=%v", caseID, gErr, wErr)
				}
				if gErr != nil {
					continue
				}
				for i := 0; i < ivf.Dim; i++ {
					if math.Abs(float64(got[i]-want[i])) > 1e-6 {
						t.Fatalf("case=%d dim=%d got=%.9f want=%.9f", caseID, i, got[i], want[i])
					}
				}
			}
		}
	}

	invalidRequested := &FraudRequest{
		Transaction: TransactionData{RequestedAt: "bad"},
	}
	if _, err := Vectorize(invalidRequested); err == nil {
		t.Fatal("expected error for invalid requested_at")
	}
	if _, err := vectorizeLegacy(invalidRequested); err == nil {
		t.Fatal("expected legacy error for invalid requested_at")
	}

	invalidLastTx := &FraudRequest{
		Transaction: TransactionData{RequestedAt: "2026-03-11T18:45:53Z"},
		LastTx:      &LastTxData{Timestamp: "bad"},
	}
	if _, err := Vectorize(invalidLastTx); err == nil {
		t.Fatal("expected error for invalid last_transaction.timestamp")
	}
	if _, err := vectorizeLegacy(invalidLastTx); err == nil {
		t.Fatal("expected legacy error for invalid last_transaction.timestamp")
	}
}

func benchmarkVectorizeFunc(b *testing.B, name string, fn func(*FraudRequest) ([ivf.Dim]float32, error), req *FraudRequest) {
	b.Run(name, func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			out, err := fn(req)
			benchVectorizeSink = out
			benchVectorizeErr = err
		}
		if benchVectorizeErr != nil {
			b.Fatalf("unexpected error: %v", benchVectorizeErr)
		}
	})
}

func BenchmarkVectorizeFast(b *testing.B) {
	in := testVectorizeInputs()
	benchmarkVectorizeFunc(b, "no_last_tx", Vectorize, in[0])
	benchmarkVectorizeFunc(b, "with_last_tx", Vectorize, in[1])
}

func BenchmarkVectorizeLegacy(b *testing.B) {
	in := testVectorizeInputs()
	benchmarkVectorizeFunc(b, "no_last_tx", vectorizeLegacy, in[0])
	benchmarkVectorizeFunc(b, "with_last_tx", vectorizeLegacy, in[1])
}

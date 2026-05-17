package fraud

import (
	"errors"
	"testing"
)

func TestRequestValidate(t *testing.T) {
	tests := []struct {
		name string
		req  Request
		err  error
	}{
		{
			name: "valid without last tx",
			req: Request{
				Transaction: Transaction{RequestedAt: "2026-03-14T05:15:12Z"},
			},
		},
		{
			name: "missing requested_at",
			req:  Request{},
			err:  ErrMissingTransactionTimestamp,
		},
		{
			name: "missing last tx timestamp",
			req: Request{
				Transaction: Transaction{RequestedAt: "2026-03-14T05:15:12Z"},
				LastTx:      &LastTransaction{},
			},
			err: ErrMissingLastTxTimestamp,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.req.Validate()
			if tc.err == nil && got != nil {
				t.Fatalf("expected nil error, got %v", got)
			}
			if tc.err != nil && !errors.Is(got, tc.err) {
				t.Fatalf("expected %v, got %v", tc.err, got)
			}
		})
	}
}

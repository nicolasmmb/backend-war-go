package api

import (
	"fmt"
	"slices"
	"strconv"
	"testing"
)

const merchantLookupSeed = uint64(0x94d049bb133111eb)

func buildKnownMerchants(size int, seed uint64) []string {
	out := make([]string, size)
	if size == 0 {
		return out
	}
	state := seed
	space := size/2 + 1
	for i := range size {
		testLCGStep(&state)
		id := int((state >> 16) % uint64(space))
		out[i] = "MERC-" + strconv.Itoa(id)
	}
	return out
}

// Confirma que loop manual preserva semantica de slices.Contains.
func TestMerchantIDInKnownMatchesLegacySlices(t *testing.T) {
	sizes := []int{0, 1, 8, 32, 64, 65, 128, 512, 1024, 1025, 4096}

	for _, size := range sizes {
		known := buildKnownMerchants(size, merchantLookupSeed)
		targets := []string{"MERC-0", "MERC-99999999"}
		if size > 0 {
			targets = append(targets, known[size/2], known[size-1])
		}

		for _, target := range targets {
			req := &FraudRequest{
				Customer: CustomerData{KnownMerchants: known},
				Merchant: MerchantData{ID: target},
			}
			got := merchantIDInKnown(req)
			want := merchantIDInKnownLegacySlices(req)
			if got != want {
				t.Fatalf("size=%d target=%q got=%v want=%v", size, target, got, want)
			}
		}
	}
}

func merchantIDInKnownLegacySlices(req *FraudRequest) bool {
	return slices.Contains(req.Customer.KnownMerchants, req.Merchant.ID)
}

func BenchmarkMerchantIDInKnownFast(b *testing.B) {
	sizes := []int{8, 64, 128, 512, 1024, 2048}
	for _, size := range sizes {
		known := buildKnownMerchants(size, merchantLookupSeed)
		for _, hit := range []bool{true, false} {
			target := "MERC-NOT-FOUND"
			if hit && size > 0 {
				target = known[size-1]
			}
			req := &FraudRequest{
				Customer: CustomerData{KnownMerchants: known},
				Merchant: MerchantData{ID: target},
			}

			name := fmt.Sprintf("n=%d/hit=%t", size, hit)
			b.Run(name, func(b *testing.B) {
				b.ReportAllocs()
				count := 0
				for i := 0; i < b.N; i++ {
					if merchantIDInKnown(req) {
						count++
					}
				}
				if count == 0 && hit {
					b.Fatal("unexpected miss on hit benchmark")
				}
			})
		}
	}
}

func BenchmarkMerchantIDInKnownLegacySlices(b *testing.B) {
	sizes := []int{8, 64, 128, 512, 1024, 2048}
	for _, size := range sizes {
		known := buildKnownMerchants(size, merchantLookupSeed)
		for _, hit := range []bool{true, false} {
			target := "MERC-NOT-FOUND"
			if hit && size > 0 {
				target = known[size-1]
			}

			req := &FraudRequest{
				Customer: CustomerData{KnownMerchants: known},
				Merchant: MerchantData{ID: target},
			}

			name := fmt.Sprintf("n=%d/hit=%t", size, hit)
			b.Run(name, func(b *testing.B) {
				b.ReportAllocs()
				count := 0
				for i := 0; i < b.N; i++ {
					if merchantIDInKnownLegacySlices(req) {
						count++
					}
				}
				if count == 0 && hit {
					b.Fatal("unexpected miss on hit benchmark")
				}
			})
		}
	}
}

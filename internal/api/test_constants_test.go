package api

import "backend/internal/ivf"

const (
	testLCGMul uint64 = 6364136223846793005
	testLCGInc uint64 = 1442695040888963407

	testFloatBitsShift = 16
	testFloatBitsMask  = 0x7fff
	testFloatCenter    = 16384
	testFloatScale     = 16384.0

	testDistanceMask = 0x3ffff
	testOrigIDMask   = 0xffff
)

func testLCGStep(state *uint64) uint64 {
	*state = *state*testLCGMul + testLCGInc
	return *state
}

func testLCGFloatUnit(state *uint64) float32 {
	v := testLCGStep(state)
	return float32(int32((v>>testFloatBitsShift)&testFloatBitsMask)-testFloatCenter) / testFloatScale
}

func testAlignKPad(k int) int {
	return (k + ivf.BlockSize - 1) &^ (ivf.BlockSize - 1)
}

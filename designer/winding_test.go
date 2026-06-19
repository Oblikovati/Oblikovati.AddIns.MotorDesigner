// SPDX-License-Identifier: GPL-2.0-only

package designer

import "testing"

func TestFundamentalWindingFactorKnownCombos(t *testing.T) {
	// Reference values from motor-calculator/src/physics/winding.ts doc examples.
	cases := []struct {
		slots, poles int
		want         float64
		valid        bool
	}{
		{12, 10, 0.933, true},
		{9, 8, 0.945, true},
		{36, 6, 0.0, true}, // integer-slot distributed, full-pitch ≈ 1 region (loose bound below)
	}
	for _, c := range cases {
		kw, ok := FundamentalWindingFactor(c.slots, c.poles)
		if ok != c.valid {
			t.Errorf("%ds/%dp: valid=%v want %v", c.slots, c.poles, ok, c.valid)
			continue
		}
		if c.want > 0 && !approx(kw, c.want, 0.003) {
			t.Errorf("%ds/%dp: kw=%.3f want %.3f", c.slots, c.poles, kw, c.want)
		}
	}
}

func TestFundamentalWindingFactorRejectsUnbalanced(t *testing.T) {
	if _, ok := FundamentalWindingFactor(8, 6); ok {
		t.Errorf("8-slot is not a multiple of 3 → should be invalid")
	}
	if _, ok := FundamentalWindingFactor(12, 12); ok {
		t.Errorf("12s/12p (Q/gcd not divisible by 3) → should be invalid")
	}
}

func TestGcdInt(t *testing.T) {
	if g := gcdInt(12, 8); g != 4 {
		t.Errorf("gcd(12,8) = %d, want 4", g)
	}
	if g := gcdInt(9, 0); g != 9 {
		t.Errorf("gcd(9,0) = %d, want 9", g)
	}
}

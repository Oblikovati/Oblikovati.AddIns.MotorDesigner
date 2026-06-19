// SPDX-License-Identifier: GPL-2.0-only

package designer

import "math"

// Winding-factor analysis via the EMF phasor method, ported from
// motor-calculator/src/physics/winding.ts. The fundamental winding factor k_w1 feeds the
// electric-loading sizing (A_rms = sigma*sqrt(2)/(Bg*kw)) and is a quick quality signal
// for a slot/pole combination.

// zoneEntry is one 60-degree phase-belt slot assignment (phase index 0=A,1=B,2=C; sign).
type zoneEntry struct {
	phase int
	sign  float64
}

// zoneMap is the 60-degree zone -> (phase, sign) table. Ported from winding.ts ZONE_MAP.
var zoneMap = [6]zoneEntry{
	{0, +1}, // [0,60)   A+
	{2, -1}, // [60,120) C-
	{1, +1}, // [120,180) B+
	{0, -1}, // [180,240) A-
	{2, +1}, // [240,300) C+
	{1, -1}, // [300,360) B-
}

func gcdInt(a, b int) int {
	a, b = abs(a), abs(b)
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// slotZone returns the phase-belt entry for slot k using integer arithmetic to avoid
// floating-point boundary errors. Ported from winding.ts slotZone.
func slotZone(k, slots, polePairs int) zoneEntry {
	zone := ((6*k*polePairs/slots)%6 + 6) % 6
	return zoneMap[zone]
}

// validatePolePairs returns p/2 if (Q,p) can form a balanced 3-phase winding, else -1.
// Ported from winding.ts validateQP.
func validatePolePairs(slots, poles int) int {
	if slots < 3 || poles < 2 || slots%3 != 0 || poles%2 != 0 {
		return -1
	}
	pp := poles / 2
	if (slots/gcdInt(slots, pp))%3 != 0 {
		return -1
	}
	return pp
}

// windingFactorAtHarmonic returns k_w at spatial harmonic order nu (pole pairs) for coil
// pitch w slots. Ported from winding.ts computeWindingFactorAtHarmonic.
func windingFactorAtHarmonic(slots, poles, nu, w int) (float64, bool) {
	pp := validatePolePairs(slots, poles)
	if pp < 0 || nu <= 0 {
		return 0, false
	}
	var re, im float64
	count := 0
	for k := 0; k < slots; k++ {
		z := slotZone(k, slots, pp)
		if z.phase != 0 { // phase A only
			continue
		}
		theta := float64(k) * 2 * math.Pi / float64(slots)
		re += z.sign * math.Cos(float64(nu)*theta)
		im += z.sign * math.Sin(float64(nu)*theta)
		count++
	}
	if count == 0 {
		return 0, false
	}
	kd := math.Hypot(re, im) / float64(count)
	kp := math.Abs(math.Sin(float64(nu) * math.Pi * float64(w) / float64(slots)))
	return kd * kp, true
}

// FundamentalWindingFactor returns k_w1 for a balanced 3-phase (Q,p) winding, choosing a
// full-pitch coil for integer-slot windings and an adjacent-slot coil for FSCW.
// Ported from winding.ts computeWindingFactor. ok is false for an invalid combination.
func FundamentalWindingFactor(slots, poles int) (kw float64, ok bool) {
	pp := validatePolePairs(slots, poles)
	if pp < 0 {
		return 0, false
	}
	q := float64(slots) / float64(3*poles)
	w := 1
	if isInteger(q) && q >= 1 {
		w = slots / poles
	}
	return windingFactorAtHarmonic(slots, poles, pp, w)
}

func isInteger(x float64) bool {
	return x == math.Trunc(x)
}

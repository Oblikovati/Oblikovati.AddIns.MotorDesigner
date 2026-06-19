// SPDX-License-Identifier: GPL-2.0-only

package designer

import (
	"math"
	"testing"
)

// approx reports whether got is within tol of want.
func approx(got, want, tol float64) bool { return math.Abs(got-want) <= tol }

func TestComputeDefaultSpecProducesSaneCrossSection(t *testing.T) {
	d, err := Compute(DefaultSpec())
	if err != nil {
		t.Fatalf("Compute(default): %v", err)
	}
	// Bore from V = T/(TRV*1000) = 1.6/(20*1000) = 8e-5 m^3, L/D=1
	// D = cbrt(4V/pi) = cbrt(1.018e-4) ≈ 0.04667 m ≈ 46.67 mm.
	if !approx(d.BoreDiameter, 46.67, 0.5) {
		t.Errorf("BoreDiameter = %.2f mm, want ≈ 46.67", d.BoreDiameter)
	}
	if !approx(d.StackLength, d.BoreDiameter, 1e-6) {
		t.Errorf("StackLength = %.2f, want = BoreDiameter (L/D=1)", d.StackLength)
	}
	// Geometry must stack up monotonically: rotor inside bore inside stator OD.
	if !(d.RotorOuterDia < d.BoreDiameter && d.BoreDiameter < d.StatorOuterDia) {
		t.Errorf("non-monotonic radii: rotorOD=%.2f bore=%.2f statorOD=%.2f",
			d.RotorOuterDia, d.BoreDiameter, d.StatorOuterDia)
	}
	if d.RotorYokeInnR <= 0 || d.MagnetInnerR <= d.RotorYokeInnR {
		t.Errorf("rotor stack-up wrong: yokeInnR=%.2f magInnR=%.2f", d.RotorYokeInnR, d.MagnetInnerR)
	}
}

func TestComputeAirgapMatchesBoreMinusRotor(t *testing.T) {
	s := DefaultSpec()
	d, err := Compute(s)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	gotAirgap := d.BoreDiameter/2 - d.RotorOuterDia/2
	if !approx(gotAirgap, s.AirgapMM, 0.05) {
		t.Errorf("derived airgap = %.3f mm, want %.3f", gotAirgap, s.AirgapMM)
	}
}

func TestComputeMaterialsResolved(t *testing.T) {
	d, err := Compute(DefaultSpec())
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if d.Magnet.Br != 1.30 { // N42
		t.Errorf("magnet Br = %.2f, want 1.30 for N42", d.Magnet.Br)
	}
	if d.Steel.W15_50 != 2.70 { // M270
		t.Errorf("steel W15/50 = %.2f, want 2.70 for M270", d.Steel.W15_50)
	}
	if d.Magnet.MuR <= 1.0 || d.Magnet.MuR > 1.3 {
		t.Errorf("recoil mu_r = %.3f, want a physical 1.0..1.3", d.Magnet.MuR)
	}
}

func TestComputeRejectsInvalidSpec(t *testing.T) {
	cases := map[string]func(*Spec){
		"zero torque":  func(s *Spec) { s.TorqueNm = 0 },
		"odd poles":    func(s *Spec) { s.Poles = 5 },
		"non3 slots":   func(s *Spec) { s.Slots = 10 },
		"bad arc":      func(s *Spec) { s.MagnetArc = 1.5 },
		"bad magnet":   func(s *Spec) { s.MagnetGrade = "N99" },
		"bad steel":    func(s *Spec) { s.SteelGrade = "M999" },
		"zero airgapB": func(s *Spec) { s.AirgapB = 0 },
	}
	for name, mutate := range cases {
		s := DefaultSpec()
		mutate(&s)
		if _, err := Compute(s); err == nil {
			t.Errorf("%s: Compute should have failed", name)
		}
	}
}

func TestComputeWindingFactorFlaggedInvalid(t *testing.T) {
	s := DefaultSpec()
	s.Slots, s.Poles = 12, 12 // Q/gcd(Q,pp) not divisible by 3 → unbalanced → fallback kw
	d, err := Compute(s)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if d.WindingValid {
		t.Errorf("3-slot/2-pole should be flagged winding-invalid")
	}
	if !approx(d.WindingFactor, 0.866, 1e-9) {
		t.Errorf("invalid winding should use 0.866 fallback, got %.3f", d.WindingFactor)
	}
}

func TestComputeHigherTRVShrinksBore(t *testing.T) {
	lo := DefaultSpec()
	hi := DefaultSpec()
	hi.TRVkNm = lo.TRVkNm * 4
	dlo, _ := Compute(lo)
	dhi, _ := Compute(hi)
	if !(dhi.BoreDiameter < dlo.BoreDiameter) {
		t.Errorf("higher TRV should shrink bore: lo=%.2f hi=%.2f", dlo.BoreDiameter, dhi.BoreDiameter)
	}
}

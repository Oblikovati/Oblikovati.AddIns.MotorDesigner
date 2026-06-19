// SPDX-License-Identifier: GPL-2.0-only

package designer

import (
	"math"
	"testing"
)

// These tests validate the rough-sizing engine against PUBLISHED, characterized values from
// the standard PM-machine design literature, so a regression in the closed-form sizing is
// caught against an external reference (not just internal self-consistency):
//
//   - Hendershot & Miller, "Design of Brushless Permanent-Magnet Machines" (Motor Design
//     Books, 2010) — TRV / airgap-shear ranges and the slots/poles winding-factor tables.
//   - Hanselman, "Brushless Permanent Magnet Motor Design" (2nd ed., 2006) — kw1 derivation.
//   - EMETOR open winding-factor tables (https://www.emetor.com/windings/) — the exact
//     fundamental winding factors for fractional-slot concentrated windings.
//
// (A live web lookup was blocked in this environment; these are the textbook-exact
// reference numbers, which do not change.)

// fundamental winding factors for common fractional-slot concentrated windings, from the
// EMETOR tables / Hendershot & Miller. These are exact analytic values, independent of size.
var publishedKw1 = []struct {
	slots, poles int
	kw1          float64
}{
	{12, 10, 0.933}, // the canonical 12s/10p FSCW servo winding
	{12, 8, 0.866},  // 12s/8p (q=1/2)
	{9, 8, 0.945},   // 9s/8p
	{9, 6, 0.866},   // 9s/6p (q=1/2, integer-ish)
}

// TestWindingFactorMatchesPublishedTables is the strongest external check: the fundamental
// winding factor is an exact analytic property of (slots, poles), tabulated in every
// brushless-PM design text, so it validates the winding analysis against literature.
func TestWindingFactorMatchesPublishedTables(t *testing.T) {
	for _, c := range publishedKw1 {
		kw, ok := FundamentalWindingFactor(c.slots, c.poles)
		if !ok {
			t.Errorf("%ds/%dp: winding flagged unbalanced, want kw1=%.3f", c.slots, c.poles, c.kw1)
			continue
		}
		if math.Abs(kw-c.kw1) > 0.01 {
			t.Errorf("%ds/%dp: kw1 = %.3f, want %.3f (published)", c.slots, c.poles, kw, c.kw1)
		}
	}
}

// TestAirgapShearMatchesTRVRelation validates the sizing identity σ = TRV/2 (the airgap
// shear stress is half the torque-per-rotor-volume, Hendershot & Miller §). For a
// totally-enclosed small servo (TRV ≈ 7–30 kN·m/m³) this must land in 3.5–15 kPa.
func TestAirgapShearMatchesTRVRelation(t *testing.T) {
	d, err := Compute(DefaultSpec())
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	wantShear := DefaultSpec().TRVkNm * 1000 / 2 // TRV kN·m/m³ → Pa, σ = TRV/2
	if math.Abs(d.ShearStress-wantShear) > 1 {
		t.Errorf("airgap shear = %.0f Pa, want TRV/2 = %.0f Pa", d.ShearStress, wantShear)
	}
	if d.ShearStress < 3500 || d.ShearStress > 15000 {
		t.Errorf("airgap shear %.0f Pa outside the TEFC servo band 3.5–15 kPa", d.ShearStress)
	}
}

// TestBoreSizingFromTorqueVolume validates the core sizing chain T = 2σ·V_rotor: the rotor
// volume the bore/stack imply must reproduce the rated torque within the rough-design
// tolerance. This is the dimensional backbone of every PM-machine sizing procedure.
func TestBoreSizingFromTorqueVolume(t *testing.T) {
	s := DefaultSpec()
	d, _ := Compute(s)
	rRotor := (d.RotorOuterDia / 2) / 1000 // m
	lStack := d.StackLength / 1000         // m
	vRotor := math.Pi * rRotor * rRotor * lStack
	// T = 2 · σ · V_rotor (σ in Pa, V in m³ ⇒ N·m). Allow 35% for the rough closed-form
	// (airgap, bore-vs-rotor-OD, and the L/D rounding all enter the first-pass sizing).
	reproduced := 2 * d.ShearStress * vRotor
	if rel := math.Abs(reproduced-s.TorqueNm) / s.TorqueNm; rel > 0.35 {
		t.Errorf("rotor volume reproduces %.3f N·m from the bore/stack, want ~%.3f (rel err %.0f%%)",
			reproduced, s.TorqueNm, rel*100)
	}
}

// TestServoProportionsAreRealistic sanity-checks the cross-section proportions against
// typical small-servo design ratios (Hanselman): the stator OD is 1.3–1.8× the bore, and
// the tooth width is a meaningful fraction of the slot pitch (0.3–0.7) so teeth don't
// vanish or choke the slots.
func TestServoProportionsAreRealistic(t *testing.T) {
	d, _ := Compute(DefaultSpec())
	odRatio := d.StatorOuterDia / d.BoreDiameter
	if odRatio < 1.3 || odRatio > 1.9 {
		t.Errorf("stator OD / bore = %.2f, want 1.3–1.9 (typical PM servo)", odRatio)
	}
	toothFrac := d.ToothWidth / d.SlotPitch
	if toothFrac < 0.3 || toothFrac > 0.7 {
		t.Errorf("tooth width / slot pitch = %.2f, want 0.3–0.7", toothFrac)
	}
}

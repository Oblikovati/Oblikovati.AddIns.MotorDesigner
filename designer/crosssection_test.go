// SPDX-License-Identifier: GPL-2.0-only

package designer

import (
	"math"
	"testing"
)

// TestBuildCrossSectionHasOneMagnetLoopPerPole pins the rotor magnet partition: one closed
// loop per pole, each a non-degenerate polygon.
func TestBuildCrossSectionHasOneMagnetLoopPerPole(t *testing.T) {
	d, err := Compute(DefaultSpec())
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	cs := BuildCrossSection(d)
	if len(cs.Magnets) != d.Spec.Poles {
		t.Fatalf("magnet loops = %d, want %d (one per pole)", len(cs.Magnets), d.Spec.Poles)
	}
	for i, loop := range cs.Magnets {
		if len(loop) < 4 {
			t.Errorf("magnet %d loop has %d points, want >= 4 (closed segment)", i, len(loop))
		}
	}
}

// TestStatorBoundaryRadiiWithinBand pins that every toothed-boundary point lies between the
// slot-bottom radius and the stator outer radius — i.e. teeth never overshoot the OD and
// slots never cut past the yoke, so the profile is clean (no self-intersection inward/outward).
func TestStatorBoundaryRadiiWithinBand(t *testing.T) {
	d, _ := Compute(DefaultSpec())
	cs := BuildCrossSection(d)
	rMin := mmToCM(d.SlotOuterR) - 1e-9
	rMax := mmToCM(d.StatorOuterDia/2) + 1e-9
	for i, p := range cs.StatorOuter {
		r := math.Hypot(p.X, p.Y)
		if r < rMin || r > rMax {
			t.Errorf("stator boundary point %d radius %.4f cm outside [%.4f, %.4f]", i, r, rMin, rMax)
		}
	}
}

// TestMagnetsSitInAirgapBand pins the rotor stack-up: magnet points lie between the magnet
// inner radius (rotor iron OD) and the rotor OD (airgap surface), and clear of the bore.
func TestMagnetsSitInAirgapBand(t *testing.T) {
	d, _ := Compute(DefaultSpec())
	cs := BuildCrossSection(d)
	rIn := mmToCM(d.MagnetInnerR) - 1e-9
	rOut := mmToCM(d.RotorOuterDia/2) + 1e-9
	for _, loop := range cs.Magnets {
		for _, p := range loop {
			r := math.Hypot(p.X, p.Y)
			if r < rIn || r > rOut {
				t.Errorf("magnet point radius %.4f cm outside magnet band [%.4f, %.4f]", r, rIn, rOut)
			}
		}
	}
	if cs.ShaftRadiusCM <= 0 {
		t.Errorf("shaft bore radius = %.4f cm, want > 0 (rotor must be hollow)", cs.ShaftRadiusCM)
	}
}

// TestToothAngularFractionClamped pins the degenerate-input guard: a zero slot pitch yields
// a sane half-split rather than a NaN/zero-width tooth.
func TestToothAngularFractionClamped(t *testing.T) {
	if got := toothAngularFraction(&Design{}); got != 0.5 {
		t.Errorf("toothAngularFraction(zero) = %v, want 0.5", got)
	}
	wide := &Design{SlotPitch: 1, ToothWidth: 5} // would be 5.0 unclamped
	if got := toothAngularFraction(wide); got > 0.9 {
		t.Errorf("toothAngularFraction not clamped: got %v, want <= 0.9", got)
	}
}

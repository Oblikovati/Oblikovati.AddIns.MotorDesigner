// SPDX-License-Identifier: GPL-2.0-only

package designer

import (
	"math"
	"testing"
)

// maxRadius/minRadius are the extreme distances-from-axis of a loop (cm).
func maxRadius(loop []Point2) float64 {
	m := 0.0
	for _, p := range loop {
		if r := math.Hypot(p.X, p.Y); r > m {
			m = r
		}
	}
	return m
}

func minRadius(loop []Point2) float64 {
	m := math.MaxFloat64
	for _, p := range loop {
		if r := math.Hypot(p.X, p.Y); r < m {
			m = r
		}
	}
	return m
}

// TestInrunnerTeethPointInward pins the inrunner correctness fix: the stator teeth must face
// the bore (the airgap is INSIDE the stator), so the toothed boundary's MAX radius (the
// smooth yoke side is StatorOuter) stays at or below the smooth outer yoke — the teeth do
// not stick out past the OD. Conversely the rotor sits entirely inside the stator bore.
func TestInrunnerTeethPointInward(t *testing.T) {
	d, _ := Compute(DefaultSpec())
	cs := BuildCrossSection(d)
	yokeOD := maxRadius(cs.StatorOuter)
	boreToothTip := minRadius(cs.StatorBore)
	slotBottom := maxRadius(cs.StatorBore)
	// Teeth point inward: tooth tips (bore) are the smallest stator radius, slots recede
	// outward toward the yoke, and nothing exceeds the smooth OD.
	if boreToothTip >= slotBottom {
		t.Errorf("inrunner: tooth tip r=%.3f should be < slot bottom r=%.3f (teeth point inward)", boreToothTip, slotBottom)
	}
	if slotBottom > yokeOD+1e-9 {
		t.Errorf("inrunner: slot bottom r=%.3f exceeds stator OD r=%.3f", slotBottom, yokeOD)
	}
	// The rotor (with magnets) is fully inside the stator bore.
	if rOut := maxRadius(cs.Magnets[0]); rOut > boreToothTip+1e-9 {
		t.Errorf("inrunner: magnet r=%.3f should be inside the stator bore r=%.3f", rOut, boreToothTip)
	}
}

// TestOutrunnerTeethPointOutward pins the outrunner layout: the stator is the inner member
// with teeth pointing OUTWARD (tooth tips are the largest stator radius), and the rotor is
// an outer ring whose magnets sit OUTSIDE the stator teeth.
func TestOutrunnerTeethPointOutward(t *testing.T) {
	s := DefaultSpec()
	s.Type = Outrunner
	d, _ := Compute(s)
	cs := BuildCrossSection(d)
	toothTip := maxRadius(cs.StatorBore)   // outward-facing tip = largest stator radius
	slotBottom := minRadius(cs.StatorBore) // slots recede inward
	statorYokeID := maxRadius(cs.StatorOuter)
	if toothTip <= slotBottom {
		t.Errorf("outrunner: tooth tip r=%.3f should be > slot bottom r=%.3f (teeth point outward)", toothTip, slotBottom)
	}
	if statorYokeID > toothTip+1e-9 {
		t.Errorf("outrunner: smooth stator yoke r=%.3f should be the inner side, below tooth tip r=%.3f", statorYokeID, toothTip)
	}
	// Magnets are OUTSIDE the stator teeth (the outer rotor ring).
	if magIn := minRadius(cs.Magnets[0]); magIn < toothTip-1e-9 {
		t.Errorf("outrunner: magnet inner r=%.3f should be outside the stator tooth tip r=%.3f", magIn, toothTip)
	}
	// The rotor ring is the outermost iron.
	if rotorOD := maxRadius(cs.RotorOuter); rotorOD < maxRadius(cs.Magnets[0])-1e-9 {
		t.Errorf("outrunner: rotor ring OD r=%.3f should enclose the magnets r=%.3f", rotorOD, maxRadius(cs.Magnets[0]))
	}
}

// TestBuildCrossSectionHasOneMagnetLoopPerPole pins the rotor magnet partition: one closed
// loop per pole, each a non-degenerate polygon, for both motor types.
func TestBuildCrossSectionHasOneMagnetLoopPerPole(t *testing.T) {
	for _, mt := range []MotorType{Inrunner, Outrunner} {
		s := DefaultSpec()
		s.Type = mt
		d, err := Compute(s)
		if err != nil {
			t.Fatalf("Compute(%s): %v", mt, err)
		}
		cs := BuildCrossSection(d)
		if len(cs.Magnets) != d.Spec.Poles {
			t.Fatalf("%s magnet loops = %d, want %d", mt, len(cs.Magnets), d.Spec.Poles)
		}
		for i, loop := range cs.Magnets {
			if len(loop) < 4 {
				t.Errorf("%s magnet %d loop has %d points, want >= 4", mt, i, len(loop))
			}
		}
	}
}

// TestStatorIsHollowBothTypes pins that the stator iron is a true annulus (toothed airgap
// boundary distinct from the smooth yoke) and the rotor has an inner hole, for both types.
func TestStatorIsHollowBothTypes(t *testing.T) {
	for _, mt := range []MotorType{Inrunner, Outrunner} {
		s := DefaultSpec()
		s.Type = mt
		d, _ := Compute(s)
		cs := BuildCrossSection(d)
		if minRadius(cs.RotorInner) <= 0 {
			t.Errorf("%s: rotor inner boundary radius must be > 0 (hollow)", mt)
		}
		if len(cs.StatorBore) < d.Spec.Slots*2 {
			t.Errorf("%s: stator bore should be toothed (>= %d points), got %d", mt, d.Spec.Slots*2, len(cs.StatorBore))
		}
	}
}

// TestDesignToothFractionClamped pins the degenerate-input guard: a zero slot pitch yields a
// sane half-split rather than a NaN/zero-width tooth.
func TestDesignToothFractionClamped(t *testing.T) {
	if got := designToothFraction(&Design{}); got != 0.5 {
		t.Errorf("designToothFraction(zero) = %v, want 0.5", got)
	}
	wide := &Design{SlotPitch: 1, ToothWidth: 5} // 5.0 unclamped
	if got := designToothFraction(wide); got > 0.9 {
		t.Errorf("designToothFraction not clamped: got %v, want <= 0.9", got)
	}
}

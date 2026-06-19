// SPDX-License-Identifier: GPL-2.0-only

package designer

import (
	"math"
	"testing"
)

// TestSlotProfilesProduceValidToothedLoops checks every literature slot profile yields a
// detailed closed stator-bore loop whose points stay inside the [tooth-tip, slot-bottom] band.
func TestSlotProfilesProduceValidToothedLoops(t *testing.T) {
	for _, st := range []SlotType{SlotParallelTooth, SlotOpenRectangular, SlotRoundBottom} {
		s := DefaultSpec()
		s.SlotType = st
		d, err := Compute(s)
		if err != nil {
			t.Fatalf("%s: compute: %v", st, err)
		}
		cs := BuildCrossSection(d)
		if len(cs.StatorBore) < d.Spec.Slots*4 {
			t.Errorf("%s: stator bore = %d pts, want a detailed toothed loop", st, len(cs.StatorBore))
		}
		lo := math.Min(mmToCM(d.ToothTipR), mmToCM(d.SlotOuterR)) - 0.05
		hi := math.Max(mmToCM(d.ToothTipR), mmToCM(d.SlotOuterR)) + 0.05
		for _, p := range cs.StatorBore {
			if r := math.Hypot(p.X, p.Y); r < lo || r > hi {
				t.Errorf("%s: bore point r=%.3f outside band [%.3f,%.3f]", st, r, lo, hi)
				break
			}
		}
	}
}

// TestParallelToothHasShoeAndOpening pins the semi-closed slot's defining features: the tooth
// is WIDER at the airgap tip (the shoe) than in its body, and the slot opening between adjacent
// shoes equals the configured SlotOpeningMM.
func TestParallelToothHasShoeAndOpening(t *testing.T) {
	d, err := Compute(DefaultSpec())
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	g := resolveLayout(d).resolveSlotGeom(d)

	tipWidth := 2 * g.rTip * math.Sin(g.shoeHalfAtTip)          // tooth shoe width at the airgap
	bodyWidth := 2 * g.rNeck * math.Sin(g.toothHalfAt(g.rNeck)) // tooth body width
	if tipWidth <= bodyWidth {
		t.Errorf("tooth shoe width %.3f cm should exceed body width %.3f cm (no shoe)", tipWidth, bodyWidth)
	}

	opening := g.rTip * (g.step - 2*g.shoeHalfAtTip) // arc gap between adjacent shoes at the bore
	want := mmToCM(d.Spec.SlotOpeningMM)
	if math.Abs(opening-want) > 1e-6 {
		t.Errorf("slot opening = %.4f cm, want %.4f (SlotOpeningMM)", opening, want)
	}
}

// SPDX-License-Identifier: GPL-2.0-only

package designer

import (
	"fmt"
	"math"
)

// validateToothGeometry rejects a sized design whose stator tooth profile would be degenerate —
// before the host wastes work building an unbuildable sketch. Unlike Spec.Validate (raw inputs),
// these checks need the SIZED radii, so Compute runs them after sizing. The constraints depend on
// the selected slot type: the constant-width profiles (open-rectangular, parallel-tooth) require
// the tooth half-width to fit inside the smallest radius its flanks span (a coarse slot count
// makes an outrunner's teeth wider than its small yoke ring, so the profile can't close); the
// shoe profiles (parallel-tooth, round-bottom) additionally require a positive shoe angle and a
// neck that stays within the slot depth. Lengths compare in cm (the builders' unit); messages
// report the user's mm.
func validateToothGeometry(d *Design) error {
	l := resolveLayout(d)
	tip, root := l.toothTipR, toothRootR(l)
	depth := math.Abs(tip - root)
	if depth < 1e-3*tip {
		return fmt.Errorf("designer: degenerate tooth — tip and slot-bottom radii nearly coincide (slot depth %.3f mm); the sized slot has no radial depth", depth*10)
	}
	st := d.Spec.normSlotType()
	if st != SlotRoundBottom { // constant-width body: the flank must reach the smallest arc
		if err := validateToothWidthFits(d, mmToCM(d.ToothWidth)/2, math.Min(tip, root)); err != nil {
			return err
		}
	}
	if st == SlotParallelTooth || st == SlotRoundBottom { // semi-closed: needs a real shoe
		return validateShoeFits(d, tip, depth)
	}
	return nil
}

// validateToothWidthFits rejects a constant-width tooth whose half-width reaches (within 5%) the
// smallest radius its flanks must span — past that the flank can no longer meet that arc and the
// profile fails to close (the coarse-slot outrunner whose teeth exceed the small yoke ring).
func validateToothWidthFits(d *Design, halfWidth, minRadius float64) error {
	if halfWidth >= 0.95*minRadius {
		return fmt.Errorf("designer: tooth too wide for the slot count — half-width %.3f mm reaches the smallest tooth radius %.3f mm; increase Slots (currently %d) or reduce the sized tooth width (raise ToothB / lower AirgapB)",
			halfWidth*10, minRadius*10, d.Spec.Slots)
	}
	return nil
}

// validateShoeFits rejects a shoe that cannot exist: a slot opening so wide it consumes the slot
// pitch (no positive shoe half-angle), or a tip height at least as deep as the slot (the shoe neck
// would reach or cross the slot bottom).
func validateShoeFits(d *Design, tip, depth float64) error {
	opening := mmToCM(d.Spec.slotOpeningMM())
	slotPitch := 2 * math.Pi / float64(d.Spec.Slots)
	if opening >= 2*tip || math.Asin(opening/(2*tip)) >= slotPitch/2 {
		return fmt.Errorf("designer: slot opening %.3f mm too wide at %d slots — no room for a tooth shoe; reduce SlotOpeningMM or increase Slots",
			d.Spec.slotOpeningMM(), d.Spec.Slots)
	}
	if tipHeight := mmToCM(d.Spec.toothTipHeightMM()); tipHeight >= depth {
		return fmt.Errorf("designer: tooth tip height %.3f mm at least as deep as the slot %.3f mm — the shoe neck would cross the slot bottom; reduce ToothTipHeightMM",
			d.Spec.toothTipHeightMM(), depth*10)
	}
	return nil
}

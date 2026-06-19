// SPDX-License-Identifier: GPL-2.0-only

package designer

import "math"

// Stator slot profiles from motor-design literature. The stator's toothed airgap boundary is
// one closed loop tracing, per slot pitch: a tooth shoe arc at the airgap, the slot walls down
// into the iron, and the slot bottom. Three standard profiles are offered (Spec.SlotType):
//
//   - parallel-tooth (semi-closed): constant-width teeth with shoes + a narrow slot opening and
//     a round bottom — the canonical PMSM/BLDC slot (the "T-shaped tooth"). Default.
//   - open-rectangular: parallel-sided teeth, slot fully open at the airgap (no shoe).
//   - round-bottom (pear): shoe + radial tooth sides widening to a large rounded bottom.
//
// All work for both inrunner (teeth point inward, radius grows into the yoke) and outrunner
// (teeth point outward) via the signed radial direction between the tooth tip and slot bottom.

// statorInnerLoop dispatches to the selected slot-profile generator.
func (l layout) statorInnerLoop(d *Design) []Point2 {
	switch d.Spec.normSlotType() {
	case SlotOpenRectangular:
		return l.openRectSlots(d)
	case SlotRoundBottom:
		return l.roundBottomSlots(d)
	default:
		return l.parallelToothSlots(d)
	}
}

// slotGeom holds the per-slot radii/angles shared by the profile builders, all in cm/radians.
type slotGeom struct {
	slots         int
	step          float64 // angular slot pitch
	rTip, rBot    float64 // tooth-tip (airgap) and slot-bottom radii
	dir           float64 // +1 if the slot recedes to larger radius (inrunner), else -1
	toothHalfAt   func(r float64) float64
	rOpen, rNeck  float64 // top of the slot opening, and where the shoe meets the tooth body
	shoeHalfAtTip float64 // half angular width of a tooth shoe at the airgap
}

// resolveSlotGeom derives the shared slot geometry from the sized design.
func (l layout) resolveSlotGeom(d *Design) slotGeom {
	step := 2 * math.Pi / float64(d.Spec.Slots)
	dir := math.Copysign(1, l.slotBottomR-l.toothTipR)
	shoe := mmToCM(d.Spec.toothTipHeightMM())
	bt := mmToCM(d.ToothWidth)
	bs0 := mmToCM(d.Spec.slotOpeningMM())
	// Parallel tooth side: the side lies at perpendicular distance bt/2 from the tooth's radial
	// centreline, so its angular offset at radius r is asin(bt/2r) — the tooth keeps constant
	// width while the slot widens with radius.
	toothHalf := func(r float64) float64 { return math.Asin(math.Min(1, bt/(2*math.Abs(r)))) }
	// Shoe half-width at the airgap leaves a slot opening bs0 between adjacent shoes.
	shoeHalf := (step - bs0/math.Abs(l.toothTipR)) / 2
	return slotGeom{
		slots: d.Spec.Slots, step: step, rTip: l.toothTipR, rBot: l.slotBottomR, dir: dir,
		toothHalfAt:   toothHalf,
		rOpen:         l.toothTipR + dir*0.5*shoe,
		rNeck:         l.toothTipR + dir*shoe,
		shoeHalfAtTip: shoeHalf,
	}
}

// parallelToothSlots builds the semi-closed, constant-tooth-width slot (the default).
func (l layout) parallelToothSlots(d *Design) []Point2 {
	g := l.resolveSlotGeom(d)
	var loop []Point2
	for i := 0; i < g.slots; i++ {
		c := float64(i) * g.step
		// Tooth shoe (outer face) at the airgap.
		loop = append(loop, arcPoints(g.rTip, c-g.shoeHalfAtTip, c+g.shoeHalfAtTip)...)
		// Right wall down into the slot: opening wall, shoe underside, parallel tooth side.
		loop = append(loop,
			polar(g.rOpen, c+g.shoeHalfAtTip),
			polar(g.rNeck, c+g.toothHalfAt(g.rNeck)),
			polar(g.rBot, c+g.toothHalfAt(g.rBot)))
		// Round slot bottom across to the next tooth.
		loop = append(loop, arcPoints(g.rBot, c+g.toothHalfAt(g.rBot), c+g.step-g.toothHalfAt(g.rBot))...)
		// Next tooth's left wall up: parallel side, shoe underside, opening wall.
		loop = append(loop,
			polar(g.rNeck, c+g.step-g.toothHalfAt(g.rNeck)),
			polar(g.rOpen, c+g.step-g.shoeHalfAtTip),
			polar(g.rTip, c+g.step-g.shoeHalfAtTip))
	}
	return loop
}

// openRectSlots builds parallel-sided teeth with the slot fully open at the airgap (no shoe).
func (l layout) openRectSlots(d *Design) []Point2 {
	g := l.resolveSlotGeom(d)
	var loop []Point2
	for i := 0; i < g.slots; i++ {
		c := float64(i) * g.step
		// Tooth tip = the tooth body width (no overhang), at the airgap.
		loop = append(loop, arcPoints(g.rTip, c-g.toothHalfAt(g.rTip), c+g.toothHalfAt(g.rTip))...)
		// Straight parallel side down to the bottom, slot bottom arc, next side up.
		loop = append(loop, polar(g.rBot, c+g.toothHalfAt(g.rBot)))
		loop = append(loop, arcPoints(g.rBot, c+g.toothHalfAt(g.rBot), c+g.step-g.toothHalfAt(g.rBot))...)
		loop = append(loop, polar(g.rTip, c+g.step-g.toothHalfAt(g.rTip)))
	}
	return loop
}

// roundBottomSlots builds a semi-closed pear slot: shoe + radial (tapered) tooth sides widening
// to a large rounded bottom. The radial sides hold a constant tooth angular fraction, and the
// bottom is a deeper arc so the slot reads as a teardrop.
func (l layout) roundBottomSlots(d *Design) []Point2 {
	g := l.resolveSlotGeom(d)
	toothFrac := designToothFraction(d)
	half := toothFrac * g.step / 2 // radial (constant-angle) tooth half-width
	var loop []Point2
	for i := 0; i < g.slots; i++ {
		c := float64(i) * g.step
		loop = append(loop, arcPoints(g.rTip, c-g.shoeHalfAtTip, c+g.shoeHalfAtTip)...)
		// Shoe underside in to the radial tooth side, then radial side down to the bottom.
		loop = append(loop,
			polar(g.rOpen, c+g.shoeHalfAtTip),
			polar(g.rNeck, c+half),
			polar(g.rBot, c+half))
		// Large rounded bottom across the full slot span between the radial sides.
		loop = append(loop, arcPoints(g.rBot, c+half, c+g.step-half)...)
		loop = append(loop,
			polar(g.rNeck, c+g.step-half),
			polar(g.rOpen, c+g.step-g.shoeHalfAtTip),
			polar(g.rTip, c+g.step-g.shoeHalfAtTip))
	}
	return loop
}

// polar is a point at radius r (cm) and angle a (rad).
func polar(r, a float64) Point2 { return Point2{X: r * math.Cos(a), Y: r * math.Sin(a)} }

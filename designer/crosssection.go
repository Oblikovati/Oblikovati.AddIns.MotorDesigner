// SPDX-License-Identifier: GPL-2.0-only

package designer

import "math"

// CrossSection is the closed-loop description of the motor's 2D cross-section, in
// centimetres (the host sketch unit), ready to lay down as sketch polylines/circles. It is
// computed purely from a Design so it unit-tests with no host. Every loop is a clean,
// non-self-intersecting closed polygon — the shape the FEMM bridge later sections via
// Body.CalculateStrokes.
//
// The component split mirrors the FEMM regions:
//   - StatorOuter: the toothed outer boundary of the stator iron (slots cut inward);
//   - BoreRadiusCM: the stator bore circle (the inner hole of the stator annulus);
//   - RotorOuterRadiusCM / ShaftRadiusCM: the rotor back-iron annulus;
//   - Magnets: one closed loop per pole (the permanent-magnet segments).
type CrossSection struct {
	StatorOuter      []Point2   // toothed outer boundary of the stator iron (cm)
	BoreRadiusCM     float64    // stator bore / tooth-tip circle radius (cm)
	RotorOuterRadius float64    // rotor iron outer radius (cm) — magnet inner radius for SPM
	ShaftRadiusCM    float64    // rotor shaft bore radius (cm)
	Magnets          [][]Point2 // one closed magnet loop per pole (cm)
}

// Point2 is a 2D sketch-space point in centimetres.
type Point2 struct{ X, Y float64 }

// mmToCM converts a millimetre Design dimension to the host's centimetre sketch unit.
func mmToCM(mm float64) float64 { return mm / 10.0 }

// teethArcSteps is how many straight segments approximate each curved span (tooth tip,
// slot bottom, magnet arc). Enough that the faceted boundary tracks the circle within the
// FEMM section tolerance, without exploding the entity count.
const teethArcSteps = 6

// BuildCrossSection turns a sized Design into its closed-loop 2D cross-section (cm).
func BuildCrossSection(d *Design) CrossSection {
	return CrossSection{
		StatorOuter:      statorOuterLoop(d),
		BoreRadiusCM:     mmToCM(d.ToothTipR),
		RotorOuterRadius: mmToCM(d.MagnetInnerR),
		ShaftRadiusCM:    mmToCM(d.RotorYokeInnR),
		Magnets:          magnetLoops(d),
	}
}

// statorOuterLoop builds the toothed outer boundary of the stator iron as one closed
// polygon. Each of the Slots teeth spans a tooth (at the yoke-inner radius, the wide part)
// and a slot opening (cut inward to the slot-bottom radius). The boundary alternates
// tooth-top arc → slot-side → slot-bottom arc → slot-side around the axis.
//
// The radii: teeth reach the stator outer radius (yoke OD); slots are cut to the
// slot-bottom radius (bore + slot depth). The tooth angular fraction is tooth_width /
// slot_pitch at the bore, so wider teeth leave narrower slots, matching the Design sizing.
func statorOuterLoop(d *Design) []Point2 {
	slots := d.Spec.Slots
	rOuter := mmToCM(d.StatorOuterDia / 2) // tooth top / yoke OD
	rSlot := mmToCM(d.SlotOuterR)          // slot bottom (yoke inner)
	toothFrac := toothAngularFraction(d)
	step := 2 * math.Pi / float64(slots)
	var loop []Point2
	for i := 0; i < slots; i++ {
		c := float64(i) * step
		half := toothFrac * step / 2
		loop = append(loop, arcPoints(rOuter, c-half, c+half)...)
		loop = append(loop, arcPoints(rSlot, c+half, c+step-half)...)
	}
	return loop
}

// toothAngularFraction is the share of one slot pitch occupied by tooth iron (vs the slot
// opening), from the sized tooth width and slot pitch, clamped to a sane (0.1, 0.9) band so
// degenerate sizing can never produce a zero-width tooth or slot.
func toothAngularFraction(d *Design) float64 {
	if d.SlotPitch <= 0 {
		return 0.5
	}
	return math.Max(0.1, math.Min(0.9, d.ToothWidth/d.SlotPitch))
}

// magnetLoops builds one closed magnet loop per pole for a surface-PM rotor: an annular
// arc segment between the magnet inner and outer radii, spanning MagnetArcDeg, centered on
// each pole position. Interior-PM (IPM) buries them in pockets; until pocket modelling
// lands, IPM reuses the same surface segments (the FEMM region map is identical).
func magnetLoops(d *Design) [][]Point2 {
	poles := d.Spec.Poles
	rIn := mmToCM(d.MagnetInnerR)
	rOut := mmToCM(d.RotorOuterDia / 2)
	half := (d.MagnetArcDeg * math.Pi / 180) / 2
	step := 2 * math.Pi / float64(poles)
	loops := make([][]Point2, 0, poles)
	for i := 0; i < poles; i++ {
		c := float64(i) * step
		loops = append(loops, magnetSegment(rIn, rOut, c-half, c+half))
	}
	return loops
}

// magnetSegment is one closed annular-arc loop: outer arc (start→end) then inner arc back
// (end→start), forming a clean closed magnet cross-section.
func magnetSegment(rIn, rOut, a0, a1 float64) []Point2 {
	loop := arcPoints(rOut, a0, a1)
	loop = append(loop, arcPoints(rIn, a1, a0)...)
	return loop
}

// arcPoints samples an arc of radius r from angle a0 to a1 (inclusive) into
// teethArcSteps+1 points (the fixed facet count that tracks curved spans within the FEMM
// section tolerance).
func arcPoints(r, a0, a1 float64) []Point2 {
	pts := make([]Point2, 0, teethArcSteps+1)
	for i := 0; i <= teethArcSteps; i++ {
		a := a0 + (a1-a0)*float64(i)/float64(teethArcSteps)
		pts = append(pts, Point2{X: r * math.Cos(a), Y: r * math.Sin(a)})
	}
	return pts
}

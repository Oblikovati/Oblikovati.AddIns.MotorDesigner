// SPDX-License-Identifier: GPL-2.0-only

package designer

import "math"

// CrossSection is the closed-loop description of the motor's 2D cross-section, in
// centimetres (the host sketch unit), ready to lay down as sketch polylines/circles. It is
// computed purely from a Design so it unit-tests with no host. Every loop is a clean,
// non-self-intersecting closed polygon — the shape the FEMM bridge later sections via
// Body.CalculateStrokes.
//
// The teeth ALWAYS face the airgap (where the magnets are): inward for an inrunner (rotor
// inside the stator), outward for an outrunner (rotor ring outside the stator). The radial
// layout is captured in a Layout the loops are built from, so both motor types share one
// loop-builder and only their radii differ.
type CrossSection struct {
	StatorOuter []Point2   // smooth outer boundary of the stator iron (cm)
	StatorBore  []Point2   // toothed (inrunner) or smooth (outrunner) inner boundary (cm)
	RotorOuter  []Point2   // rotor iron outer boundary (cm)
	RotorInner  []Point2   // rotor iron inner boundary / shaft side (cm)
	Magnets     [][]Point2 // one closed magnet loop per pole (cm)
}

// Point2 is a 2D sketch-space point in centimetres.
type Point2 struct{ X, Y float64 }

// layout holds the topology-resolved radii (cm) the cross-section loops are built from. The
// names are role-based (airgap surface, tooth tip, slot bottom, yoke) so one set of loop
// builders serves both an inrunner and an outrunner — only these radii flip.
type layout struct {
	statorYokeR  float64 // smooth stator yoke boundary (OD for inrunner, ID for outrunner)
	toothTipR    float64 // tooth tips, at the airgap surface
	slotBottomR  float64 // slot bottoms, set back from the airgap into the yoke
	magnetTipR   float64 // magnet surface facing the airgap
	magnetBackR  float64 // magnet back (against the rotor iron)
	rotorYokeR   float64 // smooth rotor yoke boundary (away from the airgap)
	teethFaceOut bool    // teeth grow from slot bottom toward larger radius (outrunner)
	toothFrac    float64 // tooth angular share of a slot pitch [-], from the sized geometry
}

// mmToCM converts a millimetre Design dimension to the host's centimetre sketch unit.
func mmToCM(mm float64) float64 { return mm / 10.0 }

// teethArcSteps is how many straight segments approximate each curved span (tooth tip,
// slot bottom, magnet arc). Enough that the faceted boundary tracks the circle within the
// FEMM section tolerance, without exploding the entity count.
const teethArcSteps = 6

// circleSteps facets a full smooth boundary circle (yoke, shaft bore).
const circleSteps = 48

// BuildCrossSection turns a sized Design into its closed-loop 2D cross-section (cm),
// honouring the motor type (inrunner vs outrunner) so the teeth face the airgap.
//
// In both cases the stator iron is the region between its smooth yoke boundary and its
// toothed airgap boundary; the rotor iron is the annulus between its outer and inner
// boundaries, with the magnets sitting on the airgap-facing rotor surface.
func BuildCrossSection(d *Design) CrossSection {
	l := resolveLayout(d)
	return CrossSection{
		StatorOuter: l.statorOuterLoop(),
		StatorBore:  l.statorInnerLoop(d.Spec.Slots),
		RotorOuter:  circleLoopCM(l.rotorOuterR()),
		RotorInner:  circleLoopCM(l.rotorInnerR()),
		Magnets:     magnetLoops(d, l),
	}
}

// resolveLayout maps the sized Design radii into role-based layout radii for the motor type.
// Inrunner: rotor inside, teeth point inward (tooth tip at the bore, slots recede outward).
// Outrunner: the radial order is mirrored about the airgap — stator inside with teeth
// pointing outward, rotor ring outside with magnets on its inner face.
func resolveLayout(d *Design) layout {
	l := inrunnerLayout(d)
	if d.Spec.normType() == Outrunner {
		l = outrunnerLayout(d)
	}
	l.toothFrac = designToothFraction(d)
	return l
}

// designToothFraction is the tooth's angular share of one slot pitch, from the sized tooth
// width and slot pitch (both at the bore), clamped to (0.1, 0.9) so degenerate sizing never
// yields a zero-width tooth or slot.
func designToothFraction(d *Design) float64 {
	if d.SlotPitch <= 0 {
		return 0.5
	}
	return math.Max(0.1, math.Min(0.9, d.ToothWidth/d.SlotPitch))
}

// inrunnerLayout: shaft .. rotorYoke .. magnet .. (airgap) .. toothTip .. slotBottom .. yoke.
func inrunnerLayout(d *Design) layout {
	return layout{
		statorYokeR:  mmToCM(d.StatorOuterDia / 2),
		toothTipR:    mmToCM(d.ToothTipR), // bore — teeth point inward to here
		slotBottomR:  mmToCM(d.SlotOuterR),
		magnetTipR:   mmToCM(d.RotorOuterDia / 2),
		magnetBackR:  mmToCM(d.MagnetInnerR),
		rotorYokeR:   mmToCM(d.RotorYokeInnR),
		teethFaceOut: false,
	}
}

// outrunnerLayout mirrors the stack-up about the airgap: the stator becomes the inner member
// (teeth pointing OUTWARD to the airgap) and the rotor an outer ring (magnets facing INWARD).
// The sized radial thicknesses (slot depth, yoke, magnet) are preserved; only the order flips.
func outrunnerLayout(d *Design) layout {
	bore := mmToCM(d.ToothTipR) // airgap radius (shared reference)
	slotDepth := mmToCM(d.SlotDepth)
	statorYoke := mmToCM(d.StatorYokeH)
	magnet := mmToCM(d.MagnetThick)
	rotorYoke := mmToCM(d.RotorYokeH)
	airgap := mmToCM(d.Spec.AirgapMM)
	return layout{
		toothTipR:    bore,                               // teeth tips at the airgap (largest stator R)
		slotBottomR:  bore - slotDepth,                   // slots recede inward
		statorYokeR:  bore - slotDepth - statorYoke,      // stator yoke ID (the smooth inner ring)
		magnetBackR:  bore + airgap,                      // magnet inner face (across the airgap)
		magnetTipR:   bore + airgap + magnet,             // magnet outer face (against rotor iron)
		rotorYokeR:   bore + airgap + magnet + rotorYoke, // rotor ring OD
		teethFaceOut: true,
	}
}

// statorOuterLoop is the stator iron's SMOOTH boundary (the yoke side, away from the
// airgap): the OD for an inrunner, the shaft-side ID for an outrunner.
func (l layout) statorOuterLoop() []Point2 {
	return circleLoopCM(l.statorYokeR)
}

// statorInnerLoop is the stator iron's TOOTHED airgap boundary. Both members alternate a
// tooth-tip arc (at the airgap) and a slot-bottom arc (set back into the yoke) around the
// axis — the teeth always point at the airgap, inward for an inrunner, outward for an
// outrunner. (For an outrunner this is the OUTER boundary, but the loop topology is identical.)
func (l layout) statorInnerLoop(slots int) []Point2 {
	step := 2 * math.Pi / float64(slots)
	var loop []Point2
	for i := 0; i < slots; i++ {
		c := float64(i) * step
		half := l.toothFrac * step / 2
		loop = append(loop, arcPoints(l.toothTipR, c-half, c+half)...)
		loop = append(loop, arcPoints(l.slotBottomR, c+half, c+step-half)...)
	}
	return loop
}

// rotorOuterR/rotorInnerR are the rotor iron's smooth boundary radii. Inrunner: outer =
// rotor yoke OD (just under the magnets), inner = shaft bore. Outrunner: outer = rotor ring
// OD, inner = just outside the magnets (the ring's inner face).
func (l layout) rotorOuterR() float64 {
	if l.teethFaceOut {
		return l.rotorYokeR
	}
	return l.magnetBackR
}

func (l layout) rotorInnerR() float64 {
	if l.teethFaceOut {
		return l.magnetTipR
	}
	return l.rotorYokeR
}

// magnetLoops builds one closed magnet loop per pole: an annular arc segment between the
// magnet back and tip radii, spanning MagnetArcDeg, centred on each pole. The radii come
// from the layout, so inrunner magnets sit on the rotor's outer surface and outrunner
// magnets line the inner face of the outer rotor ring.
func magnetLoops(d *Design, l layout) [][]Point2 {
	poles := d.Spec.Poles
	rIn := math.Min(l.magnetBackR, l.magnetTipR)
	rOut := math.Max(l.magnetBackR, l.magnetTipR)
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
// teethArcSteps+1 points.
func arcPoints(r, a0, a1 float64) []Point2 {
	pts := make([]Point2, 0, teethArcSteps+1)
	for i := 0; i <= teethArcSteps; i++ {
		a := a0 + (a1-a0)*float64(i)/float64(teethArcSteps)
		pts = append(pts, Point2{X: r * math.Cos(a), Y: r * math.Sin(a)})
	}
	return pts
}

// circleLoopCM samples a full circle of radius r (cm) into circleSteps points (closed loop).
func circleLoopCM(r float64) []Point2 {
	pts := make([]Point2, 0, circleSteps)
	for i := 0; i < circleSteps; i++ {
		a := 2 * math.Pi * float64(i) / float64(circleSteps)
		pts = append(pts, Point2{X: r * math.Cos(a), Y: r * math.Sin(a)})
	}
	return pts
}

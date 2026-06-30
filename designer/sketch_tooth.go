// SPDX-License-Identifier: GPL-2.0-only

package designer

import (
	"fmt"
	"math"

	"oblikovati.org/api/wire"
)

// The slot-type stator tooth profiles, each a single closed sketch loop fully constrained to
// DOF=0 and driven only by host parameters, so the EXTRUDED 3D tooth carries the real slot
// shape (not just the FEMM 2D cross-section) and every multiphysics consumer of the model — a
// structural/thermal mesh, a magnetics study — sees the same geometry. The constraint
// topologies are the geometry-math-advisor's: arc-endpoint pairs are mirrored about the +X
// centerline with the cheap, non-redundant Vertical (the arc's intrinsic equidistance + a
// centered hub finish the mirror), and the free shoe-neck points — which lie on no arc — are
// mirrored with Symmetric. Inner/outer roles flip with the motor type via the seed radii and
// the driving parameter names supplied by the caller.

// openRectTooth specs a constant-width, parallel-sided tooth with NO shoe (the open slot): two
// origin-centred arcs (smaller radius = inner, larger = outer) capped by two parallel flanks
// tooth_width apart, symmetric about +X.
type openRectTooth struct {
	rInnerSeed, rOuterSeed, halfWidthSeed float64 // cm seeds (placement before the solve)
	rInnerParam, rOuterParam, widthParam  string  // driving parameter expressions
}

// shoeTooth specs a semi-closed tooth: a tip arc at the airgap flaring to a shoe, a short
// underside back to the body neck, the tooth body down to the root arc. The body is either
// parallel-sided (parallel-tooth, dimensioned by tooth_width) or radial/constant-angle
// (round-bottom, dimensioned by tooth_angle), selected by radial.
type shoeTooth struct {
	radial                                        bool    // true = round-bottom (radial body)
	tipSeed, rootSeed, neckSeed                   float64 // cm radii seeds
	bodyHalfSeed, shoeHalfSeed                    float64 // rad: body half-angle, shoe half-angle seeds
	halfWidthSeed                                 float64 // cm: parallel body half-width seed
	tipParam, rootParam, neckParam, tipChordParam string  // driving parameter expressions
	bodyParam                                     string  // tooth_width (parallel) or tooth_angle (radial)
}

// polarPt is a seed point at radius r (cm) and angle a (rad).
func polarPt(r, a float64) []float64 { return []float64{r * math.Cos(a), r * math.Sin(a)} }

// flankCorners returns the +y / -y corner seeds of a parallel flank at radius r and half-width
// hw (the flank lies at perpendicular distance hw from the +X centreline).
func flankCorners(r, hw float64) (top, bot []float64) {
	x := math.Sqrt(math.Max(r*r-hw*hw, 0))
	return []float64{x, hw}, []float64{x, -hw}
}

// addOpenRectTooth lays the open-rectangular tooth: inner arc + outer arc capped by two
// parallel flanks, fully constrained and driven by rInner/rOuter/width. No centreline or
// Symmetric is needed — every corner sits on a centred arc, so Vertical mirrors each chord
// about +X at one equation each (Symmetric there would be redundant with the arc intrinsic).
func (e *Engine) addOpenRectTooth(sk int, t openRectTooth) error {
	c := e.api.Sketch()
	aiTop, aiBot := flankCorners(t.rInnerSeed, t.halfWidthSeed)
	aoTop, aoBot := flankCorners(t.rOuterSeed, t.halfWidthSeed)
	inner, err := c.AddArcByCenterStartEnd(sk, origin2D, aiBot, aiTop, true, false)
	if err != nil {
		return fmt.Errorf("inner arc: %w", err)
	}
	outer, err := c.AddArcByCenterStartEnd(sk, origin2D, aoBot, aoTop, true, false)
	if err != nil {
		return fmt.Errorf("outer arc: %w", err)
	}
	fTop, err := c.AddLine(sk, aiTop, aoTop, false)
	if err != nil {
		return fmt.Errorf("top flank: %w", err)
	}
	fBot, err := c.AddLine(sk, aiBot, aoBot, false)
	if err != nil {
		return fmt.Errorf("bottom flank: %w", err)
	}
	return e.constrainOpenRect(sk, t, inner, outer, fTop, fBot)
}

// constrainOpenRect pins the open-rect tooth: concentric hub at the origin, welded corners,
// the top flank horizontal (constant width; the bottom follows by the mirrored chords), each
// arc chord vertical (mirror about +X), and the three driving dimensions.
func (e *Engine) constrainOpenRect(sk int, t openRectTooth, inner, outer, fTop, fBot wire.AddSketchEntityResult) error {
	g := e.api.Sketch().Constrain(sk)
	// PointIDs: arc=[centre,start(bot),end(top)]; line=[start(inner),end(outer)].
	welds := [][2]uint64{
		{fTop.PointIDs[0], inner.PointIDs[2]}, {fTop.PointIDs[1], outer.PointIDs[2]},
		{fBot.PointIDs[0], inner.PointIDs[1]}, {fBot.PointIDs[1], outer.PointIDs[1]},
	}
	steps := []func() (wire.AddConstraintResult, error){
		func() (wire.AddConstraintResult, error) { return g.Coincident(inner.PointIDs[0], outer.PointIDs[0]) },
		func() (wire.AddConstraintResult, error) { return g.Fix(inner.PointIDs[0]) },
		func() (wire.AddConstraintResult, error) { return g.Horizontal(fTop.PointIDs[0], fTop.PointIDs[1]) },
		func() (wire.AddConstraintResult, error) { return g.Vertical(inner.PointIDs[2], inner.PointIDs[1]) },
		func() (wire.AddConstraintResult, error) { return g.Vertical(outer.PointIDs[2], outer.PointIDs[1]) },
	}
	for _, w := range welds {
		steps = append(steps, func() (wire.AddConstraintResult, error) { return g.Coincident(w[0], w[1]) })
	}
	if err := runConstraints("open-rect tooth", steps); err != nil {
		return err
	}
	return e.dimensionOpenRect(sk, t, inner, outer)
}

// dimensionOpenRect drives the two arc radii and the constant tooth width (the inner chord,
// which the parallel flanks hold equal to the gap everywhere).
func (e *Engine) dimensionOpenRect(sk int, t openRectTooth, inner, outer wire.AddSketchEntityResult) error {
	d := e.api.Sketch().Dimension(sk)
	if _, err := d.Radius(inner.EntityID, t.rInnerParam); err != nil {
		return fmt.Errorf("inner radius %s: %w", t.rInnerParam, err)
	}
	if _, err := d.Radius(outer.EntityID, t.rOuterParam); err != nil {
		return fmt.Errorf("outer radius %s: %w", t.rOuterParam, err)
	}
	if _, err := d.Distance(inner.PointIDs[2], inner.PointIDs[1], t.widthParam); err != nil {
		return fmt.Errorf("tooth width %s: %w", t.widthParam, err)
	}
	return nil
}

// runConstraints applies a sequence of constraint factories, failing on the first error with
// the operand index for diagnosis.
func runConstraints(what string, steps []func() (wire.AddConstraintResult, error)) error {
	for i, step := range steps {
		if _, err := step(); err != nil {
			return fmt.Errorf("%s constraint %d: %w", what, i, err)
		}
	}
	return nil
}

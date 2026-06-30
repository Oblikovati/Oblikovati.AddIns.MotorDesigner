// SPDX-License-Identifier: GPL-2.0-only

package designer

import (
	"fmt"
	"math"

	"oblikovati.org/api/client"
	"oblikovati.org/api/wire"
)

// shoeEntities holds the host ids of one laid-down semi-closed tooth (tip arc, root arc, the
// two body sides, the two shoe undersides, and the construction centreline that mirrors them).
type shoeEntities struct {
	cl, tip, root          wire.AddSketchEntityResult // centreline (construction), tip arc, root arc
	bTop, bBot, uTop, uBot wire.AddSketchEntityResult // body sides, shoe undersides
}

// addShoeTooth lays a semi-closed tooth (parallel-tooth or round-bottom) fully constrained to
// DOF=0. The construction centreline along +X is the mirror axis; the tip/root arc chords are
// mirrored with Vertical (cheap, non-redundant on centred arcs) and the free neck points with
// Symmetric. The body is parallel-sided (Horizontal, dimensioned by tooth_width) or radial
// (PointOnLine through the origin, dimensioned by tooth_angle) per t.radial. The shoe undersides
// carry no direction constraint — their flare is defined by where the neck and shoe corner land.
func (e *Engine) addShoeTooth(sk int, t shoeTooth) error {
	ents, err := e.emitShoe(sk, t)
	if err != nil {
		return err
	}
	if err := e.constrainShoe(sk, t, ents); err != nil {
		return err
	}
	return e.dimensionShoe(sk, t, ents)
}

// bodyCorners returns the +y / -y body-corner seeds at radius r: along the constant-angle ray
// for a radial body, or at the constant half-width for a parallel body.
func (t shoeTooth) bodyCorners(r float64) (top, bot []float64) {
	if t.radial {
		return polarPt(r, t.bodyHalfSeed), polarPt(r, -t.bodyHalfSeed)
	}
	return flankCorners(r, t.halfWidthSeed)
}

// emitShoe lays the seven entities (centreline, two arcs, two body sides, two shoe undersides)
// seeded at literal coordinates so the solve starts in the correct, non-inverted branch.
func (e *Engine) emitShoe(sk int, t shoeTooth) (shoeEntities, error) {
	c := e.api.Sketch()
	tTop, tBot := polarPt(t.tipSeed, t.shoeHalfSeed), polarPt(t.tipSeed, -t.shoeHalfSeed)
	nTop, nBot := t.bodyCorners(t.neckSeed)
	rTop, rBot := t.bodyCorners(t.rootSeed)
	rFar := 1.3 * math.Max(t.tipSeed, t.rootSeed)
	var x shoeEntities
	var err error
	if x.cl, err = c.AddLine(sk, origin2D, []float64{rFar, 0}, true); err != nil {
		return x, fmt.Errorf("centreline: %w", err)
	}
	if x.tip, err = c.AddArcByCenterStartEnd(sk, origin2D, tBot, tTop, true, false); err != nil {
		return x, fmt.Errorf("tip arc: %w", err)
	}
	if x.root, err = c.AddArcByCenterStartEnd(sk, origin2D, rBot, rTop, true, false); err != nil {
		return x, fmt.Errorf("root arc: %w", err)
	}
	return e.emitShoeLines(sk, x, rTop, rBot, nTop, nBot, tTop, tBot)
}

// emitShoeLines lays the four straight edges (body sides root→neck, shoe undersides neck→tip).
func (e *Engine) emitShoeLines(sk int, x shoeEntities, rTop, rBot, nTop, nBot, tTop, tBot []float64) (shoeEntities, error) {
	c := e.api.Sketch()
	var err error
	if x.bTop, err = c.AddLine(sk, rTop, nTop, false); err != nil {
		return x, fmt.Errorf("top body side: %w", err)
	}
	if x.bBot, err = c.AddLine(sk, rBot, nBot, false); err != nil {
		return x, fmt.Errorf("bottom body side: %w", err)
	}
	if x.uTop, err = c.AddLine(sk, nTop, tTop, false); err != nil {
		return x, fmt.Errorf("top shoe underside: %w", err)
	}
	if x.uBot, err = c.AddLine(sk, nBot, tBot, false); err != nil {
		return x, fmt.Errorf("bottom shoe underside: %w", err)
	}
	return x, nil
}

// constrainShoe pins the centreline (rigid on +X), the arcs concentric at the origin, the six
// welded corners, the tip chord vertical, the neck pair symmetric about the centreline, and the
// body sides (mode-specific). The body constraints differ by profile because a configuration
// where one body side is pinned and the other only inferred goes rank-deficient at the inrunner
// orientation: the RADIAL body holds both sides through the origin + a symmetric Angle, while
// the PARALLEL body holds BOTH sides Horizontal (constructed-symmetric), so neither over-
// determines the root chord.
func (e *Engine) constrainShoe(sk int, t shoeTooth, x shoeEntities) error {
	g := e.api.Sketch().Constrain(sk)
	// PointIDs: line=[start,end]; arc=[centre,start(bot),end(top)].
	welds := [][2]uint64{
		{x.bTop.PointIDs[0], x.root.PointIDs[2]}, {x.bTop.PointIDs[1], x.uTop.PointIDs[0]},
		{x.uTop.PointIDs[1], x.tip.PointIDs[2]}, {x.bBot.PointIDs[0], x.root.PointIDs[1]},
		{x.bBot.PointIDs[1], x.uBot.PointIDs[0]}, {x.uBot.PointIDs[1], x.tip.PointIDs[1]},
	}
	steps := []func() (wire.AddConstraintResult, error){
		func() (wire.AddConstraintResult, error) { return g.Fix(x.cl.PointIDs[0]) },
		func() (wire.AddConstraintResult, error) { return g.Fix(x.cl.PointIDs[1]) },
		func() (wire.AddConstraintResult, error) { return g.Coincident(x.tip.PointIDs[0], x.cl.PointIDs[0]) },
		func() (wire.AddConstraintResult, error) { return g.Coincident(x.root.PointIDs[0], x.cl.PointIDs[0]) },
		func() (wire.AddConstraintResult, error) { return g.Vertical(x.tip.PointIDs[2], x.tip.PointIDs[1]) },
		func() (wire.AddConstraintResult, error) {
			return g.Symmetric(x.bTop.PointIDs[1], x.bBot.PointIDs[1], x.cl.EntityID)
		},
	}
	steps = append(steps, e.bodyConstraints(g, t, x)...)
	for _, w := range welds {
		steps = append(steps, func() (wire.AddConstraintResult, error) { return g.Coincident(w[0], w[1]) })
	}
	return runConstraints("shoe tooth", steps)
}

// bodyConstraints returns the body-side direction constraints. A radial (round-bottom) body
// passes both sides through the origin — the top via PointOnLine, the bottom following from the
// vertical root chord + symmetric neck. A parallel body holds BOTH sides Horizontal so the
// constant-width body is symmetric by construction (no vertical root chord, which would over-
// determine it at the inrunner orientation).
func (e *Engine) bodyConstraints(g client.Constrain, t shoeTooth, x shoeEntities) []func() (wire.AddConstraintResult, error) {
	if t.radial {
		return []func() (wire.AddConstraintResult, error){
			func() (wire.AddConstraintResult, error) { return g.PointOnLine(x.cl.PointIDs[0], x.bTop.EntityID) },
			func() (wire.AddConstraintResult, error) { return g.Vertical(x.root.PointIDs[2], x.root.PointIDs[1]) },
		}
	}
	return []func() (wire.AddConstraintResult, error){
		func() (wire.AddConstraintResult, error) { return g.Horizontal(x.bTop.PointIDs[0], x.bTop.PointIDs[1]) },
		func() (wire.AddConstraintResult, error) { return g.Horizontal(x.bBot.PointIDs[0], x.bBot.PointIDs[1]) },
	}
}

// dimensionShoe drives the tip/root radii, the neck radius (origin→neck), the body size
// (tooth_width for parallel / tooth_angle for radial), and the tip-arc chord (the shoe span).
func (e *Engine) dimensionShoe(sk int, t shoeTooth, x shoeEntities) error {
	d := e.api.Sketch().Dimension(sk)
	if _, err := d.Radius(x.tip.EntityID, t.tipParam); err != nil {
		return fmt.Errorf("tip radius %s: %w", t.tipParam, err)
	}
	if _, err := d.Radius(x.root.EntityID, t.rootParam); err != nil {
		return fmt.Errorf("root radius %s: %w", t.rootParam, err)
	}
	if _, err := d.Distance(x.cl.PointIDs[0], x.bTop.PointIDs[1], t.neckParam); err != nil {
		return fmt.Errorf("neck radius %s: %w", t.neckParam, err)
	}
	if err := e.dimensionShoeBody(sk, t, x); err != nil {
		return err
	}
	if _, err := d.Distance(x.tip.PointIDs[2], x.tip.PointIDs[1], t.tipChordParam); err != nil {
		return fmt.Errorf("tip chord %s: %w", t.tipChordParam, err)
	}
	return nil
}

// dimensionShoeBody applies the body-size dimension: a radial body by the angle between its two
// sides (tooth_angle); a parallel body by the neck-chord width (tooth_width) — the neck points
// are the symmetric free pair, so the perpendicular gap between the (Horizontal) body sides is
// dimensioned there rather than at the root chord (which is no longer vertically pinned).
func (e *Engine) dimensionShoeBody(sk int, t shoeTooth, x shoeEntities) error {
	d := e.api.Sketch().Dimension(sk)
	if t.radial {
		if _, err := d.Angle(x.bTop.EntityID, x.bBot.EntityID, t.bodyParam); err != nil {
			return fmt.Errorf("tooth angle %s: %w", t.bodyParam, err)
		}
		return nil
	}
	if _, err := d.Distance(x.bTop.PointIDs[1], x.bBot.PointIDs[1], t.bodyParam); err != nil {
		return fmt.Errorf("tooth width %s: %w", t.bodyParam, err)
	}
	return nil
}

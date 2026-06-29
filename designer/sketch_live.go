// SPDX-License-Identifier: GPL-2.0-only

package designer

import (
	"fmt"
	"math"

	"oblikovati.org/api/wire"
)

// origin2D is the part-origin centre every generated circle/arc is laid down about.
var origin2D = []float64{0, 0}

// addGroundedCircle lays a circle centred on the part origin whose radius is DRIVEN by a
// parameter expression: the centre is fixed at the origin and a radius dimension binds the
// size to radiusParam. The circle is then fully constrained (DOF 0) and recomputes when the
// parameter changes — unlike a bare centre-radius circle, whose radius is resolved once at
// construction (host buildCircle) and never tracks the parameter afterwards.
func (e *Engine) addGroundedCircle(sk int, radiusParam string) error {
	c := e.api.Sketch()
	res, err := c.AddCircleByCenterRadius(sk, origin2D, radiusParam, false)
	if err != nil {
		return fmt.Errorf("circle %s: %w", radiusParam, err)
	}
	if len(res.PointIDs) > 0 {
		if _, err := c.Constrain(sk).Fix(res.PointIDs[0]); err != nil {
			return fmt.Errorf("fix circle %s centre: %w", radiusParam, err)
		}
	}
	if _, err := c.Dimension(sk).Radius(res.EntityID, radiusParam); err != nil {
		return fmt.Errorf("radius dim %s: %w", radiusParam, err)
	}
	return nil
}

// sector is the spec of one annular-sector profile (a magnet, or one stator tooth): the two
// boundary radii and the half-angle span, each as a seed value (for placement) plus the
// parameter expression that DRIVES it (for recompute).
type sector struct {
	rInnerSeed, rOuterSeed, halfSeed    float64 // cm, cm, radians — placement before the solve
	rInnerParam, rOuterParam, spanParam string  // driving parameter expressions (span = full angle)
}

// sectorIDs holds the host ids of one laid-down sector so its corners can be welded, its
// centres grounded, its flanks made radial, and its radii/span dimensioned.
type sectorIDs struct {
	arcInner, arcOuter    uint64 // arc entities (radius-dimensioned)
	cInner, cOuter        uint64 // arc centre points (fixed at origin)
	ipInner, imInner      uint64 // inner-arc +half / -half endpoints
	ipOuter, imOuter      uint64 // outer-arc +half / -half endpoints
	flankPlus, flankMinus uint64 // radial flank lines (span-angle-dimensioned, made radial)
	fpInner, fpOuter      uint64 // +half flank endpoints (inner, outer)
	fmInner, fmOuter      uint64 // -half flank endpoints
}

// assembleSectorIDs maps the host entity replies (each carrying an EntityID and ordered
// PointIDs) into the sector's named ids. Arc PointIDs are [centre, start, end]; the arcs are
// built start=-half end=+half, the flanks inner→outer.
func assembleSectorIDs(inner, outer, plus, minus wire.AddSketchEntityResult) sectorIDs {
	return sectorIDs{
		arcInner: inner.EntityID, cInner: inner.PointIDs[0], imInner: inner.PointIDs[1], ipInner: inner.PointIDs[2],
		arcOuter: outer.EntityID, cOuter: outer.PointIDs[0], imOuter: outer.PointIDs[1], ipOuter: outer.PointIDs[2],
		flankPlus: plus.EntityID, fpInner: plus.PointIDs[0], fpOuter: plus.PointIDs[1],
		flankMinus: minus.EntityID, fmInner: minus.PointIDs[0], fmOuter: minus.PointIDs[1],
	}
}

// addAnnularSector lays one closed annular sector (two concentric arcs capped by two radial
// flanks) as REAL arcs, fully constrained (DOF 0) and parameter-driven: the arc centres are
// coincident and fixed at the origin, the corners welded, the flanks made radial (through the
// centre) and symmetric about +X, with the two radii and the angular span as driving
// dimensions. So the sector recomputes in place when a parameter changes. The topology is
// validated against the real solver in Oblikovati addin/router/motor_sector_solver_test.go.
func (e *Engine) addAnnularSector(sk int, s sector) error {
	ids, err := e.emitSector(sk, s)
	if err != nil {
		return err
	}
	if err := e.groundSector(sk, ids); err != nil {
		return err
	}
	if err := e.weldSector(sk, ids); err != nil {
		return err
	}
	if err := e.shapeSector(sk, ids); err != nil {
		return err
	}
	return e.dimensionSector(sk, ids, s)
}

// emitSector lays the four entities (two arcs + two radial flanks), seeded at literal
// coordinates, and captures their ids.
func (e *Engine) emitSector(sk int, s sector) (sectorIDs, error) {
	ip, im, op, om := sectorCorners(s.rInnerSeed, s.rOuterSeed, s.halfSeed)
	c := e.api.Sketch()
	var ids sectorIDs
	inner, err := c.AddArcByCenterStartEnd(sk, origin2D, im, ip, true, false) // -half → +half, through 0
	if err != nil {
		return ids, fmt.Errorf("inner arc: %w", err)
	}
	outer, err := c.AddArcByCenterStartEnd(sk, origin2D, om, op, true, false)
	if err != nil {
		return ids, fmt.Errorf("outer arc: %w", err)
	}
	plus, err := c.AddLine(sk, ip, op, false) // +half radial flank (inner → outer)
	if err != nil {
		return ids, fmt.Errorf("+half flank: %w", err)
	}
	minus, err := c.AddLine(sk, im, om, false)
	if err != nil {
		return ids, fmt.Errorf("-half flank: %w", err)
	}
	return assembleSectorIDs(inner, outer, plus, minus), nil
}

// sectorCorners returns the four corner points (cm) of a sector centred on +X spanning ±half:
// the inner-radius arc ends (+half, -half) and the outer-radius arc ends.
func sectorCorners(rInner, rOuter, half float64) (ip, im, op, om []float64) {
	c, s := math.Cos(half), math.Sin(half)
	return []float64{rInner * c, rInner * s}, []float64{rInner * c, -rInner * s},
		[]float64{rOuter * c, rOuter * s}, []float64{rOuter * c, -rOuter * s}
}

// groundSector makes the two arc centres coincident and fixes them at the origin (concentric,
// no translation).
func (e *Engine) groundSector(sk int, ids sectorIDs) error {
	g := e.api.Sketch().Constrain(sk)
	if _, err := g.Coincident(ids.cInner, ids.cOuter); err != nil {
		return fmt.Errorf("coincident arc centres: %w", err)
	}
	if _, err := g.Fix(ids.cInner); err != nil {
		return fmt.Errorf("fix sector centre: %w", err)
	}
	return nil
}

// weldSector makes each flank endpoint coincident with the arc endpoint it meets, closing the
// loop into one profile (the host does not auto-merge coincident sketch points).
func (e *Engine) weldSector(sk int, ids sectorIDs) error {
	g := e.api.Sketch().Constrain(sk)
	welds := [][2]uint64{
		{ids.fpInner, ids.ipInner}, {ids.fpOuter, ids.ipOuter},
		{ids.fmInner, ids.imInner}, {ids.fmOuter, ids.imOuter},
	}
	for _, w := range welds {
		if _, err := g.Coincident(w[0], w[1]); err != nil {
			return fmt.Errorf("weld corner %d-%d: %w", w[0], w[1], err)
		}
	}
	return nil
}

// shapeSector makes both flanks radial (passing through the fixed centre) and pins the sector
// symmetric about +X (the two inner-arc ends share an x), removing the rigid-rotation freedom.
func (e *Engine) shapeSector(sk int, ids sectorIDs) error {
	g := e.api.Sketch().Constrain(sk)
	if _, err := g.PointOnLine(ids.cInner, ids.flankPlus); err != nil {
		return fmt.Errorf("+flank radial: %w", err)
	}
	if _, err := g.PointOnLine(ids.cInner, ids.flankMinus); err != nil {
		return fmt.Errorf("-flank radial: %w", err)
	}
	if _, err := g.Vertical(ids.ipInner, ids.imInner); err != nil {
		return fmt.Errorf("sector symmetry: %w", err)
	}
	return nil
}

// dimensionSector drives the two arc radii and the full angular span between the flanks, so the
// radii and the opening angle recompute from the parameters.
func (e *Engine) dimensionSector(sk int, ids sectorIDs, s sector) error {
	d := e.api.Sketch().Dimension(sk)
	if _, err := d.Radius(ids.arcInner, s.rInnerParam); err != nil {
		return fmt.Errorf("inner radius dim %s: %w", s.rInnerParam, err)
	}
	if _, err := d.Radius(ids.arcOuter, s.rOuterParam); err != nil {
		return fmt.Errorf("outer radius dim %s: %w", s.rOuterParam, err)
	}
	if _, err := d.Angle(ids.flankPlus, ids.flankMinus, s.spanParam); err != nil {
		return fmt.Errorf("span angle dim %s: %w", s.spanParam, err)
	}
	return nil
}

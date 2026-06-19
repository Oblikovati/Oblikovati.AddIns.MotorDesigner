// SPDX-License-Identifier: GPL-2.0-only

package designer

import (
	"encoding/json"
	"fmt"

	"oblikovati.org/api/wire"
)

// GenerateResult summarizes one host geometry-generation run.
type GenerateResult struct {
	DocumentID    uint64
	ParametersSet int
	StatorSketch  int // sketch index of the stator cross-section
	RotorSketch   int // sketch index of the rotor cross-section
	StatorBodies  int // solids the stator extrude produced (>=1 on success)
	RotorBodies   int // solids the rotor extrude produced (>=1 on success)
}

// extrudeArgs is the JSON shape the host's "extrude" feature kind expects:
// {sketchIndex, profileIndex, distance}. The distance is a literal unit expression.
type extrudeArgs struct {
	SketchIndex  int    `json:"sketchIndex"`
	ProfileIndex int    `json:"profileIndex"`
	Distance     string `json:"distance"`
}

// extrudeResult is the host's extrude reply. The host returns the feature display name,
// the number of bodies produced, and a health flag — there is no numeric feature id here
// (features are addressed by the id from model.tree, not the create reply).
type extrudeResult struct {
	Feature string `json:"feature"`
	Bodies  int    `json:"bodies"`
	Healthy bool   `json:"healthy"`
}

// Generate computes the design from a Spec and lays it down in the host as a new part
// document: it publishes the parameter program (as design drivers), then sketches +
// extrudes the stator and rotor cross-sections (concentric circles ⇒ an annular profile
// each). This is the rough first-pass solid the FEMM bridge later sections.
//
// NOTE on parameter binding: the host's sketch.addEntity radius is a LITERAL unit
// expression — it does not resolve parameter names (it goes through Units().Parse, not the
// parameter graph). So the circle radii are emitted as literal millimetre values computed
// from the Design; the published parameters document the design drivers. Live-binding the
// sketch geometry to those parameters needs parametric sketch dimensions — a follow-up,
// and a noted v1 API gap (sketch entities should accept parameter expressions).
func (e *Engine) Generate(s Spec) (*GenerateResult, error) {
	d, err := Compute(s)
	if err != nil {
		return nil, err
	}
	if _, err := e.api.Documents().Create(wire.CreateDocumentArgs{Type: "part", Name: "Motor"}); err != nil {
		return nil, fmt.Errorf("create part document: %w", err)
	}
	res := &GenerateResult{}
	if res.DocumentID, err = e.activeDocumentID(); err != nil {
		return nil, err
	}
	if res.ParametersSet, err = e.publishParameters(d); err != nil {
		return nil, err
	}
	if err := e.buildStator(d, res); err != nil {
		return nil, err
	}
	return res, e.buildRotor(d, res)
}

// activeDocumentID returns the session id of the active document (the part Generate just
// created). The create reply already carries it, but reading it back keeps Generate robust
// to the host activating a different document.
func (e *Engine) activeDocumentID() (uint64, error) {
	list, err := e.api.Documents().List()
	if err != nil {
		return 0, fmt.Errorf("list documents: %w", err)
	}
	for _, doc := range list.Documents {
		if doc.Active {
			return doc.ID, nil
		}
	}
	return 0, fmt.Errorf("no active document after create (got %d)", len(list.Documents))
}

// buildStator sketches the stator annulus (bore radius .. stator-outer radius) on the XY
// plane and extrudes it over the stack length.
func (e *Engine) buildStator(d *Design, res *GenerateResult) error {
	idx, err := e.annulus(mm(d.ToothTipR), mm(d.StatorOuterDia/2))
	if err != nil {
		return fmt.Errorf("stator sketch: %w", err)
	}
	res.StatorSketch = idx
	bodies, err := e.extrude(idx, mm(d.StackLength))
	if err != nil {
		return fmt.Errorf("stator extrude: %w", err)
	}
	res.StatorBodies = bodies
	return nil
}

// buildRotor sketches the rotor annulus (rotor-inner radius .. rotor-outer radius) on the
// XY plane and extrudes it over the stack length.
func (e *Engine) buildRotor(d *Design, res *GenerateResult) error {
	idx, err := e.annulus(mm(d.RotorYokeInnR), mm(d.RotorOuterDia/2))
	if err != nil {
		return fmt.Errorf("rotor sketch: %w", err)
	}
	res.RotorSketch = idx
	bodies, err := e.extrude(idx, mm(d.StackLength))
	if err != nil {
		return fmt.Errorf("rotor extrude: %w", err)
	}
	res.RotorBodies = bodies
	return nil
}

// mm formats a millimetre value as a host unit expression, e.g. mm(23.335) -> "23.3350 mm".
func mm(v float64) string { return fmt.Sprintf("%.4f mm", v) }

// annulus creates a new XY sketch with two concentric circles centered on the origin,
// yielding one annular profile, and returns the new sketch's index. Radii are literal unit
// expressions (see the Generate note on parameter binding). Both circles share the origin,
// so the only free dimensions are the two radii.
func (e *Engine) annulus(innerR, outerR string) (int, error) {
	sk, err := e.api.Sketch().Create(wire.CreateSketchArgs{Plane: "XY"})
	if err != nil {
		return 0, err
	}
	origin := []float64{0, 0}
	if _, err := e.api.Sketch().AddCircleByCenterRadius(sk.SketchIndex, origin, outerR, false); err != nil {
		return 0, fmt.Errorf("outer circle: %w", err)
	}
	if _, err := e.api.Sketch().AddCircleByCenterRadius(sk.SketchIndex, origin, innerR, false); err != nil {
		return 0, fmt.Errorf("inner circle: %w", err)
	}
	return sk.SketchIndex, nil
}

// extrude extrudes the first profile of a sketch by a literal unit-bearing distance and
// returns the number of solid bodies it produced.
func (e *Engine) extrude(sketchIndex int, distance string) (int, error) {
	args, err := json.Marshal(extrudeArgs{SketchIndex: sketchIndex, ProfileIndex: 0, Distance: distance})
	if err != nil {
		return 0, err
	}
	raw, err := e.api.Features().Add(wire.AddFeatureArgs{Kind: "extrude", Args: args})
	if err != nil {
		return 0, err
	}
	var out extrudeResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return 0, fmt.Errorf("decode extrude reply %q: %w", string(raw), err)
	}
	if !out.Healthy || out.Bodies == 0 {
		return 0, fmt.Errorf("extrude produced no healthy body (reply %q)", string(raw))
	}
	return out.Bodies, nil
}

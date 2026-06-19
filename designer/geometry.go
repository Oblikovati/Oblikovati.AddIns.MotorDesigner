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
	StatorFeature uint64
	RotorFeature  uint64
}

// extrudeArgs is the JSON shape the host's "extrude" feature kind expects:
// {sketchIndex, profileIndex, distance}. The distance is unit-bearing.
type extrudeArgs struct {
	SketchIndex  int    `json:"sketchIndex"`
	ProfileIndex int    `json:"profileIndex"`
	Distance     string `json:"distance"`
}

// extrudeResult is the host's extrude reply; we only read the placed feature id.
type extrudeResult struct {
	FeatureID uint64 `json:"featureId"`
}

// Generate computes the design from a Spec and lays it down in the host as a new part
// document: it publishes the parameter program, then sketches + extrudes the stator and
// rotor cross-sections (concentric circles ⇒ an annular profile each). This is the rough
// first-pass solid the FEMM bridge later sections.
func (e *Engine) Generate(s Spec) (*GenerateResult, error) {
	d, err := Compute(s)
	if err != nil {
		return nil, err
	}
	doc, err := e.api.Documents().Create(wire.CreateDocumentArgs{Type: "part", Name: "Motor"})
	if err != nil {
		return nil, fmt.Errorf("create part document: %w", err)
	}
	res := &GenerateResult{DocumentID: doc.ID}
	if res.ParametersSet, err = e.publishParameters(d); err != nil {
		return nil, err
	}
	if err := e.buildStator(res); err != nil {
		return nil, err
	}
	if err := e.buildRotor(res); err != nil {
		return nil, err
	}
	return res, nil
}

// buildStator sketches the stator annulus (bore_r .. stator_outer_r) on the XY plane and
// extrudes it over the stack length.
func (e *Engine) buildStator(res *GenerateResult) error {
	idx, err := e.annulus("bore_r", "stator_outer_r")
	if err != nil {
		return fmt.Errorf("stator sketch: %w", err)
	}
	res.StatorSketch = idx
	fid, err := e.extrude(idx, "stack_length")
	if err != nil {
		return fmt.Errorf("stator extrude: %w", err)
	}
	res.StatorFeature = fid
	return nil
}

// buildRotor sketches the rotor annulus (rotor_inner_r .. rotor_outer_r) on the XY plane
// and extrudes it over the stack length.
func (e *Engine) buildRotor(res *GenerateResult) error {
	idx, err := e.annulus("rotor_inner_r", "rotor_outer_r")
	if err != nil {
		return fmt.Errorf("rotor sketch: %w", err)
	}
	res.RotorSketch = idx
	fid, err := e.extrude(idx, "stack_length")
	if err != nil {
		return fmt.Errorf("rotor extrude: %w", err)
	}
	res.RotorFeature = fid
	return nil
}

// annulus creates a new XY sketch with two concentric circles (radii are parameter-name
// expressions) centered on the origin, yielding one annular profile. It returns the new
// sketch's index. Both circles share the origin so the sketch is fully constrained by the
// two radius parameters (DOF-0 intent).
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

// extrude extrudes the first profile of a sketch by a unit-bearing distance (a parameter
// name) and returns the placed feature id.
func (e *Engine) extrude(sketchIndex int, distance string) (uint64, error) {
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
	return out.FeatureID, nil
}

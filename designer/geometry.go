// SPDX-License-Identifier: GPL-2.0-only

package designer

import (
	"encoding/json"
	"fmt"

	"oblikovati.org/api/types"
	"oblikovati.org/api/wire"
)

// GenerateResult summarizes one host geometry-generation run.
type GenerateResult struct {
	AssemblyID    uint64 // session id of the "Motor" assembly document
	StatorDocID   uint64 // session id of the Stator part document
	RotorDocID    uint64 // session id of the Rotor part document
	MagnetDocID   uint64 // session id of the Magnets part document
	ParametersSet int    // parameters published on the stator part
	StatorBodies  int    // solids the stator extrude produced (>=1 on success)
	RotorBodies   int    // solids the rotor extrude produced (>=1 on success)
	MagnetBodies  int    // magnet solids produced (one per pole on success)
	IronMaterial  string // host material id assigned to the iron parts (steel grade)
	MagnetMatID   string // host material id assigned to the magnet part (magnet grade)
}

// extrudeArgs is the JSON shape the host's "extrude" feature kind expects:
// {sketchIndex, profileIndex, distance}. The distance is a literal unit expression.
type extrudeArgs struct {
	SketchIndex  int    `json:"sketchIndex"`
	ProfileIndex int    `json:"profileIndex"`
	Distance     string `json:"distance"`
}

// extrudeResult is the host's extrude reply: the feature display name, the number of bodies
// produced, and a health flag (no numeric feature id — features are addressed via model.tree).
type extrudeResult struct {
	Feature string `json:"feature"`
	Bodies  int    `json:"bodies"`
	Healthy bool   `json:"healthy"`
}

// Generate computes the design from a Spec and lays it down as a host ASSEMBLY of three
// separated component parts — Stator, Rotor and Magnets — each its own part document with
// its own (part-default) magnetic material and a clean planar cross-section the FEMM bridge
// can section. The components are coaxial at the origin, so they assemble at identity.
//
// Keeping each component a separate part is what makes the magnetic hand-off work: the FEMM
// bridge reads the document's material off each body, so the stator/rotor carry their steel
// grade and the magnets carry their magnet grade — distinct regions, distinct materials,
// without needing a per-body reference key (which the API does not yet expose).
//
// NOTE on parameter binding: host sketch geometry takes LITERAL coordinates/radii (cm
// points, unit-bearing radii), not parameter expressions (Oblikovati.API#187). The stator
// part still publishes the parameter program to document the design drivers.
func (e *Engine) Generate(s Spec) (*GenerateResult, error) {
	d, err := Compute(s)
	if err != nil {
		return nil, err
	}
	cs := BuildCrossSection(d)
	res := &GenerateResult{
		IronMaterial: HostSteelMaterialID(s.SteelGrade),
		MagnetMatID:  HostMagnetMaterialID(s.MagnetGrade),
	}
	if err := e.buildComponents(d, cs, res); err != nil {
		return nil, err
	}
	return res, e.assembleMotor(res)
}

// buildComponents creates and fills the three component part documents.
func (e *Engine) buildComponents(d *Design, cs CrossSection, res *GenerateResult) error {
	var err error
	if res.StatorDocID, err = e.buildStatorPart(d, cs, res); err != nil {
		return err
	}
	if res.RotorDocID, err = e.buildRotorPart(d, cs, res); err != nil {
		return err
	}
	res.MagnetDocID, err = e.buildMagnetPart(d, cs, res)
	return err
}

// buildStatorPart creates the "Stator" part: the toothed annulus (toothed outer boundary +
// bore circle), extruded over the stack length, with the steel grade assigned and the
// design parameter program published for documentation.
func (e *Engine) buildStatorPart(d *Design, cs CrossSection, res *GenerateResult) (uint64, error) {
	id, err := e.createPart("Stator")
	if err != nil {
		return 0, err
	}
	if res.ParametersSet, err = e.publishParameters(d); err != nil {
		return 0, fmt.Errorf("stator parameters: %w", err)
	}
	sk, err := e.api.Sketch().Create(wire.CreateSketchArgs{Plane: "XY"})
	if err != nil {
		return 0, fmt.Errorf("stator sketch: %w", err)
	}
	if err := e.addClosedPolyline(sk.SketchIndex, cs.StatorOuter); err != nil {
		return 0, fmt.Errorf("stator outer: %w", err)
	}
	if err := e.addCircleCM(sk.SketchIndex, cs.BoreRadiusCM); err != nil {
		return 0, fmt.Errorf("stator bore: %w", err)
	}
	if res.StatorBodies, err = e.extrudeNamed(sk.SketchIndex, 0, mm(d.StackLength), "Stator Iron"); err != nil {
		return 0, fmt.Errorf("stator extrude: %w", err)
	}
	return id, e.assignPartMaterial(res.IronMaterial)
}

// buildRotorPart creates the "Rotor" part: the back-iron annulus (rotor-iron OD circle +
// shaft-bore circle), extruded, with the steel grade assigned.
func (e *Engine) buildRotorPart(d *Design, cs CrossSection, res *GenerateResult) (uint64, error) {
	id, err := e.createPart("Rotor")
	if err != nil {
		return 0, err
	}
	sk, err := e.api.Sketch().Create(wire.CreateSketchArgs{Plane: "XY"})
	if err != nil {
		return 0, fmt.Errorf("rotor sketch: %w", err)
	}
	if err := e.addCircleCM(sk.SketchIndex, cs.RotorOuterRadius); err != nil {
		return 0, fmt.Errorf("rotor outer: %w", err)
	}
	if err := e.addCircleCM(sk.SketchIndex, cs.ShaftRadiusCM); err != nil {
		return 0, fmt.Errorf("shaft bore: %w", err)
	}
	if res.RotorBodies, err = e.extrudeNamed(sk.SketchIndex, 0, mm(d.StackLength), "Rotor Iron"); err != nil {
		return 0, fmt.Errorf("rotor extrude: %w", err)
	}
	return id, e.assignPartMaterial(res.IronMaterial)
}

// buildMagnetPart creates the "Magnets" part: one closed magnet loop per pole (one body
// each), extruded, with the magnet grade assigned to the whole part.
func (e *Engine) buildMagnetPart(d *Design, cs CrossSection, res *GenerateResult) (uint64, error) {
	id, err := e.createPart("Magnets")
	if err != nil {
		return 0, err
	}
	sk, err := e.api.Sketch().Create(wire.CreateSketchArgs{Plane: "XY"})
	if err != nil {
		return 0, fmt.Errorf("magnet sketch: %w", err)
	}
	for i, loop := range cs.Magnets {
		if err := e.addClosedPolyline(sk.SketchIndex, loop); err != nil {
			return 0, fmt.Errorf("magnet %d loop: %w", i+1, err)
		}
	}
	for i := range cs.Magnets {
		n, err := e.extrudeNamed(sk.SketchIndex, i, mm(d.StackLength), fmt.Sprintf("Magnet-%d", i+1))
		if err != nil {
			return 0, fmt.Errorf("magnet %d extrude: %w", i+1, err)
		}
		res.MagnetBodies += n
	}
	return id, e.assignPartMaterial(res.MagnetMatID)
}

// assembleMotor creates the "Motor" assembly and places the three component parts coaxially
// (identity transform — the cross-section already places everything about the origin).
func (e *Engine) assembleMotor(res *GenerateResult) error {
	asm, err := e.api.Documents().Create(wire.CreateDocumentArgs{Type: "assembly", Name: "Motor"})
	if err != nil {
		return fmt.Errorf("create motor assembly: %w", err)
	}
	res.AssemblyID = asm.ID
	if _, err := e.api.Documents().Activate(asm.ID); err != nil {
		return fmt.Errorf("activate motor assembly: %w", err)
	}
	for _, p := range []struct {
		doc  uint64
		name string
	}{{res.StatorDocID, "Stator"}, {res.RotorDocID, "Rotor"}, {res.MagnetDocID, "Magnets"}} {
		args := wire.PlaceOccurrenceArgs{Document: p.doc, Name: p.name, Transform: types.IdentityMatrix()}
		if _, err := e.api.Assembly().Place(args); err != nil {
			return fmt.Errorf("place %s: %w", p.name, err)
		}
	}
	return nil
}

// createPart creates a new part document and activates it (so subsequent sketch/feature/
// material calls target it), returning its session id.
func (e *Engine) createPart(name string) (uint64, error) {
	doc, err := e.api.Documents().Create(wire.CreateDocumentArgs{Type: "part", Name: name})
	if err != nil {
		return 0, fmt.Errorf("create part %q: %w", name, err)
	}
	if _, err := e.api.Documents().Activate(doc.ID); err != nil {
		return 0, fmt.Errorf("activate part %q: %w", name, err)
	}
	return doc.ID, nil
}

// assignPartMaterial sets the active part's default material (empty body key ⇒ part
// default). This is what the FEMM bridge reads off the body to pick the magnetic region.
func (e *Engine) assignPartMaterial(materialID string) error {
	_, err := e.api.Materials().Assign(wire.AssignMaterialArgs{MaterialID: materialID})
	if err != nil {
		return fmt.Errorf("assign material %q: %w", materialID, err)
	}
	return nil
}

// mm formats a millimetre value as a host unit expression, e.g. mm(23.335) -> "23.3350 mm".
func mm(v float64) string { return fmt.Sprintf("%.4f mm", v) }

// addCircleCM adds an origin-centered circle whose radius is a centimetre value, emitted as
// a unit-bearing millimetre expression (the host parses the radius via Units().Parse).
func (e *Engine) addCircleCM(sketchIndex int, radiusCM float64) error {
	_, err := e.api.Sketch().AddCircleByCenterRadius(sketchIndex, []float64{0, 0}, mm(radiusCM*10), false)
	return err
}

// addClosedPolyline adds one closed polyline (a clean closed profile) from cm points.
func (e *Engine) addClosedPolyline(sketchIndex int, loop []Point2) error {
	pts := make([][]float64, len(loop))
	for i, p := range loop {
		pts[i] = []float64{p.X, p.Y}
	}
	_, err := e.api.Sketch().AddEntity(wire.AddSketchEntityArgs{
		SketchIndex: sketchIndex, Kind: "polyline", Points: pts, Closed: true,
	})
	return err
}

// extrudeNamed extrudes one profile of a sketch by a literal distance and renames the
// resulting feature (best-effort), returning the number of solid bodies produced.
func (e *Engine) extrudeNamed(sketchIndex, profileIndex int, distance, name string) (int, error) {
	n, err := e.extrude(sketchIndex, profileIndex, distance)
	if err != nil {
		return 0, err
	}
	e.renameLastFeature(name) // best-effort; a rename failure must not fail generation
	return n, nil
}

// extrude extrudes one profile by a literal unit-bearing distance and returns the solid
// body count, failing loudly when the host reports an unhealthy or empty extrude.
func (e *Engine) extrude(sketchIndex, profileIndex int, distance string) (int, error) {
	args, err := json.Marshal(extrudeArgs{SketchIndex: sketchIndex, ProfileIndex: profileIndex, Distance: distance})
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

// renameLastFeature renames the active part's most recently added feature (the extrude just
// created), from the model tree. Best-effort: errors are ignored so a missing rename method
// never aborts geometry generation.
func (e *Engine) renameLastFeature(name string) {
	tree, err := e.api.Model().Tree()
	if err != nil || len(tree.Features) == 0 {
		return
	}
	last := tree.Features[len(tree.Features)-1]
	_, _ = e.api.Features().Rename(last.ID, name)
}

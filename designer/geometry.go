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
// NOTE on parameter binding: every part publishes the design's parameter program (formulas
// of the design drivers), drives its extrude depth from stack_length, and builds its CIRCULAR
// geometry from radius EXPRESSIONS (parametric circles, resolved through the evaluator since
// Oblikovati.API#187). The toothed stator bore and the magnet arcs are still computed
// polylines: expression-driven sketch LINES/ARCS (or a parametric slot-cut pattern) are an
// open API gap, so those profiles cannot yet be fully constrained from parameters.
func (e *Engine) Generate(s Spec) (*GenerateResult, error) {
	d, err := Compute(s)
	if err != nil {
		return nil, err // reject an invalid spec before touching the host
	}
	// Replace any motor a previous Generate left open (detected by the member marker) so
	// regenerating with edited parameters updates the design instead of colliding on the
	// "Stator"/"Rotor"/"Magnets"/"Motor" document names.
	e.clearExistingMotor()
	cs := BuildCrossSection(d)
	res := &GenerateResult{
		IronMaterial: HostSteelMaterialID(s.SteelGrade),
		MagnetMatID:  HostMagnetMaterialID(s.MagnetGrade),
	}
	if err := e.buildComponents(d, cs, res); err != nil {
		return nil, err
	}
	if err := e.assembleMotor(res); err != nil {
		return nil, err
	}
	// Hand the FEMM descriptor of this motor to the magnetics add-in (best-effort: a failed
	// hand-off must not fail generation; the FEMM study just won't find a fresh descriptor).
	_ = publishFEMMDescriptor(d)
	// Stamp the design onto the assembly so a re-opened motor is recognisable and
	// rebuildable from its own stored Spec (LoadSpec / IsMotorAssembly).
	return res, e.saveSpec(res.AssemblyID, s)
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
	// Outer diameter is a parametric circle (driven by stator_outer_r); the bore is the
	// toothed slot boundary (a computed profile — fully expression-driven lines/arcs are an
	// open API gap, see geometry note).
	if err := e.addParametricCircle(sk.SketchIndex, "stator_outer_r"); err != nil {
		return 0, fmt.Errorf("stator outer: %w", err)
	}
	if err := e.addClosedPolyline(sk.SketchIndex, cs.StatorBore); err != nil {
		return 0, fmt.Errorf("stator bore: %w", err)
	}
	if res.StatorBodies, err = e.extrudeNamed(sk.SketchIndex, 0, stackLengthExpr, "Stator Iron"); err != nil {
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
	if _, err := e.publishParameters(d); err != nil {
		return 0, fmt.Errorf("rotor parameters: %w", err)
	}
	sk, err := e.api.Sketch().Create(wire.CreateSketchArgs{Plane: "XY"})
	if err != nil {
		return 0, fmt.Errorf("rotor sketch: %w", err)
	}
	// The rotor back-iron annulus is fully parametric: two concentric circles driven by the
	// published radii, extruded over stack_length. Changing magnet_inner_r / rotor_inner_r /
	// stack_length re-sizes the part with no literal coordinates (inrunner; outrunner falls
	// back to the computed profile until topology-specific radii are published).
	if d.Spec.normType() == Inrunner {
		if err := e.addParametricCircle(sk.SketchIndex, "magnet_inner_r"); err != nil {
			return 0, fmt.Errorf("rotor outer: %w", err)
		}
		if err := e.addParametricCircle(sk.SketchIndex, "rotor_inner_r"); err != nil {
			return 0, fmt.Errorf("rotor inner: %w", err)
		}
	} else {
		if err := e.addClosedPolyline(sk.SketchIndex, cs.RotorOuter); err != nil {
			return 0, fmt.Errorf("rotor outer: %w", err)
		}
		if err := e.addClosedPolyline(sk.SketchIndex, cs.RotorInner); err != nil {
			return 0, fmt.Errorf("rotor inner: %w", err)
		}
	}
	if res.RotorBodies, err = e.extrudeNamed(sk.SketchIndex, 0, stackLengthExpr, "Rotor Iron"); err != nil {
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
	if _, err := e.publishParameters(d); err != nil {
		return 0, fmt.Errorf("magnet parameters: %w", err)
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
	// The host's extrude reply reports the part's TOTAL body count after the feature, not the
	// one body this extrude added — so take the final count (one magnet per pole) rather than
	// summing the running totals (which gave the 1+2+…+N triangular over-count).
	for i := range cs.Magnets {
		n, err := e.extrudeNamed(sk.SketchIndex, i, stackLengthExpr, fmt.Sprintf("Magnet-%d", i+1))
		if err != nil {
			return 0, fmt.Errorf("magnet %d extrude: %w", i+1, err)
		}
		res.MagnetBodies = n
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
	_ = e.markMotorMember(asm.ID)
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
// material calls target it), stamping the motor-member marker so a later regenerate can find
// and close it, and returning its session id.
func (e *Engine) createPart(name string) (uint64, error) {
	doc, err := e.api.Documents().Create(wire.CreateDocumentArgs{Type: "part", Name: name})
	if err != nil {
		return 0, fmt.Errorf("create part %q: %w", name, err)
	}
	if _, err := e.api.Documents().Activate(doc.ID); err != nil {
		return 0, fmt.Errorf("activate part %q: %w", name, err)
	}
	_ = e.markMotorMember(doc.ID) // best-effort: a missing marker only costs a regenerate collision
	return doc.ID, nil
}

// clearExistingMotor closes the documents a previous Generate produced (detected by the motor
// member marker), assemblies before their parts so references release cleanly. Best-effort: if
// the list/close fails, Generate proceeds and surfaces any name collision as before.
func (e *Engine) clearExistingMotor() {
	docs, err := e.api.Documents().List()
	if err != nil {
		return
	}
	for _, pass := range []string{"assembly", "part"} {
		for _, d := range docs.Documents {
			if d.Type != pass {
				continue
			}
			if ok, _ := e.isMotorMember(d.ID); ok {
				_, _ = e.api.Documents().Close(d.ID, true)
			}
		}
	}
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

// stackLengthExpr drives every part's extrude depth from the published stack_length parameter,
// so changing that one parameter re-extrudes the whole motor (no literal lengths in features).
const stackLengthExpr = "stack_length"

// addParametricCircle adds a circle centred on the part origin whose radius is a PARAMETER
// EXPRESSION (e.g. "stator_outer_r", "magnet_inner_r"), so the host recomputes the geometry when
// the parameter changes — the radius is resolved through the parameter evaluator (API#187).
func (e *Engine) addParametricCircle(sketchIndex int, radiusExpr string) error {
	_, err := e.api.Sketch().AddCircleByCenterRadius(sketchIndex, []float64{0, 0}, radiusExpr, false)
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

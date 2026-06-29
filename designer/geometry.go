// SPDX-License-Identifier: GPL-2.0-only

package designer

import (
	"encoding/json"
	"fmt"
	"math"

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

// extrudeArgs is the JSON shape the host's "extrude" feature kind expects. Operation is the
// boolean against existing bodies — "new" for a fresh body, "join" to fuse with the active
// body (the tooth joining the stator yoke). Distance is a unit-bearing expression.
type extrudeArgs struct {
	SketchIndex  int    `json:"sketchIndex"`
	ProfileIndex int    `json:"profileIndex"`
	Distance     string `json:"distance"`
	Operation    string `json:"operation"`
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
// NOTE on parameter binding: every part publishes the design's parameter program (formulas of
// the design drivers) and builds REAL, fully-constrained geometry driven by it — grounded
// radius-dimensioned circles for the yoke/rotor annuli, and real-arc annular sectors (driving
// radius + half-angle dimensions) for the stator tooth and the magnet, each circular-patterned
// by a parameter-driven count. Editing a driver recomputes every part in place — no polylines,
// no literal coordinates, no sketch recreation.
func (e *Engine) Generate(s Spec) (*GenerateResult, error) {
	d, err := Compute(s)
	if err != nil {
		return nil, err // reject an invalid spec before touching the host
	}
	// Replace any motor a previous Generate left open (detected by the member marker) so
	// regenerating with edited parameters updates the design instead of colliding on the
	// "Stator"/"Rotor"/"Magnets"/"Motor" document names.
	e.clearExistingMotor()
	res := &GenerateResult{
		IronMaterial: HostSteelMaterialID(s.SteelGrade),
		MagnetMatID:  HostMagnetMaterialID(s.MagnetGrade),
	}
	// The geometry is laid down from the topology-resolved layout (role-based radii). The FEMM
	// hand-off (publishFEMMDescriptor) builds its own faceted CrossSection from the Design for
	// 2D meshing — the host sketches are now real arcs, so the two no longer share a profile.
	if err := e.buildComponents(d, resolveLayout(d), res); err != nil {
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
func (e *Engine) buildComponents(d *Design, l layout, res *GenerateResult) error {
	var err error
	if res.StatorDocID, err = e.buildStatorPart(d, l, res); err != nil {
		return err
	}
	if res.RotorDocID, err = e.buildRotorPart(d, l, res); err != nil {
		return err
	}
	res.MagnetDocID, err = e.buildMagnetPart(d, l, res)
	return err
}

// buildStatorPart creates the "Stator" part the canonical way: a smooth yoke annulus, one
// real-arc tooth extrude-joined to it, then a circular pattern of the tooth whose count tracks
// the slots parameter. Every dimension is parameter-driven, so editing a driver recomputes the
// stator in place. The yoke + teeth fuse into a single iron body.
func (e *Engine) buildStatorPart(d *Design, l layout, res *GenerateResult) (uint64, error) {
	id, err := e.createPart("Stator")
	if err != nil {
		return 0, err
	}
	if res.ParametersSet, err = e.publishParameters(d); err != nil {
		return 0, fmt.Errorf("stator parameters: %w", err)
	}
	if err := e.buildStatorYoke(); err != nil {
		return 0, err
	}
	if err := e.buildToothPattern(d, l); err != nil {
		return 0, err
	}
	res.StatorBodies = 1 // yoke + patterned teeth are one fused iron body
	return id, e.assignPartMaterial(res.IronMaterial)
}

// buildStatorYoke extrudes the smooth annulus between the stator yoke boundary and the slot
// bottoms (two grounded, radius-dimensioned circles).
func (e *Engine) buildStatorYoke() error {
	sk, err := e.api.Sketch().Create(wire.CreateSketchArgs{Plane: "XY"})
	if err != nil {
		return fmt.Errorf("stator yoke sketch: %w", err)
	}
	if err := e.addGroundedCircle(sk.SketchIndex, "stator_yoke_r"); err != nil {
		return fmt.Errorf("stator yoke OD: %w", err)
	}
	if err := e.addGroundedCircle(sk.SketchIndex, "slot_bottom_r"); err != nil {
		return fmt.Errorf("stator yoke bore: %w", err)
	}
	_, err = e.extrudeNamed(sk.SketchIndex, "new", "Stator Yoke")
	return err
}

// buildToothPattern extrude-joins one real-arc tooth to the yoke and circular-patterns it
// across the slots, so the toothed bore is real arcs (not segments) and re-patterns when the
// slots parameter changes.
func (e *Engine) buildToothPattern(d *Design, l layout) error {
	sk, err := e.api.Sketch().Create(wire.CreateSketchArgs{Plane: "XY"})
	if err != nil {
		return fmt.Errorf("tooth sketch: %w", err)
	}
	if err := e.addAnnularSector(sk.SketchIndex, toothSector(l, d)); err != nil {
		return fmt.Errorf("tooth profile: %w", err)
	}
	if _, err := e.extrudeNamed(sk.SketchIndex, "join", "Tooth"); err != nil {
		return fmt.Errorf("tooth extrude: %w", err)
	}
	return e.patternCircular("Tooth", d.Spec.Slots, "slots")
}

// buildRotorPart creates the "Rotor" back-iron annulus from two grounded, radius-dimensioned
// circles whose role-based radii flip with the topology, so both inrunner and outrunner rotors
// are literal-free and recompute from the published radii.
func (e *Engine) buildRotorPart(d *Design, l layout, res *GenerateResult) (uint64, error) {
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
	outer, inner := rotorRadii(l)
	if err := e.addGroundedCircle(sk.SketchIndex, outer); err != nil {
		return 0, fmt.Errorf("rotor outer: %w", err)
	}
	if err := e.addGroundedCircle(sk.SketchIndex, inner); err != nil {
		return 0, fmt.Errorf("rotor inner: %w", err)
	}
	if res.RotorBodies, err = e.extrudeNamed(sk.SketchIndex, "new", "Rotor Iron"); err != nil {
		return 0, fmt.Errorf("rotor extrude: %w", err)
	}
	return id, e.assignPartMaterial(res.IronMaterial)
}

// buildMagnetPart creates the "Magnets" part: one real-arc magnet sector, circular-patterned
// across the poles (one body per pole), with the magnet grade assigned to the whole part.
func (e *Engine) buildMagnetPart(d *Design, l layout, res *GenerateResult) (uint64, error) {
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
	if err := e.addAnnularSector(sk.SketchIndex, magnetSector(l, d)); err != nil {
		return 0, fmt.Errorf("magnet profile: %w", err)
	}
	if _, err := e.extrudeNamed(sk.SketchIndex, "new", "Magnet"); err != nil {
		return 0, fmt.Errorf("magnet extrude: %w", err)
	}
	if err := e.patternCircular("Magnet", d.Spec.Poles, "poles"); err != nil {
		return 0, fmt.Errorf("magnet pattern: %w", err)
	}
	res.MagnetBodies = d.Spec.Poles // one magnet per pole after the circular pattern
	return id, e.assignPartMaterial(res.MagnetMatID)
}

// rotorRadii returns the rotor iron's outer/inner role-based radius parameters for the
// topology: inrunner = magnet back .. rotor yoke (shaft side); outrunner = rotor yoke (ring OD)
// .. magnet tip (ring inner face).
func rotorRadii(l layout) (outerParam, innerParam string) {
	if l.teethFaceOut {
		return "rotor_yoke_r", "magnet_tip_r"
	}
	return "magnet_back_r", "rotor_yoke_r"
}

// toothSector specs one stator tooth as an annular sector between the tooth tip and slot bottom,
// spanning the tooth's angular share of a slot pitch (toothFrac · π / slots half-angle).
func toothSector(l layout, d *Design) sector {
	half := l.toothFrac * math.Pi / float64(d.Spec.Slots)
	return sector{
		rInnerSeed: l.toothTipR, rOuterSeed: l.slotBottomR, halfSeed: half,
		rInnerParam: "tooth_tip_r", rOuterParam: "slot_bottom_r", spanParam: "tooth_angle",
	}
}

// magnetSector specs one magnet as an annular sector between its back and tip radii, spanning
// the pole arc (MagnetArcDeg full span; half-angle in radians seeds the placement).
func magnetSector(l layout, d *Design) sector {
	half := d.MagnetArcDeg * math.Pi / 360
	return sector{
		rInnerSeed: l.magnetBackR, rOuterSeed: l.magnetTipR, halfSeed: half,
		rInnerParam: "magnet_back_r", rOuterParam: "magnet_tip_r", spanParam: "magnet_arc_deg",
	}
}

// patternCircular replicates a named feature into count instances over a full turn about the
// part axis (+Z). The literal count satisfies the host schema and seeds the fake; countExpr is
// the parameter the live host actually uses, so the instance count tracks the parameter (#189).
func (e *Engine) patternCircular(feature string, count int, countExpr string) error {
	_, err := e.api.Features().PatternCircular(wire.CircularPatternFeatureArgs{
		SourceFeatures: []string{feature},
		Count:          count,
		CountExpr:      countExpr,
		Angle:          "360 deg",
		AxisPoint:      []float64{0, 0, 0},
		AxisDir:        []float64{0, 0, 1},
	})
	if err != nil {
		return fmt.Errorf("circular pattern of %q (count %s): %w", feature, countExpr, err)
	}
	return nil
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

// extrudeNamed extrudes a sketch's first profile over the stack length with the given boolean
// operation ("new"/"join") and renames the resulting feature (best-effort), returning the
// number of solid bodies produced.
func (e *Engine) extrudeNamed(sketchIndex int, operation, name string) (int, error) {
	n, err := e.extrude(sketchIndex, operation)
	if err != nil {
		return 0, err
	}
	e.renameLastFeature(name) // best-effort; a rename failure must not fail generation
	return n, nil
}

// extrude extrudes a sketch's first profile over the stack length with the given boolean
// operation and returns the solid body count, failing loudly when the host reports an
// unhealthy or empty extrude.
func (e *Engine) extrude(sketchIndex int, operation string) (int, error) {
	args, err := json.Marshal(extrudeArgs{
		SketchIndex: sketchIndex, ProfileIndex: 0, Distance: stackLengthExpr, Operation: operation,
	})
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

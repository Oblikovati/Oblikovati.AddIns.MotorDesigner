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
	ParametersSet int    // parameters published on the Motor assembly (the source the parts derive from)
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

// MotorAssemblyName is the document name of the generated assembly. It is both the assembly's
// display name and — because an unsaved document's full name is the name it was created with
// (model/doc.Workspace.Add) — the SourceDocument the component parts link their derived
// parameters from. Defined once so the create call and the derive calls cannot drift.
const MotorAssemblyName = "Motor"

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
// Parameter ownership (M39): the Motor ASSEMBLY owns the design's full sizing program (formulas
// of the design drivers) as the single source of truth, created FIRST so it exists before the
// parts. Each component part then LINKS only the assembly parameters it consumes as read-only
// derived parameters (a derived-parameter table), and builds REAL, fully-constrained geometry
// driven by those linked names — grounded radius-dimensioned circles for the yoke/rotor annuli,
// and real-arc annular sectors (driving radius + half-angle dimensions) for the stator tooth and
// the magnet, each circular-patterned by a parameter-driven count. Editing a driver on the
// assembly repropagates to every linked part, which recomputes in place — no duplicated program,
// no polylines, no literal coordinates, no sketch recreation.
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
	// The assembly is created and parameterized FIRST so it can be the derive source while the
	// component parts are built. The geometry is then laid down from the topology-resolved layout
	// (role-based radii). The FEMM hand-off (publishFEMMDescriptor) builds its own faceted
	// CrossSection from the Design for 2D meshing — the host sketches are real arcs, so the two
	// no longer share a profile.
	if err := e.createMotorAssembly(d, res); err != nil {
		return nil, err
	}
	if err := e.buildComponents(d, resolveLayout(d), res); err != nil {
		return nil, err
	}
	if err := e.placeComponents(res); err != nil {
		return nil, err
	}
	// Hand the FEMM descriptor of this motor to the magnetics add-in (best-effort: a failed
	// hand-off must not fail generation; the FEMM study just won't find a fresh descriptor).
	_ = publishFEMMDescriptor(d)
	// Stamp the design onto the assembly so a re-opened motor is recognisable and
	// rebuildable from its own stored Spec (LoadSpec / IsMotorAssembly).
	return res, e.saveSpec(res.AssemblyID, s)
}

// disableInference turns OFF auto-constraint inference for the session and returns the prior
// options to restore. Best-effort: a nil return means the host could not report them (an older
// host), in which case the build proceeds with inference on (and relies on seeds not tripping it).
func (e *Engine) disableInference() *wire.InferenceOptionsView {
	prior, err := e.api.Sketch().InferenceOptions()
	if err != nil {
		return nil
	}
	_, _ = e.api.Sketch().SetInferenceOptions(wire.InferenceOptionsView{InferEnabled: false, ConstrainEnabled: false})
	return &prior
}

// restoreInference puts the user's inference options back after a build (no-op if they could
// not be read).
func (e *Engine) restoreInference(prior *wire.InferenceOptionsView) {
	if prior == nil {
		return
	}
	_, _ = e.api.Sketch().SetInferenceOptions(*prior)
}

// createMotorAssembly creates the Motor assembly, activates it, marks it a motor member, and
// publishes the full sizing parameter program onto it — the single source of truth the parts
// derive from (M39-F03 makes the assembly a first-class parameter holder). It must run before
// buildComponents so the parts can link their parameters from an open, parameterized source.
func (e *Engine) createMotorAssembly(d *Design, res *GenerateResult) error {
	asm, err := e.api.Documents().Create(wire.CreateDocumentArgs{Type: "assembly", Name: MotorAssemblyName})
	if err != nil {
		return fmt.Errorf("create motor assembly: %w", err)
	}
	res.AssemblyID = asm.ID
	_ = e.markMotorMember(asm.ID) // best-effort: a missing marker only costs a regenerate collision
	if _, err := e.api.Documents().Activate(asm.ID); err != nil {
		return fmt.Errorf("activate motor assembly: %w", err)
	}
	if res.ParametersSet, err = e.publishParameters(d); err != nil {
		return fmt.Errorf("assembly parameters: %w", err)
	}
	return nil
}

// buildComponents creates and fills the three component part documents.
func (e *Engine) buildComponents(d *Design, l layout, res *GenerateResult) error {
	var err error
	if res.StatorDocID, err = e.buildStatorPart(d, l, res); err != nil {
		return err
	}
	if res.RotorDocID, err = e.buildRotorPart(l, res); err != nil {
		return err
	}
	res.MagnetDocID, err = e.buildMagnetPart(d, l, res)
	return err
}

// buildStatorPart creates the "Stator" part the canonical way: a smooth yoke annulus, one
// real-arc tooth extrude-joined to it, then a circular pattern of the tooth whose count tracks
// the slots parameter. Every dimension is driven by a parameter LINKED from the Motor assembly,
// so editing a driver on the assembly recomputes the stator in place. The yoke + teeth fuse into
// a single iron body.
func (e *Engine) buildStatorPart(d *Design, l layout, res *GenerateResult) (uint64, error) {
	id, err := e.createPart("Stator")
	if err != nil {
		return 0, err
	}
	if err := e.linkAssemblyParameters(statorLinkedParams(d)); err != nil {
		return 0, fmt.Errorf("stator parameters: %w", err)
	}
	if err := e.buildStatorYoke(l); err != nil {
		return 0, err
	}
	if err := e.buildToothPattern(d, l); err != nil {
		return 0, err
	}
	res.StatorBodies = 1 // yoke + patterned teeth are one fused iron body
	return id, e.assignPartMaterial(res.IronMaterial)
}

// statorYokeRadii returns the stator yoke annulus's OUTER then INNER radius parameter, ordered by
// the topology so the extrude's first (boundary) circle is always the larger one and the second is
// the hole. The roles FLIP with the motor type: an inrunner yoke is the OUTER ring (OD =
// stator_yoke_r, bore = slot_bottom_r), while an outrunner yoke is the INNER ring (OD =
// slot_bottom_r where the teeth meet it, bore = stator_yoke_r, the shaft side). Passing them
// inner-first (as the old code did unconditionally) built a solid disk at the smaller radius for
// the outrunner, so the teeth had nothing to fuse to and floated off the yoke.
func statorYokeRadii(l layout) (outerParam, innerParam string) {
	if l.teethFaceOut { // outrunner: yoke is the inner ring; slot bottoms are its OUTER edge
		return "slot_bottom_r", "stator_yoke_r"
	}
	return "stator_yoke_r", "slot_bottom_r" // inrunner: yoke is the outer ring
}

// buildStatorYoke extrudes the smooth annulus between the stator yoke boundary and the slot
// bottoms (two grounded, radius-dimensioned circles), outer circle first so the annulus is valid
// for both motor types.
func (e *Engine) buildStatorYoke(l layout) error {
	sk, err := e.api.Sketch().Create(wire.CreateSketchArgs{Plane: "XY"})
	if err != nil {
		return fmt.Errorf("stator yoke sketch: %w", err)
	}
	outer, inner := statorYokeRadii(l)
	if err := e.addGroundedCircle(sk.SketchIndex, outer); err != nil {
		return fmt.Errorf("stator yoke OD: %w", err)
	}
	if err := e.addGroundedCircle(sk.SketchIndex, inner); err != nil {
		return fmt.Errorf("stator yoke bore: %w", err)
	}
	_, err = e.extrudeNamed(sk.SketchIndex, "new", "Stator Yoke")
	return err
}

// buildToothPattern extrude-joins one real-arc tooth to the yoke and circular-patterns it
// across the slots, so the toothed bore is real arcs (not segments) and re-patterns when the
// slots parameter changes. The tooth's 2D profile is the one selected by Spec.SlotType, so the
// extruded 3D tooth carries the real slot shape that every multiphysics consumer meshes.
func (e *Engine) buildToothPattern(d *Design, l layout) error {
	sk, err := e.api.Sketch().Create(wire.CreateSketchArgs{Plane: "XY"})
	if err != nil {
		return fmt.Errorf("tooth sketch: %w", err)
	}
	// The tooth is the one sketch with straight flanks/undersides, so it is the only one auto-
	// constraint inference can touch. The designer constrains it explicitly to DOF=0, so an
	// inferred constraint can only over-constrain — it snapped a spurious perpendicular between
	// two shoe undersides seeded ~90° apart, leaving the inrunner parallel-tooth profile open.
	// Disable inference just for this sketch (set in the active-part context so a per-document
	// reset can't wipe it), restoring the user's setting afterward.
	defer e.restoreInference(e.disableInference())
	if err := e.addToothProfile(sk.SketchIndex, d, l); err != nil {
		return fmt.Errorf("tooth profile: %w", err)
	}
	if _, err := e.extrudeNamed(sk.SketchIndex, "join", "Tooth"); err != nil {
		return fmt.Errorf("tooth extrude: %w", err)
	}
	return e.patternCircular("Tooth", d.Spec.Slots, "slots")
}

// addToothProfile lays the stator tooth sketch for the design's selected slot type: an
// open-rectangular (parallel, no shoe), a parallel-tooth (parallel body + shoe), or a
// round-bottom (radial body + shoe). Each is fully constrained (DOF=0) and driven by the
// linked assembly parameters, and each works for both motor types via the role-based seeds and
// param names (the outrunner tooth root reaches stator_yoke_r so the join fuses to one shell).
func (e *Engine) addToothProfile(sk int, d *Design, l layout) error {
	switch d.Spec.normSlotType() {
	case SlotOpenRectangular:
		return e.addOpenRectTooth(sk, openRectToothSpec(l, d))
	case SlotRoundBottom:
		return e.addShoeTooth(sk, shoeToothSpec(l, d, true))
	default: // SlotParallelTooth
		return e.addShoeTooth(sk, shoeToothSpec(l, d, false))
	}
}

// toothRootR is the cm seed radius of the tooth root arc: the slot bottom for an inrunner, but
// the yoke INNER radius for an outrunner so the tooth overlaps the whole yoke ring and the
// boolean join fuses to one shell (mirrors toothRootParam, which names the same radius).
func toothRootR(l layout) float64 {
	if l.teethFaceOut {
		return l.statorYokeR
	}
	return l.slotBottomR
}

// openRectToothSpec builds the open-rectangular tooth spec for the layout, ordering the two
// arcs by radius (inner = smaller) so the profile winds correctly for both motor types and
// matching each arc to its driving parameter name.
func openRectToothSpec(l layout, d *Design) openRectTooth {
	tip, root := l.toothTipR, toothRootR(l)
	hw := mmToCM(d.ToothWidth) / 2
	if tip <= root { // inrunner: tip at the bore is the inner (smaller-radius) arc
		return openRectTooth{rInnerSeed: tip, rOuterSeed: root, halfWidthSeed: hw,
			rInnerParam: "tooth_tip_r", rOuterParam: toothRootParam(l), widthParam: "tooth_width"}
	}
	return openRectTooth{rInnerSeed: root, rOuterSeed: tip, halfWidthSeed: hw,
		rInnerParam: toothRootParam(l), rOuterParam: "tooth_tip_r", widthParam: "tooth_width"}
}

// shoeToothSpec builds the semi-closed (parallel-tooth / round-bottom) tooth spec: the tip,
// root and neck seed radii (neck offset from the tip toward the root, clamped to 90% of the
// tip→root span), the shoe and body half-angle seeds, and the driving parameter names.
func shoeToothSpec(l layout, d *Design, radial bool) shoeTooth {
	tip, root := l.toothTipR, toothRootR(l)
	dir := math.Copysign(1, root-tip)
	tipH := mmToCM(d.Spec.toothTipHeightMM())
	neck := tip + dir*math.Min(tipH, 0.9*math.Abs(root-tip))
	opening := mmToCM(d.Spec.slotOpeningMM())
	slotPitch := 2 * math.Pi / float64(d.Spec.Slots)
	shoeHalf := math.Max(slotPitch/2-math.Asin(math.Min(1, opening/(2*tip))), math.Pi/180)
	return shoeTooth{
		radial:        radial,
		tipSeed:       tip,
		rootSeed:      root,
		neckSeed:      neck,
		bodyHalfSeed:  l.toothFrac * math.Pi / float64(d.Spec.Slots),
		shoeHalfSeed:  shoeHalf,
		halfWidthSeed: mmToCM(d.ToothWidth) / 2,
		tipParam:      "tooth_tip_r",
		rootParam:     toothRootParam(l),
		neckParam:     "neck_r",
		tipChordParam: "tip_chord",
		bodyParam:     bodyParamName(radial),
	}
}

// bodyParamName is the body-size parameter for a slot profile: a constant angle for a radial
// (round-bottom) body, a constant width for a parallel body.
func bodyParamName(radial bool) string {
	if radial {
		return "tooth_angle"
	}
	return "tooth_width"
}

// buildRotorPart creates the "Rotor" back-iron annulus from two grounded, radius-dimensioned
// circles whose role-based radii flip with the topology, so both inrunner and outrunner rotors
// are literal-free and recompute from the radii linked off the assembly.
func (e *Engine) buildRotorPart(l layout, res *GenerateResult) (uint64, error) {
	id, err := e.createPart("Rotor")
	if err != nil {
		return 0, err
	}
	if err := e.linkAssemblyParameters(rotorLinkedParams(l)); err != nil {
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
	if err := e.linkAssemblyParameters(magnetLinkedParams()); err != nil {
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

// linkAssemblyParameters links the named Motor-assembly parameters into the active part as
// read-only derived parameters (M39 derived-parameter table). It must run while the part is
// active and the assembly is open; afterwards the part's sketches/features resolve those names
// against the linked values, which track the assembly. Linking the consumed subset (not the
// whole program) keeps each part's parameter list to exactly what its geometry dimensions.
func (e *Engine) linkAssemblyParameters(names []string) error {
	_, err := e.api.Parameters().AddDerivedTable(wire.DerivedParameterTableAddArgs{
		SourceDocument: MotorAssemblyName,
		Linked:         names,
	})
	if err != nil {
		return fmt.Errorf("link motor parameters %v from %q: %w", names, MotorAssemblyName, err)
	}
	return nil
}

// placeComponents activates the (already-created) Motor assembly and places the three component
// parts coaxially (identity transform — the cross-section already places everything about the
// origin). The assembly was created in createMotorAssembly so it could source the parts'
// parameters; this is the final step that nests the finished parts into it.
func (e *Engine) placeComponents(res *GenerateResult) error {
	if _, err := e.api.Documents().Activate(res.AssemblyID); err != nil {
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

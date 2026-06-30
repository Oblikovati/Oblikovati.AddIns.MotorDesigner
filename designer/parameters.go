// SPDX-License-Identifier: GPL-2.0-only

package designer

import (
	"fmt"

	"oblikovati.org/api/wire"
)

// motorParam is one host user-parameter the design publishes. Driven parameters carry a
// formula referencing earlier ones so the model stays parametric (CLAUDE.md: "Use
// parameters with formulas to calculate derived dimensions"); independent parameters
// carry a literal unit-bearing value.
type motorParam struct {
	name string
	expr string // unit-bearing expression, literal or formula
}

// designParameters returns the ordered parameter program for a design. Independent
// requirement/loading inputs come first (literals), then the derived cross-section
// dimensions as formulas of them, so editing e.g. bore_dia in the host re-drives the
// stator/rotor radii and the model recomputes (DOF-0 parametric intent).
func designParameters(d *Design) []motorParam {
	base := []motorParam{
		// Mathematical constant, so the pitch formulas read naturally (defined once).
		{"pi", "3.14159265358979"},

		// Independent design inputs (the user-editable drivers). The flux densities and counts
		// are unitless; lengths carry mm.
		{"airgap_b", fmt.Sprintf("%g", d.Spec.AirgapB)}, // target airgap flux density B_g [T]
		{"tooth_b", fmt.Sprintf("%g", d.Spec.ToothB)},   // target tooth flux density B_t [T]
		{"yoke_b", fmt.Sprintf("%g", d.Spec.YokeB)},     // target yoke flux density B_y [T]
		{"poles", fmt.Sprintf("%d", d.Spec.Poles)},
		{"slots", fmt.Sprintf("%d", d.Spec.Slots)},
		{"airgap", fmt.Sprintf("%.4f mm", d.Spec.AirgapMM)},
		{"magnet_thick", fmt.Sprintf("%.4f mm", d.MagnetThick)},
		{"bore_dia", fmt.Sprintf("%.4f mm", d.BoreDiameter)},
		{"stack_length", fmt.Sprintf("%.4f mm", d.StackLength)},

		// Derived dimensions as FORMULAS of the inputs (flux-balance sizing), not baked values,
		// so editing a driver re-derives the whole cross-section. Ordered by dependency.
		{"bore_r", "bore_dia / 2"},
		{"slot_pitch", "pi * bore_dia / slots"},                   // tau_s = pi*D/Q
		{"pole_pitch", "pi * bore_dia / poles"},                   // tau_p = pi*D/2p
		{"tooth_width", "airgap_b * slot_pitch / tooth_b"},        // w_t = B_g*tau_s/B_t
		{"stator_yoke", "airgap_b * bore_dia / (poles * yoke_b)"}, // h_y = B_g*D/(2p*B_y), L cancels
		{"rotor_yoke", "stator_yoke"},                             // flux-balance symmetry
		{"slot_depth", fmt.Sprintf("%.4f mm", d.SlotDepth)},       // trapezoidal-slot quadratic (no closed form)
		{"stator_outer_r", "bore_r + slot_depth + stator_yoke"},
		{"rotor_outer_r", "bore_r - airgap"},
		{"magnet_inner_r", "rotor_outer_r - magnet_thick"},
		{"rotor_inner_r", "magnet_inner_r - rotor_yoke"},

		// Magnet pole-arc fraction (independent input) and the angular spans the tooth/magnet
		// sketches dimension against. Angles carry deg so they stay angle-typed in the param DAG.
		{"magnet_arc_frac", formatFrac(d.Spec.MagnetArc)},
		{"magnet_arc_deg", "magnet_arc_frac * 360 deg / poles"},
		{"slot_pitch_angle", "360 deg / slots"},
		{"tooth_angle", "tooth_width / slot_pitch * 360 deg / slots"},
		{"pole_pitch_angle", "360 deg / poles"},
	}
	base = append(base, geometryRadii(d)...)
	return append(base, slotProfileParams(d)...)
}

// slotProfileParams are the semi-closed-slot shoe dimensions the parallel-tooth and
// round-bottom stator-tooth profiles dimension against. They depend on the role-based radii
// (geometryRadii) and the slot-pitch angle, so they are appended LAST. Each is a formula of
// the drivers (slot_opening, tooth_tip_height, tooth_tip_r, slots), with the degeneracy
// guards baked in as min/max clamps so a live driver edit can never drive the shoe
// inside-out (advisor pitfalls 1–4): shoe_half_angle stays positive, neck_r stays strictly
// between the tip and the root. The neck offset sign flips with the motor type — the neck
// sits toward the ROOT, which is at a larger radius for an inrunner and a smaller one for an
// outrunner.
func slotProfileParams(d *Design) []motorParam {
	base := []motorParam{
		// Independent shoe inputs (literals carried in mm).
		{"slot_opening", fmt.Sprintf("%.4f mm", d.Spec.slotOpeningMM())},
		{"tooth_tip_height", fmt.Sprintf("%.4f mm", d.Spec.toothTipHeightMM())},

		// Shoe half-angle at the airgap: half the slot pitch minus the half-opening it leaves
		// between adjacent shoes (exact, chord-based via asin). Clamped > 0 so an over-wide
		// slot_opening can never invert the shoe.
		{"shoe_half_angle", "max(slot_pitch_angle / 2 - asin((slot_opening / 2) / tooth_tip_r), 1 deg)"},
		// Tip-arc chord spanning ±shoe_half_angle — encodes the shoe span as a pure length so
		// the tip corners need only a point-distance dimension (no construction radial).
		{"tip_chord", "2 * tooth_tip_r * sin(shoe_half_angle)"},
	}
	return append(base, motorParam{"neck_r", neckRadiusExpr(d)})
}

// neckRadiusExpr is the radius where the shoe underside meets the parallel/radial tooth body,
// offset from the tooth tip toward the root by tooth_tip_height — clamped to 90% of the
// tip→root span so the neck stays strictly between them (never crosses the root). The sign
// flips with the motor type because the root is outward of the tip for an inrunner and inward
// for an outrunner.
func neckRadiusExpr(d *Design) string {
	if d.Spec.normType() == Outrunner {
		return "tooth_tip_r - min(tooth_tip_height, 0.9 * (tooth_tip_r - stator_yoke_r))"
	}
	return "tooth_tip_r + min(tooth_tip_height, 0.9 * (slot_bottom_r - tooth_tip_r))"
}

// geometryRadii are the role-based boundary radii the tooth/magnet/circle builders
// dimension against. Their formulas flip with the motor type: an inrunner stacks the slot
// bottoms and yoke OUTWARD beyond the bore, while an outrunner mirrors the stack-up about
// the airgap (stator inside, slot bottoms within the bore). Defined as formulas of the
// drivers so editing a driver re-derives them (and recomputes the parts).
func geometryRadii(d *Design) []motorParam {
	if d.Spec.normType() == Outrunner {
		return []motorParam{
			{"tooth_tip_r", "bore_r"},
			{"slot_bottom_r", "bore_r - slot_depth"},
			{"stator_yoke_r", "bore_r - slot_depth - stator_yoke"},
			{"magnet_back_r", "bore_r + airgap"},
			{"magnet_tip_r", "bore_r + airgap + magnet_thick"},
			{"rotor_yoke_r", "bore_r + airgap + magnet_thick + rotor_yoke"},
		}
	}
	return []motorParam{
		{"tooth_tip_r", "bore_r"},
		{"slot_bottom_r", "bore_r + slot_depth"},
		{"stator_yoke_r", "stator_outer_r"},
		{"magnet_back_r", "magnet_inner_r"},
		{"magnet_tip_r", "rotor_outer_r"},
		{"rotor_yoke_r", "rotor_inner_r"},
	}
}

// formatFrac renders a unitless pole-arc fraction as a bare decimal literal (no unit), so
// the param engine reads it as the dimensionless multiplier the angle formulas expect.
func formatFrac(v float64) string { return fmt.Sprintf("%g", v) }

// The motor sizing program lives ONCE on the Motor assembly (the single source of truth);
// each component part links only the assembly parameters its own geometry dimensions against,
// as read-only derived parameters (M39 derived-parameter tables). These three helpers name
// that consumed subset per part — kept beside geometryRadii so the names stay in lockstep with
// the role-based radii the builders reference. A part dimensioning a name not in its subset
// would have nothing to resolve, so each list is the exact closure of one part's sketches +
// pattern + extrude. The linked values track the assembly: edit a driver there and every part
// recomputes (true DOF-0, cross-document parametric intent).

// statorLinkedParams are the assembly parameters the Stator dimensions against. The yoke
// annulus always needs both yoke-boundary radii (stator_yoke_r, slot_bottom_r — one is the
// tooth root, which flips with the motor type) plus the slot count its tooth pattern tracks
// and the shared stack length. The TOOTH adds the names its selected slot profile dimensions
// against (slotProfileLinkedParams), kept to exactly that closure so each part links only
// what its geometry resolves.
func statorLinkedParams(d *Design) []string {
	base := []string{stackLengthExpr, "stator_yoke_r", "slot_bottom_r", "tooth_tip_r", "slots"}
	return append(base, slotProfileLinkedParams(d)...)
}

// slotProfileLinkedParams names the tooth-profile parameters the active slot type dimensions
// against: open-rectangular and parallel-tooth size their constant-width body by tooth_width;
// round-bottom sizes its radial body by tooth_angle. The two shoe profiles additionally need
// the neck radius and the tip-chord span. The tooth root radius is already covered by the
// yoke radii in statorLinkedParams (slot_bottom_r inrunner / stator_yoke_r outrunner).
func slotProfileLinkedParams(d *Design) []string {
	switch d.Spec.normSlotType() {
	case SlotOpenRectangular:
		return []string{"tooth_width"}
	case SlotRoundBottom:
		return []string{"tooth_angle", "neck_r", "tip_chord"}
	default: // SlotParallelTooth
		return []string{"tooth_width", "neck_r", "tip_chord"}
	}
}

// toothRootParam is the radius parameter the tooth root arc dimensions against: the slot
// bottom for an inrunner, but the yoke INNER radius for an outrunner so the tooth overlaps
// the whole yoke ring and the boolean join fuses to one shell (see the outrunner stator fix).
func toothRootParam(l layout) string {
	if l.teethFaceOut {
		return "stator_yoke_r"
	}
	return "slot_bottom_r"
}

// rotorLinkedParams are the assembly parameters the Rotor back-iron annulus dimensions against:
// its two role-based boundary radii (which flip with the topology, hence from rotorRadii) plus
// the shared stack length.
func rotorLinkedParams(l layout) []string {
	outer, inner := rotorRadii(l)
	return []string{stackLengthExpr, outer, inner}
}

// magnetLinkedParams are the assembly parameters the Magnets dimension against: the magnet
// sector's back/tip radii and pole-arc span, the pole count its pattern tracks, and the shared
// stack length.
func magnetLinkedParams() []string {
	return []string{stackLengthExpr, "magnet_back_r", "magnet_tip_r", "magnet_arc_deg", "poles"}
}

// publishParameters adds the design's full parameter program to the active document —
// the Motor assembly, which owns the sizing program as the single source of truth that the
// component parts derive from (M39-F03). Idempotent (Set when a parameter already exists, Add
// otherwise), so a regenerate updates the assembly in place. It returns the count published.
func (e *Engine) publishParameters(d *Design) (int, error) {
	existing, err := e.existingParameterNames()
	if err != nil {
		return 0, err
	}
	params := designParameters(d)
	for _, p := range params {
		if err := e.upsertParameter(p, existing); err != nil {
			return 0, fmt.Errorf("parameter %q: %w", p.name, err)
		}
	}
	return len(params), nil
}

// upsertParameter sets an existing parameter or adds a new one.
func (e *Engine) upsertParameter(p motorParam, existing map[string]bool) error {
	args := wire.ParameterSetArgs{Name: p.name, Expression: p.expr}
	if existing[p.name] {
		_, err := e.api.Parameters().Set(args)
		return err
	}
	_, err := e.api.Parameters().Add(args)
	return err
}

// existingParameterNames returns the set of parameter names already on the active part.
func (e *Engine) existingParameterNames() (map[string]bool, error) {
	list, err := e.api.Parameters().List()
	if err != nil {
		return nil, err
	}
	names := make(map[string]bool, len(list.Parameters))
	for _, p := range list.Parameters {
		names[p.Name] = true
	}
	return names, nil
}

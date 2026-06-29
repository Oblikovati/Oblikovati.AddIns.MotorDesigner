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
	return append(base, geometryRadii(d)...)
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

// publishParameters adds the design's parameter program to the active part, idempotently
// (Set when a parameter already exists, Add otherwise). It returns the count published.
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

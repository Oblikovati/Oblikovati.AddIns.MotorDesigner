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
	return []motorParam{
		// Independent inputs (the user-editable design drivers).
		{"airgap", fmt.Sprintf("%.4f mm", d.Spec.AirgapMM)},
		{"magnet_thick", fmt.Sprintf("%.4f mm", d.MagnetThick)},
		{"bore_dia", fmt.Sprintf("%.4f mm", d.BoreDiameter)},
		{"stack_length", fmt.Sprintf("%.4f mm", d.StackLength)},
		// Derived stator radii (formulas of the inputs above).
		{"bore_r", "bore_dia / 2"},
		{"slot_depth", fmt.Sprintf("%.4f mm", d.SlotDepth)},
		{"stator_yoke", fmt.Sprintf("%.4f mm", d.StatorYokeH)},
		{"stator_outer_r", "bore_r + slot_depth + stator_yoke"},
		// Derived rotor radii.
		{"rotor_outer_r", "bore_r - airgap"},
		{"magnet_inner_r", "rotor_outer_r - magnet_thick"},
		{"rotor_yoke", fmt.Sprintf("%.4f mm", d.RotorYokeH)},
		{"rotor_inner_r", fmt.Sprintf("%.4f mm", d.RotorYokeInnR)},
	}
}

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

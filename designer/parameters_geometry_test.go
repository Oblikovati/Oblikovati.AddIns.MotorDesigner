// SPDX-License-Identifier: GPL-2.0-only

package designer

import "testing"

// paramExprMap indexes a design's published parameter program by name → expression.
func paramExprMap(d *Design) map[string]string {
	m := map[string]string{}
	for _, p := range designParameters(d) {
		m[p.name] = p.expr
	}
	return m
}

// The geometry layer drives every arc radius and angular span from a published parameter,
// so editing a driver recomputes the parts. These role-based radii (mirroring the layout
// struct) are what the tooth/magnet/circle builders dimension against; an inrunner stacks
// them outward (slot bottoms beyond the bore), so each must be a FORMULA of the drivers.
func TestDesignParametersInrunnerGeometryRadii(t *testing.T) {
	d, err := Compute(DefaultSpec()) // DefaultSpec is an inrunner
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	m := paramExprMap(d)
	want := map[string]string{
		"tooth_tip_r":      "bore_r",
		"slot_bottom_r":    "bore_r + slot_depth",
		"stator_yoke_r":    "stator_outer_r",
		"magnet_back_r":    "magnet_inner_r",
		"magnet_tip_r":     "rotor_outer_r",
		"rotor_yoke_r":     "rotor_inner_r",
		"magnet_arc_frac":  formatFrac(d.Spec.MagnetArc),
		"magnet_arc_deg":   "magnet_arc_frac * 360 deg / poles",
		"slot_pitch_angle": "360 deg / slots",
		"tooth_angle":      "tooth_width / slot_pitch * 360 deg / slots",
		"pole_pitch_angle": "360 deg / poles",
	}
	for name, expr := range want {
		if m[name] != expr {
			t.Errorf("inrunner param %q = %q, want %q", name, m[name], expr)
		}
	}
}

// An outrunner mirrors the radial stack-up about the airgap: the stator becomes the inner
// member (slot bottoms INSIDE the bore) and the magnets sit beyond the airgap, so the same
// role-based radii take the flipped formulas.
func TestDesignParametersOutrunnerGeometryRadii(t *testing.T) {
	s := DefaultSpec()
	s.Type = Outrunner
	d, err := Compute(s)
	if err != nil {
		t.Fatalf("Compute(outrunner): %v", err)
	}
	m := paramExprMap(d)
	want := map[string]string{
		"tooth_tip_r":   "bore_r",
		"slot_bottom_r": "bore_r - slot_depth",
		"stator_yoke_r": "bore_r - slot_depth - stator_yoke",
		"magnet_back_r": "bore_r + airgap",
		"magnet_tip_r":  "bore_r + airgap + magnet_thick",
		"rotor_yoke_r":  "bore_r + airgap + magnet_thick + rotor_yoke",
	}
	for name, expr := range want {
		if m[name] != expr {
			t.Errorf("outrunner param %q = %q, want %q", name, m[name], expr)
		}
	}
}

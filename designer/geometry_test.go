// SPDX-License-Identifier: GPL-2.0-only

package designer

import (
	"testing"

	"oblikovati.org/api/wire"
)

func TestGenerateBuildsAssemblyOfThreeParts(t *testing.T) {
	h := &fakeHost{}
	e := NewEngine(h)
	res, err := e.Generate(DefaultSpec())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got := h.docTypeCount("part"); got != 3 {
		t.Errorf("part documents = %d, want 3 (stator + rotor + magnets)", got)
	}
	if got := h.docTypeCount("assembly"); got != 1 {
		t.Errorf("assembly documents = %d, want 1 (Motor)", got)
	}
	if res.StatorBodies != 1 || res.RotorBodies != 1 {
		t.Errorf("stator/rotor each want 1 body: stator=%d rotor=%d", res.StatorBodies, res.RotorBodies)
	}
	if res.MagnetBodies != DefaultSpec().Poles {
		t.Errorf("magnet bodies = %d, want %d (one per pole)", res.MagnetBodies, DefaultSpec().Poles)
	}
	if res.AssemblyID == 0 || res.StatorDocID == 0 {
		t.Errorf("document ids not captured: %+v", res)
	}
}

func TestGeneratePlacesEveryComponentInAssembly(t *testing.T) {
	h := &fakeHost{}
	e := NewEngine(h)
	if _, err := e.Generate(DefaultSpec()); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := map[string]bool{"Stator": false, "Rotor": false, "Magnets": false}
	for _, name := range h.placed {
		if _, tracked := want[name]; tracked {
			want[name] = true
		}
	}
	for name, placed := range want {
		if !placed {
			t.Errorf("component %q was not placed in the assembly (placed=%v)", name, h.placed)
		}
	}
}

func TestGenerateAssignsMagneticMaterials(t *testing.T) {
	h := &fakeHost{}
	e := NewEngine(h)
	res, err := e.Generate(DefaultSpec())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// Iron is assigned to two parts (stator + rotor), the magnet grade to one (magnets).
	iron, magnet := 0, 0
	for _, id := range h.assigned {
		switch id {
		case res.IronMaterial:
			iron++
		case res.MagnetMatID:
			magnet++
		}
	}
	if iron != 2 {
		t.Errorf("iron material %q assigned %d times, want 2 (stator+rotor); assigned=%v", res.IronMaterial, iron, h.assigned)
	}
	if magnet != 1 {
		t.Errorf("magnet material %q assigned %d times, want 1; assigned=%v", res.MagnetMatID, magnet, h.assigned)
	}
}

func TestGeneratePublishesParametricProgram(t *testing.T) {
	h := &fakeHost{}
	e := NewEngine(h)
	if _, err := e.Generate(DefaultSpec()); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := map[string]bool{
		"bore_dia": false, "bore_r": false, "stator_outer_r": false,
		"rotor_outer_r": false, "magnet_inner_r": false, "stack_length": false,
	}
	for _, p := range h.params {
		if _, tracked := want[p.Name]; tracked {
			want[p.Name] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("parameter %q was not published", name)
		}
	}
}

func TestGenerateRejectsInvalidSpecBeforeHostCalls(t *testing.T) {
	h := &fakeHost{}
	e := NewEngine(h)
	bad := DefaultSpec()
	bad.Poles = 7
	if _, err := e.Generate(bad); err == nil {
		t.Fatalf("Generate should reject an invalid spec")
	}
	if len(h.calls) != 0 {
		t.Errorf("no host calls should be made for an invalid spec, saw %v", h.calls)
	}
}

func TestGenerateOutrunnerBuildsAssembly(t *testing.T) {
	h := &fakeHost{}
	e := NewEngine(h)
	s := DefaultSpec()
	s.Type = Outrunner
	res, err := e.Generate(s)
	if err != nil {
		t.Fatalf("Generate(outrunner): %v", err)
	}
	if got := h.docTypeCount("part"); got != 3 {
		t.Errorf("outrunner part documents = %d, want 3", got)
	}
	if res.MagnetBodies != s.Poles {
		t.Errorf("outrunner magnet bodies = %d, want %d", res.MagnetBodies, s.Poles)
	}
}

// sameStringSet reports whether two name slices hold the same set of names (order-independent),
// so a link assertion does not depend on the order parameters were linked.
func sameStringSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[string]int, len(got))
	for _, g := range got {
		seen[g]++
	}
	for _, w := range want {
		if seen[w] == 0 {
			return false
		}
		seen[w]--
	}
	return true
}

// TestAssemblyOwnsTheParameterProgramExactlyOnce pins the M39 refactor's central invariant: the
// design's full sizing program is published ONCE, on the Motor assembly (the single source of
// truth), not redundantly on each of the three parts as before. ParametersSet reports the
// assembly's program, and the fake — which records every parameters.add/set — sees exactly the
// program length, proving the per-part duplication is gone.
func TestAssemblyOwnsTheParameterProgramExactlyOnce(t *testing.T) {
	h := &fakeHost{}
	e := NewEngine(h)
	d, err := Compute(DefaultSpec())
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	res, err := e.Generate(DefaultSpec())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := len(designParameters(d))
	if res.ParametersSet != want {
		t.Errorf("ParametersSet = %d, want %d (the full program on the assembly)", res.ParametersSet, want)
	}
	if len(h.params) != want {
		t.Errorf("published %d parameters, want %d once on the assembly (no per-part duplication)", len(h.params), want)
	}
	owned := map[string]bool{}
	for _, p := range designParameters(d) {
		owned[p.name] = true
	}
	for _, l := range h.derived {
		for _, n := range l.linked {
			if !owned[n] {
				t.Errorf("part (doc %d) linked %q, which the assembly does not own", l.doc, n)
			}
		}
	}
}

// TestGenerateLinksConsumedAssemblyParametersIntoEachPart proves each component part derives —
// from the Motor assembly — exactly the parameters its own geometry dimensions against (and no
// more), so editing a driver on the assembly repropagates to the parts. This is the consumer
// half of the assembly-owns-parameters refactor (M39 derived-parameter tables).
func TestGenerateLinksConsumedAssemblyParametersIntoEachPart(t *testing.T) {
	h := &fakeHost{}
	e := NewEngine(h)
	res, err := e.Generate(DefaultSpec()) // DefaultSpec is an inrunner
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	cases := []struct {
		part string
		doc  uint64
		want []string
	}{
		{"Stator", res.StatorDocID, []string{"stack_length", "stator_yoke_r", "slot_bottom_r", "tooth_tip_r", "tooth_angle", "slots"}},
		{"Rotor", res.RotorDocID, []string{"stack_length", "magnet_back_r", "rotor_yoke_r"}},
		{"Magnets", res.MagnetDocID, []string{"stack_length", "magnet_back_r", "magnet_tip_r", "magnet_arc_deg", "poles"}},
	}
	for _, c := range cases {
		names, source, ok := h.linkedFrom(c.doc)
		if !ok {
			t.Errorf("%s linked no assembly parameters", c.part)
			continue
		}
		if source != MotorAssemblyName {
			t.Errorf("%s linked from %q, want %q", c.part, source, MotorAssemblyName)
		}
		if !sameStringSet(names, c.want) {
			t.Errorf("%s linked %v, want %v", c.part, names, c.want)
		}
	}
}

func TestPropagatesHostError(t *testing.T) {
	h := &fakeHost{failOn: wire.MethodDocumentsCreate}
	e := NewEngine(h)
	if _, err := e.Generate(DefaultSpec()); err == nil {
		t.Errorf("Generate should propagate a documents.create failure")
	}
}

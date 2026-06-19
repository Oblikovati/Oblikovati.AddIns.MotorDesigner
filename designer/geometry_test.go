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

func TestGenerateEmitsToothedStatorBoundary(t *testing.T) {
	h := &fakeHost{}
	e := NewEngine(h)
	d, _ := Compute(DefaultSpec())
	if _, err := e.Generate(DefaultSpec()); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// The stator outer boundary is a single closed polyline with many points (toothed), not
	// a plain circle — the central improvement over the old concentric-rings cross-section.
	var sawToothedLoop bool
	for _, ent := range h.entities {
		if ent.Kind == "polyline" && ent.Closed && len(ent.Points) >= d.Spec.Slots*2 {
			sawToothedLoop = true
		}
	}
	if !sawToothedLoop {
		t.Errorf("no toothed stator polyline (>= %d points) found among entities", d.Spec.Slots*2)
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

func TestGeneratePropagatesHostError(t *testing.T) {
	h := &fakeHost{failOn: wire.MethodDocumentsCreate}
	e := NewEngine(h)
	if _, err := e.Generate(DefaultSpec()); err == nil {
		t.Errorf("Generate should propagate a documents.create failure")
	}
}

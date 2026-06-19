// SPDX-License-Identifier: GPL-2.0-only

package designer

import (
	"testing"

	"oblikovati.org/api/wire"
)

func TestGenerateDrivesFullHostSequence(t *testing.T) {
	h := &fakeHost{}
	e := NewEngine(h)
	res, err := e.Generate(DefaultSpec())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if h.docs != 1 {
		t.Errorf("documents.create calls = %d, want 1", h.docs)
	}
	if h.sketches != 2 {
		t.Errorf("sketch.create calls = %d, want 2 (stator + rotor)", h.sketches)
	}
	if h.features != 2 {
		t.Errorf("features.add calls = %d, want 2 (two extrudes)", h.features)
	}
	if len(h.entities) != 4 {
		t.Errorf("sketch entities = %d, want 4 (two circles per annulus)", len(h.entities))
	}
	if res.StatorFeature == 0 || res.RotorFeature == 0 {
		t.Errorf("feature ids not captured: stator=%d rotor=%d", res.StatorFeature, res.RotorFeature)
	}
	if res.DocumentID == 0 {
		t.Errorf("document id not captured")
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

func TestGenerateUsesParameterDrivenRadii(t *testing.T) {
	h := &fakeHost{}
	e := NewEngine(h)
	if _, err := e.Generate(DefaultSpec()); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// The stator outer circle radius must be the parameter name, not a literal — proving
	// the geometry is parameter-driven (DOF-0 intent), so editing the parameter re-drives
	// the model.
	var sawStatorOuter bool
	for _, ent := range h.entities {
		if ent.Radius == "stator_outer_r" {
			sawStatorOuter = true
		}
	}
	if !sawStatorOuter {
		t.Errorf("no circle bound to parameter stator_outer_r; entities=%+v", h.entities)
	}
}

func TestGenerateUpsertsExistingParameters(t *testing.T) {
	// A part that already carries bore_dia should be Set, not Add (idempotent re-run).
	h := &fakeHost{existing: []wire.ParameterInfo{{Name: "bore_dia"}}}
	e := NewEngine(h)
	if _, err := e.Generate(DefaultSpec()); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var sawSet bool
	for i, m := range h.calls {
		if m == wire.MethodParametersSet && h.params[paramIndexAt(h, i)].Name == "bore_dia" {
			sawSet = true
		}
	}
	if !sawSet {
		t.Errorf("existing bore_dia should be Set, not Add")
	}
}

// paramIndexAt maps a call index to its position in h.params by counting add/set calls
// up to (and including) that point.
func paramIndexAt(h *fakeHost, callIdx int) int {
	n := -1
	for i := 0; i <= callIdx; i++ {
		if h.calls[i] == wire.MethodParametersAdd || h.calls[i] == wire.MethodParametersSet {
			n++
		}
	}
	return n
}

func TestGeneratePropagatesHostError(t *testing.T) {
	h := &fakeHost{failOn: wire.MethodDocumentsCreate}
	e := NewEngine(h)
	if _, err := e.Generate(DefaultSpec()); err == nil {
		t.Errorf("Generate should propagate a documents.create failure")
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

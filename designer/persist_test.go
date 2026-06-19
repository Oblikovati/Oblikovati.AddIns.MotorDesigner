// SPDX-License-Identifier: GPL-2.0-only

package designer

import "testing"

// TestGenerateStampsSpecOnAssembly pins that Generate persists the design Spec onto the
// Motor assembly, so a re-opened motor is recognisable as a motor-designer assembly and its
// inputs are recoverable.
func TestGenerateStampsSpecOnAssembly(t *testing.T) {
	h := &fakeHost{}
	e := NewEngine(h)
	res, err := e.Generate(DefaultSpec())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	ok, err := e.IsMotorAssembly(res.AssemblyID)
	if err != nil {
		t.Fatalf("IsMotorAssembly: %v", err)
	}
	if !ok {
		t.Errorf("assembly %d should be recognised as a motor-designer assembly", res.AssemblyID)
	}
}

// TestSpecRoundTripsThroughAttributes pins that the persisted Spec reloads byte-for-byte,
// so a rebuild/adjust uses the exact inputs that built the motor (including the motor type).
func TestSpecRoundTripsThroughAttributes(t *testing.T) {
	h := &fakeHost{}
	e := NewEngine(h)
	want := DefaultSpec()
	want.Type = Outrunner
	want.Poles = 14
	want.Slots = 12
	res, err := e.Generate(want)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	got, ok, err := e.LoadSpec(res.AssemblyID)
	if err != nil || !ok {
		t.Fatalf("LoadSpec: ok=%v err=%v", ok, err)
	}
	if got != want {
		t.Errorf("spec round-trip mismatch:\n got %+v\nwant %+v", got, want)
	}
}

// TestLoadSpecOnNonMotorDocumentReportsAbsent pins that a document the add-in never stamped
// is not mistaken for a motor (no false rebuild offer).
func TestLoadSpecOnNonMotorDocumentReportsAbsent(t *testing.T) {
	h := &fakeHost{}
	e := NewEngine(h)
	_, ok, err := e.LoadSpec(999)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if ok {
		t.Errorf("an un-stamped document must not be recognised as a motor assembly")
	}
}

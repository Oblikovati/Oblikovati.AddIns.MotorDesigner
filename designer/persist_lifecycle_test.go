// SPDX-License-Identifier: GPL-2.0-only

package designer

import (
	"testing"
	"time"

	"oblikovati.org/api/wire"
)

func TestMotorMemberMarkerRoundTrips(t *testing.T) {
	h := &fakeHost{}
	e := NewEngine(h)
	if err := e.markMotorMember(1); err != nil {
		t.Fatalf("markMotorMember: %v", err)
	}
	if ok, _ := e.isMotorMember(1); !ok {
		t.Error("a stamped document should be a motor member")
	}
	if ok, _ := e.isMotorMember(2); ok {
		t.Error("an unstamped document must not be a motor member")
	}
}

// TestActivatingMotorAssemblyRepopulatesForm pins the load-on-activate behavior: activating a
// document that carries the stored Spec repopulates the engine's spec (and re-shows the panel),
// so opening a saved motor restores its parameters into the form.
func TestActivatingMotorAssemblyRepopulatesForm(t *testing.T) {
	h := &fakeHost{}
	e := NewEngine(h)

	stored := DefaultSpec()
	stored.Poles = 8
	stored.Slots = 9
	stored.SlotType = SlotRoundBottom
	if err := e.saveSpec(7, stored); err != nil {
		t.Fatalf("saveSpec: %v", err)
	}

	e.Notify([]byte(`{"type":"` + wire.EventDocumentActivated + `","id":7}`)) // runs on a goroutine
	waitFor(func() bool { return e.Spec().Poles == 8 })

	got := e.Spec()
	if got.Poles != 8 || got.Slots != 9 || got.SlotType != SlotRoundBottom {
		t.Errorf("form spec after activate = %+v, want the stored 8/9/round-bottom", got)
	}
	if !sawCall(h, wire.MethodDockableWindowsSet) {
		t.Error("activating a motor assembly should re-show the panel with the loaded values")
	}
}

func TestActivatingNonMotorDocLeavesSpecUnchanged(t *testing.T) {
	h := &fakeHost{}
	e := NewEngine(h)
	e.SetSpec(DefaultSpec())
	e.Notify([]byte(`{"type":"` + wire.EventDocumentActivated + `","id":99}`))
	time.Sleep(50 * time.Millisecond) // let any goroutine run
	if e.Spec().Poles != DefaultSpec().Poles {
		t.Error("activating a non-motor document must not change the form spec")
	}
}

// waitFor spins (up to ~2s) until cond holds — for asserting on the load-on-activate goroutine.
func waitFor(cond func() bool) {
	for i := 0; i < 200; i++ {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

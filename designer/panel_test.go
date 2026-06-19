// SPDX-License-Identifier: GPL-2.0-only

package designer

import (
	"strings"
	"testing"
	"time"

	"oblikovati.org/api/types"
	"oblikovati.org/api/wire"
)

func TestShowPanelSetsDockableWindow(t *testing.T) {
	h := &fakeHost{}
	e := NewEngine(h)
	if _, err := e.ShowPanel(DefaultSpec()); err != nil {
		t.Fatalf("ShowPanel: %v", err)
	}
	if got := lastCall(h); got != wire.MethodDockableWindowsSet {
		t.Errorf("last call = %q, want %q", got, wire.MethodDockableWindowsSet)
	}
}

func TestPanelControlsHaveGenerateButton(t *testing.T) {
	controls, err := panelControls(DefaultSpec())
	if err != nil {
		t.Fatalf("panelControls: %v", err)
	}
	var btn *wire.PanelControlSpec
	for i := range controls {
		if controls[i].Kind == types.PanelButton {
			btn = &controls[i]
		}
	}
	if btn == nil {
		t.Fatalf("no button in panel controls")
	}
	if btn.CommandID != GenerateCommandID {
		t.Errorf("button command = %q, want %q", btn.CommandID, GenerateCommandID)
	}
}

func TestPanelControlsReflectSpecAndResults(t *testing.T) {
	controls, err := panelControls(DefaultSpec())
	if err != nil {
		t.Fatalf("panelControls: %v", err)
	}
	joined := joinLabels(controls)
	for _, want := range []string{"Torque", "Poles", "Slots", "Slot profile", "Bore", "Stator OD", "k_w1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("panel labels missing %q; got: %s", want, joined)
		}
	}
}

// TestFormHasEditableControls pins that the design drivers are now EDITABLE controls (dropdowns,
// value editors, text boxes), not static labels — the form, not an info readout.
func TestFormHasEditableControls(t *testing.T) {
	controls, err := panelControls(DefaultSpec())
	if err != nil {
		t.Fatalf("panelControls: %v", err)
	}
	kinds := map[types.PanelControlKind]int{}
	for _, c := range controls {
		kinds[c.Kind]++
	}
	if kinds[types.PanelDropdown] < 3 { // motor type, magnet grade, steel grade, slot type
		t.Errorf("want several dropdowns, got %d", kinds[types.PanelDropdown])
	}
	if kinds[types.PanelValueEditor] < 3 { // magnet thickness, airgap, slot opening, tooth tip
		t.Errorf("want several value editors, got %d", kinds[types.PanelValueEditor])
	}
	if kinds[types.PanelTextBox] < 3 { // torque, speed, poles, slots, flux densities
		t.Errorf("want several text boxes, got %d", kinds[types.PanelTextBox])
	}
}

// TestApplyControlWritesSpec pins the form→Spec mapping used when the host reports an edit.
func TestApplyControlWritesSpec(t *testing.T) {
	s := DefaultSpec()
	applyControl(&s, "poles", "14")
	applyControl(&s, "slots", "12")
	applyControl(&s, "slot_type", string(SlotRoundBottom))
	applyControl(&s, "magnet_mm", "5 mm") // unit-bearing value editor
	applyControl(&s, "type", string(Outrunner))
	applyControl(&s, "magnet_arc", "0.7")
	if s.Poles != 14 || s.Slots != 12 {
		t.Errorf("poles/slots = %d/%d, want 14/12", s.Poles, s.Slots)
	}
	if s.SlotType != SlotRoundBottom {
		t.Errorf("slot type = %q, want %q", s.SlotType, SlotRoundBottom)
	}
	if s.MagnetMM != 5 {
		t.Errorf("magnet thickness = %g, want 5 (unit suffix stripped)", s.MagnetMM)
	}
	if s.Type != Outrunner || s.MagnetArc != 0.7 {
		t.Errorf("type/arc = %q/%g, want outrunner/0.7", s.Type, s.MagnetArc)
	}
}

// TestNotifyPanelEditUpdatesSpec pins that a panel.valueChanged event updates the engine's spec
// (so the next Generate uses the edited value) without making host calls.
func TestNotifyPanelEditUpdatesSpec(t *testing.T) {
	h := &fakeHost{}
	e := NewEngine(h)
	ev := `{"type":"` + wire.EventPanelValueChanged + `","windowId":"` + PanelID + `","controlId":"poles","value":"8"}`
	e.Notify([]byte(ev))
	if got := e.Spec().Poles; got != 8 {
		t.Errorf("after panel edit, spec poles = %d, want 8", got)
	}
	if n := h.callCount(); n != 0 {
		t.Errorf("a panel edit must not call the host, saw %d calls", n)
	}
}

func TestRegisterCommandsCreatesGenerateCommand(t *testing.T) {
	h := &fakeHost{}
	e := NewEngine(h)
	if err := e.RegisterCommands(); err != nil {
		t.Fatalf("RegisterCommands: %v", err)
	}
	if got := lastCall(h); got != wire.MethodCommandsCreate {
		t.Errorf("last call = %q, want %q", got, wire.MethodCommandsCreate)
	}
}

func TestSetupRegistersCommandAndShowsPanel(t *testing.T) {
	h := &fakeHost{}
	e := NewEngine(h)
	if err := e.Setup(); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	// Setup must register the command AND set the dockable window — both, in order.
	if !sawCall(h, wire.MethodCommandsCreate) {
		t.Errorf("Setup did not register the Generate command")
	}
	if !sawCall(h, wire.MethodDockableWindowsSet) {
		t.Errorf("Setup did not show the dockable panel")
	}
}

func sawCall(h *fakeHost, method string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, m := range h.calls {
		if m == method {
			return true
		}
	}
	return false
}

func TestNotifyGenerateCommandTriggersGeneration(t *testing.T) {
	h := &fakeHost{}
	e := NewEngine(h)
	ev := []byte(`{"type":"` + wire.EventCommandStarted + `","command":"` + GenerateCommandID + `"}`)
	e.Notify(ev) // dispatches generation onto its own goroutine (never the caller's)
	if !h.waitForDocs(1) {
		t.Errorf("Generate command should have created a document; docs=%d", h.docCount())
	}
}

func TestNotifySurfacesGenerateSuccessOnStatusBar(t *testing.T) {
	h := &fakeHost{}
	e := NewEngine(h)
	e.Notify([]byte(`{"type":"` + wire.EventCommandStarted + `","command":"` + GenerateCommandID + `"}`))
	if !h.waitForDocs(4) { // 3 parts + 1 assembly
		t.Fatalf("generation did not complete; docs=%d", h.docCount())
	}
	waitForStatus(h)
	if msg := h.lastStatus(); !strings.Contains(msg, "generated") {
		t.Errorf("status should report success, got %q", msg)
	}
}

func TestNotifySurfacesGenerateFailureOnStatusBar(t *testing.T) {
	h := &fakeHost{failOn: wire.MethodDocumentsCreate}
	e := NewEngine(h)
	e.Notify([]byte(`{"type":"` + wire.EventCommandStarted + `","command":"` + GenerateCommandID + `"}`))
	waitForStatus(h)
	if msg := h.lastStatus(); !strings.Contains(msg, "failed") {
		t.Errorf("status should report the failure, got %q", msg)
	}
}

// waitForStatus spins (up to ~2s) until a status.setText message has been recorded.
func waitForStatus(h *fakeHost) {
	for i := 0; i < 200; i++ {
		if h.lastStatus() != "" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestNotifyIgnoresUnrelatedEvents(t *testing.T) {
	h := &fakeHost{}
	e := NewEngine(h)
	e.Notify([]byte(`{"type":"selection.changed","count":3}`))
	e.Notify([]byte(`not json`))
	time.Sleep(50 * time.Millisecond) // let any (erroneously) spawned goroutine run
	if n := h.callCount(); n != 0 {
		t.Errorf("unrelated events must not drive the host, saw %d calls", n)
	}
}

func lastCall(h *fakeHost) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.calls) == 0 {
		return ""
	}
	return h.calls[len(h.calls)-1]
}

func joinLabels(controls []wire.PanelControlSpec) string {
	var b strings.Builder
	for _, c := range controls {
		b.WriteString(c.Text)
		b.WriteString(" | ")
	}
	return b.String()
}

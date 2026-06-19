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
	for _, want := range []string{"Torque", "Poles/Slots", "Bore", "Stator OD", "k_w1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("panel labels missing %q; got: %s", want, joined)
		}
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

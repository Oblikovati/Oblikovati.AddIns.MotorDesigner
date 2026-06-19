// SPDX-License-Identifier: GPL-2.0-only

package designer

import (
	"strings"
	"testing"

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

func TestNotifyGenerateCommandTriggersGeneration(t *testing.T) {
	h := &fakeHost{}
	e := NewEngine(h)
	ev := []byte(`{"type":"` + wire.EventCommandStarted + `","command":"` + GenerateCommandID + `"}`)
	e.Notify(ev)
	if h.docs != 1 {
		t.Errorf("Generate command should have created a document; docs=%d", h.docs)
	}
}

func TestNotifyIgnoresUnrelatedEvents(t *testing.T) {
	h := &fakeHost{}
	e := NewEngine(h)
	e.Notify([]byte(`{"type":"selection.changed","count":3}`))
	e.Notify([]byte(`not json`))
	if len(h.calls) != 0 {
		t.Errorf("unrelated events must not drive the host, saw %v", h.calls)
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

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
	// The Generate button is a command-less panel action (id "generate"); a host CommandID would
	// register a second ribbon button, which is exactly what this change removes.
	if btn.CommandID != "" {
		t.Errorf("Generate button has CommandID %q, want empty (panel action, not a ribbon command)", btn.CommandID)
	}
	if btn.ID != generateControlID {
		t.Errorf("Generate button id = %q, want %q", btn.ID, generateControlID)
	}
}

// generatePanelEvent is the host event for clicking the panel's command-less Generate button:
// a PanelValueChanged for the generate control id (see clickAddInPanelButton in the head).
func generatePanelEvent() []byte {
	return []byte(`{"type":"` + wire.EventPanelValueChanged + `","windowId":"` + PanelID +
		`","controlId":"` + generateControlID + `","value":""}`)
}

// typeDropdownEvent is the host event for choosing a motor type in the panel's "type" dropdown.
func typeDropdownEvent(t MotorType) []byte {
	return []byte(`{"type":"` + wire.EventPanelValueChanged + `","windowId":"` + PanelID +
		`","controlId":"type","value":"` + string(t) + `"}`)
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

// TestRegisterCommandsRibbonButtonPlusHeadlessGenerate pins the command surface: exactly ONE
// ribbon button — the "Motor Designer" show button, carrying an SVG glyph and a large-icon
// style — plus the two HEADLESS generation commands (no icon, no button style, so they stay off
// the ribbon) that let a script/MCP driver generate via execute_command.
func TestRegisterCommandsRibbonButtonPlusHeadlessGenerate(t *testing.T) {
	h := &fakeHost{}
	e := NewEngine(h)
	if err := e.RegisterCommands(); err != nil {
		t.Fatalf("RegisterCommands: %v", err)
	}
	byID := map[string]wire.CreateCommandArgs{}
	for _, c := range h.commands {
		byID[c.ID] = c
	}
	show, ok := byID[ShowCommandID]
	if !ok || show.DisplayName != "Motor Designer" || show.ButtonStyle != types.LargeIconButton {
		t.Fatalf("show button missing/misconfigured: %+v", show)
	}
	if !strings.Contains(show.IconSVG, "<svg") || !strings.Contains(show.IconSVG, "viewBox=\"0 0 24 24\"") {
		t.Errorf("show button must ship a 24×24 SVG glyph, got IconSVG=%q", show.IconSVG)
	}
	// The two headless generate commands are registered for execute_command but carry no ribbon
	// styling — the ribbon stays a single button.
	for _, id := range []string{GenerateCommandID, GenerateOutrunnerCommandID} {
		gen, ok := byID[id]
		if !ok {
			t.Fatalf("headless command %q was not registered", id)
		}
		if gen.IconSVG != "" || gen.ButtonStyle == types.LargeIconButton {
			t.Errorf("headless command %q must not be a ribbon button: %+v", id, gen)
		}
	}
}

// TestNotifyHeadlessGenerateCommandTriggersGeneration pins the scriptable/MCP path: a
// command.started for MotorDesigner.Generate (no panel interaction) runs a full generation.
func TestNotifyHeadlessGenerateCommandTriggersGeneration(t *testing.T) {
	h := &fakeHost{}
	e := NewEngine(h)
	e.Notify(commandStartedEvent(GenerateCommandID))
	if !h.waitForDocs(4) {
		t.Fatalf("headless Generate command should have built the assembly; docs=%d", h.docCount())
	}
}

// TestNotifyHeadlessGenerateOutrunnerForcesTopology pins that MotorDesigner.GenerateOutrunner
// generates the outrunner topology regardless of the current spec, mirroring the dropdown.
func TestNotifyHeadlessGenerateOutrunnerForcesTopology(t *testing.T) {
	h := &fakeHost{}
	e := NewEngine(h)
	e.Notify(commandStartedEvent(GenerateOutrunnerCommandID))
	if !h.waitForDocs(4) {
		t.Fatalf("headless GenerateOutrunner did not complete; docs=%d", h.docCount())
	}
	waitForStatus(h)
	if expr, ok := h.paramExpression("slot_bottom_r"); !ok || expr != "bore_r - slot_depth" {
		t.Errorf("slot_bottom_r = %q (ok=%v), want outrunner formula \"bore_r - slot_depth\"", expr, ok)
	}
}

// commandStartedEvent builds the host's command.started event payload for a command id.
func commandStartedEvent(id string) []byte {
	return []byte(`{"type":"` + wire.EventCommandStarted + `","command":"` + id + `"}`)
}

// TestNotifyGenerateRespectsTypeDropdown is the regression for the reported bug: choosing
// "outrunner" in the panel's type dropdown must be honoured by Generate. It drives the dropdown
// edit then the Generate action and checks the published program carries the OUTRUNNER slot-bottom
// formula (slot bottoms inside the bore), which only the outrunner layout emits.
func TestNotifyGenerateRespectsTypeDropdown(t *testing.T) {
	h := &fakeHost{}
	e := NewEngine(h)
	e.Notify(typeDropdownEvent(Outrunner)) // pick outrunner in the form
	e.Notify(generatePanelEvent())         // then Generate
	if !h.waitForDocs(4) {
		t.Fatalf("generation did not complete; docs=%d", h.docCount())
	}
	waitForStatus(h) // generation goroutine has finished once it reports its outcome
	expr, ok := h.paramExpression("slot_bottom_r")
	if !ok {
		t.Fatal("slot_bottom_r was never published; cannot confirm the motor type was respected")
	}
	if expr != "bore_r - slot_depth" {
		t.Errorf("slot_bottom_r = %q, want outrunner formula \"bore_r - slot_depth\" — type dropdown ignored", expr)
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
	e.Notify(generatePanelEvent()) // command-less Generate button → panel action → generation on its own goroutine
	if !h.waitForDocs(1) {
		t.Errorf("Generate command should have created a document; docs=%d", h.docCount())
	}
}

func TestNotifySurfacesGenerateSuccessOnStatusBar(t *testing.T) {
	h := &fakeHost{}
	e := NewEngine(h)
	e.Notify(generatePanelEvent())
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
	e.Notify(generatePanelEvent())
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

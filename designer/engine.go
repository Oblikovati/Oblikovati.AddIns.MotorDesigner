// SPDX-License-Identifier: GPL-2.0-only

package designer

import (
	"encoding/json"
	"fmt"
	"sync"

	"oblikovati.org/api/client"
	"oblikovati.org/api/types"
	"oblikovati.org/api/wire"
)

// HostCaller is the transport the engine talks to the host through — exactly the
// api/client Caller contract, supplied by the cgo shell at Activate (or a fake in tests).
// Keeping it an interface here keeps this package cgo-free and testable.
type HostCaller interface {
	Call(method string, req []byte) ([]byte, error)
}

// Engine designs motor geometry against a live host: it computes a rough cross-section
// from a Spec and drives the host API to realize it as parameters + sketches + features.
// The current Spec is the panel's editable state; the Generate button (re)runs it.
type Engine struct {
	host HostCaller
	api  *client.Client

	mu         sync.Mutex
	spec       Spec
	generating bool // a Generate run is in flight (set under mu; see runGenerate)
}

// NewEngine binds the engine to the host transport, seeded with the default design.
func NewEngine(host HostCaller) *Engine {
	return &Engine{host: host, api: client.New(host), spec: DefaultSpec()}
}

// API exposes the underlying typed client (used by the dockable-window + geometry code).
func (e *Engine) API() *client.Client { return e.api }

// RegisterCommands registers the add-in's single ribbon command: a "Motor Designer" button
// (with its own SVG glyph) that opens the design window. Generation is driven from inside that
// window (the panel's Generate button), so the ribbon stays one button — not one per action.
// The command is also invocable over the MCP bridge's execute_command, which re-opens the panel.
func (e *Engine) RegisterCommands() error {
	_, err := e.api.Commands().Create(wire.CreateCommandArgs{
		ID:          ShowCommandID,
		DisplayName: "Motor Designer",
		Category:    "Motor Designer",
		Tooltip:     "Open the Motor Designer window to size and generate a motor.",
		IconSVG:     motorIconSVG,
		ButtonStyle: types.LargeIconButton,
	})
	return err
}

// Setup performs the one-time host-facing initialization: register the Generate command
// and show the design-options panel. It MUST NOT run on the host's session goroutine
// (e.g. directly inside the C-ABI Activate) — those host calls block until the frame loop
// drains the dispatcher, so calling them on the session goroutine before the loop starts
// deadlocks the head. The cgo shell runs Setup on its own goroutine, where the live frame
// loop drains the calls (mirroring how the MCP bridge serves on a goroutine). Errors are
// returned for logging; partial setup never crashes the host.
func (e *Engine) Setup() error {
	if err := e.RegisterCommands(); err != nil {
		return err
	}
	_, err := e.ShowPanel(e.Spec())
	return err
}

// Spec returns a copy of the engine's current design spec.
func (e *Engine) Spec() Spec {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.spec
}

// SetSpec replaces the engine's current design spec.
func (e *Engine) SetSpec(s Spec) {
	e.mu.Lock()
	e.spec = s
	e.mu.Unlock()
}

// Notify receives host event bytes. A command.started carrying the Generate command runs
// the geometry generation for the current spec; everything else is ignored.
//
// CRITICAL: Notify is invoked ON the host's session goroutine (events are emitted from
// inside the frame loop). A host call from this goroutine blocks until the frame loop
// drains the dispatcher — which cannot happen while we're still inside it — so doing the
// generation inline deadlocks every host call (the empty-geometry symptom). The work is
// therefore dispatched to a SEPARATE goroutine, where the live frame loop drains its host
// calls normally. A guard coalesces overlapping triggers so one run is in flight at a time.
//
// A run's outcome is surfaced on the host status bar (success or the error message), so a
// failed generation is visible to the user rather than silently producing nothing.
func (e *Engine) Notify(ev []byte) {
	var hdr struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(ev, &hdr) != nil {
		return
	}
	switch hdr.Type {
	case wire.EventCommandStarted:
		e.handleCommand(ev)
	case wire.EventPanelValueChanged:
		e.handlePanelEdit(ev)
	case wire.EventDocumentActivated:
		// Loading/activating a motor assembly repopulates the form from its stored Spec. This
		// needs host calls (read attributes + re-show the panel), which deadlock on the session
		// goroutine, so it runs on its own goroutine.
		go e.handleDocumentActivated(ev)
	}
}

// handleDocumentActivated repopulates the design form when the activated document is a motor
// assembly the add-in stamped: it reads the Spec back from the assembly's attribute set and
// re-shows the panel seeded with those values. Activations during our own generation are
// ignored (the assembly carries no Spec yet, and the guard avoids reentrancy).
func (e *Engine) handleDocumentActivated(ev []byte) {
	var a struct {
		ID uint64 `json:"id"`
	}
	if json.Unmarshal(ev, &a) != nil {
		return
	}
	e.mu.Lock()
	generating := e.generating
	e.mu.Unlock()
	if generating {
		return
	}
	spec, ok, err := e.LoadSpec(a.ID)
	if err != nil || !ok {
		return
	}
	e.SetSpec(spec)
	_, _ = e.ShowPanel(spec)
}

// handleCommand handles the add-in's single ribbon command: the "Motor Designer" button
// (re)opens the design window. ShowPanel makes host calls, which deadlock on the session
// goroutine (see Notify), so it runs on its own goroutine.
func (e *Engine) handleCommand(ev []byte) {
	var c struct {
		Command string `json:"command"`
	}
	if json.Unmarshal(ev, &c) != nil {
		return
	}
	if c.Command == ShowCommandID {
		go func() { _, _ = e.ShowPanel(e.Spec()) }()
	}
}

// handlePanelEdit handles one panel interaction. The Generate button (a command-less panel
// action) starts a generation of the current spec; every other control is a field edit written
// back into the spec. Field edits only mutate the spec (no host call), so they are safe on the
// session goroutine; runGenerate dispatches the host work to its own goroutine.
func (e *Engine) handlePanelEdit(ev []byte) {
	var p struct {
		WindowId  string `json:"windowId"`
		ControlId string `json:"controlId"`
		Value     string `json:"value"`
	}
	if json.Unmarshal(ev, &p) != nil || p.WindowId != PanelID {
		return
	}
	if p.ControlId == generateControlID {
		e.runGenerate()
		return
	}
	e.mu.Lock()
	applyControl(&e.spec, p.ControlId, p.Value)
	e.mu.Unlock()
}

// runGenerate launches a generation pass on its own goroutine (never the session goroutine —
// see Notify) for the CURRENT spec, so the motor-type dropdown (and every other edited field)
// is honoured. The generating flag coalesces overlapping triggers so at most one run is in
// flight.
func (e *Engine) runGenerate() {
	e.mu.Lock()
	if e.generating {
		e.mu.Unlock()
		return
	}
	e.generating = true
	spec := e.spec
	e.mu.Unlock()

	go func() {
		e.reportOutcome(e.Generate(spec))
		e.mu.Lock()
		e.generating = false
		e.mu.Unlock()
	}()
}

// reportOutcome surfaces a Generate run's result on the host status bar — a one-line
// summary on success, the error message on failure — so a bad design is visible rather
// than silently producing nothing. Status updates are best-effort (a status failure must
// not mask the original outcome).
func (e *Engine) reportOutcome(res *GenerateResult, err error) {
	if err != nil {
		_, _ = e.api.Status().SetText("Motor Designer: generation failed — " + err.Error())
		return
	}
	msg := fmt.Sprintf("Motor Designer: generated %d magnets, stator + rotor (%s iron, %s magnets)",
		res.MagnetBodies, res.IronMaterial, res.MagnetMatID)
	_, _ = e.api.Status().SetText(msg)
}

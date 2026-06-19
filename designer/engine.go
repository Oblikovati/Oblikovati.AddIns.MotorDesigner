// SPDX-License-Identifier: GPL-2.0-only

package designer

import (
	"encoding/json"
	"fmt"
	"sync"

	"oblikovati.org/api/client"
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

// RegisterCommands registers the add-in's ribbon command(s) with the host so they can be
// invoked the same way a ribbon click is — including over the MCP bridge's
// execute_command. The host action is a no-op; executing the command fires the
// command.started event the engine's Notify turns into a Generate run. This is what makes
// the add-in drivable headlessly (no panel click needed).
func (e *Engine) RegisterCommands() error {
	_, err := e.api.Commands().Create(wire.CreateCommandArgs{
		ID:          GenerateCommandID,
		DisplayName: "Generate Motor",
		Category:    "Motor Designer",
		Tooltip:     "Generate the rough motor cross-section from the current design.",
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
		Type    string `json:"type"`
		Command string `json:"command"`
	}
	if json.Unmarshal(ev, &hdr) != nil {
		return
	}
	if hdr.Type == wire.EventCommandStarted && hdr.Command == GenerateCommandID {
		e.runGenerate()
	}
}

// runGenerate launches a generation pass on its own goroutine (never the session
// goroutine — see Notify). The generating flag coalesces overlapping command triggers so
// at most one run is in flight.
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

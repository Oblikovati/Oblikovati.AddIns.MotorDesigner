// SPDX-License-Identifier: GPL-2.0-only

package designer

import (
	"encoding/json"
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

	mu   sync.Mutex
	spec Spec
}

// NewEngine binds the engine to the host transport, seeded with the default design.
func NewEngine(host HostCaller) *Engine {
	return &Engine{host: host, api: client.New(host), spec: DefaultSpec()}
}

// API exposes the underlying typed client (used by the dockable-window + geometry code).
func (e *Engine) API() *client.Client { return e.api }

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

// Notify receives host event bytes. A command.started carrying the panel's Generate
// command runs the geometry generation for the current spec; everything else is ignored.
// Errors are swallowed (an add-in must never crash the host on a bad event); the panel is
// the user-facing surface for surfacing them in a later phase.
func (e *Engine) Notify(ev []byte) {
	var hdr struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}
	if json.Unmarshal(ev, &hdr) != nil {
		return
	}
	if hdr.Type == wire.EventCommandStarted && hdr.Command == GenerateCommandID {
		_, _ = e.Generate(e.Spec())
	}
}

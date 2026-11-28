//go:build js && wasm

// SPDX-License-Identifier: GPL-2.0-only

package main

import (
	"sync"

	"oblikovati.org/api/client"
	"oblikovati.org/api/wasmaddin"
	"oblikovati.org/motor-designer/designer"
)

const addInID = "com.oblikovati.motor-designer"

type wasmMotorDesigner struct {
	mu     sync.Mutex
	engine *designer.Engine
}

func (a *wasmMotorDesigner) ID() string       { return addInID }
func (a *wasmMotorDesigner) Manifest() string { return manifestJSON }

func (a *wasmMotorDesigner) Activate(caller client.Caller) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	eng := designer.NewEngine(caller)
	a.engine = eng
	go func() { _ = eng.Setup() }()
	return nil
}

func (a *wasmMotorDesigner) Deactivate() error {
	a.mu.Lock()
	a.engine = nil
	a.mu.Unlock()
	return nil
}

func (a *wasmMotorDesigner) Notify(event []byte) {
	a.mu.Lock()
	eng := a.engine
	a.mu.Unlock()
	if eng != nil {
		eng.Notify(event)
	}
}

func main() {
	wasmaddin.Serve(&wasmMotorDesigner{})
}

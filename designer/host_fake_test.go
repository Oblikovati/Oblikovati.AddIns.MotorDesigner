// SPDX-License-Identifier: GPL-2.0-only

package designer

import (
	"encoding/json"
	"sync"
	"time"

	"oblikovati.org/api/wire"
)

// fakeHost is a named fake HostCaller (no live host): it answers the wire methods an
// assembly-based design-generation run issues with canned JSON and records the methods,
// documents, sketch entities, materials and occurrences it saw, so a test can assert the
// full multi-document → parameters → sketch → feature → material → assemble call sequence.
// It is the single mock for this package's host I/O (no inline stubs), mutex-guarded
// because Notify dispatches generation onto its own goroutine.
type fakeHost struct {
	mu         sync.Mutex
	calls      []string                   // every method name, in order
	entities   []wire.AddSketchEntityArgs // sketch.addEntity requests, decoded
	params     []wire.ParameterSetArgs    // parameters.add/set requests, decoded
	assigned   []string                   // material ids assigned (model.assignMaterial)
	placed     []string                   // occurrence names placed (assembly.place)
	statusText []string                   // status.setText messages, in order
	docTypes   []string                   // every created document's type, in order
	features   int                        // features.add calls
	failOn     string                     // method to fail (error-path tests); "" = none
	existing   []wire.ParameterInfo       // parameters.list reply
	nextDoc    uint64                     // id stamped on the next documents.create reply
	featSeq    uint64                     // running feature-id sequence (model.tree)
	sketchByID map[uint64]int             // sketch count per active doc (for sketch indices)
	activeDoc  uint64                     // currently active document id
}

func (h *fakeHost) Call(method string, req []byte) ([]byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, method)
	if method == h.failOn {
		return nil, errFake
	}
	return h.dispatch(method, req)
}

// dispatch routes one wire method to its canned reply (split out to keep Call short).
func (h *fakeHost) dispatch(method string, req []byte) ([]byte, error) {
	switch method {
	case wire.MethodDocumentsCreate:
		return h.createDoc(req)
	case wire.MethodDocumentsActivate:
		return h.activateDoc(req)
	case wire.MethodDocumentsList:
		return json.Marshal(wire.ListDocumentsResult{
			Documents: []wire.DocumentInfo{{ID: h.activeDoc, Type: "part", Active: true}},
		})
	case wire.MethodParametersList:
		return json.Marshal(wire.ListParametersResult{Parameters: h.existing})
	case wire.MethodParametersAdd, wire.MethodParametersSet:
		return h.recordParam(req)
	case wire.MethodSketchCreate:
		return h.createSketch()
	case wire.MethodSketchAddEntity:
		return h.recordEntity(req)
	case wire.MethodFeaturesAdd:
		return h.addFeature()
	case wire.MethodModelTree:
		return json.Marshal(wire.ModelTreeResult{Features: []wire.FeatureInfo{{ID: h.featSeq, Name: "Extrusion"}}})
	case wire.MethodFeaturesRename:
		return []byte("{}"), nil
	case wire.MethodModelAssignMaterial:
		return h.recordAssign(req)
	case wire.MethodAssemblyPlace:
		return h.recordPlace(req)
	case wire.MethodStatusSetText:
		return h.recordStatus(req)
	default:
		return []byte("{}"), nil // dockableWindows.set etc. return no body the engine reads
	}
}

func (h *fakeHost) createDoc(req []byte) ([]byte, error) {
	var a wire.CreateDocumentArgs
	if err := json.Unmarshal(req, &a); err != nil {
		return nil, err
	}
	h.nextDoc++
	h.activeDoc = h.nextDoc
	h.docTypes = append(h.docTypes, a.Type)
	return json.Marshal(wire.DocumentInfo{ID: h.nextDoc, Type: a.Type, Active: true})
}

func (h *fakeHost) activateDoc(req []byte) ([]byte, error) {
	var a wire.ActivateDocumentArgs
	if err := json.Unmarshal(req, &a); err != nil {
		return nil, err
	}
	h.activeDoc = a.ID
	return json.Marshal(wire.OKResult{OK: true})
}

func (h *fakeHost) createSketch() ([]byte, error) {
	if h.sketchByID == nil {
		h.sketchByID = map[uint64]int{}
	}
	idx := h.sketchByID[h.activeDoc]
	h.sketchByID[h.activeDoc] = idx + 1
	return json.Marshal(wire.CreateSketchResult{SketchIndex: idx, Plane: "XY"})
}

func (h *fakeHost) addFeature() ([]byte, error) {
	h.features++
	h.featSeq++
	// Mirror the host's real extrude reply (no numeric feature id; bodies + healthy).
	return json.Marshal(extrudeResult{Feature: "Extrusion", Bodies: 1, Healthy: true})
}

func (h *fakeHost) recordParam(req []byte) ([]byte, error) {
	var a wire.ParameterSetArgs
	if err := json.Unmarshal(req, &a); err != nil {
		return nil, err
	}
	h.params = append(h.params, a)
	return json.Marshal(wire.ParameterInfo{Name: a.Name, Expression: a.Expression})
}

func (h *fakeHost) recordEntity(req []byte) ([]byte, error) {
	var a wire.AddSketchEntityArgs
	if err := json.Unmarshal(req, &a); err != nil {
		return nil, err
	}
	h.entities = append(h.entities, a)
	return json.Marshal(wire.AddSketchEntityResult{})
}

func (h *fakeHost) recordAssign(req []byte) ([]byte, error) {
	var a wire.AssignMaterialArgs
	if err := json.Unmarshal(req, &a); err != nil {
		return nil, err
	}
	h.assigned = append(h.assigned, a.MaterialID)
	return json.Marshal(wire.OKResult{OK: true})
}

func (h *fakeHost) recordPlace(req []byte) ([]byte, error) {
	var a wire.PlaceOccurrenceArgs
	if err := json.Unmarshal(req, &a); err != nil {
		return nil, err
	}
	h.placed = append(h.placed, a.Name)
	return json.Marshal(wire.OccurrenceResult{})
}

func (h *fakeHost) recordStatus(req []byte) ([]byte, error) {
	var a wire.SetStatusTextArgs
	if err := json.Unmarshal(req, &a); err != nil {
		return nil, err
	}
	h.statusText = append(h.statusText, a.Text)
	return json.Marshal(wire.OKResult{OK: true})
}

// lastStatus returns the most recent status.setText message under the lock ("" if none).
func (h *fakeHost) lastStatus() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.statusText) == 0 {
		return ""
	}
	return h.statusText[len(h.statusText)-1]
}

// docTypeCount returns how many documents of a given type were created, under the lock.
func (h *fakeHost) docTypeCount(t string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, dt := range h.docTypes {
		if dt == t {
			n++
		}
	}
	return n
}

// docCount returns the total documents.create count under the lock.
func (h *fakeHost) docCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.docTypes)
}

// callCount returns how many host calls were made under the lock.
func (h *fakeHost) callCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.calls)
}

// waitForDocs spins (up to ~2s) until at least n documents have been created — used to join
// the async generation goroutine Notify spawns.
func (h *fakeHost) waitForDocs(n int) bool {
	for i := 0; i < 200; i++ {
		if h.docCount() >= n {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// errFake is the canned failure the fake returns for failOn.
var errFake = fakeError("fake host: forced failure")

type fakeError string

func (e fakeError) Error() string { return string(e) }

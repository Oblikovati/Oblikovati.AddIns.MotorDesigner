// SPDX-License-Identifier: GPL-2.0-only

package designer

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"oblikovati.org/api/types"
	"oblikovati.org/api/wire"
)

// fakeHost is a named fake HostCaller (no live host): it answers the wire methods an
// assembly-based design-generation run issues with canned JSON and records the methods,
// documents, sketch entities, materials and occurrences it saw, so a test can assert the
// full multi-document → parameters → sketch → feature → material → assemble call sequence.
// It is the single mock for this package's host I/O (no inline stubs), mutex-guarded
// because Notify dispatches generation onto its own goroutine.
type fakeHost struct {
	mu          sync.Mutex
	calls       []string                   // every method name, in order
	entities    []wire.AddSketchEntityArgs // sketch.addEntity requests, decoded
	constraints []wire.AddConstraintArgs   // sketch.addConstraint requests, decoded
	dimensions  []wire.AddDimensionArgs    // sketch.addDimension requests, decoded
	featureArgs []wire.AddFeatureArgs      // features.add requests, decoded (kind + raw args)
	commands    []wire.CreateCommandArgs   // commands.create requests, decoded (id + icon + style)
	entitySeq   uint64                     // running sketch entity-id sequence (AddSketchEntityResult.EntityID)
	pointSeq    uint64                     // running sketch point-id sequence (AddSketchEntityResult.PointIDs)
	params      []wire.ParameterSetArgs    // parameters.add/set requests, decoded
	derived     []derivedLink              // parameters.derivedTables.add requests (which doc linked what)
	assigned    []string                   // material ids assigned (model.assignMaterial)
	placed      []string                   // occurrence names placed (assembly.place)
	statusText  []string                   // status.setText messages, in order
	attrs       map[string]string          // "<doc>/<set>/<name>" -> string value (attributes.set)
	docTypes    []string                   // every created document's type, in order
	features    int                        // features.add calls
	failOn      string                     // method to fail (error-path tests); "" = none
	existing    []wire.ParameterInfo       // parameters.list reply
	nextDoc     uint64                     // id stamped on the next documents.create reply
	featSeq     uint64                     // running feature-id sequence (model.tree)
	sketchByID  map[uint64]int             // sketch count per active doc (for sketch indices)
	bodiesByDoc map[uint64]int             // cumulative body count per doc (mirrors the host's extrude reply)
	activeDoc   uint64                     // currently active document id
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
	case wire.MethodParametersDerivedTablesAdd:
		return h.recordDerivedTable(req)
	case wire.MethodSketchCreate:
		return h.createSketch()
	case wire.MethodSketchAddEntity:
		return h.recordEntity(req)
	case wire.MethodSketchAddConstraint:
		return h.recordConstraint(req)
	case wire.MethodSketchAddDimension:
		return h.recordDimension(req)
	case wire.MethodFeaturesAdd:
		return h.addFeature(req)
	case wire.MethodModelTree:
		return json.Marshal(wire.ModelTreeResult{Features: []wire.FeatureInfo{{ID: h.featSeq, Name: "Extrusion"}}})
	case wire.MethodFeaturesRename:
		return []byte("{}"), nil
	case wire.MethodModelAssignMaterial:
		return h.recordAssign(req)
	case wire.MethodAssemblyPlace:
		return h.recordPlace(req)
	case wire.MethodCommandsCreate:
		return h.recordCommand(req)
	case wire.MethodStatusSetText:
		return h.recordStatus(req)
	case wire.MethodAttributesSet:
		return h.setAttr(req)
	case wire.MethodAttributesGet:
		return h.getAttr(req)
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

func (h *fakeHost) addFeature(req []byte) ([]byte, error) {
	var a wire.AddFeatureArgs
	if err := json.Unmarshal(req, &a); err != nil {
		return nil, err
	}
	h.featureArgs = append(h.featureArgs, a)
	h.features++
	h.featSeq++
	// Mirror the host's real extrude reply: Bodies is the part's TOTAL body count after the
	// feature (len(SurfaceBodies)), NOT the one body just added — so a caller that sums these
	// over-counts (the magnet 1+2+…+N bug). Track a cumulative count per active document.
	if h.bodiesByDoc == nil {
		h.bodiesByDoc = map[uint64]int{}
	}
	h.bodiesByDoc[h.activeDoc]++
	return json.Marshal(extrudeResult{Feature: "Extrusion", Bodies: h.bodiesByDoc[h.activeDoc], Healthy: true})
}

// recordConstraint logs a geometric-constraint request and reports a fully-solved DOF (the
// fake has no solver; DOF=0 is verified live, not here).
func (h *fakeHost) recordConstraint(req []byte) ([]byte, error) {
	var a wire.AddConstraintArgs
	if err := json.Unmarshal(req, &a); err != nil {
		return nil, err
	}
	h.constraints = append(h.constraints, a)
	return json.Marshal(wire.AddConstraintResult{Kind: a.Kind, DOF: 0})
}

// recordDimension logs a dimensional-constraint request, echoing the expression as the
// backing parameter name so a caller reading the reply sees a non-empty parameter.
func (h *fakeHost) recordDimension(req []byte) ([]byte, error) {
	var a wire.AddDimensionArgs
	if err := json.Unmarshal(req, &a); err != nil {
		return nil, err
	}
	h.dimensions = append(h.dimensions, a)
	return json.Marshal(wire.AddDimensionResult{Kind: a.Kind, Parameter: a.Expression, DOF: 0})
}

// derivedLink records one parameters.derivedTables.add: the document that linked (the active
// doc at the time), the source it linked from, and the parameter names linked — so a test can
// assert each part derives exactly its consumed subset from the Motor assembly.
type derivedLink struct {
	doc    uint64
	source string
	linked []string
}

// recordDerivedTable logs a derived-parameter-table link request against the active document
// and echoes the linked subset back as the created table (mirroring the host's reply shape).
func (h *fakeHost) recordDerivedTable(req []byte) ([]byte, error) {
	var a wire.DerivedParameterTableAddArgs
	if err := json.Unmarshal(req, &a); err != nil {
		return nil, err
	}
	h.derived = append(h.derived, derivedLink{doc: h.activeDoc, source: a.SourceDocument, linked: a.Linked})
	return json.Marshal(wire.DerivedParameterTableInfo{ID: len(h.derived), SourceDocument: a.SourceDocument, Linked: a.Linked})
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
	// Hand back a unique entity id and one point id per defining point (literal or expression)
	// so the geometry code can reference a circle/arc for a dimension and its centre/endpoints
	// for a constraint — mirroring the host's AddSketchEntityResult shape.
	h.entitySeq++
	n := len(a.Points)
	if len(a.PointExprs) > n {
		n = len(a.PointExprs)
	}
	pts := make([]uint64, n)
	for i := range pts {
		h.pointSeq++
		pts[i] = h.pointSeq
	}
	return json.Marshal(wire.AddSketchEntityResult{EntityID: h.entitySeq, Kind: a.Kind, PointIDs: pts})
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

func (h *fakeHost) setAttr(req []byte) ([]byte, error) {
	var a wire.SetAttributeArgs
	if err := json.Unmarshal(req, &a); err != nil {
		return nil, err
	}
	if h.attrs == nil {
		h.attrs = map[string]string{}
	}
	val, _ := a.Value.Str()
	h.attrs[attrKey(a.Document, a.Set, a.Name)] = val
	return json.Marshal(wire.AttributeResult{Attribute: wire.AttributeInfo{Set: a.Set, Name: a.Name, Value: a.Value}, Found: true})
}

func (h *fakeHost) getAttr(req []byte) ([]byte, error) {
	var a wire.GetAttributeArgs
	if err := json.Unmarshal(req, &a); err != nil {
		return nil, err
	}
	val, ok := h.attrs[attrKey(a.Document, a.Set, a.Name)]
	if !ok {
		// Mirror the router: an absent attribute still serializes a valid (empty-string)
		// variant so the typed client decodes the reply; Found=false is the signal.
		empty := wire.AttributeInfo{Value: types.StringVariant("")}
		return json.Marshal(wire.AttributeResult{Attribute: empty, Found: false})
	}
	info := wire.AttributeInfo{Set: a.Set, Name: a.Name, Value: types.StringVariant(val)}
	return json.Marshal(wire.AttributeResult{Attribute: info, Found: true})
}

func attrKey(doc uint64, set, name string) string {
	return fmt.Sprintf("%d/%s/%s", doc, set, name)
}

func (h *fakeHost) recordCommand(req []byte) ([]byte, error) {
	var a wire.CreateCommandArgs
	if err := json.Unmarshal(req, &a); err != nil {
		return nil, err
	}
	h.commands = append(h.commands, a)
	return json.Marshal(wire.CommandInfo{ID: a.ID})
}

func (h *fakeHost) recordStatus(req []byte) ([]byte, error) {
	var a wire.SetStatusTextArgs
	if err := json.Unmarshal(req, &a); err != nil {
		return nil, err
	}
	h.statusText = append(h.statusText, a.Text)
	return json.Marshal(wire.OKResult{OK: true})
}

// paramExpression returns the published expression for a parameter name (under the lock), and
// whether it was published — used by tests that read params after async generation.
func (h *fakeHost) paramExpression(name string) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, p := range h.params {
		if p.Name == name {
			return p.Expression, true
		}
	}
	return "", false
}

// linkedFrom returns the parameter names a given document linked, and the source it linked them
// from, under the lock — collapsing a document's derived-table adds (the motor parts each link
// once). ok=false means the document linked nothing.
func (h *fakeHost) linkedFrom(doc uint64) (names []string, source string, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, l := range h.derived {
		if l.doc == doc {
			names = append(names, l.linked...)
			source = l.source
			ok = true
		}
	}
	return names, source, ok
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

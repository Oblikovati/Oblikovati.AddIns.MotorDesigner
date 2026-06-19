// SPDX-License-Identifier: GPL-2.0-only

package designer

import (
	"encoding/json"

	"oblikovati.org/api/wire"
)

// fakeHost is a named fake HostCaller (no live host): it answers the wire methods a
// design-generation run issues with canned JSON and records the methods + sketch-entity
// requests it saw, so a test can assert the full document→parameters→sketch→feature call
// sequence ran. It is the single mock for this package's host I/O (no inline stubs).
type fakeHost struct {
	calls    []string                   // every method name, in order
	entities []wire.AddSketchEntityArgs // sketch.addEntity requests, decoded
	params   []wire.ParameterSetArgs    // parameters.add/set requests, decoded
	docs     int                        // documents.create calls
	sketches int                        // sketch.create calls
	features int                        // features.add calls
	failOn   string                     // method to fail (error-path tests); "" = none
	existing []wire.ParameterInfo       // parameters.list reply
	nextDoc  uint64                     // id stamped on the next documents.create reply
	nextFeat uint64                     // id stamped on the next features.add reply
}

func (h *fakeHost) Call(method string, req []byte) ([]byte, error) {
	h.calls = append(h.calls, method)
	if method == h.failOn {
		return nil, errFake
	}
	switch method {
	case wire.MethodDocumentsCreate:
		h.docs++
		h.nextDoc++
		return json.Marshal(wire.DocumentInfo{ID: h.nextDoc, Type: "part", Active: true})
	case wire.MethodParametersList:
		return json.Marshal(wire.ListParametersResult{Parameters: h.existing})
	case wire.MethodParametersAdd, wire.MethodParametersSet:
		return h.recordParam(req)
	case wire.MethodSketchCreate:
		h.sketches++
		return json.Marshal(wire.CreateSketchResult{SketchIndex: h.sketches - 1, Plane: "XY"})
	case wire.MethodSketchAddEntity:
		return h.recordEntity(req)
	case wire.MethodFeaturesAdd:
		h.features++
		h.nextFeat++
		return json.Marshal(struct {
			FeatureID uint64 `json:"featureId"`
		}{FeatureID: h.nextFeat})
	default:
		return []byte("{}"), nil // dockableWindows.set etc. return no body the engine reads
	}
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

// errFake is the canned failure the fake returns for failOn.
var errFake = fakeError("fake host: forced failure")

type fakeError string

func (e fakeError) Error() string { return string(e) }

//go:build integration

// SPDX-License-Identifier: GPL-2.0-only

// These designer<->host integration tests import the GPL host (oblikovati.org/app, addin/router)
// to drive the REAL router + solver + boolean, so they are gated behind the `integration` build
// tag: the normal `go test ./...` run skips them (no host import, no test-scope host require), and
// they run locally via `go test -tags integration ./designer/...` with the go.work-resolved host.
// Wiring them into CI needs the host's transitive deps in go.sum (the siblings action injects the
// replace, not a require) — tracked as a separate infrastructure task.

package designer

import (
	"testing"

	"oblikovati.org/addin/opregistry"
	"oblikovati.org/addin/router"
	"oblikovati.org/api/client"
	"oblikovati.org/app"
)

// liveHost drives the REAL host router + sketch solver in-process (not the fake), so these
// tests exercise the actual constraint solve and boolean extrude/pattern — the only way to
// prove the slot-type tooth profiles solve to DOF=0 and fuse to a single, mesh-able shell.
type liveHost struct {
	r *router.Router
	s *app.Session
}

func (h *liveHost) Call(method string, req []byte) ([]byte, error) {
	return h.r.Handle(h.s, method, req)
}

// newLiveHost builds a fresh in-process host (one session) for one generation run.
func newLiveHost() *liveHost {
	return &liveHost{r: router.New(opregistry.Default()), s: app.NewSession()}
}

// NOTE on outrunner live coverage: the outrunner boolean tooth-join is pathologically slow at
// the counts where its small yoke ring keeps the teeth valid (~50s/gen at 9–12 slots), and
// degenerate at the coarse counts that would be fast — so there is no cheap valid outrunner
// fixture. The outrunner-specific invariant (the tooth root reaches stator_yoke_r so the teeth
// overlap+fuse) is pinned by the fast fake-host TestOutrunnerToothRootReachesYoke, and was
// validated live across all six slot×motor configurations during development. The slot PROFILES'
// constraint solves are motor-type-independent, so the inrunner live matrix below covers them.
// The outrunner boolean perf is tracked separately (host kernel issue).

// generateLive runs a full generation of the spec against the real host and returns a client
// for reading back the model, failing the test on any generation error.
func generateLive(t *testing.T, spec Spec) *client.Client {
	t.Helper()
	host := newLiveHost()
	if _, err := NewEngine(host).Generate(spec); err != nil {
		t.Fatalf("Generate(%v/%v): %v", spec.Type, spec.SlotType, err)
	}
	return client.New(host)
}

// TestSlotProfilesSolveDOF0AndFuse is the simulation-grade regression for the 3D slot-type tooth
// profiles: for every slot type, every generated sketch must solve to DOF=0 (fully constrained /
// parametric) and the stator must fuse to exactly ONE shell (yoke + teeth joined — a disconnected
// body cannot be meshed or carry load/heat). This is the invariant that makes the motor consumable
// by ANY multiphysics simulation, not just the FEMM 2D cross-section. Run on the (fast) inrunner
// at the realistic default counts; the outrunner radial arrangement is smoke-tested separately.
func TestSlotProfilesSolveDOF0AndFuse(t *testing.T) {
	for _, st := range []SlotType{SlotOpenRectangular, SlotParallelTooth, SlotRoundBottom} {
		t.Run(string(st), func(t *testing.T) {
			spec := DefaultSpec()
			spec.SlotType = st
			c := generateLive(t, spec)
			assertEveryPartFullyConstrained(t, c)
			if shells := statorShells(t, c); shells != 1 {
				t.Errorf("stator fused to %d shells, want 1 (teeth must join the yoke)", shells)
			}
		})
	}
}

// TestSlotTypeChangesToothGeometry proves the slot type actually changes the 3D stator geometry
// (not only the FEMM cross-section): a shoe profile (parallel-tooth) adds the overhang faces an
// open-rectangular tooth lacks, so its fused stator has strictly more faces.
func TestSlotTypeChangesToothGeometry(t *testing.T) {
	openSpec, shoeSpec := DefaultSpec(), DefaultSpec()
	openSpec.SlotType, shoeSpec.SlotType = SlotOpenRectangular, SlotParallelTooth
	open := statorFaces(t, generateLive(t, openSpec))
	shoe := statorFaces(t, generateLive(t, shoeSpec))
	if shoe <= open {
		t.Errorf("parallel-tooth stator faces %d not > open-rect %d — the shoe must add geometry", shoe, open)
	}
}

// assertEveryPartFullyConstrained activates each part and asserts all its sketches are DOF=0.
func assertEveryPartFullyConstrained(t *testing.T, c *client.Client) {
	t.Helper()
	docs, err := c.Documents().List()
	if err != nil {
		t.Fatalf("documents.list: %v", err)
	}
	for _, d := range docs.Documents {
		if d.Type != "part" {
			continue
		}
		if _, err := c.Documents().Activate(d.ID); err != nil {
			t.Fatalf("activate %s: %v", d.Name, err)
		}
		sks, err := c.Sketch().List()
		if err != nil {
			t.Fatalf("%s sketch.list: %v", d.Name, err)
		}
		for _, sk := range sks.Sketches {
			if sk.DOF != 0 {
				t.Errorf("%s sketch %q DOF=%d, want 0 (under-constrained / not parametric)", d.Name, sk.Name, sk.DOF)
			}
		}
	}
}

// statorShells activates the Stator and returns its first body's shell count.
func statorShells(t *testing.T, c *client.Client) int {
	t.Helper()
	return statorFirstBody(t, c).Shells
}

// statorFaces activates the Stator and returns its first body's face count.
func statorFaces(t *testing.T, c *client.Client) int {
	t.Helper()
	return statorFirstBody(t, c).Faces
}

// statorFirstBody activates the Stator part and returns its first body summary.
func statorFirstBody(t *testing.T, c *client.Client) (info struct {
	Shells, Faces int
}) {
	t.Helper()
	docs, err := c.Documents().List()
	if err != nil {
		t.Fatalf("documents.list: %v", err)
	}
	for _, d := range docs.Documents {
		if d.Name != "Stator" {
			continue
		}
		if _, err := c.Documents().Activate(d.ID); err != nil {
			t.Fatalf("activate Stator: %v", err)
		}
		bodies, err := c.Body().List()
		if err != nil || len(bodies.Bodies) == 0 {
			t.Fatalf("stator body.list: %v (bodies=%d)", err, len(bodies.Bodies))
		}
		info.Shells, info.Faces = bodies.Bodies[0].Shells, bodies.Bodies[0].Faces
		return info
	}
	t.Fatal("no Stator document")
	return info
}

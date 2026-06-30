// SPDX-License-Identifier: GPL-2.0-only

package designer

import (
	"encoding/json"
	"testing"

	"oblikovati.org/api/wire"
)

// generateDefault runs a full inrunner generation against a fresh fake and returns it for
// inspection of the recorded entities / constraints / dimensions / feature args.
func generateDefault(t *testing.T) *fakeHost {
	t.Helper()
	h := &fakeHost{}
	if _, err := NewEngine(h).Generate(DefaultSpec()); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return h
}

// The headline fix: arcs are real sketch arcs, never faceted line segments. No profile may
// be laid down as a polyline (the old segmented-arc representation).
func TestGenerateEmitsNoPolylines(t *testing.T) {
	h := generateDefault(t)
	for _, e := range h.entities {
		if e.Kind == "polyline" {
			t.Fatalf("found a polyline entity %+v — arcs must be real arcs, not segmented lines", e)
		}
	}
}

// The toothed stator bore is one tooth built from real arcs, replicated by a circular
// pattern whose count tracks the slots parameter (so editing slots re-patterns).
func TestGenerateStatorToothIsArcsWithCircularPattern(t *testing.T) {
	h := generateDefault(t)
	if !hasEntityKind(h, "arc") {
		t.Errorf("no arc entities emitted; tooth/magnet must use real arcs")
	}
	if !hasEntityKind(h, "circle") {
		t.Errorf("no circle entities emitted; yoke/rotor must use circles")
	}
	if expr := circularPatternCountExpr(t, h, 0); expr != "slots" {
		t.Errorf("first circular pattern countExpr = %q, want \"slots\" (stator teeth)", expr)
	}
}

// The magnets are one sector replicated by a circular pattern whose count tracks poles.
func TestGenerateMagnetSectorIsCircularPatternedByPoles(t *testing.T) {
	h := generateDefault(t)
	if expr := circularPatternCountExpr(t, h, 1); expr != "poles" {
		t.Errorf("second circular pattern countExpr = %q, want \"poles\" (magnets)", expr)
	}
}

// Every arc radius and the magnet pole-arc span are DRIVEN by a parameter expression, so a
// parameter edit recomputes the geometry (driving dimensions, not one-shot literals).
func TestGenerateDrivesArcRadiiAndSpanFromParameters(t *testing.T) {
	h := generateDefault(t)
	dimExprs := map[string]bool{}
	for _, d := range h.dimensions {
		dimExprs[d.Expression] = true
	}
	// The default (parallel-tooth) stator tooth is driven by the tip/root radii, the constant
	// tooth width, the shoe neck radius and the tip-chord span; the magnet by its radii + arc.
	for _, want := range []string{"tooth_tip_r", "slot_bottom_r", "tooth_width", "neck_r", "tip_chord",
		"magnet_tip_r", "magnet_back_r", "magnet_arc_deg"} {
		if !dimExprs[want] {
			t.Errorf("no driving dimension references %q; got %v", want, dimExprs)
		}
	}
}

func hasEntityKind(h *fakeHost, kind string) bool {
	for _, e := range h.entities {
		if e.Kind == kind {
			return true
		}
	}
	return false
}

// circularPatternCountExpr returns the countExpr of the nth (0-based) circular-pattern
// feature recorded, failing if fewer were emitted.
func circularPatternCountExpr(t *testing.T, h *fakeHost, n int) string {
	t.Helper()
	seen := 0
	for _, f := range h.featureArgs {
		if f.Kind != wire.FeatureKindPatternCircular {
			continue
		}
		if seen == n {
			var args wire.CircularPatternFeatureArgs
			if err := json.Unmarshal(f.Args, &args); err != nil {
				t.Fatalf("decode circular pattern args: %v", err)
			}
			return args.CountExpr
		}
		seen++
	}
	t.Fatalf("fewer than %d circular-pattern features (saw %d)", n+1, seen)
	return ""
}

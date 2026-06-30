// SPDX-License-Identifier: GPL-2.0-only

package designer

import "testing"

// layoutFor computes the role-based layout for a motor type from the default spec.
func layoutFor(t *testing.T, typ MotorType) (layout, *Design) {
	t.Helper()
	s := DefaultSpec()
	s.Type = typ
	d, err := Compute(s)
	if err != nil {
		t.Fatalf("Compute(%v): %v", typ, err)
	}
	return resolveLayout(d), d
}

// TestStatorYokeRadiiOuterFirst pins the yoke-annulus fix: the first (boundary) circle must be the
// geometrically LARGER radius for both motor types, else the extrude builds a solid disk at the
// smaller radius. The roles flip — an inrunner yoke is the OUTER ring (OD = stator_yoke_r), an
// outrunner yoke is the INNER ring (its OUTER edge is slot_bottom_r, where the teeth meet it).
func TestStatorYokeRadiiOuterFirst(t *testing.T) {
	in, _ := layoutFor(t, Inrunner)
	if o, i := statorYokeRadii(in); o != "stator_yoke_r" || i != "slot_bottom_r" {
		t.Errorf("inrunner yoke radii = (%q,%q), want (stator_yoke_r, slot_bottom_r)", o, i)
	}
	if in.statorYokeR <= in.slotBottomR {
		t.Errorf("inrunner yoke OD %.3f must exceed slot bottom %.3f", in.statorYokeR, in.slotBottomR)
	}
	out, _ := layoutFor(t, Outrunner)
	if o, i := statorYokeRadii(out); o != "slot_bottom_r" || i != "stator_yoke_r" {
		t.Errorf("outrunner yoke radii = (%q,%q), want (slot_bottom_r, stator_yoke_r)", o, i)
	}
	if out.slotBottomR <= out.statorYokeR {
		t.Errorf("outrunner yoke OD (slot bottom %.3f) must exceed its bore (stator yoke %.3f)", out.slotBottomR, out.statorYokeR)
	}
}

// TestOutrunnerToothRootReachesYoke pins the fusion invariant across ALL slot types: the
// outrunner tooth root must dimension against the yoke's INNER radius (stator_yoke_r) and seed
// AT that radius, so the tooth overlaps the whole yoke ring (real volume) and the boolean join
// fuses to one shell — a tangent contact did not fuse and left the teeth as disconnected shells.
func TestOutrunnerToothRootReachesYoke(t *testing.T) {
	l, _ := layoutFor(t, Outrunner)
	if got := toothRootParam(l); got != "stator_yoke_r" {
		t.Errorf("outrunner tooth root param = %q, want stator_yoke_r", got)
	}
	if got := toothRootR(l); got != l.statorYokeR {
		t.Errorf("outrunner tooth root seed = %.3f, want the yoke inner %.3f", got, l.statorYokeR)
	}
	// The tip (airgap) must be the LARGER radius for an outrunner.
	if l.toothTipR <= l.statorYokeR {
		t.Errorf("outrunner tip %.3f must exceed the yoke inner %.3f", l.toothTipR, l.statorYokeR)
	}
}

// TestInrunnerToothRootIsSlotBottom guards the inrunner tooth root: it dimensions against the
// slot bottom (the larger radius), where it meets the yoke.
func TestInrunnerToothRootIsSlotBottom(t *testing.T) {
	l, _ := layoutFor(t, Inrunner)
	if got := toothRootParam(l); got != "slot_bottom_r" {
		t.Errorf("inrunner tooth root param = %q, want slot_bottom_r", got)
	}
	if l.slotBottomR <= l.toothTipR {
		t.Errorf("inrunner slot bottom %.3f must exceed the tip %.3f", l.slotBottomR, l.toothTipR)
	}
}

// TestOpenRectToothOrdersArcsByRadius pins the winding invariant: the open-rect tooth's inner
// arc is always the smaller radius and its outer the larger, for both motor types, with the
// driving param names matching the seeds.
func TestOpenRectToothOrdersArcsByRadius(t *testing.T) {
	for _, typ := range []MotorType{Inrunner, Outrunner} {
		l, d := layoutFor(t, typ)
		s := openRectToothSpec(l, d)
		if s.rInnerSeed >= s.rOuterSeed {
			t.Errorf("%v open-rect inverted: rInner %.3f must be < rOuter %.3f", typ, s.rInnerSeed, s.rOuterSeed)
		}
		if s.widthParam != "tooth_width" {
			t.Errorf("%v open-rect width param = %q, want tooth_width", typ, s.widthParam)
		}
	}
}

// TestShoeToothNeckBetweenTipAndRoot pins the shoe neck-radius guard: the neck must sit
// strictly between the tip and the root for both motor types, so the shoe never crosses the
// root (advisor pitfall 4). It also confirms the body parameter switches with the profile.
func TestShoeToothNeckBetweenTipAndRoot(t *testing.T) {
	for _, typ := range []MotorType{Inrunner, Outrunner} {
		l, d := layoutFor(t, typ)
		for _, radial := range []bool{false, true} {
			s := shoeToothSpec(l, d, radial)
			lo, hi := minMax(s.tipSeed, s.rootSeed)
			if s.neckSeed <= lo || s.neckSeed >= hi {
				t.Errorf("%v radial=%v neck %.3f not strictly between tip %.3f and root %.3f",
					typ, radial, s.neckSeed, s.tipSeed, s.rootSeed)
			}
			want := "tooth_width"
			if radial {
				want = "tooth_angle"
			}
			if s.bodyParam != want {
				t.Errorf("%v radial=%v body param = %q, want %q", typ, radial, s.bodyParam, want)
			}
		}
	}
}

// minMax returns its two arguments in ascending order.
func minMax(a, b float64) (lo, hi float64) {
	if a <= b {
		return a, b
	}
	return b, a
}

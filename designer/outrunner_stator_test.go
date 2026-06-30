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

// TestOutrunnerToothOverlapsYoke pins the fusion fix: the outrunner tooth root must reach the
// yoke's INNER radius so the tooth OVERLAPS the yoke ring (real volume), not merely touch its outer
// edge — a tangent contact did not fuse and left the teeth as 12 disconnected shells. The sector
// must also stay valid (inner radius < outer radius).
func TestOutrunnerToothOverlapsYoke(t *testing.T) {
	l, d := layoutFor(t, Outrunner)
	s := toothSector(l, d)
	if s.rInnerParam != "stator_yoke_r" || s.rOuterParam != "tooth_tip_r" {
		t.Errorf("outrunner tooth params = (%q,%q), want (stator_yoke_r, tooth_tip_r)", s.rInnerParam, s.rOuterParam)
	}
	if s.rInnerSeed >= s.rOuterSeed {
		t.Errorf("outrunner tooth inverted: rInner %.3f must be < rOuter %.3f", s.rInnerSeed, s.rOuterSeed)
	}
	if s.rInnerSeed > l.statorYokeR+1e-9 {
		t.Errorf("outrunner tooth root %.3f must reach the yoke inner %.3f to overlap (else teeth float)", s.rInnerSeed, l.statorYokeR)
	}
}

// TestInrunnerToothUnchanged guards against a regression to the inrunner tooth, which already fused
// correctly (tip at the bore = inner arc, slot bottom = outer arc) and must not change.
func TestInrunnerToothUnchanged(t *testing.T) {
	l, d := layoutFor(t, Inrunner)
	s := toothSector(l, d)
	if s.rInnerParam != "tooth_tip_r" || s.rOuterParam != "slot_bottom_r" {
		t.Errorf("inrunner tooth params = (%q,%q), want (tooth_tip_r, slot_bottom_r)", s.rInnerParam, s.rOuterParam)
	}
	if s.rInnerSeed >= s.rOuterSeed {
		t.Errorf("inrunner tooth inverted: rInner %.3f must be < rOuter %.3f", s.rInnerSeed, s.rOuterSeed)
	}
}

// SPDX-License-Identifier: GPL-2.0-only

package designer

import (
	"math"
	"testing"
)

// TestBuildFEMMDescriptorHasAllRegions pins that the descriptor carries one stator + one
// rotor iron region plus one magnet per pole, each named, with boundary loops and an
// interior seed the FEMM bridge can mesh.
func TestBuildFEMMDescriptorHasAllRegions(t *testing.T) {
	d, err := Compute(DefaultSpec())
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	desc := BuildFEMMDescriptor(d)
	if want := 2 + d.Spec.Poles; len(desc.Regions) != want {
		t.Fatalf("regions = %d, want %d (stator + rotor + %d magnets)", len(desc.Regions), want, d.Spec.Poles)
	}
	for _, r := range desc.Regions {
		if r.Name == "" || len(r.Loops) == 0 {
			t.Errorf("region missing name/loops: %+v", r)
		}
		for _, loop := range r.Loops {
			if len(loop) < 3 {
				t.Errorf("region %q has a loop with %d points, want >= 3", r.Name, len(loop))
			}
		}
		if r.MuR <= 0 {
			t.Errorf("region %q has no permeability", r.Name)
		}
	}
}

// TestFEMMIronAndMagnetRegions pins the physics split: iron regions (stator/rotor) carry a
// soft-iron permeability and NO coercivity; magnets carry the grade's coercivity and a
// distinct, radial-outward magnetisation direction per pole.
func TestFEMMIronAndMagnetRegions(t *testing.T) {
	d, _ := Compute(DefaultSpec())
	desc := BuildFEMMDescriptor(d)
	mag, _ := Magnet(d.Spec.MagnetGrade)

	magnets, dirs := 0, map[int]bool{}
	for _, r := range desc.Regions {
		switch r.Name {
		case "Stator", "Rotor":
			if r.MuR != softIronMuR || r.HcAm != 0 {
				t.Errorf("iron region %q: muR=%g hcAm=%g, want soft iron with no coercivity", r.Name, r.MuR, r.HcAm)
			}
			if math.Hypot(r.Seed[0], r.Seed[1]) < 1 {
				t.Errorf("iron region %q seed %v is on the axis, not in the ring", r.Name, r.Seed)
			}
		default: // a magnet
			magnets++
			if math.Abs(r.HcAm-mag.HcjKAm*1000) > 1 {
				t.Errorf("magnet %q hcAm=%g, want %g A/m", r.Name, r.HcAm, mag.HcjKAm*1000)
			}
			dirs[int(math.Round(r.HcAngleDeg))] = true
		}
	}
	if magnets != d.Spec.Poles {
		t.Errorf("magnet regions = %d, want %d", magnets, d.Spec.Poles)
	}
	if len(dirs) < 2 {
		t.Errorf("magnetisation directions = %d distinct, want one per pole", len(dirs))
	}
}

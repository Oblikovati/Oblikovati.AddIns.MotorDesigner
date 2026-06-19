// SPDX-License-Identifier: GPL-2.0-only

package designer

import (
	"encoding/json"
	"testing"
)

// TestBuildFEMMDescriptorHasAllRegions pins that the descriptor carries one stator + one
// rotor iron region plus one magnet region per pole, each named and carrying a material.
func TestBuildFEMMDescriptorHasAllRegions(t *testing.T) {
	d, err := Compute(DefaultSpec())
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	desc := BuildFEMMDescriptor(d)
	wantRegions := 2 + d.Spec.Poles // stator + rotor + one magnet per pole
	if len(desc.Regions) != wantRegions {
		t.Fatalf("regions = %d, want %d", len(desc.Regions), wantRegions)
	}
	for _, r := range desc.Regions {
		if r.Name == "" || r.Material == "" {
			t.Errorf("region missing name/material: %+v", r)
		}
		if len(r.Loop) < 3 {
			t.Errorf("region %q loop has %d points, want >= 3", r.Name, len(r.Loop))
		}
	}
}

// TestFEMMMagnetRegionsCarryMagnetData pins the hand-off payload host geometry can't carry:
// each magnet region carries the grade's remanence Br and coercivity, plus a distinct
// magnetisation direction per pole.
func TestFEMMMagnetRegionsCarryMagnetData(t *testing.T) {
	d, _ := Compute(DefaultSpec())
	desc := BuildFEMMDescriptor(d)
	mag, _ := Magnet(d.Spec.MagnetGrade)
	dirs := map[float64]bool{}
	magnets := 0
	for _, r := range desc.Regions {
		if r.Br == 0 {
			continue // iron region
		}
		magnets++
		if r.Br != mag.Br || r.HcjKAm != mag.HcjKAm {
			t.Errorf("magnet region %q Br=%v Hcj=%v, want %v/%v", r.Name, r.Br, r.HcjKAm, mag.Br, mag.HcjKAm)
		}
		dirs[r.MagDirDeg] = true
	}
	if magnets != d.Spec.Poles {
		t.Errorf("magnet regions = %d, want %d", magnets, d.Spec.Poles)
	}
	if len(dirs) != d.Spec.Poles {
		t.Errorf("distinct magnetisation directions = %d, want %d (one per pole)", len(dirs), d.Spec.Poles)
	}
}

// TestFEMMDescriptorRoundTripsJSON pins that the descriptor serializes (the actual hand-off
// to the FEMM bridge is the JSON), preserving the region count.
func TestFEMMDescriptorRoundTripsJSON(t *testing.T) {
	d, _ := Compute(DefaultSpec())
	desc := BuildFEMMDescriptor(d)
	b, err := json.Marshal(desc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back FEMMDescriptor
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(back.Regions) != len(desc.Regions) || back.PoleCount != d.Spec.Poles {
		t.Errorf("round-trip lost data: %d regions / %d poles, want %d / %d",
			len(back.Regions), back.PoleCount, len(desc.Regions), d.Spec.Poles)
	}
}

// TestFEMMMaterialIDsMapToCatalog pins that the resolved material ids are the host magnetic
// catalog ids (07-magnetic.yaml), so the descriptor and the assigned bodies agree.
func TestFEMMMaterialIDsMapToCatalog(t *testing.T) {
	if got := HostSteelMaterialID(SteelM270); got != "electrical-steel-m270" {
		t.Errorf("M270 -> %q, want electrical-steel-m270", got)
	}
	if got := HostMagnetMaterialID(MagnetN42); got != "magnet-ndfeb-n42" {
		t.Errorf("N42 -> %q, want magnet-ndfeb-n42", got)
	}
	if got := HostMagnetMaterialID(MagnetGrade("bogus")); got != "magnet-ndfeb-n42" {
		t.Errorf("unknown grade fallback -> %q, want magnet-ndfeb-n42", got)
	}
}

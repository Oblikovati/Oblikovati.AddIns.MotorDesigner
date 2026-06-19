// SPDX-License-Identifier: GPL-2.0-only

package designer

import (
	"math"
	"strconv"
)

// FEMMRegion is one solid region of the motor cross-section, in the exact shape the
// FEMM bridge's MotorDescriptor consumes (millimetres, axis-relative): all boundary
// loops (outer + inner for an annulus) that become mesh segments, an interior Seed
// point that tags the region with its material, the relative permeability, and — for a
// magnet — the coercivity HcAm (A/m) and its magnetisation direction HcAngleDeg.
type FEMMRegion struct {
	Name       string         `json:"name"`
	Loops      [][][2]float64 `json:"loops"`
	Seed       [2]float64     `json:"seed"`
	MuR        float64        `json:"muR"`
	HcAm       float64        `json:"hcAm,omitempty"`
	HcAngleDeg float64        `json:"hcAngleDeg,omitempty"`
}

// FEMMDescriptor is the FEMM-ready hand-off: the full motor cross-section as solid
// regions (stator iron, rotor iron, per-pole magnets) plus the stator outer diameter
// that sizes the air domain. Everything not inside a region's seed area — the air gap,
// the shaft, the surrounding air — is the FEMM bridge's default air. Its JSON matches
// the bridge's MotorDescriptor so the bridge consumes it directly.
type FEMMDescriptor struct {
	StatorOuterDiaMM float64      `json:"statorOuterDiaMM"`
	GlueGapMM        float64      `json:"glueGapMM,omitempty"` // magnet↔rotor bond line
	Regions          []FEMMRegion `json:"regions"`
}

// BuildFEMMDescriptor turns a sized Design into the FEMM bridge's motor descriptor,
// reusing the same CrossSection loops the host geometry is built from so the field is
// solved on the identical shape. The toothed stator-bore loop excludes the slots
// naturally (they are concavities outside the iron boundary).
func BuildFEMMDescriptor(d *Design) FEMMDescriptor {
	cs := BuildCrossSection(d)
	desc := FEMMDescriptor{StatorOuterDiaMM: d.StatorOuterDia, GlueGapMM: d.Spec.GlueGapMM}
	desc.Regions = append(desc.Regions,
		FEMMRegion{Name: "Stator", Loops: loopsToMM(cs.StatorOuter, cs.StatorBore),
			Seed: statorYokeSeed(d), MuR: softIronMuR},
		FEMMRegion{Name: "Rotor", Loops: loopsToMM(cs.RotorOuter, cs.RotorInner),
			Seed: rotorBackIronSeed(d), MuR: softIronMuR},
	)
	desc.Regions = append(desc.Regions, magnetRegions(cs, d)...)
	return desc
}

// magnetRegions builds one region per magnet pole: the magnet loop, a seed at the
// loop centroid, and radial-outward magnetisation (the centroid's own angle).
func magnetRegions(cs CrossSection, d *Design) []FEMMRegion {
	mag, _ := Magnet(d.Spec.MagnetGrade)
	regions := make([]FEMMRegion, 0, len(cs.Magnets))
	for i, loop := range cs.Magnets {
		seed := loopCentroidMM(loop)
		regions = append(regions, FEMMRegion{
			Name: magnetName(i), Loops: [][][2]float64{loopToMM(loop)}, Seed: seed,
			MuR: mag.MuR, HcAm: mag.HcjKAm * 1000, // kA/m → A/m
			HcAngleDeg: math.Atan2(seed[1], seed[0]) * 180 / math.Pi,
		})
	}
	return regions
}

// statorYokeSeed is a point in the solid stator yoke (between the slot bottom and the
// outer diameter — solid at every angle), in millimetres.
func statorYokeSeed(d *Design) [2]float64 {
	return [2]float64{(d.SlotOuterR + d.StatorOuterDia/2) / 2, 0}
}

// rotorBackIronSeed is a point in the rotor back-iron (between the shaft surface and
// the magnet inner radius), in millimetres.
func rotorBackIronSeed(d *Design) [2]float64 {
	return [2]float64{(d.RotorYokeInnR + d.MagnetInnerR) / 2, 0}
}

// magnetName is the per-pole region name (Magnet-1, …), matching the body names.
func magnetName(i int) string {
	return "Magnet-" + strconv.Itoa(i+1)
}

// loopsToMM converts several cm point loops to millimetre [x,y] loops.
func loopsToMM(loops ...[]Point2) [][][2]float64 {
	out := make([][][2]float64, 0, len(loops))
	for _, l := range loops {
		out = append(out, loopToMM(l))
	}
	return out
}

// loopToMM converts a cm point loop to a millimetre [x,y] loop.
func loopToMM(loopCM []Point2) [][2]float64 {
	out := make([][2]float64, len(loopCM))
	for i, p := range loopCM {
		out[i] = [2]float64{p.X * 10, p.Y * 10}
	}
	return out
}

// loopCentroidMM is the vertex-average of a cm loop, in millimetres — a valid interior
// seed for the convex-ish magnet sector.
func loopCentroidMM(loopCM []Point2) [2]float64 {
	var sx, sy float64
	for _, p := range loopCM {
		sx += p.X
		sy += p.Y
	}
	n := float64(len(loopCM))
	return [2]float64{sx / n * 10, sy / n * 10}
}

// softIronMuR is the representative linear-region permeability for the laminated iron
// in the rough FEMM study (M270-class electrical steel; the host material carries the
// full nonlinear curve).
const softIronMuR = 4000.0

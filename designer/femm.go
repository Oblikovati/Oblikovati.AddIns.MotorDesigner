// SPDX-License-Identifier: GPL-2.0-only

package designer

import "strconv"

// FEMMRegion is one magnetostatics region of the motor cross-section: a named closed
// boundary loop (millimetres, axis-relative) plus the magnetic material the FEMM solver
// assigns to it. Magnet regions also carry the magnetisation direction (radial outward for
// a surface-PM pole, the angle of the pole centre).
type FEMMRegion struct {
	Name      string       `json:"name"`
	Material  string       `json:"material"`            // host catalog material id
	Loop      [][2]float64 `json:"loop"`                // closed boundary, [x,y] mm
	Br        float64      `json:"br,omitempty"`        // magnet remanence [T] (0 for iron)
	HcjKAm    float64      `json:"hcjKAm,omitempty"`    // magnet coercivity [kA/m]
	MuR       float64      `json:"muR"`                 // relative permeability (recoil for PM)
	MagDirDeg float64      `json:"magDirDeg,omitempty"` // magnetisation direction [deg], magnets only
}

// FEMMDescriptor is the FEMM-ready hand-off: the full motor cross-section as named regions
// with magnetic materials, in millimetres, axis-relative — everything a 2D magnetostatic
// study needs that the host geometry alone does not carry (Br/Hc per region, magnetisation
// direction). The FEMM bridge (or a follow-up driver) consumes this to build a .fem problem.
type FEMMDescriptor struct {
	PoleCount      int          `json:"poleCount"`
	SlotCount      int          `json:"slotCount"`
	StackLengthMM  float64      `json:"stackLengthMM"`
	AirgapMM       float64      `json:"airgapMM"`
	StatorOuterDia float64      `json:"statorOuterDiaMM"`
	Regions        []FEMMRegion `json:"regions"`
}

// BuildFEMMDescriptor turns a sized Design into its FEMM region descriptor. It reuses the
// same CrossSection loops the host geometry is built from (so the descriptor and the bodies
// are the identical shape), converted back to millimetres, and attaches each region's
// magnetic material data resolved from the catalogs.
func BuildFEMMDescriptor(d *Design) FEMMDescriptor {
	cs := BuildCrossSection(d)
	desc := FEMMDescriptor{
		PoleCount: d.Spec.Poles, SlotCount: d.Spec.Slots,
		StackLengthMM: d.StackLength, AirgapMM: d.Spec.AirgapMM,
		StatorOuterDia: d.StatorOuterDia,
	}
	desc.Regions = append(desc.Regions, ironRegion("Stator", cs.StatorBore, d))
	desc.Regions = append(desc.Regions, ironRegion("Rotor", cs.RotorOuter, d))
	desc.Regions = append(desc.Regions, magnetRegions(cs, d)...)
	return desc
}

// ironRegion builds a soft-iron region from a cm loop, attaching the steel grade's μr.
func ironRegion(name string, loopCM []Point2, d *Design) FEMMRegion {
	return FEMMRegion{
		Name: name, Material: HostSteelMaterialID(d.Spec.SteelGrade),
		Loop: loopToMM(loopCM), MuR: softIronMuR,
	}
}

// magnetRegions builds one FEMM region per magnet pole, with the magnet grade's Br/Hc/μr and
// the radial-outward magnetisation direction at the pole centre.
func magnetRegions(cs CrossSection, d *Design) []FEMMRegion {
	mag, _ := Magnet(d.Spec.MagnetGrade)
	step := 360.0 / float64(d.Spec.Poles)
	regions := make([]FEMMRegion, 0, len(cs.Magnets))
	for i, loop := range cs.Magnets {
		regions = append(regions, FEMMRegion{
			Name: magnetName(i), Material: HostMagnetMaterialID(d.Spec.MagnetGrade),
			Loop: loopToMM(loop), Br: mag.Br, HcjKAm: mag.HcjKAm, MuR: mag.MuR,
			MagDirDeg: float64(i) * step, // radial-outward at the pole centre
		})
	}
	return regions
}

// magnetName is the per-pole region name (Magnet-1, Magnet-2, …), matching the body names.
func magnetName(i int) string {
	return "Magnet-" + strconv.Itoa(i+1)
}

// loopToMM converts a cm point loop to a millimetre [x,y] loop for the descriptor.
func loopToMM(loopCM []Point2) [][2]float64 {
	out := make([][2]float64, len(loopCM))
	for i, p := range loopCM {
		out[i] = [2]float64{p.X * 10, p.Y * 10}
	}
	return out
}

// softIronMuR is the representative linear-region permeability the rough FEMM study uses for
// the laminated iron (the host material carries the full nonlinear figure; this is the
// closed-form approximation the descriptor publishes). M270-class electrical steel.
const softIronMuR = 4000.0

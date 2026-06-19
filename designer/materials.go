// SPDX-License-Identifier: GPL-2.0-only

// Package designer is the host-facing core of the motor-designer add-in: it sizes a
// rough (~20% accuracy) PM electric-motor cross-section from a small set of requirement
// inputs, then drives the Apache-2.0 oblikovati.org/api client to lay the design down as
// host parameters + sketches + features. The cgo c-shared shell (../export.go) owns the
// C ABI; this package owns the design math + geometry generation and stays cgo-free so it
// unit-tests on every platform.
//
// The data model is deliberately FEMM-export friendly (clean cross-section dimensions +
// named magnet/steel materials) so the rough design can be handed to the FEMM bridge
// (../../Oblikovati.AddIns.FEMMBridge) for magnetostatic optimization.
package designer

// MagnetGrade names a permanent-magnet material in the catalog.
type MagnetGrade string

// Magnet grades carried in the catalog (a representative subset of motor-calculator's).
const (
	MagnetN35     MagnetGrade = "N35"
	MagnetN42     MagnetGrade = "N42"
	MagnetN42SH   MagnetGrade = "N42SH"
	MagnetN52     MagnetGrade = "N52"
	MagnetFerrite MagnetGrade = "ferrite"
	MagnetSmCo    MagnetGrade = "smco"
)

// SteelGrade names an electrical-steel lamination material in the catalog.
type SteelGrade string

// Steel grades carried in the catalog.
const (
	SteelM235    SteelGrade = "M235"
	SteelM270    SteelGrade = "M270"
	SteelM330    SteelGrade = "M330"
	SteelM400    SteelGrade = "M400"
	SteelHiperCo SteelGrade = "HiperCo"
)

// MagnetData is the magnetic + physical properties FEMM needs for a PM region.
// Ported from motor-calculator/src/presets.ts MAGNET_GRADES (B_r and H_cj at 20 C).
type MagnetData struct {
	Br      float64 // remanence at 20 C [T]
	HcjKAm  float64 // intrinsic coercivity at 20 C [kA/m]
	MuR     float64 // recoil relative permeability [-] (Br/(mu0*Hc))
	Density float64 // [kg/m^3]
	TmaxC   float64 // max operating temperature [C]
}

// SteelData is the lamination properties FEMM needs for a back-iron / tooth region.
// Ported from motor-calculator/src/presets.ts STEEL_GRADES.
type SteelData struct {
	W15_50  float64 // specific loss at 1.5 T, 50 Hz [W/kg]
	Density float64 // [kg/m^3]
}

// magnetCatalog maps each grade to its data. Recoil mu_r is derived from Br and Hcj as
// mu_r = Br / (mu0 * Hcj) with Hcj in A/m — the standard linear-recoil approximation
// FEMM uses for a PM region. (motor-calculator carries mu_r as a user input ~1.05; we
// derive it so the catalog is self-contained.)
var magnetCatalog = map[MagnetGrade]MagnetData{
	MagnetN35:     {Br: 1.20, HcjKAm: 955, MuR: recoilMuR(1.20, 955), Density: 7500, TmaxC: 80},
	MagnetN42:     {Br: 1.30, HcjKAm: 875, MuR: recoilMuR(1.30, 875), Density: 7500, TmaxC: 80},
	MagnetN42SH:   {Br: 1.30, HcjKAm: 1592, MuR: recoilMuR(1.30, 1592), Density: 7500, TmaxC: 150},
	MagnetN52:     {Br: 1.45, HcjKAm: 875, MuR: recoilMuR(1.45, 875), Density: 7500, TmaxC: 80},
	MagnetFerrite: {Br: 0.40, HcjKAm: 250, MuR: recoilMuR(0.40, 250), Density: 4900, TmaxC: 250},
	MagnetSmCo:    {Br: 1.10, HcjKAm: 1592, MuR: recoilMuR(1.10, 1592), Density: 8400, TmaxC: 250},
}

var steelCatalog = map[SteelGrade]SteelData{
	SteelM235:    {W15_50: 2.35, Density: 7600},
	SteelM270:    {W15_50: 2.70, Density: 7650},
	SteelM330:    {W15_50: 3.30, Density: 7700},
	SteelM400:    {W15_50: 4.00, Density: 7700},
	SteelHiperCo: {W15_50: 1.50, Density: 8120},
}

const mu0 = 4e-7 * 3.141592653589793 // vacuum permeability [H/m]

// recoilMuR returns the linear-recoil relative permeability of a magnet from its
// remanence Br [T] and intrinsic coercivity Hcj [kA/m]: mu_r = Br / (mu0 * Hcj).
func recoilMuR(brT, hcjKAm float64) float64 {
	return brT / (mu0 * hcjKAm * 1000)
}

// hostMagnetID maps a designer magnet grade to the host material catalog id (the built-in
// permanent-magnet entries added in 07-magnetic.yaml), so the generated magnet bodies can be
// assigned a real host material the FEMM bridge reads. Unmapped grades fall back to N42.
var hostMagnetID = map[MagnetGrade]string{
	MagnetN35:     "magnet-ndfeb-n35",
	MagnetN42:     "magnet-ndfeb-n42",
	MagnetN42SH:   "magnet-ndfeb-n42", // SH is a temperature variant of N42's Br/Hc class
	MagnetN52:     "magnet-ndfeb-n52",
	MagnetFerrite: "magnet-ferrite-y30",
	MagnetSmCo:    "magnet-smco-2-17",
}

// hostSteelID maps a designer steel grade to the host electrical-steel catalog id. The
// designer's M-grades (loss class) map to the nearest host lamination grade; unmapped grades
// fall back to M270.
var hostSteelID = map[SteelGrade]string{
	SteelM235:    "electrical-steel-m270",
	SteelM270:    "electrical-steel-m270",
	SteelM330:    "electrical-steel-m400",
	SteelM400:    "electrical-steel-m400",
	SteelHiperCo: "cobalt-iron-vacoflux",
}

// HostMagnetMaterialID returns the host catalog material id for a magnet grade (N42 default).
func HostMagnetMaterialID(g MagnetGrade) string {
	if id, ok := hostMagnetID[g]; ok {
		return id
	}
	return "magnet-ndfeb-n42"
}

// HostSteelMaterialID returns the host catalog material id for a steel grade (M270 default).
func HostSteelMaterialID(g SteelGrade) string {
	if id, ok := hostSteelID[g]; ok {
		return id
	}
	return "electrical-steel-m270"
}

// Magnet returns the catalog data for a grade, or false if the grade is unknown.
func Magnet(g MagnetGrade) (MagnetData, bool) {
	d, ok := magnetCatalog[g]
	return d, ok
}

// Steel returns the catalog data for a grade, or false if the grade is unknown.
func Steel(g SteelGrade) (SteelData, bool) {
	d, ok := steelCatalog[g]
	return d, ok
}

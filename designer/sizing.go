// SPDX-License-Identifier: GPL-2.0-only

package designer

import "math"

// Pure geometry derivations, ported from motor-calculator/src/physics/geometry.ts.
// All functions here work in SI (metres, m^3); the Design assembler (design.go) converts
// the mm-valued Spec into SI, calls these, and converts results back to mm.

// rotorVolume returns the rotor bore volume from torque and TRV.
// Ported from geometry.ts rotorVolume: V = T / (TRV[kNm/m^3] * 1000).
func rotorVolume(torqueNm, trvKNm float64) float64 {
	return torqueNm / (trvKNm * 1000)
}

// shearStress returns the airgap shear stress [Pa] from TRV [kNm/m^3].
// Ported from geometry.ts shearStress: sigma = TRV*1000 / 2.
func shearStress(trvKNm float64) float64 {
	return (trvKNm * 1000) / 2
}

// boreDiameter returns the stator bore diameter from rotor volume and L/D aspect ratio.
// Ported from geometry.ts boreDiameter: D = cbrt(4V / (pi * L/D)).
func boreDiameter(vM3, aspect float64) float64 {
	return math.Cbrt((4 * vM3) / (math.Pi * aspect))
}

// activeLength returns the stack length L = (L/D) * D.
// Ported from geometry.ts activeLength.
func activeLength(dM, aspect float64) float64 {
	return aspect * dM
}

// rotorDiameter returns the rotor OD = D - 2*airgap, clamped to >= 0.5*D (inrunner).
// Ported from geometry.ts rotorDiameter.
func rotorDiameter(boreM, airgapM float64) float64 {
	return math.Max(boreM*0.5, boreM-2*airgapM)
}

// slotPitch returns the slot pitch at the bore surface tau_s = pi*D/Q.
// Ported from geometry.ts slotPitch.
func slotPitch(dM float64, slots int) float64 {
	return (math.Pi * dM) / float64(slots)
}

// polePitch returns the pole pitch at the bore surface tau_p = pi*D/p.
// Ported from geometry.ts polePitch.
func polePitch(dM float64, poles int) float64 {
	return (math.Pi * dM) / float64(poles)
}

// toothWidth returns the tooth width from flux balance w_t = Bg*tau_s / Btooth.
// Ported from geometry.ts toothWidth.
func toothWidth(bg, tauS, bTooth float64) float64 {
	return (bg * tauS) / bTooth
}

// averageFluxPerPole returns the average flux per pole phi_p = Bg*tau_p*L*(2/pi).
// Ported from geometry.ts averageFluxPerPole.
func averageFluxPerPole(bg, tauP, lM float64) float64 {
	return bg * tauP * lM * (2 / math.Pi)
}

// yokeThickness returns the stator/rotor yoke radial thickness from flux balance
// h_yoke = phi_p / (2*By*L). Ported from geometry.ts yokeThickness.
func yokeThickness(phiPAvg, by, lM float64) float64 {
	return phiPAvg / (2 * by * lM)
}

// slotHeightQuadratic solves the trapezoidal-slot quadratic for the inrunner slot depth
// (slots widen outward): (pi/Q)*h^2 + (tau_s - w_t)*h - A_slot = 0.
// Ported from geometry.ts slotHeightQuadratic. Returns 0 if the discriminant is negative.
func slotHeightQuadratic(slotAreaM2, tauS, toothW float64, slots int) float64 {
	a := math.Pi / float64(slots)
	b := tauS - toothW
	c := -slotAreaM2
	disc := b*b - 4*a*c
	if disc < 0 || a <= 0 {
		return 0
	}
	return (-b + math.Sqrt(disc)) / (2 * a)
}

// statorOuterDiameter returns D_so = D_bore + 2*(h_slot + h_yoke).
// Ported from geometry.ts statorOuterDiameter.
func statorOuterDiameter(boreM, slotHM, yokeHM float64) float64 {
	return boreM + 2*(slotHM+yokeHM)
}

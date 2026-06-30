// SPDX-License-Identifier: GPL-2.0-only

package designer

import "math"

// Design is the rough motor cross-section produced from a Spec: every dimension a FEMM
// 2D study (and the host geometry generator) needs, in millimetres, plus the resolved
// materials. It is the clean, exportable hand-off to the FEMM bridge.
//
// Radii are measured from the motor axis. The inrunner stack-up, inside -> out, is:
//
//	shaft .. RotorYokeInnerR .. (magnet) .. RotorOuterR | airgap | BoreR .. (slots+teeth) .. (yoke) .. StatorOuterR
type Design struct {
	Spec Spec

	// Core diameters [mm].
	BoreDiameter   float64 // stator bore D (airgap surface) [mm]
	StatorOuterDia float64 // stator OD [mm]
	RotorOuterDia  float64 // rotor OD (magnet outer surface for SPM) [mm]
	StackLength    float64 // axial active length L [mm]

	// Stator detail [mm].
	SlotPitch   float64 // tau_s at bore [mm]
	PolePitch   float64 // tau_p at bore [mm]
	ToothWidth  float64 // tooth tangential width [mm]
	SlotDepth   float64 // radial slot depth [mm]
	StatorYokeH float64 // stator yoke radial thickness [mm]
	SlotAreaMM2 float64 // single-slot cross-section area [mm^2]
	ToothTipR   float64 // bore radius (tooth-tip circle) [mm]
	SlotOuterR  float64 // radius at slot bottom / yoke inner [mm]

	// Rotor detail [mm].
	MagnetThick   float64 // magnet radial thickness h_m [mm]
	MagnetInnerR  float64 // magnet inner radius (rotor back-iron OD) [mm]
	RotorYokeH    float64 // rotor back-iron radial thickness [mm]
	RotorYokeInnR float64 // rotor back-iron inner radius (shaft surface) [mm]
	MagnetArcDeg  float64 // magnet pole-arc span [deg]

	// Electromagnetic loading derived for the record / downstream FEMM excitation.
	ShearStress   float64 // airgap shear stress [Pa]
	ElectricLoad  float64 // RMS linear current density A_rms [A/m]
	WindingFactor float64 // fundamental winding factor k_w1 [-]
	WindingValid  bool    // false when (slots,poles) cannot form a balanced winding
	FluxPerPole   float64 // average flux per pole [Wb]

	// Resolved materials (carried for the FEMM export).
	Magnet MagnetData
	Steel  SteelData
}

// Compute sizes a rough design from a Spec, or returns the validation error. It ports the
// inrunner branch of motor-calculator/src/lib/calculate.ts: volume -> bore -> stack ->
// rotor -> tooth/yoke flux balance -> electric loading -> slot depth.
func Compute(s Spec) (*Design, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	d := &Design{Spec: s}
	d.Magnet, _ = Magnet(s.MagnetGrade)
	d.Steel, _ = Steel(s.SteelGrade)
	d.sizeCore(s)
	d.sizeStatorIron(s)
	d.sizeElectricLoading(s)
	d.sizeSlots(s)
	d.sizeRotor(s)
	if err := validateToothGeometry(d); err != nil {
		return nil, err
	}
	return d, nil
}

// sizeCore sets bore, stack length and rotor OD from the torque/volume requirement.
func (d *Design) sizeCore(s Spec) {
	airgapM := s.AirgapMM / 1000
	vol := rotorVolume(s.TorqueNm, s.TRVkNm)
	boreM := boreDiameter(vol, s.StackAspect)
	d.BoreDiameter = boreM * 1000
	d.StackLength = activeLength(boreM, s.StackAspect) * 1000
	d.RotorOuterDia = rotorDiameter(boreM, airgapM) * 1000
	d.ShearStress = shearStress(s.TRVkNm)
	d.ToothTipR = d.BoreDiameter / 2
}

// sizeStatorIron sets tooth width and stator yoke thickness from the flux-balance
// equations at the bore.
func (d *Design) sizeStatorIron(s Spec) {
	boreM := d.BoreDiameter / 1000
	lM := d.StackLength / 1000
	tauS := slotPitch(boreM, s.Slots)
	tauP := polePitch(boreM, s.Poles)
	d.SlotPitch = tauS * 1000
	d.PolePitch = tauP * 1000
	d.ToothWidth = toothWidth(s.AirgapB, tauS, s.ToothB) * 1000
	d.FluxPerPole = averageFluxPerPole(s.AirgapB, tauP, lM)
	d.StatorYokeH = yokeThickness(d.FluxPerPole, s.YokeB, lM) * 1000
}

// sizeElectricLoading sets the airgap shear-derived RMS electric loading, using the
// winding factor. Ported from calculate.ts: A_rms = sigma*sqrt(2)/(Bg*kw).
func (d *Design) sizeElectricLoading(s Spec) {
	kw, ok := FundamentalWindingFactor(s.Slots, s.Poles)
	d.WindingValid = ok
	if !ok {
		kw = 0.866 // calculate.ts fallback when the combination is unbalanced
	}
	d.WindingFactor = kw
	d.ElectricLoad = d.ShearStress * math.Sqrt2 / (s.AirgapB * kw)
}

// sizeSlots sets the single-slot area and the radial slot depth from the electric loading
// using a representative current density and fill factor (the rough-design closure that
// calculate.ts derives from the cooling preset; we use forced-convection defaults).
func (d *Design) sizeSlots(s Spec) {
	const jSI = 7e6   // A/m^2 — forced-convection current density (presets.ts forced.J=7 A/mm^2)
	const kFill = 0.4 // slot fill factor [-]
	tauS := d.SlotPitch / 1000
	niSlot := d.ElectricLoad * tauS
	slotAreaM2 := niSlot / (jSI * kFill)
	d.SlotAreaMM2 = slotAreaM2 * 1e6
	toothWM := d.ToothWidth / 1000
	slotHM := slotHeightQuadratic(slotAreaM2, tauS, toothWM, s.Slots)
	d.SlotDepth = slotHM * 1000
	d.SlotOuterR = d.ToothTipR + d.SlotDepth
	boreM := d.BoreDiameter / 1000
	d.StatorOuterDia = statorOuterDiameter(boreM, slotHM, d.StatorYokeH/1000) * 1000
}

// sizeRotor sets the magnet and rotor back-iron stack-up. The rotor yoke carries half the
// pole flux, like the stator yoke (flux-balance symmetry), so we reuse yokeThickness.
func (d *Design) sizeRotor(s Spec) {
	d.MagnetThick = s.MagnetMM
	d.MagnetArcDeg = s.MagnetArc * (360.0 / float64(s.Poles))
	magOuterR := d.RotorOuterDia / 2
	d.MagnetInnerR = magOuterR - d.MagnetThick
	lM := d.StackLength / 1000
	d.RotorYokeH = yokeThickness(d.FluxPerPole, s.YokeB, lM) * 1000
	d.RotorYokeInnR = math.Max(0.1, d.MagnetInnerR-d.RotorYokeH)
}

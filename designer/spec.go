// SPDX-License-Identifier: GPL-2.0-only

package designer

import "fmt"

// RotorTopology selects how the magnets sit on the rotor.
type RotorTopology string

const (
	// SurfacePM places magnets on the rotor outer surface (SPM).
	SurfacePM RotorTopology = "spm"
	// InteriorPM buries the magnets inside the rotor back-iron (IPM).
	InteriorPM RotorTopology = "ipm"
)

// MotorType selects the radial arrangement of rotor vs stator.
type MotorType string

const (
	// Inrunner: the rotor spins INSIDE the stator (the common servo layout). The stator
	// teeth point INWARD toward the bore; magnets sit on the rotor's outer surface.
	Inrunner MotorType = "inrunner"
	// Outrunner: the rotor is an OUTER ring spinning AROUND the stator (hub/gimbal motors).
	// The stator teeth point OUTWARD; magnets line the inside of the outer rotor ring.
	Outrunner MotorType = "outrunner"
)

// SlotType selects the stator slot cross-section profile (motor-design literature shapes).
type SlotType string

const (
	// SlotParallelTooth is the semi-closed slot with constant-width (parallel-sided) teeth,
	// tooth shoes/tips, a narrow slot opening and a round bottom — the standard PMSM/BLDC slot
	// (e.g. Pyrhönen, "Design of Rotating Electrical Machines", §4; FEMM/Motor-CAD default).
	SlotParallelTooth SlotType = "parallel-tooth"
	// SlotOpenRectangular is a fully open, parallel-sided slot (no shoe), used in large or
	// form-wound machines — simplest to wind, highest slot-opening flux ripple.
	SlotOpenRectangular SlotType = "open-rectangular"
	// SlotRoundBottom is a semi-closed pear/teardrop slot: shoe + radial tooth sides widening
	// to a large rounded bottom, common in small induction and some PM machines.
	SlotRoundBottom SlotType = "round-bottom"
)

// Spec is the small set of requirement + electromagnetic-loading inputs that drive a
// rough motor design. Lengths are in millimetres, torque in N*m, speed in rpm — the
// units a designer thinks in. It mirrors the controllable subset of motor-calculator's
// MotorInputs (src/types.ts) that matters for a first-pass cross-section.
type Spec struct {
	// Requirements.
	TorqueNm    float64 // rated shaft torque [N*m]
	SpeedRPM    float64 // rated speed [rpm]
	Poles       int     // pole count (even, >= 2)
	Slots       int     // stator slot count (multiple of 3)
	StackAspect float64 // active length / bore diameter, L/D [-]

	// Electromagnetic loading (motor-calculator MotorInputs: TRV, Bg, Btooth, Byoke).
	TRVkNm  float64 // torque per rotor volume [kNm/m^3] — sets the bore size
	AirgapB float64 // target airgap flux density B_g [T]
	ToothB  float64 // target tooth flux density [T]
	YokeB   float64 // target yoke flux density [T]

	// Magnet + airgap geometry.
	AirgapMM    float64 // mechanical airgap [mm]
	MagnetMM    float64 // magnet radial thickness h_m [mm]
	MagnetArc   float64 // magnet pole-arc fraction alpha_m [-] (0..1)
	GlueGapMM   float64 // magnet↔rotor bond line [mm] (FEMM magnet/iron separation)
	Topology    RotorTopology
	Type        MotorType // inrunner (rotor inside) or outrunner (rotor outside)
	MagnetGrade MagnetGrade
	SteelGrade  SteelGrade

	// Stator slot geometry (literature profile + its semi-closed-slot dimensions).
	SlotType         SlotType // slot cross-section profile
	SlotOpeningMM    float64  // slot opening width b_s0 at the airgap [mm]
	ToothTipHeightMM float64  // tooth shoe (tip) radial height h_s0+h_s1 [mm]
}

// DefaultSpec is a sane mid-size servo-motor starting point: ~1.6 N*m, 3000 rpm,
// 10-pole / 12-slot FSCW, N42 surface magnets in M270 steel. The dockable window seeds
// its fields from this so the user always has a valid design to generate.
func DefaultSpec() Spec {
	return Spec{
		TorqueNm:    1.6,
		SpeedRPM:    3000,
		Poles:       10,
		Slots:       12,
		StackAspect: 1.0,
		TRVkNm:      20,
		AirgapB:     0.85,
		ToothB:      1.6,
		YokeB:       1.3,
		AirgapMM:    0.7,
		MagnetMM:    3.5,
		MagnetArc:   0.83,
		GlueGapMM:   0.05,
		Topology:    SurfacePM,
		Type:        Inrunner,
		MagnetGrade: MagnetN42,
		SteelGrade:  SteelM270,

		SlotType:         SlotParallelTooth,
		SlotOpeningMM:    2.0,
		ToothTipHeightMM: 1.5,
	}
}

// normSlotType returns the spec's slot profile, defaulting an unset value to the
// parallel-tooth slot so a zero-value Spec keeps a realistic semi-closed slot.
func (s Spec) normSlotType() SlotType {
	switch s.SlotType {
	case SlotOpenRectangular, SlotRoundBottom:
		return s.SlotType
	default:
		return SlotParallelTooth
	}
}

// slotOpeningMM / toothTipHeightMM return the semi-closed-slot dimensions with literature
// defaults when the spec leaves them unset (a zero-value Spec stays valid).
func (s Spec) slotOpeningMM() float64 {
	if s.SlotOpeningMM > 0 {
		return s.SlotOpeningMM
	}
	return 2.0
}

func (s Spec) toothTipHeightMM() float64 {
	if s.ToothTipHeightMM > 0 {
		return s.ToothTipHeightMM
	}
	return 1.5
}

// normType returns the spec's motor type, defaulting an unset value to Inrunner so a
// zero-value Spec (and existing callers) keep the common layout.
func (s Spec) normType() MotorType {
	if s.Type == Outrunner {
		return Outrunner
	}
	return Inrunner
}

// Validate rejects inputs that would produce degenerate geometry, naming the offending
// value and the expected shape (per the project's exception-message rule).
func (s Spec) Validate() error {
	if s.TorqueNm <= 0 {
		return fmt.Errorf("designer: TorqueNm must be > 0, got %g", s.TorqueNm)
	}
	if s.SpeedRPM <= 0 {
		return fmt.Errorf("designer: SpeedRPM must be > 0, got %g", s.SpeedRPM)
	}
	if s.Poles < 2 || s.Poles%2 != 0 {
		return fmt.Errorf("designer: Poles must be an even integer >= 2, got %d", s.Poles)
	}
	if s.Slots < 3 || s.Slots%3 != 0 {
		return fmt.Errorf("designer: Slots must be a multiple of 3 (>= 3), got %d", s.Slots)
	}
	return s.validateLoading()
}

// validateLoading checks the magnetic-loading + geometry inputs (split out to keep
// Validate within the function-length budget).
func (s Spec) validateLoading() error {
	switch {
	case s.StackAspect <= 0:
		return fmt.Errorf("designer: StackAspect (L/D) must be > 0, got %g", s.StackAspect)
	case s.TRVkNm <= 0:
		return fmt.Errorf("designer: TRVkNm must be > 0, got %g", s.TRVkNm)
	case s.AirgapB <= 0 || s.ToothB <= 0 || s.YokeB <= 0:
		return fmt.Errorf("designer: flux densities must be > 0, got Bg=%g Bt=%g By=%g",
			s.AirgapB, s.ToothB, s.YokeB)
	case s.AirgapMM <= 0:
		return fmt.Errorf("designer: AirgapMM must be > 0, got %g", s.AirgapMM)
	case s.MagnetMM <= 0:
		return fmt.Errorf("designer: MagnetMM must be > 0, got %g", s.MagnetMM)
	case s.MagnetArc <= 0 || s.MagnetArc > 1:
		return fmt.Errorf("designer: MagnetArc must be in (0,1], got %g", s.MagnetArc)
	}
	if _, ok := magnetCatalog[s.MagnetGrade]; !ok {
		return fmt.Errorf("designer: unknown MagnetGrade %q", s.MagnetGrade)
	}
	if _, ok := steelCatalog[s.SteelGrade]; !ok {
		return fmt.Errorf("designer: unknown SteelGrade %q", s.SteelGrade)
	}
	return nil
}

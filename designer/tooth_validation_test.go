// SPDX-License-Identifier: GPL-2.0-only

package designer

import (
	"strings"
	"testing"
)

// TestValidateAcceptsDefaultAllSlotTypes pins that the realistic default motor is valid for every
// slot type and both motor types — the degenerate-shoe guard must not reject a sound design.
func TestValidateAcceptsDefaultAllSlotTypes(t *testing.T) {
	for _, mt := range []MotorType{Inrunner, Outrunner} {
		for _, st := range []SlotType{SlotOpenRectangular, SlotParallelTooth, SlotRoundBottom} {
			s := DefaultSpec()
			s.Type, s.SlotType = mt, st
			if _, err := Compute(s); err != nil {
				t.Errorf("%v/%v default should be valid, got: %v", mt, st, err)
			}
		}
	}
}

// TestValidateRejectsTooWideTooth pins the primary degeneracy: a coarse slot count makes an
// outrunner's constant-width teeth wider than its small yoke ring, so the profile can't close.
// Compute must reject it with a clear, value-naming message instead of leaving it to fail at the
// host extrude ("profile is open").
func TestValidateRejectsTooWideTooth(t *testing.T) {
	s := DefaultSpec()
	s.Type, s.SlotType = Outrunner, SlotOpenRectangular
	s.Slots, s.Poles = 6, 4
	_, err := Compute(s)
	if err == nil {
		t.Fatal("6-slot outrunner with wide teeth should be rejected")
	}
	if !strings.Contains(err.Error(), "too wide") || !strings.Contains(err.Error(), "Slots") {
		t.Errorf("error should name the width/slot problem, got: %v", err)
	}
}

// TestValidateRejectsWideSlotOpening pins the shoe-angle guard: a slot opening wider than the slot
// pitch leaves no room for a tooth shoe.
func TestValidateRejectsWideSlotOpening(t *testing.T) {
	s := DefaultSpec()
	s.SlotType = SlotParallelTooth
	s.SlotOpeningMM = 40 // far wider than the slot pitch at the bore
	_, err := Compute(s)
	if err == nil || !strings.Contains(err.Error(), "slot opening") {
		t.Errorf("a slot opening consuming the pitch should be rejected with a shoe message, got: %v", err)
	}
}

// TestValidateRejectsDeepTipHeight pins the neck-depth guard: a tip height at least as deep as the
// slot would push the shoe neck across the slot bottom.
func TestValidateRejectsDeepTipHeight(t *testing.T) {
	s := DefaultSpec()
	s.SlotType = SlotRoundBottom
	s.ToothTipHeightMM = 100 // deeper than any slot
	_, err := Compute(s)
	if err == nil || !strings.Contains(err.Error(), "tip height") {
		t.Errorf("a tip height deeper than the slot should be rejected, got: %v", err)
	}
}

// TestValidateOpenRectIgnoresShoeInputs pins that the open-rectangular slot (no shoe) is NOT
// rejected for shoe inputs that would be invalid on a shoe profile — it doesn't have a shoe, so
// a wide slot opening / deep tip height are irrelevant to it.
func TestValidateOpenRectIgnoresShoeInputs(t *testing.T) {
	s := DefaultSpec()
	s.SlotType = SlotOpenRectangular
	s.SlotOpeningMM, s.ToothTipHeightMM = 40, 100
	if _, err := Compute(s); err != nil {
		t.Errorf("open-rectangular has no shoe; shoe inputs must not invalidate it, got: %v", err)
	}
}

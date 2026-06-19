// SPDX-License-Identifier: GPL-2.0-only

package designer

import (
	"fmt"
	"strconv"
	"strings"

	"oblikovati.org/api/client"
	"oblikovati.org/api/types"
	"oblikovati.org/api/wire"
)

// PanelID is the stable dockable-window id the add-in owns.
const PanelID = "com.oblikovati.motor-designer.panel"

// GenerateCommandID is the command the panel's Generate button names; the host reports
// the click as an ordinary command-ended event the engine handles in Notify. It generates
// the current spec (inrunner by default).
const GenerateCommandID = "MotorDesigner.Generate"

// GenerateOutrunnerCommandID generates the current spec as an OUTRUNNER (rotor ring outside
// the stator), without needing panel inputs — drivable headlessly for testing both layouts.
const GenerateOutrunnerCommandID = "MotorDesigner.GenerateOutrunner"

// ShowPanel creates (or replaces) the design-options dockable window, seeded from a Spec
// and the cross-section it computes. Inputs are rendered as labels (the panel vocabulary
// is declarative labels/buttons today, M05-F03) plus a Generate button; a richer editor
// arrives when the panel control set grows fields.
func (e *Engine) ShowPanel(s Spec) (wire.OKResult, error) {
	controls, err := panelControls(s)
	if err != nil {
		return wire.OKResult{}, err
	}
	return e.api.DockableWindows().Set(wire.DockableWindowSpec{
		ID:       PanelID,
		Title:    "Motor Designer",
		Dock:     types.DockRight,
		Visible:  true,
		Controls: controls,
	})
}

// panelControls renders the design-options surface: the requirement/loading inputs, the
// computed cross-section summary, and the Generate button.
func panelControls(s Spec) ([]wire.PanelControlSpec, error) {
	d, err := Compute(s)
	if err != nil {
		return nil, err
	}
	controls := inputControls(s)
	controls = append(controls, wire.PanelControlSpec{Kind: types.PanelSeparator})
	controls = append(controls, resultControls(d)...)
	controls = append(controls, wire.PanelControlSpec{Kind: types.PanelSeparator})
	controls = append(controls, wire.PanelControlSpec{
		Kind: types.PanelButton, ID: "generate", Text: "Generate Geometry",
		CommandID: GenerateCommandID,
	})
	return controls, nil
}

// inputControls renders the editable design drivers as form fields. Each control's ID is the
// key applyControl uses to write the edited value back into the Spec (M05-F03 editable panel).
func inputControls(s Spec) []wire.PanelControlSpec {
	return []wire.PanelControlSpec{
		client.PanelLabel("hdr-req", "— Requirements —"),
		client.PanelDropdown("type", "Motor type", motorTypeOptions, string(s.normType())),
		client.PanelTextBox("torque", "Torque (N·m)", fmt.Sprintf("%g", s.TorqueNm)),
		client.PanelTextBox("speed", "Speed (rpm)", fmt.Sprintf("%g", s.SpeedRPM)),
		client.PanelTextBox("poles", "Poles", fmt.Sprintf("%d", s.Poles)),
		client.PanelTextBox("slots", "Slots", fmt.Sprintf("%d", s.Slots)),

		client.PanelLabel("hdr-load", "— Magnetic loading —"),
		client.PanelTextBox("airgap_b", "Airgap B (T)", fmt.Sprintf("%g", s.AirgapB)),
		client.PanelTextBox("tooth_b", "Tooth B (T)", fmt.Sprintf("%g", s.ToothB)),
		client.PanelTextBox("yoke_b", "Yoke B (T)", fmt.Sprintf("%g", s.YokeB)),

		client.PanelLabel("hdr-mag", "— Magnets —"),
		client.PanelDropdown("magnet_grade", "Grade", magnetGradeOptions, string(s.MagnetGrade)),
		client.PanelValueEditor("magnet_mm", "Thickness", fmt.Sprintf("%g mm", s.MagnetMM)),
		client.PanelSlider("magnet_arc", "Pole arc", s.MagnetArc, 0.4, 1.0, 0.01),
		client.PanelValueEditor("airgap", "Airgap", fmt.Sprintf("%g mm", s.AirgapMM)),

		client.PanelLabel("hdr-slot", "— Stator slots —"),
		client.PanelDropdown("steel_grade", "Steel", steelGradeOptions, string(s.SteelGrade)),
		client.PanelDropdown("slot_type", "Slot profile", slotTypeOptions, string(s.normSlotType())),
		client.PanelValueEditor("slot_open", "Slot opening", fmt.Sprintf("%g mm", s.slotOpeningMM())),
		client.PanelValueEditor("tooth_tip", "Tooth tip height", fmt.Sprintf("%g mm", s.toothTipHeightMM())),
	}
}

// Option lists for the form's dropdowns.
var (
	motorTypeOptions   = []string{string(Inrunner), string(Outrunner)}
	slotTypeOptions    = []string{string(SlotParallelTooth), string(SlotOpenRectangular), string(SlotRoundBottom)}
	magnetGradeOptions = []string{
		string(MagnetN35), string(MagnetN42), string(MagnetN42SH),
		string(MagnetN52), string(MagnetFerrite), string(MagnetSmCo),
	}
	steelGradeOptions = []string{
		string(SteelM235), string(SteelM270), string(SteelM330),
		string(SteelM400), string(SteelHiperCo),
	}
)

// specSetters maps a form control ID to the function that writes its edited value into the Spec.
// A table keeps applyControl flat (one lookup) instead of a long switch.
var specSetters = map[string]func(*Spec, string){
	"type":         func(s *Spec, v string) { s.Type = MotorType(v) },
	"torque":       func(s *Spec, v string) { s.TorqueNm = parseNum(v, s.TorqueNm) },
	"speed":        func(s *Spec, v string) { s.SpeedRPM = parseNum(v, s.SpeedRPM) },
	"poles":        func(s *Spec, v string) { s.Poles = int(parseNum(v, float64(s.Poles))) },
	"slots":        func(s *Spec, v string) { s.Slots = int(parseNum(v, float64(s.Slots))) },
	"airgap_b":     func(s *Spec, v string) { s.AirgapB = parseNum(v, s.AirgapB) },
	"tooth_b":      func(s *Spec, v string) { s.ToothB = parseNum(v, s.ToothB) },
	"yoke_b":       func(s *Spec, v string) { s.YokeB = parseNum(v, s.YokeB) },
	"magnet_grade": func(s *Spec, v string) { s.MagnetGrade = MagnetGrade(v) },
	"magnet_mm":    func(s *Spec, v string) { s.MagnetMM = parseNum(v, s.MagnetMM) },
	"magnet_arc":   func(s *Spec, v string) { s.MagnetArc = parseNum(v, s.MagnetArc) },
	"airgap":       func(s *Spec, v string) { s.AirgapMM = parseNum(v, s.AirgapMM) },
	"steel_grade":  func(s *Spec, v string) { s.SteelGrade = SteelGrade(v) },
	"slot_type":    func(s *Spec, v string) { s.SlotType = SlotType(v) },
	"slot_open":    func(s *Spec, v string) { s.SlotOpeningMM = parseNum(v, s.SlotOpeningMM) },
	"tooth_tip":    func(s *Spec, v string) { s.ToothTipHeightMM = parseNum(v, s.ToothTipHeightMM) },
}

// applyControl writes one edited form value back into the Spec, keyed by the control ID. Unknown
// ids and unparseable numbers are ignored (the field keeps its previous value).
func applyControl(s *Spec, id, value string) {
	if set, ok := specSetters[id]; ok {
		set(s, value)
	}
}

// resultControls renders the computed cross-section summary as label rows.
func resultControls(d *Design) []wire.PanelControlSpec {
	return []wire.PanelControlSpec{
		label("hdr-out", "— Cross-section —"),
		label("bore", fmt.Sprintf("Bore Ø: %.1f mm", d.BoreDiameter)),
		label("statod", fmt.Sprintf("Stator OD: %.1f mm", d.StatorOuterDia)),
		label("rotod", fmt.Sprintf("Rotor OD: %.1f mm", d.RotorOuterDia)),
		label("stack", fmt.Sprintf("Stack L: %.1f mm", d.StackLength)),
		label("tooth", fmt.Sprintf("Tooth w: %.2f mm", d.ToothWidth)),
		label("slotd", fmt.Sprintf("Slot depth: %.2f mm", d.SlotDepth)),
		label("kw", fmt.Sprintf("k_w1: %.3f%s", d.WindingFactor, windingNote(d))),
	}
}

func windingNote(d *Design) string {
	if d.WindingValid {
		return ""
	}
	return " (unbalanced!)"
}

// label is a static text row.
func label(id, text string) wire.PanelControlSpec {
	return wire.PanelControlSpec{Kind: types.PanelLabel, ID: id, Text: text}
}

// parseNum reads the leading number from a form value (e.g. "46.7 mm" → 46.7), returning
// fallback when there is no parseable number so a half-typed field never zeroes the Spec.
func parseNum(value string, fallback float64) float64 {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return fallback
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return fallback
	}
	return v
}

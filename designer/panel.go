// SPDX-License-Identifier: GPL-2.0-only

package designer

import (
	"fmt"

	"oblikovati.org/api/types"
	"oblikovati.org/api/wire"
)

// PanelID is the stable dockable-window id the add-in owns.
const PanelID = "com.oblikovati.motor-designer.panel"

// GenerateCommandID is the command the panel's Generate button names; the host reports
// the click as an ordinary command-ended event the engine handles in Notify.
const GenerateCommandID = "MotorDesigner.Generate"

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

// inputControls renders the editable design drivers as label rows.
func inputControls(s Spec) []wire.PanelControlSpec {
	return []wire.PanelControlSpec{
		label("hdr-req", "— Requirements —"),
		label("torque", fmt.Sprintf("Torque: %.2f N·m", s.TorqueNm)),
		label("speed", fmt.Sprintf("Speed: %.0f rpm", s.SpeedRPM)),
		label("poleslot", fmt.Sprintf("Poles/Slots: %d / %d", s.Poles, s.Slots)),
		label("hdr-load", "— Loading —"),
		label("bg", fmt.Sprintf("Airgap B: %.2f T", s.AirgapB)),
		label("mag", fmt.Sprintf("Magnet: %s, %.1f mm, arc %.2f", s.MagnetGrade, s.MagnetMM, s.MagnetArc)),
		label("steel", fmt.Sprintf("Steel: %s", s.SteelGrade)),
		label("topo", fmt.Sprintf("Topology: %s", s.Topology)),
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

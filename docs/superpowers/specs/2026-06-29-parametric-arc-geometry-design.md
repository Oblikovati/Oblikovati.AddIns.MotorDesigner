# MotorDesigner: parametric, arc-based, dimensionally-constrained geometry

Date: 2026-06-29
Status: Approved (design) — pending implementation plan

## Problem

The MotorDesigner add-in generates sketches that are wrong in three ways:

1. **Arcs are segmented lines.** `designer/crosssection.go` facets every curved span
   into straight segments (`arcPoints` → 6 segments/arc, `circleLoopCM` → 48/circle) and
   `geometry.go:addClosedPolyline` lays them down as `polyline` entities. Magnet arcs and
   the toothed stator bore therefore become faceted polylines, not real sketch arcs.
2. **Sketches are not dimensionally constrained.** The faceted profiles are baked literal
   centimetre coordinates with no dimensions or geometric constraints — DOF ≠ 0, violating
   the project rule "Fully Constraint the sketch with parameters. DOF=0".
3. **Editing parameters cannot recompute the parts.** Because the toothed bore and magnet
   profiles are literal-coordinate polylines (only the smooth circles and extrude depth are
   parameter-driven), changing a core dimension cannot recompute them. Only re-running
   `Generate` — which closes and recreates every document and sketch via `clearExistingMotor`
   — updates the design.

The code comments attribute this to "an open API gap (Oblikovati.API#187): expression-driven
sketch LINES/ARCS … are an open API gap". **This is stale.** The add-in pins API `v0.89.0`;
the current API is `v0.95.0` and already exposes everything required:

- `Sketch.AddArcByCenterStartEndExpr(center, start, end []string, ccw, construction)` — real
  arcs from parameter expressions.
- `Sketch.AddLineExpr` / `AddPointExpr` — parametric lines/points.
- `Sketch.AddCircleByCenterRadius(center, radiusExpr, …)` — already used for the circles.
- `Sketch.Constrain(idx)` — full geometric-constraint set (concentric, coincident,
  symmetric, equalRadius, …).
- `Sketch.Dimension(idx)` — `Radius`/`Diameter`/`Distance`/`Angle`/`ArcLength`, expression
  unit-bearing **and parameter-aware**.
- `Features().PatternCircular(CircularPatternFeatureArgs{SourceFeatures, CountExpr, Angle,
  AxisPoint, AxisDir})` — feature-level circular pattern with a **parameter-driven count**.

Verified host behaviour: `addin/router/param_expr.go:resolveQuantity` evaluates dimension and
arc expressions through `part.Parameters().EvaluateExpression(src)` **before** falling back to
a literal parse — so a parameter name or formula ("stator_outer_r", "tooth_width / bore_r")
resolves and stays live. Recompute on parameter edit is supported by the host recompute engine
(M39 / ADR-0044).

**Conclusion:** no API extension is needed. This is an add-in rewrite of the geometry layer to
emit real arcs, full dimensional + geometric constraints, and parameter-driven features.

## Decisions (agreed)

- **Toothed bore strategy:** sector + circular pattern, realised at the **feature** level
  (yoke ring + one tooth feature + `PatternCircular`), because only the feature pattern carries
  a parameter-driven `CountExpr` — keeping the slot/pole *count* live as well as the dimensions.
- **Scope:** all three parts (stator, rotor incl. outrunner, magnets) in one coherent change.
- **CrossSection removal:** the faceting helpers are dead once geometry is arc-based; remove
  them. Confirm in code that nothing outside sketch lay-down (notably `femm.go` /
  `publishFEMMDescriptor`) reads `CrossSection` before deleting.

## Design

### API pin

Bump `go.mod`: `oblikovati.org/api v0.89.0` → `v0.95.0`. No other dependency change.

### Stator part (canonical: yoke ring + patterned tooth)

Three features, each with its own sketch (project rule: one sketch per feature):

1. **Stator Yoke** — sketch: outer circle `stator_outer_r` + bore circle `slot_bottom_r`
   (both `AddCircleByCenterRadius` with parameter expressions, centred at origin). Extrude by
   `stack_length`. Produces the smooth annulus between yoke OD and slot bottoms.
2. **Tooth** — sketch: ONE tooth profile built from real arcs and lines —
   - slot-bottom base arc on radius `slot_bottom_r`,
   - two slot-flank lines (radial),
   - tooth-tip arc on radius `tooth_tip_r`,
   laid down with `AddArcByCenterStartEndExpr` / `AddLineExpr`. Constrained to DOF 0: arc
   centres concentric at the origin, flanks symmetric about the tooth centre-line, radius
   dimensions `tooth_tip_r` / `slot_bottom_r`, tooth angular width dimensioned from a parameter
   (`tooth_angle`). Extrude-join over `stack_length`. The base arc lies on the yoke bore circle
   so the tooth fuses to the yoke.
3. **Tooth pattern** — `PatternCircular({SourceFeatures:["Tooth"], CountExpr:"slots",
   Angle:"360 deg", AxisPoint:[0,0,0], AxisDir:[0,0,1]})`.

For an inrunner the teeth protrude inward (`tooth_tip_r` < `slot_bottom_r`); the outrunner flips
the radial order (teeth outward). One tooth-profile builder parameterised by the layout serves
both, mirroring the existing inrunner/outrunner layout split.

### Magnets part

One magnet sector, then pattern:

1. **Magnet** — sketch: an annular sector from two concentric arcs (`magnet_back_r`,
   `magnet_tip_r`) closed by two radial flank lines, centred on pole 0. Constrained: arc
   centres at origin, radius dimensions, angular span dimensioned `magnet_arc_deg`, symmetric
   about the pole centre-line. Extrude over `stack_length`.
2. **Magnet pattern** — `PatternCircular({SourceFeatures:["Magnet"], CountExpr:"poles",
   Angle:"360 deg", AxisPoint:[0,0,0], AxisDir:[0,0,1]})`.

This replaces both the per-pole polyline loops (`magnetLoops`/`magnetSegment`) and the per-pole
extrude loop in `buildMagnetPart`. Body count after the pattern = `poles` (assert this).

### Rotor part

- **Inrunner:** already two parametric circles (`magnet_inner_r`, `rotor_inner_r`) — keep.
- **Outrunner:** replace the two `addClosedPolyline` circle fallbacks with parametric circles
  driven by published outrunner radii (rotor-ring OD and inner face). Publish those radii so the
  outrunner path is literal-free too.

### Parameters

Extend `designParameters` with derived formulas of the existing drivers (no baked geometry):

- `tooth_tip_r`, `slot_bottom_r` — stator tooth radii.
- `tooth_angle` — tooth angular width, e.g. `tooth_width / bore_r` expressed in degrees (or a
  clamped tooth-fraction of the slot pitch, matching `designToothFraction`).
- `magnet_tip_r` (= `rotor_outer_r`), `magnet_back_r` (= `magnet_inner_r`), `magnet_arc_deg`.
- Outrunner rotor-ring radii (ring OD, inner face).

Mapping of these to the sized `Design` fields (`d.ToothTipR`, `d.SlotOuterR`, `d.MagnetArcDeg`,
…) is fixed during planning; each must be a formula of already-published parameters so a driver
edit re-derives them.

### crosssection.go

Delete the faceting layer: `arcPoints`, `circleLoopCM`, `magnetSegment`, `magnetLoops`,
`addClosedPolyline`, and the `CrossSection` struct/fields that only fed sketch polylines. Retain
only what a non-sketch consumer needs (verify `femm.go` first). The `layout` radii logic is kept
and repurposed to drive parameter expressions and the tooth/magnet builders.

### Recompute (the core requirement)

Every radius, dimension, pattern count, angle, and extrude depth is a parameter expression.
After `Generate`, editing a driver in the host parameter table (e.g. `bore_dia`,
`magnet_thick`, `stack_length`) recomputes all three parts in place through the host recompute
engine — **without** re-running `Generate` and **without** recreating sketches. `Generate`'s
clear-and-recreate path stays only for the explicit regenerate action.

## Testing

- **Unit (fake host, `host_fake_test`):** the stator/tooth/magnet builders emit
  `AddArcByCenterStartEndExpr` / `AddLineExpr` / `AddCircleByCenterRadius` with parameter-name
  expressions, the expected `Constrain`/`Dimension` calls, and `PatternCircular` with
  `CountExpr` — and emit **zero** `polyline` entities (explicit regression assertion).
- **Geometry math:** tooth/magnet builder produces the correct number of entities and the
  expected radii/angles for representative inrunner and outrunner specs.
- **DOF:** after building each sketch, `Sketch.ConstraintStatus` reports DOF 0 (fully
  constrained) — drives the "fully constrained" rule.
- **Live MCP test (mandatory before PR):** generate a motor; screenshot the viewport and
  confirm true arcs (smooth tooth tips / magnet faces, not facets); then edit `bore_dia` and
  `magnet_thick` via the parameter API and confirm the three parts recompute in place (body
  counts stable, geometry resized) **without** calling `Generate`; screenshot again.

## Out of scope

- No change to the FEMM hand-off semantics (it sections the solid body, not the sketch).
- No change to the regenerate/collision logic in `Generate` / `clearExistingMotor`.
- No new public API surface.

# Oblikovati Motor Designer

A parametric **electric-motor designer** add-in for the [Oblikovati](https://oblikovati.org)
CAD host. It sizes a rough (~20 % accuracy) permanent-magnet motor cross-section — stator
(slots, teeth, yoke), rotor (poles, back-iron) and magnets (surface / interior PM) — from a
handful of requirement inputs, exposes the design options in a dockable window, and drives the
host (parameters + sketches + features) to generate the first-pass 3D geometry.

## Why "rough"? — the FEMM hand-off

This add-in is the **front of a two-stage workflow**:

1. **Motor Designer (this repo)** — closed-form magnetic + geometric sizing produces a clean,
   exportable cross-section and a named material set (magnet grade, steel grade). It is
   deliberately approximate; the goal is a *valid starting geometry*, not a final design.
2. **[FEMM Bridge](../Oblikovati.AddIns.FEMMBridge)** — takes that cross-section + materials and
   runs a 2D magnetostatic finite-element study to verify and optimize it (airgap flux,
   demagnetization margin, torque).

So the [`Design`](designer/design.go) record is shaped for that hand-off: every dimension is in
millimetres, radii are measured from the axis, and the magnet/steel data ([`materials.go`](designer/materials.go))
carries exactly the magnetic properties (Br, Hcj, recoil µr, lamination loss) a FEMM region needs.

## Architecture

The add-in is a **c-shared library** (`.so`/`.dll`/`.dylib`) the host loads at runtime. It links
**only** the Apache-2.0 public API module `oblikovati.org/api` and reaches the host over the
C ABI (ADR-0016) — never the GPL app internals.

```
export.go / hostcaller.go / manifest.go   ← cgo C-ABI shell (the only cgo in the repo)
designer/                                  ← cgo-free engine (unit-tests on every OS)
  ├─ spec.go        requirement + loading inputs (Spec) and validation
  ├─ sizing.go      pure geometry math, ported from motor-calculator/physics/geometry.ts
  ├─ winding.go     winding-factor analysis, ported from physics/winding.ts
  ├─ materials.go   magnet + steel catalogs, ported from presets.ts
  ├─ design.go      Compute(Spec) → Design (the full cross-section + materials)
  ├─ parameters.go  publishes the parametric program to the host
  ├─ geometry.go    Generate(Spec): document → parameters → sketches → extrudes
  ├─ panel.go       the design-options dockable window
  └─ engine.go      HostCaller transport + Notify event handling
```

The design math is ported from the [motor-calculator](../../motor-calculator) TypeScript app;
each formula cites its source file in a comment.

## Design parameters modeled

**Inputs** ([`Spec`](designer/spec.go)): rated torque & speed, pole/slot count, L/D aspect, torque
per rotor volume (TRV), target airgap/tooth/yoke flux densities, airgap & magnet thickness, magnet
pole-arc fraction, rotor topology (SPM/IPM), magnet grade, steel grade.

**Derived cross-section** ([`Design`](designer/design.go)): bore Ø, stator OD, rotor OD, stack
length, slot pitch, pole pitch, tooth width, slot depth & area, stator/rotor yoke thickness, magnet
inner/outer radii & arc span, airgap shear stress, RMS electric loading, fundamental winding factor,
flux per pole.

## Host geometry generated

`Generate(Spec)` builds a host **assembly** of three separated component parts — so each region
carries its own magnetic material (the FEMM bridge reads a body's material to pick its region, and
the API exposes no per-body reference key for a single multi-body part):

- **Stator** part: a toothed annulus — a smooth yoke boundary + a toothed airgap boundary with
  `Slots` teeth/slots, the teeth pointing at the airgap (inward for an inrunner, outward for an
  outrunner). Steel grade assigned as the part material.
- **Rotor** part: a back-iron annulus with a shaft bore. Steel grade assigned.
- **Magnets** part: one closed permanent-magnet segment per pole (arc `MagnetArcDeg`, thickness
  `MagnetThick`), one body each. Magnet grade assigned.

The three coaxial parts are placed into a **Motor** assembly. The cross-section math
([`crosssection.go`](designer/crosssection.go)) is pure/cgo-free and unit-tested, producing clean
non-self-intersecting closed polylines that section cleanly via `Body.CalculateStrokes`.

**Inrunner vs outrunner**: `Spec.Type` (or the `MotorDesigner.GenerateOutrunner` command) flips the
radial layout so the rotor is inside (inrunner) or an outer ring (outrunner); the teeth always face
the airgap. Both are live-confirmed.

**Re-detection / rebuild**: the design `Spec` is stamped onto the Motor assembly as a JSON attribute
(set `com.oblikovati.motor-designer`), so an opened motor is recognisable (`IsMotorAssembly`) and its
exact inputs are recoverable (`LoadSpec`) to rebuild/adjust when settings change.

## FEMM hand-off

Two complementary mechanisms feed the [FEMM bridge](../Oblikovati.AddIns.FEMMBridge):

1. **Host materials on the bodies.** Each part carries its magnetic material (`electrical-steel-m270`,
   `magnet-ndfeb-n42`, …). These are real host catalog materials whose new **`magnetic` property
   group** (API ≥ 0.69.0: μr, remanence Br, coercivity Hc, saturation Bsat) the FEMM bridge reads to
   build a block material — closing the bridge's previous GAP #3.
2. **A FEMM-ready descriptor** ([`femm.go`](designer/femm.go), `BuildFEMMDescriptor`): the full
   cross-section as named regions (stator + rotor iron + one magnet per pole) in millimetres,
   axis-relative, each carrying Br/Hc/μr and the per-pole magnetisation direction — the data the host
   geometry alone cannot carry.

## Build & run

Local development uses a git-ignored `go.work` that resolves the sibling
`../Oblikovati.API` and `../Oblikovati` checkouts.

```sh
# cgo-free engine: unit-tests on every OS
CGO_ENABLED=0 go test ./designer/...

# the shipped c-shared add-in (+ vendored C ABI header)
make build           # → build/oblikovati-motor-designer.<ext>
make install         # build + copy lib + manifest into the host's add-ins dir
```

In the host the add-in shows a **"Motor Designer"** dockable window on activation; click
**Generate Geometry** to lay the current design down in a new part.

## Status

**Live-confirmed**: the add-in loads in the running Oblikovati host and is drivable over the
MCP bridge — `execute_command MotorDesigner.Generate` produces a "Motor" part with 12
parameters, 2 sketches, and 2 extruded bodies (stator + rotor).

| Area | State |
|------|-------|
| Rough sizing engine (stator/rotor/magnet) | ✅ working, tested |
| Winding-factor analysis | ✅ working, tested |
| Magnet + steel catalogs (FEMM-ready) | ✅ working |
| C-ABI load without deadlocking the head | ✅ working (Setup + Generate run off the session goroutine) |
| `MotorDesigner.Generate` command (MCP-drivable) | ✅ working, live-confirmed |
| Dockable design-options window | ✅ working (read-only labels + Generate button) |
| Host geometry: toothed stator + rotor + per-pole magnets | ✅ working, live-confirmed (assembly of 3 parts) |
| Inrunner + outrunner topology (teeth face the airgap) | ✅ working, live-confirmed both |
| Material assignment (steel/magnet grades on the parts) | ✅ working (verified by part density over MCP) |
| Magnetic material data on host catalog (μr/Br/Hc/Bsat) | ✅ shipped (API `types.Magnetic`, ≥ 0.69.0) |
| FEMM hand-off (host materials + `BuildFEMMDescriptor`) | ✅ working |
| Spec persisted on the assembly (re-detect / rebuild) | ✅ working (attribute set) |
| Error surfacing on Generate | ✅ working (host status bar) |
| Sizing validated vs PM-machine literature | ✅ tested (kw1 tables, σ=TRV/2, T=2σV) |
| Parametric sketch binding | ⏳ literals today — blocked on [Oblikovati.API#187](https://github.com/Oblikovati/Oblikovati.API/issues/187) |
| Editable panel fields | ⏳ stubbed — host panel vocabulary has no input controls yet |
| Assembly viewport shows material appearances | ⛔ blocked on host renderer bug [Oblikovati#1103](https://github.com/Oblikovati/Oblikovati/issues/1103) |

See **[REMAINING-WORK.md](REMAINING-WORK.md)** for the detailed backlog and the live-test recipe.

## License

GPL-2.0-only (every `.go` file carries the SPDX header). The add-in links only the Apache-2.0
`oblikovati.org/api`.

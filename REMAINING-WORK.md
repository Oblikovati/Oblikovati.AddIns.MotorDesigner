# Motor Designer — Remaining Work

Status as of the first live MCP-bridge validation. The add-in **loads in the running
Oblikovati host and is drivable over the MCP bridge**: `execute_command
MotorDesigner.Generate` produces a "Motor" part with 12 parameters, 2 sketches, and 2
extruded bodies (stator + rotor), confirmed live (`/tmp/motor-shot.png`).

## Working (live-confirmed)

- Rough sizing engine (stator/rotor/magnet) — closed-form, ported from `motor-calculator`.
- Winding-factor analysis; magnet + steel catalogs (FEMM-ready material data).
- C-ABI shell loads without deadlocking the head (Setup + Generate run off the session
  goroutine — see the fix commit for why this is mandatory).
- `MotorDesigner.Generate` ribbon command registered; drivable via `execute_command`.
- Dockable "Motor Designer" panel (read-only labels + Generate button).
- Host geometry: 12-parameter program + stator/rotor annular extrudes (2 solid bodies).

## Remaining work

### 1. Parametric sketch binding (blocked on API #187)
Sketch entity radii are currently emitted as **literal millimetre values**, not bound to
the published parameters, because `sketch.addEntity`'s radius field only parses literal
unit expressions — it does not resolve parameter names/formulas
(**Oblikovati.API#187**). Once that lands, switch `geometry.go` back to passing parameter
expressions (`"stator_outer_r"`, `"bore_r"`, …) so editing a parameter in the host
re-drives the geometry (true DOF-0 intent). Until then the model is geometrically correct
but not live-bound to its own parameters.

### 2. True annular profiles / real motor detail
The current cross-section is two concentric circles per body. Next increments:
- **Stator slots + teeth**: replace the plain outer ring with `Slots` tooth/slot profiles
  (tooth width, slot depth, slot-opening `b_0` already sized in `Design`) — one sketch,
  patterned `Slots` times about the axis.
- **Rotor magnets**: place `Poles` magnet segments (arc `MagnetArcDeg`, thickness
  `MagnetThick`) on the rotor surface (SPM) or buried pockets (IPM) — separate bodies with
  the magnet material assigned.
- **Shaft bore** and proper inner/outer trim so the extrudes are true annuli (hollow),
  not solid disks.

### 3. Editable panel inputs (blocked on API panel vocabulary)
The dockable panel renders the spec as labels only; the host's `PanelControlKind` is
`label`/`button`/`separator` — no input fields. The spec can only be changed in code today.
File/track an API request for panel number/dropdown controls, then wire the panel fields to
`Engine.SetSpec`.

### 4. FEMM export hand-off
The `Design` record is already FEMM-export-shaped (mm dimensions, axis-relative radii,
magnet/steel magnetic properties). Still to build: serialize the cross-section + material
map into the form the FEMM bridge (`../Oblikovati.AddIns.FEMMBridge`) consumes, and/or assign
the host `materials` so the bridge reads them off the bodies. This is the point of the whole
add-in (rough design → FEMM magnetostatic optimization).

### 5. Error surfacing
`Generate` errors are currently swallowed in the async `Notify` path. Surface failures in
the panel (a status label) and/or the host notice bar so a bad design is visible to the user
rather than silently producing nothing.

### 6. Material assignment + body naming
Assign the resolved steel grade to the stator/rotor bodies and the magnet grade to the
magnet bodies (via the `materials` client group), and name the bodies/features (Stator,
Rotor, Magnet) so the model tree is legible and the FEMM hand-off can pick regions by name.

## Notes for the next session

- Live-test recipe: `make install` into `../Oblikovati/head/addins`, launch
  `head/cmd/oblikovati-head` with `OBK_ADDINS_DIR` pointing there and `DISPLAY=:1`, wait for
  the MCP bridge port `127.0.0.1:7800`, then drive with an MCP client
  (`Oblikovati.AddIns.MCPBridge/cmd/mcpmotor` is the throwaway driver used for validation).
- A Go c-shared add-in **cannot** be hot-swapped in-process: rebuild → reinstall → restart
  the head to pick up a new `.so`.
- **Never make host calls on the session goroutine** (inside `ObkAddInActivate` or directly
  inside `Notify`) — they deadlock the head (black window / empty geometry). Always dispatch
  to a goroutine, like the MCP bridge does.

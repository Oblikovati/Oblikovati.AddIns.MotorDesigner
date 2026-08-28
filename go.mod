// The oblikovati-motor-designer add-in: a c-shared library (.so/.dll) loaded by the
// host at runtime. It parametrically designs electric-motor cross-sections (stator
// slots/teeth/yoke, rotor poles/back-iron, surface/interior magnets) from a handful of
// requirement inputs, exposes the design options in an Oblikovati dockable window, and
// drives the host API (parameters + sketches + features) to generate the rough first-pass
// 3D geometry. The design targets ~20% accuracy so the cross-section + materials can be
// handed downstream to the FEMM add-in (../Oblikovati.AddIns.FEMMBridge) for
// magnetostatic optimization.
//
// Its own module so the designer deps stay independent of the host — the runtime
// boundary is the C ABI, not Go (see ./include/oblikovati_addin.h).
//
// The SHIPPED library links only the Apache-2.0 contract (oblikovati.org/api). The
// require on the GPL application module (oblikovati) is TEST-SCOPE ONLY — the
// designer<->real-host integration tests drive the live router/model. Both modules are
// sibling repos resolved by the go.work workspace at this repo's root (no committed
// replace); CI injects the equivalent replaces via .github/actions/siblings.
module oblikovati.org/motor-designer

go 1.27.0

require (
	oblikovati.org v0.0.0-00010101000000-000000000000
	oblikovati.org/api v0.154.0
)

require (
	github.com/bitfield/gotestdox v0.2.2 // indirect
	github.com/dnephin/pflag v1.0.7 // indirect
	github.com/fatih/color v1.18.0 // indirect
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	golang.org/x/image v0.0.0-20211028202545-6944b10bf410 // indirect
	golang.org/x/mod v0.38.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/term v0.35.0 // indirect
	golang.org/x/text v0.22.0 // indirect
	golang.org/x/tools v0.48.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	gotest.tools/gotestsum v1.13.0 // indirect
)

tool gotest.tools/gotestsum

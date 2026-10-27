module github.com/Nomadcxx/sysc-terminal

// The module calls x509.Certificate.Verify and http.Client.Do, so it needs a
// standard library without GO-2026-6809 (fixed in go1.26.10) or GO-2026-6617
// (fixed in go1.26.9). CI reads this directive for the toolchain it installs,
// so the floor here is the floor everywhere.
go 1.26.10

require (
	github.com/Nomadcxx/sysc-Go v1.0.11
	github.com/Nomadcxx/sysc-wayland v0.3.3
	github.com/go-text/typesetting v0.3.5
	github.com/mattn/go-runewidth v0.0.31
	golang.org/x/image v0.47.0
	golang.org/x/sys v0.49.0
)

require (
	// hold: the module moved to the charm.land path and the set moves
	// together (see sysc-2021)
	github.com/charmbracelet/colorprofile v0.3.2 // indirect
	// hold: the module moved to the charm.land path and the set moves
	// together (see sysc-2021)
	github.com/charmbracelet/lipgloss/v2 v2.0.0-beta.3.0.20250917201909-41ff0bf215ea // indirect
	// hold: the module moved to the charm.land path and the set moves
	// together (see sysc-2021)
	github.com/charmbracelet/ultraviolet v0.0.0-20250915111650-81d4262876ef // indirect
	// hold: the module moved to the charm.land path and the set moves
	// together (see sysc-2021)
	github.com/charmbracelet/x/ansi v0.10.1 // indirect
	// hold: the module moved to the charm.land path and the set moves
	// together (see sysc-2021)
	github.com/charmbracelet/x/cellbuf v0.0.13 // indirect
	// hold: the module moved to the charm.land path and the set moves
	// together (see sysc-2021)
	github.com/charmbracelet/x/term v0.2.1 // indirect
	// hold: the module moved to the charm.land path and the set moves
	// together (see sysc-2021)
	github.com/charmbracelet/x/termios v0.1.1 // indirect
	// hold: the module moved to the charm.land path and the set moves
	// together (see sysc-2021)
	github.com/charmbracelet/x/windows v0.2.2 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/xo/terminfo v1.2.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	gonum.org/v1/gonum v0.17.0 // indirect
)

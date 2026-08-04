package debugger

import zone "github.com/lrstanley/bubblezone/v2"

// BreakpointView and the control buttons mark mouse zones at render time, so
// the package tests need a zone manager before any View call.
func init() {
	zone.NewGlobal()
}

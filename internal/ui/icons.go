package ui

import (
	"os"
	"strconv"
	"strings"
)

// NerdFontEnabled reports whether UI components may render Nerd Font glyphs.
// Font availability cannot be detected reliably from a terminal, so the
// default preserves the rich UI and users without the font can opt out with
// TEAK_NO_NERD_FONT=1. A present non-boolean value is treated as an opt-out,
// matching the usual environment-variable convention.
func NerdFontEnabled() bool {
	return nerdFontEnabled(os.LookupEnv)
}

func nerdFontEnabled(lookupEnv func(string) (string, bool)) bool {
	value, set := lookupEnv("TEAK_NO_NERD_FONT")
	if !set {
		return true
	}

	disabled, err := strconv.ParseBool(strings.TrimSpace(value))
	return err == nil && !disabled
}

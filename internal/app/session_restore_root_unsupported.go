//go:build js || plan9

package app

// os.Root is documented not to retain directory identity across renames on
// Plan 9 and to have a symlink-validation TOCTOU limitation on js. Disable
// persisted session restoration there rather than weakening confinement.
func sessionRestorePinnedRootSupported() bool { return false }

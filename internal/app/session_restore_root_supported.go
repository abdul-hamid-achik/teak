//go:build !js && !plan9

package app

func sessionRestorePinnedRootSupported() bool { return true }

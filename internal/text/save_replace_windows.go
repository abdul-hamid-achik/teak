//go:build windows

package text

import "golang.org/x/sys/windows"

// replaceFileAtomically uses the Windows replacement primitive rather than a
// delete-then-rename fallback. The latter creates a window where a save can
// lose the destination or be redirected by another process.
func replaceFileAtomically(source, destination string) error {
	sourcePath, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	destinationPath, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(
		sourcePath,
		destinationPath,
		windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH,
	)
}

package text

import (
	"fmt"
	"os"
)

// openRegularBufferFile opens only the same regular file that was inspected.
// The path stat is deliberately performed before opening, so the portable
// fallback rejects obvious non-regular paths before it reaches os.Open. Unix
// implementations additionally use O_NONBLOCK, which closes the TOCTOU window
// where a regular path could otherwise become a FIFO and make the caller hang.
func openRegularBufferFile(path string) (*os.File, error) {
	before, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if err := validateBufferFileInfo(before); err != nil {
		return nil, err
	}

	file, err := openBufferFileReadOnly(path)
	if err != nil {
		return nil, err
	}

	after, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := validateBufferFileInfo(after); err != nil {
		_ = file.Close()
		return nil, err
	}
	if !os.SameFile(before, after) {
		_ = file.Close()
		return nil, fmt.Errorf("%w: %q", ErrBufferFileChanged, path)
	}
	return file, nil
}

func validateBufferFileInfo(info os.FileInfo) error {
	if !info.Mode().IsRegular() {
		return ErrBufferFileNotRegular
	}
	if info.Size() > MaxBufferFileBytes {
		return ErrBufferFileTooLarge
	}
	return nil
}

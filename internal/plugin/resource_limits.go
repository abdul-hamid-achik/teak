package plugin

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ErrPluginResourceLimit identifies input or registry growth rejected before
// it can consume unbounded host memory.
var ErrPluginResourceLimit = errors.New("plugin resource limit exceeded")

const (
	maxPluginManifestBytes         int64 = 64 << 10
	maxPluginSourceBytes           int64 = 2 << 20
	maxLoadedPlugins                     = 64
	maxPluginDirectoryEntries            = 256
	maxPluginKeymaps                     = 512
	maxPluginAutocmds                    = 512
	maxPluginCommands                    = 512
	maxPluginUIConfirmCallbacks          = 256
	maxPluginUIConfirmOptions            = 8
	maxPluginUISelectOptions             = 128
	maxPluginUIInputBytes                = 4096
	maxPluginUIInputPromptBytes          = 4096
	maxPluginUIConfirmMessageBytes       = 16 << 10
	maxPluginUIConfirmOptionBytes        = 256
	maxPluginUISelectOptionBytes         = 256
	maxPluginUIFloatContentBytes         = 64 << 10
	maxPluginUIFloatWidth                = 120
	maxPluginUIFloatHeight               = 40
	maxPluginUIHighlights                = 512
	maxPluginUIHighlightRangeBytes       = 4096
	maxPluginUIHighlightColorBytes       = 64
)

func readPluginRootFile(root *os.Root, name string, limit int64) ([]byte, error) {
	if name == "" || filepath.IsAbs(name) {
		return nil, fmt.Errorf("invalid plugin file path %q", name)
	}
	clean := filepath.Clean(name)
	if clean == "." || clean == ".." || clean != name {
		return nil, fmt.Errorf("invalid plugin file path %q", name)
	}

	// Stat before open avoids blocking on ordinary FIFOs/devices. Root.Open
	// then resolves every path component beneath the pinned plugin directory
	// and rejects symlinks escaping that root.
	info, err := root.Stat(clean)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("plugin file %q is not a regular file", name)
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("%w: %q is %d bytes (max %d)", ErrPluginResourceLimit, name, info.Size(), limit)
	}

	file, err := root.Open(clean)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	if openedInfo, err := file.Stat(); err != nil {
		return nil, err
	} else if !openedInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("plugin file %q changed to a non-regular file", name)
	} else if openedInfo.Size() > limit {
		return nil, fmt.Errorf("%w: %q grew beyond %d bytes", ErrPluginResourceLimit, name, limit)
	}

	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: %q grew beyond %d bytes", ErrPluginResourceLimit, name, limit)
	}
	return data, nil
}

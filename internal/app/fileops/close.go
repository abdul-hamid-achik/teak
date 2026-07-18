package fileops

import (
	"os"
)

type CloseOptions struct {
	Force     bool
	SaveFirst bool
}

func CanClose(path string, opts CloseOptions) (bool, error) {
	if opts.Force {
		return true, nil
	}

	if opts.SaveFirst {
		if !Exists(path) {
			return true, nil
		}
		info, err := Stat(path)
		if err != nil {
			return false, err
		}
		return info.Mode().Perm()&0200 != 0, nil
	}

	return true, nil
}

func EnsureClosed(f *os.File) error {
	if f == nil {
		return nil
	}
	return f.Close()
}

func CloseIfNotNull(f *os.File) error {
	if f != nil {
		return f.Close()
	}
	return nil
}

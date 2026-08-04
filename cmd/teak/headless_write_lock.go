package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"time"
)

const headlessWriteLockTimeout = 2 * time.Second

type headlessWriteLock interface {
	Unlock() error
}

func headlessWriteLockKey(root, path string) (string, error) {
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve workspace for write lock: %w", err)
	}
	pathReal, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve buffer for write lock: %w", err)
	}
	identity := filepath.Clean(rootReal) + "\x00" + filepath.Clean(pathReal)
	digest := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(digest[:]), nil
}

package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// checkLegacyEnv refuses variables with the pre-rename BENEVOLE_ prefix:
// ignoring them would, for one, silently drop the creation password.
func checkLegacyEnv(environ []string) error {
	var old []string
	for _, kv := range environ {
		k, v, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "BENEVOLE_") && v != "" {
			old = append(old, k)
		}
	}
	if len(old) == 0 {
		return nil
	}
	slices.Sort(old)
	return fmt.Errorf("%s: rename BENEVOLE_* variables to JEVIENSAIDER_*", strings.Join(old, ", "))
}

// checkLegacyDB refuses to create a new, empty jeviensaider.db next to a
// pre-rename benevole.db, which would hide every existing event.
func checkLegacyDB(path string) error {
	if filepath.Base(path) != "jeviensaider.db" {
		return nil
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	old := filepath.Join(filepath.Dir(path), "benevole.db")
	if _, err := os.Stat(old); err != nil {
		return nil
	}
	return fmt.Errorf("found %s but no %s: rename it (with its -wal and -shm files, server stopped), or set -db", old, path)
}

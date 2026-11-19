package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckLegacyEnv(t *testing.T) {
	if err := checkLegacyEnv([]string{"JEVIENSAIDER_DB=x", "BENEVOLE_DB=", "PATH=/bin"}); err != nil {
		t.Errorf("new or empty names: %v", err)
	}
	err := checkLegacyEnv([]string{"BENEVOLE_DB=x", "BENEVOLE_CREATE_PASSWORD=s"})
	if err == nil || err.Error() != "BENEVOLE_CREATE_PASSWORD, BENEVOLE_DB: rename BENEVOLE_* variables to JEVIENSAIDER_*" {
		t.Errorf("old names: %v", err)
	}
}

func TestCheckLegacyDB(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jeviensaider.db")
	if err := checkLegacyDB(path); err != nil {
		t.Errorf("fresh install: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "benevole.db"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkLegacyDB(path); err == nil {
		t.Error("old database only: want an error")
	}
	if err := checkLegacyDB(filepath.Join(dir, "other.db")); err != nil {
		t.Errorf("explicit -db: %v", err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkLegacyDB(path); err != nil {
		t.Errorf("both databases: %v", err)
	}
}

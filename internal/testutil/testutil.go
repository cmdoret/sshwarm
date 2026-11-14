// Package testutil holds what the tests of several packages share.
package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

// FakeBin puts an executable shell script called name first on PATH for the rest of
// the test: a stand-in for a command such as ssh, docker or systemctl.
func FakeBin(t *testing.T, name, script string) {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

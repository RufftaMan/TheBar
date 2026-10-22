package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A project name from the URL must never let a backup read escape the backups folder.
func TestReadBackupCannotEscapeBackupDir(t *testing.T) {
	data := t.TempDir()
	secret := filepath.Join(data, "outside.txt")
	if err := os.WriteFile(secret, []byte("TOP SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := &Compose{dataDir: data}
	for _, project := range []string{"..", "../..", "../../" + filepath.Base(data), "a/../.."} {
		if got, err := c.ReadBackup(project, "outside.txt"); err == nil {
			t.Errorf("project %q: read a file outside the backups folder: %q", project, got)
		}
	}
}

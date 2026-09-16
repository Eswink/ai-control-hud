package architecture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsSpecificImportsStayInsidePlatformPackages(t *testing.T) {
	internalRoot := filepath.Clean("..")
	err := filepath.WalkDir(internalRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != internalRoot && filepath.Base(path) == "platform" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(data)
		if strings.Contains(text, `"golang.org/x/sys/windows`) {
			t.Errorf("Windows-specific import escaped platform boundary: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

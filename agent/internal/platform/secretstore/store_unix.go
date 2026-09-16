//go:build linux || darwin

package secretstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func Supported() bool { return true }

func Write(path string, record Record) error {
	if err := record.Validate(); err != nil {
		return err
	}
	return writeProtectedJSON(path, record)
}

func Read(path string) (Record, error) {
	var record Record
	if err := readProtectedJSON(path, &record); err != nil {
		return record, err
	}
	if err := record.Validate(); err != nil {
		return Record{}, err
	}
	return record, nil
}

func WriteHub(path string, record HubRecord) error {
	if err := record.Validate(); err != nil {
		return err
	}
	return writeProtectedJSON(path, record)
}

func ReadHub(path string) (HubRecord, error) {
	var record HubRecord
	if err := readProtectedJSON(path, &record); err != nil {
		return record, err
	}
	if err := record.Validate(); err != nil {
		return HubRecord{}, err
	}
	return record, nil
}

func Remove(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func writeProtectedJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return errors.New("encode secret record failed")
	}
	defer zero(data)

	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create secret store directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return fmt.Errorf("protect secret store directory: %w", err)
	}

	temporary := path + ".tmp"
	backup := path + ".bak"
	_ = os.Remove(temporary)
	_ = os.Remove(backup)
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return fmt.Errorf("write secret store: %w", err)
	}
	if err := os.Chmod(temporary, 0o600); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("protect temporary secret store: %w", err)
	}

	hadExisting := false
	if _, statErr := os.Stat(path); statErr == nil {
		if err := os.Rename(path, backup); err != nil {
			_ = os.Remove(temporary)
			return fmt.Errorf("stage existing secret store: %w", err)
		}
		hadExisting = true
	} else if !errors.Is(statErr, os.ErrNotExist) {
		_ = os.Remove(temporary)
		return fmt.Errorf("inspect existing secret store: %w", statErr)
	}

	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		if hadExisting {
			_ = os.Rename(backup, path)
		}
		return fmt.Errorf("commit secret store: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = os.Remove(path)
		if hadExisting {
			_ = os.Rename(backup, path)
		}
		return fmt.Errorf("protect secret store: %w", err)
	}
	_ = os.Remove(backup)
	return nil
}

func readProtectedJSON(path string, target any) error {
	directory := filepath.Dir(path)
	dirInfo, err := os.Stat(directory)
	if err != nil {
		return fmt.Errorf("read secret store directory: %w", err)
	}
	if !dirInfo.IsDir() {
		return errors.New("secret store parent is not a directory")
	}
	if dirInfo.Mode().Perm()&0o077 != 0 {
		return errors.New("secret store directory permissions are too broad")
	}

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("read secret store: %w", err)
	}
	if info.IsDir() {
		return errors.New("secret store path is a directory")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return errors.New("secret store permissions are too broad")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read secret store: %w", err)
	}
	defer zero(data)
	if err := json.Unmarshal(data, target); err != nil {
		return errors.New("secret store payload is invalid")
	}
	return nil
}

func zero(data []byte) {
	for i := range data {
		data[i] = 0
	}
}

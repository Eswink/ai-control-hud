//go:build windows

package secretstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"github.com/Eswink/ai-control-hud/agent/internal/platform/fileacl"
	"golang.org/x/sys/windows"
)

func Supported() bool { return true }

func Write(path string, record Record) error {
	if err := record.Validate(); err != nil {
		return err
	}
	plain, err := json.Marshal(record)
	if err != nil {
		return errors.New("encode secret record failed")
	}
	cipher, err := protect(plain)
	zero(plain)
	if err != nil {
		return err
	}
	defer zero(cipher)

	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create secret store directory: %w", err)
	}
	if err := fileacl.Protect(directory); err != nil {
		return fmt.Errorf("protect secret store directory: %w", err)
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, cipher, 0o600); err != nil {
		return fmt.Errorf("write secret store: %w", err)
	}
	if err := fileacl.Protect(temporary); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("protect temporary secret store: %w", err)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(temporary)
		return fmt.Errorf("replace secret store: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("commit secret store: %w", err)
	}
	if err := fileacl.Protect(path); err != nil {
		return fmt.Errorf("protect secret store: %w", err)
	}
	return nil
}

func Read(path string) (Record, error) {
	var record Record
	cipher, err := os.ReadFile(path)
	if err != nil {
		return record, fmt.Errorf("read secret store: %w", err)
	}
	defer zero(cipher)
	plain, err := unprotect(cipher)
	if err != nil {
		return record, err
	}
	defer zero(plain)
	if err := json.Unmarshal(plain, &record); err != nil {
		return record, errors.New("secret store payload is invalid")
	}
	if err := record.Validate(); err != nil {
		return Record{}, err
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

func protect(data []byte) ([]byte, error) {
	input := dataBlob(data)
	var output windows.DataBlob
	flags := uint32(windows.CRYPTPROTECT_LOCAL_MACHINE | windows.CRYPTPROTECT_UI_FORBIDDEN)
	if err := windows.CryptProtectData(&input, nil, nil, 0, nil, flags, &output); err != nil {
		return nil, errors.New("DPAPI protect failed")
	}
	return copyAndFreeBlob(&output), nil
}

func unprotect(data []byte) ([]byte, error) {
	input := dataBlob(data)
	var output windows.DataBlob
	flags := uint32(windows.CRYPTPROTECT_UI_FORBIDDEN)
	if err := windows.CryptUnprotectData(&input, nil, nil, 0, nil, flags, &output); err != nil {
		return nil, errors.New("DPAPI unprotect failed")
	}
	return copyAndFreeBlob(&output), nil
}

func dataBlob(data []byte) windows.DataBlob {
	if len(data) == 0 {
		return windows.DataBlob{}
	}
	return windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
}

func copyAndFreeBlob(blob *windows.DataBlob) []byte {
	if blob == nil || blob.Size == 0 || blob.Data == nil {
		return nil
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(blob.Data)))
	view := unsafe.Slice(blob.Data, int(blob.Size))
	result := make([]byte, len(view))
	copy(result, view)
	return result
}

func zero(data []byte) {
	for i := range data {
		data[i] = 0
	}
}

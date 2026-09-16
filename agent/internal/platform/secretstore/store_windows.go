//go:build windows

package secretstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

const protectedFileSDDL = "D:P(A;;FA;;;SY)(A;;FA;;;BA)"

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
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, cipher, 0o600); err != nil {
		return fmt.Errorf("write secret store: %w", err)
	}
	if err := protectACL(temporary); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	_ = os.Remove(path)
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("commit secret store: %w", err)
	}
	if err := protectACL(path); err != nil {
		return err
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

func protectACL(path string) error {
	descriptor, err := windows.SecurityDescriptorFromString(protectedFileSDDL)
	if err != nil {
		return errors.New("build secret store ACL failed")
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return errors.New("read secret store ACL failed")
	}
	information := windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, information, nil, nil, dacl, nil); err != nil {
		return errors.New("apply secret store ACL failed")
	}
	return nil
}

func zero(data []byte) {
	for i := range data {
		data[i] = 0
	}
}

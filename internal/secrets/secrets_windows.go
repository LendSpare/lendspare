//go:build windows

package secrets

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

// Windows Credential Manager through advapi32 (CredWriteW / CredReadW / CredDeleteW), like the
// rest of this program's Windows code: no third-party module. Generic credentials, kept for
// this user on this machine only (CRED_PERSIST_LOCAL_MACHINE).

var (
	credAPI     = syscall.NewLazyDLL("advapi32.dll")
	pCredWrite  = credAPI.NewProc("CredWriteW")
	pCredRead   = credAPI.NewProc("CredReadW")
	pCredDelete = credAPI.NewProc("CredDeleteW")
	pCredFree   = credAPI.NewProc("CredFree")
)

const (
	credTypeGeneric       = 1
	credPersistLocalMachn = 2
	errorNotFound         = 1168
)

type credential struct {
	flags              uint32
	typ                uint32
	targetName         *uint16
	comment            *uint16
	lastWritten        syscall.Filetime
	credentialBlobSize uint32
	credentialBlob     *byte
	persist            uint32
	attributeCount     uint32
	attributes         uintptr
	targetAlias        *uint16
	userName           *uint16
}

type credManager struct{}

func system() Store { return credManager{} }

func credTarget(name string) *uint16 { return credUTF16("LendSpare/" + name) }

func (credManager) Get(name string) (string, error) {
	var p *credential
	r, _, errno := pCredRead.Call(uintptr(unsafe.Pointer(credTarget(name))), credTypeGeneric, 0, uintptr(unsafe.Pointer(&p)))
	if r == 0 {
		if errors.Is(errno, syscall.Errno(errorNotFound)) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("credential manager: %w", errno)
	}
	defer pCredFree.Call(uintptr(unsafe.Pointer(p)))
	blob := unsafe.Slice(p.credentialBlob, p.credentialBlobSize)
	return string(blob), nil
}

func (credManager) Set(name, value string) error {
	if value == "" {
		return errors.New("empty secret")
	}
	b := []byte(value)
	c := credential{typ: credTypeGeneric, targetName: credTarget(name), credentialBlobSize: uint32(len(b)),
		credentialBlob: &b[0], persist: credPersistLocalMachn, userName: credUTF16("LendSpare")}
	r, _, errno := pCredWrite.Call(uintptr(unsafe.Pointer(&c)), 0)
	if r == 0 {
		return fmt.Errorf("credential manager: %w", errno)
	}
	return nil
}

func (credManager) Delete(name string) error {
	r, _, errno := pCredDelete.Call(uintptr(unsafe.Pointer(credTarget(name))), credTypeGeneric, 0)
	if r == 0 {
		if errors.Is(errno, syscall.Errno(errorNotFound)) {
			return nil
		}
		return fmt.Errorf("credential manager: %w", errno)
	}
	return nil
}

func credUTF16(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

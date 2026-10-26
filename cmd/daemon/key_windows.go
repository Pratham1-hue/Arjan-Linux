//go:build windows

package main

import (
	"encoding/base64"
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// protectAPIKey encrypts the key with Windows DPAPI, so only this
// Windows user (on this machine) can decrypt config.json's copy.
func protectAPIKey(key string) (string, error) {
	data := []byte(key)
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var out windows.DataBlob
	if err := windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return "", err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	enc := append([]byte(nil), unsafe.Slice(out.Data, out.Size)...)
	return base64.StdEncoding.EncodeToString(enc), nil
}

func unprotectAPIKey(enc string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", err
	}
	if len(raw) == 0 {
		return "", errors.New("empty ciphertext")
	}
	in := windows.DataBlob{Size: uint32(len(raw)), Data: &raw[0]}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return "", err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return string(append([]byte(nil), unsafe.Slice(out.Data, out.Size)...)), nil
}

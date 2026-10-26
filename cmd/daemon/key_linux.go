//go:build linux

package main

import (
	"encoding/base64"
	"errors"
	"strings"
)

// Windows protects the stored API key with DPAPI. Linux has no
// equivalent that works without an extra keyring daemon, so the key is
// kept base64-encoded in config.json, which is written with mode 0600
// (only your user can read it). The "plain:" prefix records the
// format, so a config.json copied from a Windows machine is detected
// rather than silently producing an empty key.
const plainKeyPrefix = "plain:"

func protectAPIKey(key string) (string, error) {
	return plainKeyPrefix + base64.StdEncoding.EncodeToString([]byte(key)), nil
}

func unprotectAPIKey(enc string) (string, error) {
	if !strings.HasPrefix(enc, plainKeyPrefix) {
		return "", errors.New("API key was saved on another OS — re-enter it in Settings")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(enc, plainKeyPrefix))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

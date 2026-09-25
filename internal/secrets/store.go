// Package secrets stores credentials such as the Nanoleaf auth token.
//
// It exposes a small Store interface and two implementations:
//
//	keyring.go   the desktop keyring through the Secret Service D-Bus API
//	             (GNOME Keyring, KDE Wallet via ksecretd). The default.
//	file.go      a JSON file with 0600 permissions. The fallback for
//	             machines without a keyring, or when asked explicitly.
//
// Non-secret configuration (device id, address) does not belong here; see
// package config.
package secrets

import (
	"errors"
	"fmt"
	"log"
)

// Store is the minimal key/value contract both backends satisfy.
//
// An interface in Go is a set of method signatures. Any type that has these
// methods implements the interface automatically; there is no "implements"
// keyword. Code that takes a Store works with either backend.
type Store interface {
	// Name describes the backend for messages: "keyring" or "file <path>".
	Name() string
	Get(key string) (string, error)
	Set(key, value string) error
	Delete(key string) error
}

// ErrNotFound is returned by Get and Delete when the key does not exist.
var ErrNotFound = errors.New("secret not found")

// Backend names accepted by Open.
const (
	BackendAuto    = "auto"
	BackendKeyring = "keyring"
	BackendFile    = "file"
)

// Open returns the requested backend. With BackendAuto it prefers the
// keyring and falls back to the file when no Secret Service is reachable
// (for example over SSH without a desktop session). filePath is the location
// of the fallback file. log receives debug output; nil disables it.
func Open(backend, filePath string, log *log.Logger) (Store, error) {
	switch backend {
	case BackendKeyring:
		return openKeyring(log)
	case BackendFile:
		return openFile(filePath, log)
	case BackendAuto, "":
		store, err := openKeyring(log)
		if err == nil {
			return store, nil
		}
		debugf(log, "secrets: keyring unavailable (%v), falling back to file store", err)
		return openFile(filePath, log)
	default:
		return nil, fmt.Errorf("unknown secret store %q (want auto, keyring or file)", backend)
	}
}

// debugf mirrors the helper in package hue: silent when the logger is nil.
func debugf(l *log.Logger, format string, args ...any) {
	if l != nil {
		l.Printf(format, args...)
	}
}

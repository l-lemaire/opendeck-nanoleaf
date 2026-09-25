package secrets

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// fileStore keeps secrets in a JSON file readable only by the owner.
//
// This is weaker than the keyring: anyone who can read the user's files
// (a backup, a copied home directory, malware running as the user) reads the
// keys. It exists for headless machines and is chosen only explicitly or
// when no keyring is reachable, and the CLI warns when that happens.
type fileStore struct {
	path string
	log  *log.Logger
}

// fileFormat is the on-disk layout. A version field lets us migrate later.
type fileFormat struct {
	Version int               `json:"version"`
	Secrets map[string]string `json:"secrets"`
}

func openFile(path string, log *log.Logger) (Store, error) {
	if path == "" {
		return nil, errors.New("file store: no path given")
	}
	debugf(log, "secrets: using file store %s", path)
	return fileStore{path: path, log: log}, nil
}

func (f fileStore) Name() string { return "file " + f.path }

func (f fileStore) Get(key string) (string, error) {
	data, err := f.load()
	if err != nil {
		return "", err
	}
	value, ok := data.Secrets[key]
	if !ok {
		return "", ErrNotFound
	}
	debugf(f.log, "secrets: file get %s", key)
	return value, nil
}

func (f fileStore) Set(key, value string) error {
	data, err := f.load()
	if err != nil {
		return err
	}
	data.Secrets[key] = value
	debugf(f.log, "secrets: file set %s (%d bytes)", key, len(value))
	return f.save(data)
}

func (f fileStore) Delete(key string) error {
	data, err := f.load()
	if err != nil {
		return err
	}
	if _, ok := data.Secrets[key]; !ok {
		return ErrNotFound
	}
	delete(data.Secrets, key) // built-in for removing a map entry
	debugf(f.log, "secrets: file delete %s", key)
	return f.save(data)
}

// load reads the file; a missing file is an empty store, not an error.
func (f fileStore) load() (fileFormat, error) {
	data := fileFormat{Version: 1, Secrets: map[string]string{}}
	raw, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return data, nil
	}
	if err != nil {
		return data, fmt.Errorf("file store: %w", err)
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return data, fmt.Errorf("file store %s is corrupt: %w", f.path, err)
	}
	if data.Secrets == nil {
		data.Secrets = map[string]string{}
	}
	return data, nil
}

// save writes atomically: to a temporary file with 0600 permissions in the
// same directory, then rename over the target. A crash mid-write leaves the
// old file intact, and the secrets are never on disk with loose permissions.
func (f fileStore) save(data fileFormat) error {
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return fmt.Errorf("file store: %w", err)
	}
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(f.path), ".credentials-*.tmp")
	if err != nil {
		return fmt.Errorf("file store: %w", err)
	}
	tmpName := tmp.Name()
	// On any failure below, remove the temp file. os.Remove on an already
	// renamed file just fails silently, which is what we want.
	defer os.Remove(tmpName)

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, f.path)
}

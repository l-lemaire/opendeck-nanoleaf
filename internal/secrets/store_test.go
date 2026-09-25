package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zalando/go-keyring"
)

// exercise runs the same scenario against any Store.
func exercise(t *testing.T, s Store) {
	t.Helper()
	if _, err := s.Get("bridge1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get on empty store: got %v, want ErrNotFound", err)
	}
	if err := s.Set("bridge1", `{"app_key":"k1"}`); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("bridge2", `{"app_key":"k2"}`); err != nil {
		t.Fatal(err)
	}
	if v, err := s.Get("bridge1"); err != nil || v != `{"app_key":"k1"}` {
		t.Errorf("Get bridge1 = %q, %v", v, err)
	}
	if err := s.Set("bridge1", "updated"); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.Get("bridge1"); v != "updated" {
		t.Errorf("after update: %q", v)
	}
	if err := s.Delete("bridge1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("bridge1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("after delete: %v", err)
	}
	if err := s.Delete("bridge1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("double delete: got %v, want ErrNotFound", err)
	}
	if v, _ := s.Get("bridge2"); v != `{"app_key":"k2"}` {
		t.Errorf("bridge2 damaged: %q", v)
	}
}

func TestFileStore(t *testing.T) {
	// t.TempDir gives a fresh directory removed when the test ends.
	path := filepath.Join(t.TempDir(), "sub", "credentials.json")
	s, err := Open(BackendFile, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	exercise(t, s)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("credentials file has permissions %o, want 600", perm)
	}
	dirInfo, _ := os.Stat(filepath.Dir(path))
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("directory has permissions %o, want 700", perm)
	}
	// No temp file left behind.
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("unexpected files in store dir: %v", entries)
	}
}

func TestFileStoreCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	os.WriteFile(path, []byte("not json"), 0o600)
	s, _ := Open(BackendFile, path, nil)
	if _, err := s.Get("x"); err == nil {
		t.Error("expected an error on corrupt file")
	}
}

func TestKeyringStoreWithMock(t *testing.T) {
	// MockInit swaps the real keyring for an in-memory one for this process.
	keyring.MockInit()
	s, err := Open(BackendKeyring, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name() != "keyring" {
		t.Errorf("Name = %q", s.Name())
	}
	exercise(t, s)
}

func TestOpenAutoFallsBackToFile(t *testing.T) {
	keyring.MockInitWithError(errors.New("no dbus session"))
	path := filepath.Join(t.TempDir(), "credentials.json")
	s, err := Open(BackendAuto, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name() != "file "+path {
		t.Errorf("expected file fallback, got %q", s.Name())
	}
}

func TestOpenUnknownBackend(t *testing.T) {
	if _, err := Open("vault", "", nil); err == nil {
		t.Error("expected an error")
	}
}

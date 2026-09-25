package secrets

import (
	"errors"
	"fmt"
	"log"

	"github.com/zalando/go-keyring"
)

// keyringStore keeps secrets in the desktop keyring.
//
// On Linux the library speaks the Secret Service API over the user's session
// D-Bus to whatever daemon owns org.freedesktop.secrets (gnome-keyring,
// ksecretd, KeePassXC...). Secrets are encrypted at rest by that daemon and
// unlocked with the user's login. The D-Bus transport itself is the "plain"
// session type: the value crosses a local socket only this user can read.
//
// On macOS it uses the Keychain and on Windows the Credential Manager, so
// the future plugin gets sensible storage everywhere without extra code.
type keyringStore struct {
	log *log.Logger
}

// service is the namespace all our entries live under. In a keyring UI the
// entries show up as service "opendeck-nanoleaf" with the device id as the
// account name.
const service = "opendeck-nanoleaf"

// openKeyring checks that a keyring is actually reachable before returning
// the store, so the caller can fall back cleanly.
func openKeyring(log *log.Logger) (Store, error) {
	// Looking up a key that cannot exist is the cheapest end-to-end probe:
	// ErrNotFound proves the daemon answered; anything else means trouble
	// (no D-Bus session, no daemon, locked wallet the user declined...).
	_, err := keyring.Get(service, "probe-does-not-exist")
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return nil, fmt.Errorf("keyring: %w", err)
	}
	debugf(log, "secrets: keyring reachable (service %q)", service)
	return keyringStore{log: log}, nil
}

func (k keyringStore) Name() string { return "keyring" }

func (k keyringStore) Get(key string) (string, error) {
	debugf(k.log, "secrets: keyring get %s/%s", service, key)
	value, err := keyring.Get(service, key)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("keyring: %w", err)
	}
	return value, nil
}

func (k keyringStore) Set(key, value string) error {
	debugf(k.log, "secrets: keyring set %s/%s (%d bytes)", service, key, len(value))
	if err := keyring.Set(service, key, value); err != nil {
		return fmt.Errorf("keyring: %w", err)
	}
	return nil
}

func (k keyringStore) Delete(key string) error {
	debugf(k.log, "secrets: keyring delete %s/%s", service, key)
	err := keyring.Delete(service, key)
	if errors.Is(err, keyring.ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("keyring: %w", err)
	}
	return nil
}

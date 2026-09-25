package nanoleaf

import (
	"encoding/json"
	"fmt"

	"github.com/l-lemaire/opendeck-nanoleaf/internal/secrets"
)

// Credentials are the secret a pairing produces: one auth token per device.
type Credentials struct {
	Token string `json:"token"`
}

// SaveCredentials writes the credentials for deviceID into the store.
func SaveCredentials(store secrets.Store, deviceID string, creds Credentials) error {
	encoded, err := json.Marshal(creds)
	if err != nil {
		return err
	}
	return store.Set(deviceID, string(encoded))
}

// LoadCredentials reads the credentials for deviceID. It returns
// secrets.ErrNotFound (wrapped) when the device was never paired.
func LoadCredentials(store secrets.Store, deviceID string) (Credentials, error) {
	raw, err := store.Get(deviceID)
	if err != nil {
		return Credentials{}, fmt.Errorf("credentials for device %s: %w", deviceID, err)
	}
	var creds Credentials
	if err := json.Unmarshal([]byte(raw), &creds); err != nil {
		return Credentials{}, fmt.Errorf("credentials for device %s are corrupt: %w", deviceID, err)
	}
	return creds, nil
}

package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// actionPrefix is shared by the action UUIDs in plugin/manifest.json.
const actionPrefix = "com.github.l-lemaire.nanoleaf."

// actionToggle is the one action for now.
const actionToggle = actionPrefix + "toggle-device"

// Settings is what a button remembers. The property inspector writes it
// with setSettings; OpenDeck stores it in the profile and hands it back in
// every event about the button. Nothing here is secret.
type Settings struct {
	// Device is the paired device's id (its mDNS id). Empty means none
	// chosen yet.
	Device string `json:"device,omitempty"`
	// Name is the device's name at the time it was chosen, for the title.
	Name string `json:"name,omitempty"`

	// Label controls the text the plugin writes on the key:
	//   ""  or "name"  the device's name (default)
	//   "custom"       the text in CustomLabel
	//   "none"         nothing from the plugin; OpenDeck's own title
	//                  settings for the key apply
	Label       string `json:"label,omitempty"`
	CustomLabel string `json:"custom_label,omitempty"`
}

// title returns the text the plugin should put on the key, and whether it
// should touch the title at all.
func (s Settings) title() (text string, set bool) {
	switch s.Label {
	case "none":
		return "", true
	case "custom":
		return s.CustomLabel, true
	default:
		if s.Name == "" {
			return "", false
		}
		return s.Name, true
	}
}

// decodeSettings turns the raw JSON from an event into Settings. Empty or
// absent settings decode to the zero value.
func decodeSettings(raw json.RawMessage) (Settings, error) {
	var s Settings
	if len(raw) == 0 || string(raw) == "null" {
		return s, nil
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return s, fmt.Errorf("decode settings: %w", err)
	}
	return s, nil
}

// checkAction rejects events for actions this plugin does not declare.
func checkAction(actionUUID string) error {
	if actionUUID != actionToggle {
		return fmt.Errorf("unknown action %q", actionUUID)
	}
	return nil
}

// shortAction strips the plugin prefix for log lines.
func shortAction(uuid string) string {
	return strings.TrimPrefix(uuid, actionPrefix)
}

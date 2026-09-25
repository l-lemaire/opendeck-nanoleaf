package openaction

import "encoding/json"

// Event is any message from the host. The fields present depend on the
// event name; Payload is kept raw and decoded by the dispatcher into one of
// the typed payload structs below once the event name is known.
type Event struct {
	Event   string          `json:"event"`
	Action  string          `json:"action,omitempty"`  // action UUID from the manifest
	Context string          `json:"context,omitempty"` // button instance id
	Device  string          `json:"device,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Names of the incoming events this package understands.
const (
	EventWillAppear                 = "willAppear"
	EventWillDisappear              = "willDisappear"
	EventKeyDown                    = "keyDown"
	EventKeyUp                      = "keyUp"
	EventDidReceiveSettings         = "didReceiveSettings"
	EventDidReceiveGlobalSettings   = "didReceiveGlobalSettings"
	EventSendToPlugin               = "sendToPlugin"
	EventPropertyInspectorDidAppear = "propertyInspectorDidAppear"
	EventSystemDidWakeUp            = "systemDidWakeUp"
	EventDeviceDidConnect           = "deviceDidConnect"
	EventDeviceDidDisconnect        = "deviceDidDisconnect"
)

// Coordinates locate a button on the deck.
type Coordinates struct {
	Row    int `json:"row"`
	Column int `json:"column"`
}

// AppearPayload accompanies willAppear and willDisappear.
type AppearPayload struct {
	// Settings are whatever the property inspector stored for this button.
	// Raw so the plugin decodes them into its own struct.
	Settings        json.RawMessage `json:"settings"`
	Coordinates     Coordinates     `json:"coordinates"`
	Controller      string          `json:"controller"` // "Keypad" or "Encoder"
	State           int             `json:"state"`
	IsInMultiAction bool            `json:"isInMultiAction"`
}

// KeyPayload accompanies keyDown and keyUp.
type KeyPayload struct {
	Settings        json.RawMessage `json:"settings"`
	Coordinates     Coordinates     `json:"coordinates"`
	State           int             `json:"state"`
	IsInMultiAction bool            `json:"isInMultiAction"`
}

// SettingsPayload accompanies didReceiveSettings.
type SettingsPayload struct {
	Settings        json.RawMessage `json:"settings"`
	Coordinates     Coordinates     `json:"coordinates"`
	IsInMultiAction bool            `json:"isInMultiAction"`
}

// GlobalSettingsPayload accompanies didReceiveGlobalSettings.
type GlobalSettingsPayload struct {
	Settings json.RawMessage `json:"settings"`
}

// Outgoing messages. Each is a small struct so the JSON shape is visible
// here rather than assembled from maps at the call sites.

type registerMessage struct {
	Event string `json:"event"`
	UUID  string `json:"uuid"`
}

// contextMessage covers events that carry only a context: showAlert, showOk,
// getSettings.
type contextMessage struct {
	Event   string `json:"event"`
	Context string `json:"context"`
}

type setStateMessage struct {
	Event   string `json:"event"`
	Context string `json:"context"`
	Payload struct {
		State int `json:"state"`
	} `json:"payload"`
}

// Target values for setTitle/setImage: which display to update.
const (
	TargetBoth     = 0
	TargetHardware = 1
	TargetSoftware = 2
)

type setTitleMessage struct {
	Event   string `json:"event"`
	Context string `json:"context"`
	Payload struct {
		Title  string `json:"title"`
		Target int    `json:"target"`
		// State is a pointer so "all states" can be expressed by omitting it.
		State *int `json:"state,omitempty"`
	} `json:"payload"`
}

type setImageMessage struct {
	Event   string `json:"event"`
	Context string `json:"context"`
	Payload struct {
		Image  string `json:"image"` // base64 data URL, or "" to restore the manifest image
		Target int    `json:"target"`
		State  *int   `json:"state,omitempty"`
	} `json:"payload"`
}

type settingsMessage struct {
	Event   string `json:"event"`
	Context string `json:"context"`
	Payload any    `json:"payload"`
}

type sendToPIMessage struct {
	Event   string `json:"event"`
	Action  string `json:"action"`
	Context string `json:"context"`
	Payload any    `json:"payload"`
}

type logMessage struct {
	Event   string `json:"event"`
	Payload struct {
		Message string `json:"message"`
	} `json:"payload"`
}

type openURLMessage struct {
	Event   string `json:"event"`
	Payload struct {
		URL string `json:"url"`
	} `json:"payload"`
}

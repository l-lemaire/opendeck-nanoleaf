package openaction

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/coder/websocket"
)

// Conn is the registered WebSocket connection to the host.
//
// Sends may happen from several goroutines at once (the library allows
// concurrent writes). Receive must be called from one goroutine only, which
// Run takes care of.
type Conn struct {
	ws   *websocket.Conn
	args Args
	log  *log.Logger // nil = silent
}

// Connect dials the host and sends the registration message. It returns
// once the host has the message; the host sends nothing in reply.
func Connect(ctx context.Context, args Args, logger *log.Logger) (*Conn, error) {
	url := "ws://127.0.0.1:" + strconv.Itoa(args.Port)
	ws, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to host at %s: %w", url, err)
	}
	// Incoming messages are small JSON events; settings could in theory be
	// larger. 4 MiB is generous and still protects against a runaway host.
	ws.SetReadLimit(4 << 20)

	c := &Conn{ws: ws, args: args, log: logger}
	if err := c.send(ctx, registerMessage{Event: args.RegisterEvent, UUID: args.PluginUUID}); err != nil {
		ws.CloseNow()
		return nil, fmt.Errorf("register with host: %w", err)
	}
	debugf(logger, "openaction: connected to %s and registered as %s", url, args.PluginUUID)
	return c, nil
}

// Close performs the WebSocket closing handshake.
func (c *Conn) Close() error {
	return c.ws.Close(websocket.StatusNormalClosure, "plugin exiting")
}

// Receive waits for the next event from the host. It returns an error when
// the connection is closed or ctx ends.
func (c *Conn) Receive(ctx context.Context) (Event, error) {
	_, data, err := c.ws.Read(ctx)
	if err != nil {
		return Event{}, err
	}
	debugf(c.log, "openaction: <- %s", data)
	var ev Event
	if err := json.Unmarshal(data, &ev); err != nil {
		return Event{}, fmt.Errorf("host sent invalid JSON: %w", err)
	}
	return ev, nil
}

// send marshals v and writes it as one text message. Every outgoing event
// goes through here, so the debug log sees all of them.
func (c *Conn) send(ctx context.Context, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	debugf(c.log, "openaction: -> %s", data)
	// A bounded timeout so a stuck host cannot block the plugin forever.
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return c.ws.Write(ctx, websocket.MessageText, data)
}

// SetState switches a button to state index n (0-based, as listed in the
// manifest's States array).
func (c *Conn) SetState(ctx context.Context, buttonContext string, n int) error {
	m := setStateMessage{Event: "setState", Context: buttonContext}
	m.Payload.State = n
	return c.send(ctx, m)
}

// SetTitle sets the text shown on a button, for all its states.
func (c *Conn) SetTitle(ctx context.Context, buttonContext, title string) error {
	m := setTitleMessage{Event: "setTitle", Context: buttonContext}
	m.Payload.Title = title
	m.Payload.Target = TargetBoth
	return c.send(ctx, m)
}

// SetImage replaces a button's image with a base64 data URL such as
// "data:image/svg+xml;base64,....". An empty string restores the manifest
// image.
func (c *Conn) SetImage(ctx context.Context, buttonContext, dataURL string) error {
	m := setImageMessage{Event: "setImage", Context: buttonContext}
	m.Payload.Image = dataURL
	m.Payload.Target = TargetBoth
	return c.send(ctx, m)
}

// ShowAlert flashes the warning triangle on a button.
func (c *Conn) ShowAlert(ctx context.Context, buttonContext string) error {
	return c.send(ctx, contextMessage{Event: "showAlert", Context: buttonContext})
}

// ShowOk flashes the check mark on a button.
func (c *Conn) ShowOk(ctx context.Context, buttonContext string) error {
	return c.send(ctx, contextMessage{Event: "showOk", Context: buttonContext})
}

// SetSettings stores settings for a button. The host persists them in the
// profile and sends them back in every later event about that button.
func (c *Conn) SetSettings(ctx context.Context, buttonContext string, settings any) error {
	return c.send(ctx, settingsMessage{Event: "setSettings", Context: buttonContext, Payload: settings})
}

// GetSettings asks the host to send a didReceiveSettings event for a button.
func (c *Conn) GetSettings(ctx context.Context, buttonContext string) error {
	return c.send(ctx, contextMessage{Event: "getSettings", Context: buttonContext})
}

// SetGlobalSettings stores plugin-wide settings. Never put secrets here:
// the host writes them to disk in plain text.
func (c *Conn) SetGlobalSettings(ctx context.Context, settings any) error {
	return c.send(ctx, settingsMessage{Event: "setGlobalSettings", Context: c.args.PluginUUID, Payload: settings})
}

// GetGlobalSettings asks for a didReceiveGlobalSettings event.
func (c *Conn) GetGlobalSettings(ctx context.Context) error {
	return c.send(ctx, contextMessage{Event: "getGlobalSettings", Context: c.args.PluginUUID})
}

// SendToPropertyInspector delivers a payload to the open property inspector
// of a button.
func (c *Conn) SendToPropertyInspector(ctx context.Context, action, buttonContext string, payload any) error {
	return c.send(ctx, sendToPIMessage{Event: "sendToPropertyInspector", Action: action, Context: buttonContext, Payload: payload})
}

// LogMessage writes a line into the host's own log file.
func (c *Conn) LogMessage(ctx context.Context, message string) error {
	m := logMessage{Event: "logMessage"}
	m.Payload.Message = message
	return c.send(ctx, m)
}

// OpenURL asks the host to open a URL in the user's browser.
func (c *Conn) OpenURL(ctx context.Context, url string) error {
	m := openURLMessage{Event: "openUrl"}
	m.Payload.URL = url
	return c.send(ctx, m)
}

func debugf(l *log.Logger, format string, args ...any) {
	if l != nil {
		l.Printf(format, args...)
	}
}

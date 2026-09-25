package openaction

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeHost is an in-process stand-in for OpenDeck: a WebSocket server that
// records what the plugin sends and lets the test push events to it.
type fakeHost struct {
	server   *httptest.Server
	port     int
	conn     chan *websocket.Conn // the accepted plugin connection
	received chan map[string]any  // messages from the plugin, decoded
}

func newFakeHost(t *testing.T) *fakeHost {
	t.Helper()
	fh := &fakeHost{conn: make(chan *websocket.Conn, 1), received: make(chan map[string]any, 64)}
	fh.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		fh.conn <- c
		// Read everything the plugin sends until it goes away.
		for {
			_, data, err := c.Read(context.Background())
			if err != nil {
				return
			}
			var m map[string]any
			if err := json.Unmarshal(data, &m); err != nil {
				t.Errorf("plugin sent invalid JSON: %s", data)
				continue
			}
			fh.received <- m
		}
	}))
	t.Cleanup(fh.server.Close)
	_, portText, _ := strings.Cut(strings.TrimPrefix(fh.server.URL, "http://127.0.0.1"), ":")
	fh.port, _ = strconv.Atoi(portText)
	return fh
}

// next returns the next message from the plugin or fails after a timeout.
func (fh *fakeHost) next(t *testing.T) map[string]any {
	t.Helper()
	select {
	case m := <-fh.received:
		return m
	case <-time.After(2 * time.Second):
		t.Fatal("plugin sent nothing within 2s")
		return nil
	}
}

// push sends an event to the plugin.
func (fh *fakeHost) push(t *testing.T, c *websocket.Conn, v any) {
	t.Helper()
	data, _ := json.Marshal(v)
	if err := c.Write(context.Background(), websocket.MessageText, data); err != nil {
		t.Fatal(err)
	}
}

func testLogger() *log.Logger {
	if testing.Verbose() {
		return log.New(os.Stderr, "    debug: ", 0)
	}
	return nil
}

func testArgs(port int) Args {
	return Args{Port: port, PluginUUID: "com.example.test", RegisterEvent: "registerPlugin"}
}

func TestParseArgs(t *testing.T) {
	info := `{"application":{"platform":"linux","version":"2.14.0"},"plugin":{"uuid":"com.example.test","version":"0.1"},"devices":[{"id":"sd-1","name":"Stream Deck","size":{"rows":3,"columns":5}}]}`
	a, err := ParseArgs([]string{"-port", "57116", "-pluginUUID", "com.example.test", "-registerEvent", "registerPlugin", "-info", info})
	if err != nil {
		t.Fatal(err)
	}
	if a.Port != 57116 || a.PluginUUID != "com.example.test" || a.RegisterEvent != "registerPlugin" {
		t.Errorf("args = %+v", a)
	}
	if a.Info.Application.Platform != "linux" || len(a.Info.Devices) != 1 || a.Info.Devices[0].Size.Columns != 5 {
		t.Errorf("info = %+v", a.Info)
	}

	bad := [][]string{
		{},
		{"-port", "0", "-pluginUUID", "x", "-registerEvent", "r"},
		{"-port", "1", "-registerEvent", "r"},
		{"-port", "1", "-pluginUUID", "x"},
		{"-port", "1", "-pluginUUID", "x", "-registerEvent", "r", "-info", "{not json"},
		{"-port", "1", "-pluginUUID", "x", "-registerEvent", "r", "stray"},
	}
	for _, argv := range bad {
		if _, err := ParseArgs(argv); err == nil {
			t.Errorf("ParseArgs(%q) should fail", argv)
		}
	}
}

func TestConnectRegisters(t *testing.T) {
	fh := newFakeHost(t)
	c, err := Connect(context.Background(), testArgs(fh.port), testLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	reg := fh.next(t)
	if reg["event"] != "registerPlugin" || reg["uuid"] != "com.example.test" {
		t.Errorf("registration = %v", reg)
	}
}

func TestRunDispatchesAndSends(t *testing.T) {
	fh := newFakeHost(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c, err := Connect(ctx, testArgs(fh.port), testLogger())
	if err != nil {
		t.Fatal(err)
	}
	fh.next(t) // registration
	host := <-fh.conn

	type seen struct{ event, context string }
	got := make(chan seen, 16)
	handlers := Handlers{
		WillAppear: func(ctx context.Context, ev Event, p AppearPayload) error {
			var s struct{ Target string }
			json.Unmarshal(p.Settings, &s)
			got <- seen{"willAppear:" + s.Target + ":" + p.Controller, ev.Context}
			return c.SetTitle(ctx, ev.Context, "Kitchen")
		},
		KeyDown: func(ctx context.Context, ev Event, p KeyPayload) error {
			got <- seen{"keyDown:" + strconv.Itoa(p.State), ev.Context}
			return c.SetState(ctx, ev.Context, 1)
		},
		SendToPlugin: func(ctx context.Context, ev Event, payload json.RawMessage) error {
			got <- seen{"sendToPlugin:" + string(payload), ev.Context}
			return c.SendToPropertyInspector(ctx, ev.Action, ev.Context, map[string]any{"items": []string{"a", "b"}})
		},
		Unknown: func(ctx context.Context, ev Event) error {
			got <- seen{"unknown:" + ev.Event, ev.Context}
			return nil
		},
	}
	runDone := make(chan error, 1)
	go func() { runDone <- c.Run(ctx, handlers) }()

	// willAppear with settings and controller
	fh.push(t, host, map[string]any{
		"event": "willAppear", "action": "com.example.test.toggle", "context": "ctx-1", "device": "sd-1",
		"payload": map[string]any{"settings": map[string]any{"Target": "light-42"}, "coordinates": map[string]int{"row": 0, "column": 1}, "controller": "Keypad", "state": 0, "isInMultiAction": false},
	})
	if s := <-got; s != (seen{"willAppear:light-42:Keypad", "ctx-1"}) {
		t.Errorf("willAppear seen = %+v", s)
	}
	if m := fh.next(t); m["event"] != "setTitle" || m["context"] != "ctx-1" || m["payload"].(map[string]any)["title"] != "Kitchen" {
		t.Errorf("setTitle = %v", m)
	}

	// keyDown -> setState 1
	fh.push(t, host, map[string]any{
		"event": "keyDown", "action": "com.example.test.toggle", "context": "ctx-1",
		"payload": map[string]any{"settings": map[string]any{}, "coordinates": map[string]int{"row": 0, "column": 1}, "state": 0},
	})
	if s := <-got; s != (seen{"keyDown:0", "ctx-1"}) {
		t.Errorf("keyDown seen = %+v", s)
	}
	if m := fh.next(t); m["event"] != "setState" || m["payload"].(map[string]any)["state"] != float64(1) {
		t.Errorf("setState = %v", m)
	}

	// sendToPlugin -> sendToPropertyInspector
	fh.push(t, host, map[string]any{"event": "sendToPlugin", "action": "com.example.test.toggle", "context": "ctx-1", "payload": map[string]any{"list": "lights"}})
	if s := <-got; s.event != `sendToPlugin:{"list":"lights"}` {
		t.Errorf("sendToPlugin seen = %+v", s)
	}
	if m := fh.next(t); m["event"] != "sendToPropertyInspector" || m["action"] != "com.example.test.toggle" {
		t.Errorf("sendToPropertyInspector = %v", m)
	}

	// An event with no handler set (keyUp) is ignored; an unknown one hits Unknown.
	fh.push(t, host, map[string]any{"event": "keyUp", "context": "ctx-1", "payload": map[string]any{}})
	fh.push(t, host, map[string]any{"event": "titleParametersDidChange", "context": "ctx-1", "payload": map[string]any{}})
	if s := <-got; s.event != "unknown:titleParametersDidChange" {
		t.Errorf("unknown seen = %+v", s)
	}

	// A malformed payload is logged, not fatal: the loop keeps serving.
	fh.push(t, host, map[string]any{"event": "keyDown", "context": "ctx-1", "payload": "not-an-object"})
	fh.push(t, host, map[string]any{"event": "keyDown", "context": "ctx-2", "payload": map[string]any{"state": 1}})
	if s := <-got; s != (seen{"keyDown:1", "ctx-2"}) {
		t.Errorf("after bad payload, seen = %+v", s)
	}
	fh.next(t) // its setState

	// Host closes normally -> Run returns nil.
	host.Close(websocket.StatusNormalClosure, "bye")
	select {
	case err := <-runDone:
		if err != nil {
			t.Errorf("Run returned %v on normal close, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after host closed")
	}
}

func TestRunStopsOnCancel(t *testing.T) {
	fh := newFakeHost(t)
	ctx, cancel := context.WithCancel(context.Background())
	c, err := Connect(ctx, testArgs(fh.port), nil)
	if err != nil {
		t.Fatal(err)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- c.Run(ctx, Handlers{}) }()
	cancel()
	select {
	case err := <-runDone:
		if err != nil {
			t.Errorf("Run returned %v on cancel, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

func TestConnectFailsWithoutHost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := Connect(ctx, testArgs(1), nil); err == nil { // port 1: nothing listens
		t.Fatal("expected an error")
	}
}

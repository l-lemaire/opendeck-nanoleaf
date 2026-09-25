package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/l-lemaire/opendeck-nanoleaf/internal/config"
	"github.com/l-lemaire/opendeck-nanoleaf/internal/nanoleaf"
	"github.com/l-lemaire/opendeck-nanoleaf/internal/nanoleaf/nanoleaftest"
	"github.com/l-lemaire/opendeck-nanoleaf/internal/openaction"
	"github.com/l-lemaire/opendeck-nanoleaf/internal/pairing"
)

const fakeID = "00:55:da:12:34:56"

// fakeConnector points the plugin at a nanoleaftest device, bypassing the
// config file and the keyring.
type fakeConnector struct {
	fd     *nanoleaftest.Device
	client *nanoleaf.Client

	unpaired    bool
	pairErr     error
	discoverErr error
	pairedWith  string
}

func (f *fakeConnector) Connect(ctx context.Context, id string) (*nanoleaf.Client, config.Device, error) {
	if f.unpaired {
		return nil, config.Device{}, errors.New("no device paired yet")
	}
	return f.client, config.Device{ID: fakeID, Host: "fake", Port: 16021, Name: "Fake Aurora"}, nil
}

func (f *fakeConnector) Devices() ([]config.Device, string, error) {
	if f.unpaired {
		return nil, "", nil
	}
	return []config.Device{{ID: fakeID, Name: "Fake Aurora"}}, fakeID, nil
}

func (f *fakeConnector) Discover(ctx context.Context) ([]nanoleaf.Device, error) {
	if f.discoverErr != nil {
		return nil, f.discoverErr
	}
	return []nanoleaf.Device{{ID: fakeID, Host: "10.0.0.7", Port: 16021, Name: "Fake Aurora", Model: "NL22"}}, nil
}

func (f *fakeConnector) Pair(ctx context.Context, addr, id string, report func(pairing.Progress)) (config.Device, error) {
	f.pairedWith = addr + "|" + id
	report(pairing.Progress{Stage: pairing.StageSearching, Message: "Using"})
	if f.pairErr != nil {
		return config.Device{}, f.pairErr
	}
	report(pairing.Progress{Stage: pairing.StageFound, Message: "Found"})
	report(pairing.Progress{Stage: pairing.StageWaiting, Message: pairing.Instruction, SecondsLeft: 90})
	report(pairing.Progress{Stage: pairing.StagePaired, Message: "Paired"})
	f.unpaired = false
	return config.Device{ID: fakeID, Name: "Fake Aurora"}, nil
}

func startPlugin(t *testing.T) (*websocket.Conn, chan map[string]any, *nanoleaftest.Device) {
	host, sent, fd, _ := startPluginWith(t, &fakeConnector{})
	return host, sent, fd
}

func startPluginWith(t *testing.T, connector *fakeConnector) (*websocket.Conn, chan map[string]any, *nanoleaftest.Device, *fakeConnector) {
	t.Helper()
	fd := nanoleaftest.New(t)
	client := nanoleaf.NewClient(nanoleaf.ClientOptions{Addr: fd.Addr(), Token: fd.Token})

	hostConn := make(chan *websocket.Conn, 1)
	sent := make(chan map[string]any, 64)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		hostConn <- c
		for {
			_, data, err := c.Read(context.Background())
			if err != nil {
				return
			}
			var m map[string]any
			json.Unmarshal(data, &m)
			sent <- m
		}
	}))
	t.Cleanup(srv.Close)
	port, _ := strconv.Atoi(strings.TrimPrefix(srv.URL, "http://127.0.0.1:"))

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	var debug *log.Logger
	if testing.Verbose() {
		debug = log.New(os.Stderr, "    debug: ", 0)
	}
	conn, err := openaction.Connect(ctx, openaction.Args{Port: port, PluginUUID: "com.github.l-lemaire.nanoleaf.sdPlugin", RegisterEvent: "registerPlugin"}, debug)
	if err != nil {
		t.Fatal(err)
	}
	connector.fd, connector.client = fd, client
	p := &plugin{conn: conn, info: log.New(io.Discard, "", 0), debug: debug, devices: connector}
	go conn.Run(ctx, p.handlers())

	host := <-hostConn
	if reg := next(t, sent); reg["event"] != "registerPlugin" {
		t.Fatalf("first message = %v", reg)
	}
	return host, sent, fd, connector
}

func next(t *testing.T, ch chan map[string]any) map[string]any {
	t.Helper()
	select {
	case m := <-ch:
		return m
	case <-time.After(3 * time.Second):
		t.Fatal("plugin sent nothing within 3s")
		return nil
	}
}

func expectEvent(t *testing.T, ch chan map[string]any, event string) map[string]any {
	t.Helper()
	m := next(t, ch)
	if m["event"] != event {
		t.Fatalf("got %v, want event %q", m, event)
	}
	return m
}

func waitForState(t *testing.T, ch chan map[string]any, buttonContext string, want int) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case m := <-ch:
			if m["event"] == "setState" && m["context"] == buttonContext && stateOf(m) == want {
				return
			}
		case <-deadline:
			t.Fatalf("no setState %d for %s within 5s", want, buttonContext)
		}
	}
}

func stateOf(m map[string]any) int { return int(m["payload"].(map[string]any)["state"].(float64)) }

func push(t *testing.T, host *websocket.Conn, v any) {
	t.Helper()
	data, _ := json.Marshal(v)
	if err := host.Write(context.Background(), websocket.MessageText, data); err != nil {
		t.Fatal(err)
	}
}

func sendToPlugin(context string, payload map[string]any) map[string]any {
	return map[string]any{"event": "sendToPlugin", "action": actionToggle, "context": context, "payload": payload}
}

func keyDown(context string, settings map[string]any) map[string]any {
	return map[string]any{"event": "keyDown", "action": actionToggle, "context": context,
		"payload": map[string]any{"settings": settings, "state": 0}}
}

func willAppear(context string, settings map[string]any) map[string]any {
	return map[string]any{"event": "willAppear", "action": actionToggle, "context": context,
		"payload": map[string]any{"settings": settings, "controller": "Keypad"}}
}

func TestInspectorListsDevices(t *testing.T) {
	host, sent, _ := startPlugin(t)
	push(t, host, sendToPlugin("ctx-1", map[string]any{"event": "listTargets"}))
	payload := expectEvent(t, sent, "sendToPropertyInspector")["payload"].(map[string]any)
	items := payload["items"].([]any)
	if payload["event"] != "targets" || len(items) != 1 {
		t.Fatalf("payload = %v", payload)
	}
	item := items[0].(map[string]any)
	if item["id"] != fakeID || item["name"] != "Fake Aurora" || item["on"] != true || item["reachable"] != true {
		t.Errorf("item = %v", item)
	}
}

func TestUnpairedInspectorGetsNotPairedCode(t *testing.T) {
	host, sent, _, _ := startPluginWith(t, &fakeConnector{unpaired: true})
	push(t, host, sendToPlugin("ctx-2", map[string]any{"event": "listTargets"}))
	payload := expectEvent(t, sent, "sendToPropertyInspector")["payload"].(map[string]any)
	if payload["event"] != "error" || payload["code"] != "not_paired" {
		t.Errorf("payload = %v", payload)
	}
}

func TestDiscoverAndPairFromInspector(t *testing.T) {
	host, sent, _, connector := startPluginWith(t, &fakeConnector{unpaired: true})
	push(t, host, sendToPlugin("ctx-3", map[string]any{"event": "discover"}))
	payload := expectEvent(t, sent, "sendToPropertyInspector")["payload"].(map[string]any)
	if payload["event"] != "devices" || len(payload["items"].([]any)) != 1 {
		t.Fatalf("payload = %v", payload)
	}

	push(t, host, sendToPlugin("ctx-3", map[string]any{"event": "pair", "address": "10.0.0.7:16021", "id": fakeID}))
	var stages []string
	for len(stages) < 4 {
		payload := expectEvent(t, sent, "sendToPropertyInspector")["payload"].(map[string]any)
		stages = append(stages, payload["stage"].(string))
		if payload["stage"] == "waiting" && (payload["seconds_left"] != float64(90) || !strings.Contains(payload["message"].(string), "power button")) {
			t.Errorf("waiting payload = %v", payload)
		}
	}
	if stages[0] != "searching" || stages[3] != "paired" {
		t.Errorf("stages = %v", stages)
	}
	if connector.pairedWith != "10.0.0.7:16021|"+fakeID {
		t.Errorf("paired with %q", connector.pairedWith)
	}
	push(t, host, sendToPlugin("ctx-3", map[string]any{"event": "listTargets"}))
	if payload := expectEvent(t, sent, "sendToPropertyInspector")["payload"].(map[string]any); payload["event"] != "targets" {
		t.Errorf("after pairing, payload = %v", payload)
	}
}

func TestPairFailureReachesInspector(t *testing.T) {
	host, sent, _, _ := startPluginWith(t, &fakeConnector{unpaired: true, pairErr: errors.New("the device did not enter pairing mode within 1m30s")})
	push(t, host, sendToPlugin("ctx-4", map[string]any{"event": "pair"}))
	expectEvent(t, sent, "sendToPropertyInspector") // searching
	payload := expectEvent(t, sent, "sendToPropertyInspector")["payload"].(map[string]any)
	if payload["stage"] != "error" || !strings.Contains(payload["message"].(string), "pairing mode") {
		t.Errorf("payload = %v", payload)
	}
}

func TestWillAppearSetsTitleAndState(t *testing.T) {
	host, sent, _ := startPlugin(t)
	push(t, host, willAppear("ctx-5", map[string]any{"device": fakeID, "name": "Fake Aurora"}))
	if m := expectEvent(t, sent, "setTitle"); m["payload"].(map[string]any)["title"] != "Fake Aurora" {
		t.Errorf("title = %v", m)
	}
	waitForState(t, sent, "ctx-5", stateOn) // fake starts on
}

func TestKeyDownToggles(t *testing.T) {
	host, sent, fd := startPlugin(t)
	settings := map[string]any{"device": fakeID, "name": "Fake Aurora"}
	push(t, host, keyDown("ctx-6", settings))
	waitForState(t, sent, "ctx-6", stateOff)
	push(t, host, keyDown("ctx-6", settings))
	waitForState(t, sent, "ctx-6", stateOn)
	if puts := fd.Puts(); len(puts) != 2 {
		t.Errorf("device PUTs = %v", puts)
	}
}

func TestKeyDownWithoutDeviceAlerts(t *testing.T) {
	host, sent, fd := startPlugin(t)
	push(t, host, keyDown("ctx-7", map[string]any{}))
	expectEvent(t, sent, "showAlert")
	if len(fd.Puts()) != 0 {
		t.Error("device was written to")
	}
}

func TestExternalChangeUpdatesButtons(t *testing.T) {
	host, sent, fd := startPlugin(t)
	push(t, host, willAppear("key-a", map[string]any{"device": fakeID, "name": "Fake Aurora"}))
	waitForState(t, sent, "key-a", stateOn)

	fd.SetOn(false) // from the Nanoleaf app
	waitForState(t, sent, "key-a", stateOff)
	fd.SetOn(true)
	waitForState(t, sent, "key-a", stateOn)

	push(t, host, map[string]any{"event": "willDisappear", "action": actionToggle, "context": "key-a", "payload": map[string]any{}})
	time.Sleep(100 * time.Millisecond)
	fd.SetOn(false)
	select {
	case m := <-sent:
		if m["context"] == "key-a" {
			t.Errorf("removed button was updated: %v", m)
		}
	case <-time.After(300 * time.Millisecond):
	}
}

func TestSettingsTitle(t *testing.T) {
	cases := []struct {
		s        Settings
		wantText string
		wantSet  bool
	}{
		{Settings{Name: "Aurora"}, "Aurora", true},
		{Settings{Name: "Aurora", Label: "custom", CustomLabel: "Desk"}, "Desk", true},
		{Settings{Name: "Aurora", Label: "none"}, "", true},
		{Settings{}, "", false},
	}
	for _, c := range cases {
		if text, set := c.s.title(); text != c.wantText || set != c.wantSet {
			t.Errorf("%+v.title() = %q,%v want %q,%v", c.s, text, set, c.wantText, c.wantSet)
		}
	}
	if err := checkAction("com.other.thing"); err == nil {
		t.Error("foreign action should be rejected")
	}
}

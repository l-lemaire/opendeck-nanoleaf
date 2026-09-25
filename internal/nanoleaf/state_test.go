package nanoleaf

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/l-lemaire/opendeck-nanoleaf/internal/nanoleaf/nanoleaftest"
)

func paired(t *testing.T, fd *nanoleaftest.Device) *Client {
	return NewClient(ClientOptions{Addr: fd.Addr(), Token: fd.Token, Log: testLogger(t)})
}

func TestToggle(t *testing.T) {
	fd := nanoleaftest.New(t)
	c := paired(t, fd)
	ctx := context.Background()

	on, err := c.IsOn(ctx)
	if err != nil || !on {
		t.Fatalf("initial state on=%v err=%v", on, err)
	}
	if nowOn, err := c.Toggle(ctx); err != nil || nowOn {
		t.Fatalf("first toggle: on=%v err=%v", nowOn, err)
	}
	if nowOn, err := c.Toggle(ctx); err != nil || !nowOn {
		t.Fatalf("second toggle: on=%v err=%v", nowOn, err)
	}
	if puts := fd.Puts(); len(puts) != 2 || puts[0] != `{"on":{"value":false}}` {
		t.Errorf("puts = %v", puts)
	}
}

func TestDryRunNeverWrites(t *testing.T) {
	fd := nanoleaftest.New(t)
	var out bytes.Buffer
	c := NewClient(ClientOptions{Addr: fd.Addr(), Token: fd.Token, DryRun: &out})
	nowOn, err := c.Toggle(context.Background())
	if err != nil || nowOn {
		t.Fatalf("dry toggle: on=%v err=%v", nowOn, err)
	}
	if len(fd.Puts()) != 0 {
		t.Error("dry run reached the device")
	}
	if text := out.String(); !strings.Contains(text, "would send PUT") || strings.Contains(text, fd.Token) || !strings.Contains(text, `"value": false`) {
		t.Errorf("dry-run output = %q", text)
	}
}

func TestStreamEvents(t *testing.T) {
	fd := nanoleaftest.New(t)
	c := paired(t, fd)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	connected := make(chan struct{})
	changes := make(chan Change, 16)
	done := make(chan error, 1)
	go func() {
		done <- c.StreamEvents(ctx, EventHandler{
			OnConnect: func() { close(connected) },
			OnChange:  func(ch Change) { changes <- ch },
		})
	}()
	select {
	case <-connected:
	case <-time.After(3 * time.Second):
		t.Fatal("stream never connected")
	}

	fd.SetOn(false)                                        // from the app
	c.SetOn(ctx, true)                                     // through the API
	fd.Emit("4", `{"events":[{"panelId":1,"gesture":0}]}`) // touch: ignored
	fd.Emit("1", `not json`)                               // garbage: ignored
	fd.Emit("1", `{"events":[{"attr":2,"value":55}]}`)     // brightness
	fd.SetEffect("Nemo")

	want := []string{"off", "on", "brightness 55%", "effect Nemo"}
	for i, w := range want {
		select {
		case got := <-changes:
			if got.String() != w {
				t.Errorf("change %d = %q, want %q", i, got, w)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("change %d (%s) never arrived", i, w)
		}
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("StreamEvents returned %v after cancel", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("StreamEvents did not return after cancel")
	}
}

func TestWatchReconnects(t *testing.T) {
	fd := nanoleaftest.New(t)
	c := paired(t, fd)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	connects := make(chan struct{}, 4)
	errs := make(chan error, 4)
	go c.Watch(ctx, EventHandler{
		OnConnect: func() { connects <- struct{}{} },
		OnError:   func(err error, _ time.Duration) { errs <- err },
	})
	<-connects
	fd.CloseClientConnections()
	select {
	case <-errs:
	case <-time.After(3 * time.Second):
		t.Fatal("no error after the connection dropped")
	}
	select {
	case <-connects:
	case <-time.After(5 * time.Second):
		t.Fatal("did not reconnect")
	}
}

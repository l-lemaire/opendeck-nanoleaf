package nanoleaf

import (
	"context"
	"errors"
	"testing"

	"github.com/l-lemaire/opendeck-nanoleaf/internal/nanoleaf/nanoleaftest"
)

func TestRequestToken(t *testing.T) {
	fd := nanoleaftest.New(t)
	fd.Refusals.Store(1)
	c := NewClient(ClientOptions{Addr: fd.Addr(), Log: testLogger(t)})
	ctx := context.Background()

	if _, err := c.RequestToken(ctx); !errors.Is(err, ErrNotInPairingMode) {
		t.Fatalf("first attempt: got %v, want ErrNotInPairingMode", err)
	}
	token, err := c.RequestToken(ctx)
	if err != nil || token != fd.Token {
		t.Fatalf("second attempt: %q, %v", token, err)
	}
}

func TestInfoAndDelete(t *testing.T) {
	fd := nanoleaftest.New(t)
	ctx := context.Background()

	good := NewClient(ClientOptions{Addr: fd.Addr(), Token: fd.Token, Log: testLogger(t)})
	info, err := good.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "Fake Aurora" || info.Model != "NL22" || !info.State.On.Value || info.State.Brightness.Value != 80 || info.Effects.Selected != "Forest" {
		t.Errorf("info = %+v", info)
	}

	bad := NewClient(ClientOptions{Addr: fd.Addr(), Token: "wrong"})
	if _, err := bad.Info(ctx); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("wrong token: got %v, want ErrUnauthorized", err)
	}

	if err := good.DeleteToken(ctx); err != nil {
		t.Fatal(err)
	}
	if !fd.Deleted() {
		t.Error("token not revoked on the device")
	}
	if _, err := good.Info(ctx); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("after delete: got %v, want ErrUnauthorized", err)
	}
}

// Package nanoleaftest provides a fake Nanoleaf device for tests: a plain
// HTTP server implementing the endpoints the nanoleaf package uses, with
// mutable state.
package nanoleaftest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// Device is the fake.
type Device struct {
	*httptest.Server
	// Token is the only auth token the fake accepts once issued.
	Token string
	// Refusals is how many POST /api/v1/new attempts get 403 (not in
	// pairing mode) before one succeeds. Negative means never succeed.
	Refusals atomic.Int32

	mu         sync.Mutex
	name       string
	on         bool
	brightness int
	effect     string
	effects    []string
	puts       []string // bodies of PUT /state calls, for assertions
	deleted    bool
}

// New starts a fake Light Panels device. Closed when the test ends.
func New(t *testing.T) *Device {
	t.Helper()
	d := &Device{
		Token:      "FAKEtoken0123456789abcdefghijklmnop",
		name:       "Fake Aurora",
		on:         true,
		brightness: 80,
		effect:     "Forest",
		effects:    []string{"Flames", "Forest", "Nemo"},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/new", d.newToken)
	mux.HandleFunc("/api/v1/{token}", d.root)
	mux.HandleFunc("/api/v1/{token}/{rest...}", d.authed) // rest may be "" for the trailing-slash root
	d.Server = httptest.NewServer(mux)
	t.Cleanup(d.Server.Close)
	return d
}

// Addr returns host:port.
func (d *Device) Addr() string { return strings.TrimPrefix(d.URL, "http://") }

// Puts returns the bodies of every PUT /state received, in order.
func (d *Device) Puts() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.puts...)
}

// Deleted reports whether the token was revoked.
func (d *Device) Deleted() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.deleted
}

func (d *Device) newToken(w http.ResponseWriter, r *http.Request) {
	if n := d.Refusals.Add(-1); n >= 0 || n < -1_000_000 {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"auth_token": d.Token})
}

func (d *Device) checkToken(w http.ResponseWriter, r *http.Request) bool {
	d.mu.Lock()
	ok := r.PathValue("token") == d.Token && !d.deleted
	d.mu.Unlock()
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
	}
	return ok
}

// root serves GET (info) and DELETE (revoke) on /api/v1/<token>.
func (d *Device) root(w http.ResponseWriter, r *http.Request) {
	if !d.checkToken(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		d.mu.Lock()
		defer d.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{
			"name": d.name, "serialNo": "S17081A0001", "manufacturer": "Nanoleaf",
			"firmwareVersion": "5.3.1", "model": "NL22",
			"state":   d.stateJSON(),
			"effects": map[string]any{"select": d.effect, "effectsList": d.effects},
		})
	case http.MethodDelete:
		d.mu.Lock()
		d.deleted = true
		d.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (d *Device) stateJSON() map[string]any {
	return map[string]any{
		"on":         map[string]any{"value": d.on},
		"brightness": map[string]any{"value": d.brightness, "max": 100, "min": 0},
	}
}

// authed serves the sub-resources: state, state/on, effects.
func (d *Device) authed(w http.ResponseWriter, r *http.Request) {
	if !d.checkToken(w, r) {
		return
	}
	rest := r.PathValue("rest")
	if rest == "" { // "/api/v1/<token>/" is the root too
		d.root(w, r)
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	switch {
	case r.Method == http.MethodGet && rest == "state":
		json.NewEncoder(w).Encode(d.stateJSON())
	case r.Method == http.MethodGet && rest == "state/on":
		json.NewEncoder(w).Encode(map[string]any{"value": d.on})
	case r.Method == http.MethodPut && rest == "state":
		var patch struct {
			On         *struct{ Value bool } `json:"on"`
			Brightness *struct{ Value int }  `json:"brightness"`
		}
		raw, _ := readAll(r)
		if err := json.Unmarshal(raw, &patch); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if patch.On != nil {
			d.on = patch.On.Value
		}
		if patch.Brightness != nil {
			d.brightness = patch.Brightness.Value
		}
		d.puts = append(d.puts, string(raw))
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet && rest == "effects/effectsList":
		json.NewEncoder(w).Encode(d.effects)
	case r.Method == http.MethodGet && rest == "effects/select":
		json.NewEncoder(w).Encode(d.effect)
	case r.Method == http.MethodPut && rest == "effects":
		var body struct {
			Select string `json:"select"`
		}
		raw, _ := readAll(r)
		if json.Unmarshal(raw, &body) != nil || body.Select == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		d.effect = body.Select
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func readAll(r *http.Request) ([]byte, error) {
	buf := make([]byte, 0, 512)
	tmp := make([]byte, 512)
	for {
		n, err := r.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			return buf, nil
		}
	}
}

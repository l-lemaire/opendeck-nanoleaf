package nanoleaf

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRedactPath(t *testing.T) {
	cases := map[string]string{
		"/api/v1/AbCdEfGh0123456789/state":          "/api/v1/AbCd[redacted]/state",
		"http://10.0.0.5:16021/api/v1/tok/state/on": "http://10.0.0.5:16021/api/v1/tok[redacted]/state/on",
		"/api/v1/new": "/api/v1/new",
		"/api/v1/":    "/api/v1/",
		"/other":      "/other",
	}
	for in, want := range cases {
		if got := RedactPath(in); got != want {
			t.Errorf("RedactPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoggingTransportRedactsToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"value":true}`))
	}))
	defer srv.Close()

	var out bytes.Buffer
	client := &http.Client{Transport: loggingTransport{next: http.DefaultTransport, log: log.New(&out, "", 0)}}
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/v1/SUPERSECRETTOKEN123/state", strings.NewReader(`{"on":{"value":true}}`))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	logged := out.String()
	if strings.Contains(logged, "SUPERSECRETTOKEN123") {
		t.Errorf("token leaked into debug output:\n%s", logged)
	}
	for _, want := range []string{"PUT /api/v1/SUPE[redacted]/state", `{"on":{"value":true}}`, "200 OK", `{"value":true}`} {
		if !strings.Contains(logged, want) {
			t.Errorf("debug output missing %q:\n%s", want, logged)
		}
	}
	if !strings.Contains(req.URL.Path, "SUPERSECRETTOKEN123") {
		t.Error("original request URL was modified")
	}
}

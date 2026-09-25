package nanoleaf

import (
	"strings"
	"testing"
)

func TestReadSSE(t *testing.T) {
	input := strings.Join([]string{
		": hi", // keep-alive comment
		"",     // blank after a comment: nothing to dispatch
		"id: 1:0",
		`data: [{"type":"update"}]`,
		"",
		"event: custom",
		"data: line one",
		"data:line two", // no space after the colon is legal
		"retry: 5000",
		"",
		"data: trailing without blank line",
	}, "\n")

	var got []sseEvent
	if err := readSSE(strings.NewReader(input), func(ev sseEvent) { got = append(got, ev) }); err != nil {
		t.Fatal(err)
	}
	want := []sseEvent{
		{ID: "1:0", Data: `[{"type":"update"}]`},
		{Name: "custom", Data: "line one\nline two"},
		{Data: "trailing without blank line"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d events %+v, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestReadSSELongLine(t *testing.T) {
	long := "data: " + strings.Repeat("x", 200_000) + "\n\n"
	var n int
	if err := readSSE(strings.NewReader(long), func(ev sseEvent) { n = len(ev.Data) }); err != nil {
		t.Fatal(err)
	}
	if n != 200_000 {
		t.Errorf("long data truncated to %d", n)
	}
}

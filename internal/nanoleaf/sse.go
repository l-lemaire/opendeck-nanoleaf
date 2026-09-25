package nanoleaf

import (
	"bufio"
	"io"
	"strings"
)

// The device pushes changes as Server-Sent Events (SSE): a plain text HTTP
// response that never ends, made of blank-line separated blocks like
//
//	id: 1758801234:0
//	data: [{"type":"update","data":[...]}]
//
// Lines starting with ":" are comments a server may use as keep-alives.
// A field may repeat ("data:" over several lines joins with "\n").

// sseEvent is one dispatched block.
type sseEvent struct {
	ID   string
	Name string // the "event:" field; Nanoleaf leaves it empty
	Data string
}

// readSSE parses the stream and calls handle for every complete event. It
// returns when the reader ends or fails; io.EOF is reported as nil because
// a stream simply ending is not a parse error.
func readSSE(r io.Reader, handle func(sseEvent)) error {
	// bufio.Reader.ReadString handles lines of any length; bufio.Scanner
	// would stop at 64 KiB, which a large payload could exceed.
	br := bufio.NewReaderSize(r, 64<<10)
	var ev sseEvent
	var data []string

	dispatch := func() {
		if len(data) > 0 || ev.ID != "" || ev.Name != "" {
			ev.Data = strings.Join(data, "\n")
			handle(ev)
		}
		ev, data = sseEvent{}, nil
	}

	for {
		line, err := br.ReadString('\n')
		if err != nil && err != io.EOF {
			return err
		}
		atEOF := err == io.EOF // line may still hold a final unterminated line
		line = strings.TrimRight(line, "\r\n")

		switch {
		case line == "":
			dispatch()
		case strings.HasPrefix(line, ":"):
			// keep-alive comment, ignored
		default:
			field, value, _ := strings.Cut(line, ":")
			value = strings.TrimPrefix(value, " ") // the spec allows one leading space
			switch field {
			case "id":
				ev.ID = value
			case "event":
				ev.Name = value
			case "data":
				data = append(data, value)
			}
			// "retry" and unknown fields are ignored.
		}
		if atEOF {
			dispatch() // a last block without trailing blank line
			return nil
		}
	}
}

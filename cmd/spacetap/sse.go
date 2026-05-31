package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/habitat-network/habitat/internal/sync"
)

// SSEStream reads Server-Sent Events from a response body.
type SSEStream struct {
	body    io.ReadCloser
	scanner *bufio.Scanner
}

func newSSEStream(body io.ReadCloser) *SSEStream {
	return &SSEStream{
		body:    body,
		scanner: bufio.NewScanner(body),
	}
}

func (s *SSEStream) Close() error {
	return s.body.Close()
}

// ReadEvent reads the next space event from the SSE stream.
func (s *SSEStream) ReadEvent() (sync.Event, error) {
	var eventType string
	var data string
	for s.scanner.Scan() {
		line := s.scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			eventType = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimPrefix(line, "data: ")
		case line == "":
			if eventType == "message" {
				var ev sync.Event
				if err := json.Unmarshal([]byte(data), &ev); err != nil {
					return ev, fmt.Errorf("parse event: %w", err)
				}
				return ev, nil
			}
			// non-message event (e.g. error) — skip
			eventType = ""
			data = ""
		}
	}
	if err := s.scanner.Err(); err != nil {
		return sync.Event{}, fmt.Errorf("sse read: %w", err)
	}
	return sync.Event{}, io.EOF
}

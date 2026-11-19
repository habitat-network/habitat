package mcpsearch

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// resultJSON decodes a tool result's JSON: its structured content, or else
// the JSON text of its content blocks. A result with neither is
// ErrUnparseableResult.
func resultJSON(result *mcp.CallToolResult) (any, error) {
	var raw []byte
	if result.StructuredContent != nil {
		var err error
		if raw, err = json.Marshal(result.StructuredContent); err != nil {
			return nil, fmt.Errorf("encode structured content: %w", err)
		}
	} else {
		raw = []byte(resultText(result))
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, ErrUnparseableResult
	}
	return doc, nil
}

// calendarWindow is how far either side of now the Calendar search looks.
// list_events otherwise covers only the next seven days.
const calendarWindow = 365 * 24 * time.Hour

// calendarProvider searches Google Calendar with list_events and its
// fullText filter (the server's search_events tool isn't part of Google's
// Calendar MCP server). Its result is a JSON object whose "events" list
// holds Calendar API events:
//
//	{"events": [{"summary": "daily sync", "htmlLink": "https://www.google.com/calendar/event?eid=…",
//	  "start": {"dateTime": "2026-11-02T09:30:00-08:00"}, "location": "…", "description": "…"}]}
//
// An all-day event has "start": {"date": "2026-11-02"}, and a search that
// matches nothing omits "events". Each event becomes one item: its summary
// as the title, its link as the URL, and its time, location and description
// as the snippet.
type calendarProvider struct {
	*ToolProvider
	now func() time.Time
}

var _ Provider = (*calendarProvider)(nil)

func newCalendarProvider(serverURL string) *calendarProvider {
	p := &calendarProvider{now: time.Now}
	p.ToolProvider = &ToolProvider{
		Name: "google-calendar-events",
		URL:  serverURL,
		Tool: "list_events",
		Args: func(q Query) map[string]any {
			// The tool takes local times without an offset or a Z.
			const layout = "2006-01-02T15:04:05"
			now := p.now().UTC()
			return map[string]any{
				"fullText":  q.Text,
				"pageSize":  min(q.Limit, 250),
				"startTime": now.Add(-calendarWindow).Format(layout),
				"endTime":   now.Add(calendarWindow).Format(layout),
			}
		},
	}
	return p
}

func (p *calendarProvider) Normalize(result *mcp.CallToolResult) ([]Item, error) {
	doc, err := resultJSON(result)
	if err != nil {
		return nil, err
	}
	events, _ := lookup(doc, "events").([]any)
	var items []Item
	for _, event := range events {
		item := Item{
			Title: firstString(event, []string{"summary"}),
			URL:   firstString(event, []string{"htmlLink"}),
		}
		var details []string
		if when := calendarTime(event); when != "" {
			details = append(details, when)
		}
		if loc := firstString(event, []string{"location"}); loc != "" {
			details = append(details, loc)
		}
		snippet := strings.Join(details, " · ")
		if desc := strings.TrimSpace(firstString(event, []string{"description"})); desc != "" {
			snippet += "\n" + desc
		}
		item.Snippet = strings.TrimSpace(snippet)
		if item != (Item{}) {
			items = append(items, item)
		}
	}
	return items, nil
}

// calendarTime formats an event's start, in the zone the event gives it.
func calendarTime(event any) string {
	if s := firstString(event, []string{"start.dateTime"}); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t.Format("Mon Jan 2, 2006 3:04 PM")
		}
		return s
	}
	if s := firstString(event, []string{"start.date"}); s != "" {
		if t, err := time.Parse(time.DateOnly, s); err == nil {
			return t.Format("Mon Jan 2, 2006")
		}
		return s
	}
	return ""
}

// gmailProvider searches Gmail with search_threads. Its result is a JSON
// object whose "threads" list holds thread summaries, each with its
// messages:
//
//	{"threads": [{"id": "19a…", "messages": [{"id": "19a…", "subject": "Launch plan",
//	  "sender": "ada@example.com", "date": "2026-11-02", "snippet": "…"}]}]}
//
// Each thread becomes one item: the subject as the title, the thread's
// Gmail link as the URL, and the sender, date and snippet of its first
// message as the snippet. A search that matches nothing omits "threads".
type gmailProvider struct {
	*ToolProvider
}

var _ Provider = (*gmailProvider)(nil)

// gmailMaxPageSize is the most threads search_threads returns per page.
const gmailMaxPageSize = 50

func newGmailProvider(serverURL string) *gmailProvider {
	return &gmailProvider{&ToolProvider{
		Name: "gmail-threads",
		URL:  serverURL,
		Tool: "search_threads",
		Args: func(q Query) map[string]any {
			// The query is Gmail search syntax, in which plain words match
			// anywhere in a message.
			return map[string]any{
				"query":    q.Text,
				"pageSize": min(q.Limit, gmailMaxPageSize),
				"view":     "THREAD_VIEW_MINIMAL",
			}
		},
	}}
}

func (p *gmailProvider) Normalize(result *mcp.CallToolResult) ([]Item, error) {
	doc, err := resultJSON(result)
	if err != nil {
		return nil, err
	}
	threads, _ := lookup(doc, "threads").([]any)
	var items []Item
	for _, thread := range threads {
		id := firstString(thread, []string{"id"})
		messages, _ := lookup(thread, "messages").([]any)
		var item Item
		if id != "" {
			// The thread's page in the member's first signed-in account;
			// Gmail has no link the tool could return for any account.
			item.URL = "https://mail.google.com/mail/u/0/#all/" + url.PathEscape(id)
		}
		if len(messages) > 0 {
			first := messages[0]
			item.Title = firstString(first, []string{"subject"})
			var by []string
			for _, path := range []string{"sender", "date"} {
				if s := firstString(first, []string{path}); s != "" {
					by = append(by, s)
				}
			}
			item.Snippet = strings.TrimSpace(
				strings.Join(by, " · ") + "\n" + firstString(first, []string{"snippet"}))
		}
		if item.Title == "" && item.Snippet != "" {
			item.Title = "(no subject)"
		}
		if item.Title != "" || item.Snippet != "" {
			items = append(items, item)
		}
	}
	return items, nil
}

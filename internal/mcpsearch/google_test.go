package mcpsearch

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCalendarProvider(t *testing.T) {
	p := newCalendarProvider("https://calendarmcp.googleapis.com/mcp/v1")
	p.now = func() time.Time { return time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC) }
	require.Equal(t, "list_events", p.ToolName())
	args := p.Arguments(Query{Text: "sync", Limit: 5})
	require.Equal(t, "sync", args["fullText"])
	require.Equal(t, 5, args["pageSize"])
	require.Equal(t, "2025-11-17T12:00:00", args["startTime"])
	require.Equal(t, "2027-11-17T12:00:00", args["endTime"])

	// list_events answers with the calendar's details around the events;
	// this is its reply shape, with names and links changed.
	items, err := p.Normalize(textResult(`{"accessRole":"owner","events":[
	  {"attendees":[{"email":"ada@example.com","self":true}],
	   "htmlLink":"https://www.google.com/calendar/event?eid=abc&ctz=America/Los_Angeles",
	   "id":"abc_20261102T173000Z","status":"confirmed","summary":"daily sync",
	   "start":{"dateTime":"2026-11-02T09:30:00-08:00","timeZone":"America/Los_Angeles"},
	   "end":{"dateTime":"2026-11-02T10:00:00-08:00"},
	   "location":"Room 4","description":"Agenda: launch\n"},
	  {"htmlLink":"https://www.google.com/calendar/event?eid=def","summary":"Offsite",
	   "start":{"date":"2026-12-03"}}
	 ],"nextPageToken":"x","summary":"ada@example.com","timeZone":"America/Los_Angeles"}`))
	require.NoError(t, err)
	require.Equal(t, []Item{
		{
			Title:   "daily sync",
			URL:     "https://www.google.com/calendar/event?eid=abc&ctz=America/Los_Angeles",
			Snippet: "Mon Nov 2, 2026 9:30 AM · Room 4\nAgenda: launch",
		},
		{
			Title:   "Offsite",
			URL:     "https://www.google.com/calendar/event?eid=def",
			Snippet: "Thu Dec 3, 2026",
		},
	}, items)

	// A search with no matches omits "events".
	items, err = p.Normalize(textResult(
		`{"accessRole":"owner","summary":"ada@example.com","timeZone":"America/Los_Angeles"}`))
	require.NoError(t, err)
	require.Empty(t, items)

	_, err = p.Normalize(textResult("Calendar is unavailable"))
	require.ErrorIs(t, err, ErrUnparseableResult)
}

func TestGmailProvider(t *testing.T) {
	p := providerByID(t, "gmail-threads")
	require.Equal(t, "search_threads", p.ToolName())
	require.Equal(t,
		map[string]any{"query": "from:ada launch", "pageSize": 50, "view": "THREAD_VIEW_MINIMAL"},
		p.Arguments(Query{Text: "from:ada launch", Limit: 80}))

	// The reply shape of search_threads as Google documents it.
	items, err := p.Normalize(textResult(`{"threads":[
	  {"id":"19a1b2","messages":[
	    {"id":"19a1b2","subject":"Launch plan","sender":"ada@example.com",
	     "toRecipients":["bo@example.com"],"date":"2026-11-02","snippet":"Let's ship Tuesday",
	     "labelIds":["INBOX"]},
	    {"id":"19a1b3","subject":"Re: Launch plan","snippet":"Works for me"}]},
	  {"id":"19c4d5","messages":[{"id":"19c4d5","sender":"bo@example.com","snippet":"hi"}]}
	 ],"resultCountEstimate":"2"}`))
	require.NoError(t, err)
	require.Equal(t, []Item{
		{
			Title:   "Launch plan",
			URL:     "https://mail.google.com/mail/u/0/#all/19a1b2",
			Snippet: "ada@example.com · 2026-11-02\nLet's ship Tuesday",
		},
		{
			Title:   "(no subject)",
			URL:     "https://mail.google.com/mail/u/0/#all/19c4d5",
			Snippet: "bo@example.com\nhi",
		},
	}, items)

	// A search with no matches omits "threads".
	items, err = p.Normalize(textResult(`{"resultCountEstimate":"0"}`))
	require.NoError(t, err)
	require.Empty(t, items)
}

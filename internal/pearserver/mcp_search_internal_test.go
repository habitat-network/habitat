package pearserver

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/fedsearch"
	"github.com/habitat-network/habitat/internal/search"
)

func TestMcpSearchOutputMapsPage(t *testing.T) {
	data, err := json.Marshal(search.RecordData{
		Space: "ats://did:plc:org/com.example.type/k", Collection: "com.example.note",
		Value: map[string]any{"text": "hi"},
	})
	require.NoError(t, err)
	out := mcpSearchOutput(fedsearch.Page{
		Results: []fedsearch.Result{
			{
				Backend: "records",
				Kind:    search.ResultKind,
				ID:      "ats://r",
				URL:     "ats://r",
				Data:    data,
			},
			{Backend: "mcp:slack/slack-messages", Kind: "item", ID: "u", Title: "Issue", URL: "u"},
		},
		Backends: []fedsearch.BackendStatus{
			{
				BackendState: fedsearch.BackendState{
					ID: "records", Kind: "records", Name: "Habitat records",
					Status: fedsearch.StatusOK, Count: 1, Elapsed: 30 * time.Millisecond,
				},
				Cursor: "c1",
			},
			{BackendState: fedsearch.BackendState{
				ID:     "mcp:slack/slack-messages",
				Kind:   "mcp",
				Name:   "slack",
				Status: fedsearch.StatusError,
				Error: &fedsearch.BackendError{
					Code:    fedsearch.CodeUpstreamError,
					Message: "rate limited",
				},
			}},
		},
		Cursor:  "next",
		Ranking: fedsearch.RankingReranker,
	})
	require.Equal(t, habitat.NetworkHabitatMcpSearchOutput{
		Results: []habitat.NetworkHabitatMcpSearchResult{
			{
				Backend: "records", Kind: "record", Id: "ats://r", Url: "ats://r",
				Record: &habitat.NetworkHabitatMcpSearchRecord{
					Space: "ats://did:plc:org/com.example.type/k", Collection: "com.example.note",
					Value: map[string]any{"text": "hi"},
				},
			},
			{Backend: "mcp:slack/slack-messages", Kind: "item", Id: "u", Title: "Issue", Url: "u"},
		},
		Backends: []habitat.NetworkHabitatMcpSearchBackend{
			{
				Id: "records", Kind: "records", Name: "Habitat records", Status: "ok",
				Count: 1, ElapsedMs: 30, Cursor: "c1",
			},
			{
				Id: "mcp:slack/slack-messages", Kind: "mcp", Name: "slack", Status: "error",
				ErrorCode: "upstream_error", Error: "rate limited",
			},
		},
		Cursor:  "next",
		Ranking: "reranker",
	}, out)
}

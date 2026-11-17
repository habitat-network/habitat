package mcpsearch

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// slackProvider reads the result of Slack's search tools. They answer with a
// JSON object whose single "results" field is a markdown report, one
// "### Result N of M" section per message:
//
//	### Result 1 of 9
//	Channel: #eng (ID: C0CG6UEKZS4)
//	From: Ada <ada@example.com> (ID: U0CEE8E6WS0)
//	Permalink: [link](https://example.slack.com/archives/C0CG6UEKZS4/p1793975777012069)
//	Text:
//	the message
//	Context after:
//	- ...
//	---
//
// Each section becomes one item: the channel and author as its title, the
// permalink as its URL and the message text as its snippet.
type slackProvider struct {
	*ToolProvider
}

var _ Provider = (*slackProvider)(nil)

var (
	slackPermalink = regexp.MustCompile(`^Permalink:\s*\[[^\]]*\]\(([^)\s]+)\)`)
	// slackIDSuffix matches the " (ID: …)" Slack adds after channel and
	// user names.
	slackIDSuffix = regexp.MustCompile(`\s*\(ID:[^)]*\)`)
	// slackEmail matches the " <address>" after a sender's name.
	slackEmail = regexp.MustCompile(`\s*<[^>]*>`)
)

func (p *slackProvider) Normalize(result *mcp.CallToolResult) ([]Item, error) {
	report := resultText(result)
	// The report is the "results" field of a JSON object; if the tool ever
	// answers with the markdown itself, read that.
	var doc struct {
		Results *string `json:"results"`
	}
	if json.Unmarshal([]byte(report), &doc) == nil && doc.Results != nil {
		report = *doc.Results
	}
	_, sections, found := strings.Cut(report, "### Result ")
	if !found {
		// No message sections: no matches ("No results found."), or a report
		// in a shape this doesn't know, whose text is kept rather than
		// dropped.
		if strings.TrimSpace(report) == "" || strings.Contains(report, "No results found") {
			return nil, nil
		}
		return []Item{{Snippet: report}}, nil
	}
	var items []Item
	for section := range strings.SplitSeq(sections, "### Result ") {
		if item := parseSlackMessage(section); item != (Item{}) {
			items = append(items, item)
		}
	}
	return items, nil
}

// parseSlackMessage reads one "### Result" section, without its heading
// prefix.
func parseSlackMessage(section string) Item {
	var (
		channel, from, link string
		text                []string
		inText              bool
	)
	lines := strings.Split(section, "\n")
	for _, line := range lines[1:] { // lines[0] is the rest of the heading
		switch {
		case inText:
			// The message ends where its context or the next separator starts.
			if strings.HasPrefix(line, "Context before:") ||
				strings.HasPrefix(line, "Context after:") ||
				strings.TrimSpace(line) == "---" {
				inText = false
				continue
			}
			text = append(text, line)
		case strings.HasPrefix(line, "Channel:"):
			channel = strings.TrimSpace(slackIDSuffix.ReplaceAllString(
				strings.TrimPrefix(line, "Channel:"), ""))
		case strings.HasPrefix(line, "From:") && from == "":
			from = strings.TrimSpace(slackEmail.ReplaceAllString(
				slackIDSuffix.ReplaceAllString(strings.TrimPrefix(line, "From:"), ""), ""))
		case slackPermalink.MatchString(line):
			link = slackPermalink.FindStringSubmatch(line)[1]
		case strings.HasPrefix(line, "Text:"):
			inText = true
			text = append(text, strings.TrimPrefix(line, "Text:"))
		}
	}
	title := strings.TrimSpace(channel + " · " + from)
	title = strings.Trim(title, " ·")
	return Item{
		Title:   title,
		URL:     link,
		Snippet: strings.TrimSpace(strings.Join(text, "\n")),
	}
}

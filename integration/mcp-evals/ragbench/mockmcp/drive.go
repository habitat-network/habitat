package mockmcp

import (
	"context"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/habitat-network/habitat/integration/mcp-evals/ragbench/corpus"
)

// Tool names of Google's Drive MCP server
// (https://developers.google.com/workspace/drive/api/reference/mcp).
const (
	driveSearchTool = "search_files"
	driveReadTool   = "read_file_content"
)

type driveSearchInput struct {
	Query    string `json:"query"              jsonschema:"a Drive search query, like: fullText contains 'quarterly plan'"`
	PageSize int    `json:"pageSize,omitempty" jsonschema:"most files to return"`
}

type driveFile struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	MimeType       string `json:"mimeType"`
	ViewURL        string `json:"viewUrl"`
	Owner          string `json:"owner,omitempty"`
	ModifiedTime   string `json:"modifiedTime,omitempty"`
	ContentSnippet string `json:"contentSnippet,omitempty"`
}

type driveSearchOutput struct {
	Files []driveFile `json:"files"`
}

type driveReadInput struct {
	FileID string `json:"fileId" jsonschema:"the file's id, from search_files"`
}

func addDriveTools(srv *mcp.Server, b *backend) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: driveSearchTool,
		Description: "Search for files in Google Drive by name or content. The query uses " +
			"Drive's query syntax, for example: fullText contains 'budget' or name contains " +
			"'roadmap'. Returns each file's id, title, link and a snippet.",
	}, func(
		_ context.Context, _ *mcp.CallToolRequest, in driveSearchInput,
	) (*mcp.CallToolResult, driveSearchOutput, error) {
		docs := b.search(
			driveSearchTool,
			in,
			driveQueryText(in.Query),
			limitOr(in.PageSize, 10),
			nil,
		)
		out := driveSearchOutput{Files: make([]driveFile, len(docs))}
		for i, doc := range docs {
			out.Files[i] = driveFile{
				ID: driveFileID(doc), Title: doc.Title, MimeType: driveMimeType(doc),
				ViewURL: doc.URL, Owner: doc.Field("owner"),
				ModifiedTime:   doc.Field("last_modified"),
				ContentSnippet: snippet(doc.Text, 300),
			}
		}
		return nil, out, nil
	})
	mcp.AddTool(srv, &mcp.Tool{
		Name:        driveReadTool,
		Description: "Read the text content of a Google Drive file, by its id.",
	}, func(
		_ context.Context, _ *mcp.CallToolRequest, in driveReadInput,
	) (*mcp.CallToolResult, any, error) {
		doc := b.find(func(doc *corpus.Doc) bool { return driveFileID(doc) == in.FileID })
		if doc == nil {
			b.record(driveReadTool, in)
			return errorResult("no file with id %q", in.FileID), nil, nil
		}
		b.record(driveReadTool, in, doc)
		return textResult(doc.Title + "\n\n" + doc.Text), nil, nil
	})
	addOtherTools(srv, b, []otherTool{
		{"get_file_metadata", "Get a Google Drive file's metadata: name, type, owners, " +
			"modified time and sharing settings."},
		{"list_recent_files", "List the files in Google Drive the user opened or edited most " +
			"recently."},
		{"download_file_content", "Download a Google Drive file's content in its original " +
			"format, base64-encoded."},
		{"get_file_permissions", "List who a Google Drive file is shared with and their roles."},
		{"create_file", "Create a new file in Google Drive from text content."},
	})
}

// driveFileID is the document's Drive file id: the id in its URL.
func driveFileID(doc *corpus.Doc) string {
	return strings.TrimSuffix(
		strings.TrimPrefix(doc.URL, "https://drive.google.com/file/d/"),
		"/view",
	)
}

func driveMimeType(doc *corpus.Doc) string {
	switch doc.Field("doc_type") {
	case "sheet", "spreadsheet":
		return "application/vnd.google-apps.spreadsheet"
	case "slides", "presentation":
		return "application/vnd.google-apps.presentation"
	default:
		return "application/vnd.google-apps.document"
	}
}

// driveTerm matches one "field contains 'value'" term of a Drive query, with
// backslash escapes in the value.
var driveTerm = regexp.MustCompile(`(?:fullText|name)\s+contains\s+'((?:[^'\\]|\\.)*)'`)

// driveQueryText returns the text a Drive query searches for: the values of
// its "contains" terms, or the whole query if it has none.
func driveQueryText(query string) string {
	var values []string
	for _, m := range driveTerm.FindAllStringSubmatch(query, -1) {
		value := strings.ReplaceAll(m[1], `\'`, `'`)
		values = append(values, strings.ReplaceAll(value, `\\`, `\`))
	}
	if len(values) == 0 {
		return query
	}
	return strings.Join(values, " ")
}

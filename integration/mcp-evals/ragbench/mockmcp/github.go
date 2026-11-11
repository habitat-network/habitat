package mockmcp

import (
	"context"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/habitat-network/habitat/integration/mcp-evals/ragbench/corpus"
)

// Tool names of GitHub's MCP server (https://github.com/github/github-mcp-server).
const (
	githubSearchTool       = "search_pull_requests"
	githubSearchIssuesTool = "search_issues"
	githubSearchCodeTool   = "search_code"
	githubReadTool         = "pull_request_read"
)

type githubSearchInput struct {
	Query   string `json:"query"             jsonschema:"a GitHub search query, keywords plus qualifiers like repo:owner/name or author:login"`
	PerPage int    `json:"perPage,omitempty" jsonschema:"results per page"`
}

type githubUser struct {
	Login string `json:"login"`
}

type githubLabel struct {
	Name string `json:"name"`
}

type githubPullRequest struct {
	Number    int           `json:"number"`
	Title     string        `json:"title"`
	State     string        `json:"state"`
	HTMLURL   string        `json:"html_url"`
	User      githubUser    `json:"user"`
	Labels    []githubLabel `json:"labels"`
	CreatedAt string        `json:"created_at,omitempty"`
	UpdatedAt string        `json:"updated_at,omitempty"`
	Body      string        `json:"body"`
}

// githubSearchOutput is the shape of GitHub's search API responses, which the
// server passes on as JSON text.
type githubSearchOutput struct {
	TotalCount        int                 `json:"total_count"`
	IncompleteResults bool                `json:"incomplete_results"`
	Items             []githubPullRequest `json:"items"`
}

type githubReadInput struct {
	Method     string `json:"method"     jsonschema:"what to read; only get is supported here"`
	Owner      string `json:"owner"      jsonschema:"the repository's owner"`
	Repo       string `json:"repo"       jsonschema:"the repository's name"`
	PullNumber int    `json:"pullNumber" jsonschema:"the pull request's number"`
}

func addGitHubTools(srv *mcp.Server, b *backend) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: githubSearchTool,
		Description: "Search for pull requests across GitHub repositories, by keywords and " +
			"qualifiers such as repo:, author:, label: and state:.",
	}, func(
		_ context.Context, _ *mcp.CallToolRequest, in githubSearchInput,
	) (*mcp.CallToolResult, any, error) {
		text, keep := githubQuery(in.Query)
		docs := b.search(githubSearchTool, in, text, limitOr(in.PerPage, 30), keep)
		out := githubSearchOutput{
			TotalCount: len(docs),
			Items:      make([]githubPullRequest, len(docs)),
		}
		for i, doc := range docs {
			out.Items[i] = githubPR(doc, snippet(doc.Text, 300))
		}
		res, err := jsonResult(out)
		return res, nil, err
	})
	// The dataset's GitHub documents are all pull requests, so searching
	// issues or code finds nothing, as it would on a real account holding
	// only these.
	for _, tool := range []otherTool{
		{githubSearchIssuesTool, "Search for issues across GitHub repositories, by keywords " +
			"and qualifiers such as repo:, author:, label: and state:."},
		{githubSearchCodeTool, "Search code across GitHub repositories, by keywords and " +
			"qualifiers such as repo:, path: and language:."},
	} {
		mcp.AddTool(srv, &mcp.Tool{Name: tool.name, Description: tool.description}, func(
			_ context.Context, _ *mcp.CallToolRequest, in githubSearchInput,
		) (*mcp.CallToolResult, any, error) {
			b.record(tool.name, in)
			res, err := jsonResult(githubSearchOutput{Items: []githubPullRequest{}})
			return res, nil, err
		})
	}
	mcp.AddTool(srv, &mcp.Tool{
		Name: githubReadTool,
		Description: "Get a pull request's details: its description, state, reviews and " +
			"comments, given the repository's owner and name and the pull request's number.",
	}, func(
		_ context.Context, _ *mcp.CallToolRequest, in githubReadInput,
	) (*mcp.CallToolResult, any, error) {
		number := strconv.Itoa(in.PullNumber)
		doc := b.find(func(doc *corpus.Doc) bool {
			return doc.Repo() == in.Repo && doc.Field("pr_number") == number
		})
		if doc == nil {
			b.record(githubReadTool, in)
			return errorResult("no pull request #%d in %s/%s", in.PullNumber, in.Owner, in.Repo),
				nil, nil
		}
		b.record(githubReadTool, in, doc)
		res, err := jsonResult(githubPR(doc, doc.Text))
		return res, nil, err
	})
	addOtherTools(srv, b, []otherTool{
		{"search_repositories", "Find GitHub repositories by name, description, topics or " +
			"other metadata."},
		{"search_users", "Find GitHub users by username, name or other profile information."},
		{"list_pull_requests", "List the pull requests of one GitHub repository, filtered by " +
			"state, base or head branch."},
		{"list_issues", "List the issues of one GitHub repository, filtered by state, labels " +
			"or date."},
		{"issue_read", "Get an issue's details, comments, sub-issues or labels."},
		{"list_commits", "List the commits of a branch in a GitHub repository."},
		{"get_commit", "Get a commit's details and diff."},
		{"get_file_contents", "Get the contents of a file or directory in a GitHub repository."},
		{"list_branches", "List the branches of a GitHub repository."},
		{"list_releases", "List the releases of a GitHub repository."},
		{"get_latest_release", "Get the latest release of a GitHub repository."},
		{"get_me", "Get the authenticated GitHub user's profile."},
		{"create_pull_request", "Open a new pull request in a GitHub repository."},
		{"update_pull_request", "Edit a pull request's title, description, state or reviewers."},
		{"merge_pull_request", "Merge a pull request."},
		{"add_issue_comment", "Add a comment to an issue or pull request."},
		{"issue_write", "Create a new issue or update an existing one."},
		{"create_branch", "Create a new branch in a GitHub repository."},
		{"create_or_update_file", "Create or update a single file in a GitHub repository."},
		{"push_files", "Push several files to a GitHub repository in one commit."},
		{"fork_repository", "Fork a GitHub repository to your account or an organization."},
		{
			"actions_list",
			"List GitHub Actions workflows, runs, jobs and artifacts of a repository.",
		},
		{"get_job_logs", "Get the logs of a GitHub Actions job, or of a run's failed jobs."},
	})
}

func githubPR(doc *corpus.Doc, body string) githubPullRequest {
	number, _ := strconv.Atoi(doc.Field("pr_number"))
	pr := githubPullRequest{
		Number: number, Title: doc.Title, State: doc.Field("state"), HTMLURL: doc.URL,
		User:      githubUser{Login: doc.Field("author")},
		Labels:    []githubLabel{},
		CreatedAt: doc.Field("created_at"), UpdatedAt: doc.Field("updated_at"),
		Body: body,
	}
	for label := range strings.SplitSeq(doc.Field("labels"), "\n") {
		if label != "" {
			pr.Labels = append(pr.Labels, githubLabel{Name: label})
		}
	}
	return pr
}

// githubQuery splits a GitHub search query into its keywords and a filter
// for its qualifiers. repo:, author: and label: are applied; the rest (is:pr,
// state: and so on) are dropped, as they'd match everything here or aren't
// in the dataset.
func githubQuery(query string) (text string, keep func(*corpus.Doc) bool) {
	var words, repos, authors, labels []string
	for word := range strings.FieldsSeq(query) {
		name, value, ok := strings.Cut(word, ":")
		value = strings.ToLower(strings.Trim(value, `"`))
		switch {
		case !ok || value == "":
			words = append(words, word)
		case name == "repo":
			// repo:owner/name; the dataset has one owner.
			_, repo, found := strings.Cut(value, "/")
			if !found {
				repo = value
			}
			repos = append(repos, repo)
		case name == "author":
			authors = append(authors, value)
		case name == "label":
			labels = append(labels, value)
		case name == "is" || name == "type" || name == "state" || name == "in" ||
			name == "org" || name == "user" || name == "sort":
		default:
			words = append(words, word)
		}
	}
	if len(repos)+len(authors)+len(labels) == 0 {
		return strings.Join(words, " "), nil
	}
	return strings.Join(words, " "), func(doc *corpus.Doc) bool {
		has := func(wanted []string, field string) bool {
			have := strings.ToLower(doc.Field(field))
			for _, w := range wanted {
				if !strings.Contains(have, w) {
					return false
				}
			}
			return true
		}
		return (len(repos) == 0 || strings.ToLower(doc.Repo()) == repos[0]) &&
			has(authors, "author") && has(labels, "labels")
	}
}

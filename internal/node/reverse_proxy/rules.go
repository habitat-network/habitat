package reverse_proxy

// ReverseProxyRule matches a URL path to a target of the given type.
// There are two types of rules currently:
//  1. File server: serves files from a given directory (useful for serving websites from Habitat)
//  2. Redirect: redirects to a given URL (useful for exposing APIs for Habitat applications)
//
// The matcher field represents the path that the rule should match.
// The semantics of the target field changes depending on the type. For file servers, it represents the
// path to the directory to serve files from. For redirects, it represents the URL to redirect to.
type Rule struct {
	ID      string   `json:"id"      yaml:"id"`
	Type    RuleType `json:"type"    yaml:"type"`
	Matcher string   `json:"matcher" yaml:"matcher"`
	Target  string   `json:"target"  yaml:"target"`
	AppID   string   `json:"app_id"  yaml:"app_id"`
}

type RuleType = string

const (
	ProxyRuleFileServer RuleType = "file"
	ProxyRuleRedirect   RuleType = "redirect"
)

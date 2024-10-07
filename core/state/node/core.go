package node

// Core structs for the node state. These are intended to be embedable in other structs
// throughout the application. That way, it's easy to modify the core struct, while having
// the component specific structs to be decoupled. Fields in these structs should be immutable.

// TODO to make these truly immutable, only methods should be exported, all fields should be private.

const AppLifecycleStateInstalling = "installing"
const AppLifecycleStateInstalled = "installed"

type Package struct {
	Driver             string                 `json:"driver" yaml:"driver"`
	DriverConfig       map[string]interface{} `json:"driver_config" yaml:"driver_config"`
	RegistryURLBase    string                 `json:"registry_url_base" yaml:"registry_url_base"`
	RegistryPackageID  string                 `json:"registry_app_id" yaml:"registry_app_id"`
	RegistryPackageTag string                 `json:"registry_tag" yaml:"registry_tag"`
}

// TODO some fields should be ignored by the REST api
type AppInstallation struct {
	ID      string `json:"id" yaml:"id"`
	UserID  string `json:"user_id" yaml:"user_id"`
	Name    string `json:"name" yaml:"name"`
	Version string `json:"version" yaml:"version"`
	Package `yaml:",inline"`
}

const ProcessStateStarting = "starting"
const ProcessStateRunning = "running"

type Process struct {
	ID      string `json:"id"`
	AppID   string `json:"app_id"`
	UserID  string `json:"user_id"`
	Created string `json:"created"`
	Driver  string `json:"driver"`
}

// ReverseProxyRule matches a URL path to a target of the given type.
// There are two types of rules currently:
//  1. File server: serves files from a given directory (useful for serving websites from Habitat)
//  2. Redirect: redirects to a given URL (useful for exposing APIs for Habitat applications)
//
// The matcher field represents the path that the rule should match.
// The semantics of the target field changes depending on the type. For file servers, it represents the
// path to the directory to serve files from. For redirects, it represents the URL to redirect to.
type ReverseProxyRule struct {
	ID      string `json:"id" yaml:"id"`
	Type    string `json:"type" yaml:"type"`
	Matcher string `json:"matcher" yaml:"matcher"`
	Target  string `json:"target" yaml:"target"`
	AppID   string `json:"app_id" yaml:"app_id"`
}

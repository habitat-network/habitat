package constants

type HabitatContextKey string

const (
	// Environment names
	EnvironmentDev  = "dev"
	EnvironmentProd = "prod"

	// Default values
	RootUsername      = "root"
	RootUserID        = "0"
	NodeDBDefaultName = "node"

	// Request context keys
	ContextKeyUserID HabitatContextKey = "user_id"

	// Default port values
	DefaultPortHabitatAPI   = "3000"
	DefaultPortReverseProxy = "3001"

	PortReverseProxyTSFunnel = "443"

	TSNetHostnameDefault = "habitat"
	TSNetHostnameDev     = "habitat-dev"
)

package config

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/constants"
	"github.com/eagraf/habitat-new/internal/node/controller"
	"github.com/mitchellh/mapstructure"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	viper "github.com/spf13/viper"
)

func loadEnv(v *viper.Viper) error {
	err := v.BindEnv("environment", "ENVIRONMENT")
	if err != nil {
		return err
	}
	v.SetDefault("environment", constants.EnvironmentProd)

	err = v.BindEnv("debug", "DEBUG")
	if err != nil {
		return err
	}
	v.SetDefault("debug", false)

	err = v.BindEnv("habitat_path", "HABITAT_PATH")
	if err != nil {
		return err
	}
	homedir, err := homedir()
	if err != nil {
		return err
	}
	v.SetDefault("habitat_path", filepath.Join(homedir, ".habitat"))

	err = v.BindEnv("habitat_app_path", "HABITAT_APP_PATH")
	if err != nil {
		return err
	}

	err = v.BindEnv("use_tls", "USE_TLS")
	if err != nil {
		return err
	}
	v.SetDefault("use_tls", false)

	err = v.BindEnv("tailscale_authkey", "TS_AUTHKEY")
	if err != nil {
		return err
	}

	err = v.BindEnv("tailnet", "TS_TAILNET")
	if err != nil {
		return err
	}

	err = v.BindEnv("tailscale_funnel_enabled", "TS_FUNNEL_ENABLED")
	if err != nil {
		return err
	}

	err = v.BindEnv("domain", "DOMAIN")
	if err != nil {
		return err
	}

	err = v.BindEnv("frontend_dev", "FRONTEND_DEV")
	if err != nil {
		return err
	}
	v.SetDefault("frontend_dev", false)

	return nil
}

func loadViperConfig() (*viper.Viper, error) {
	v := viper.New()

	err := loadEnv(v)
	if err != nil {
		return nil, err
	}

	homedir, err := homedir()
	if err != nil {
		return nil, err
	}

	v.AddConfigPath(filepath.Join(homedir, ".habitat"))
	v.AddConfigPath(v.GetString("habitat_path"))

	v.SetConfigType("yml")
	v.SetConfigName("habitat")

	if err := v.ReadInConfig(); err != nil {
		return nil, err
	}

	return v, nil
}

func decodePemCert(certPath string) (*x509.Certificate, error) {
	pemBytes, err := os.ReadFile(certPath)
	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("got nil block after decoding PEM")
	}

	if block.Type != "CERTIFICATE" {
		return nil, errors.New("expected CERTIFICATE PEM block")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	return cert, nil
}

// TODO @eagraf look at whether we should put all available fields in the config struct.
type NodeConfig struct {
	RootUserCert *x509.Certificate
	NodeCert     *x509.Certificate

	viper *viper.Viper
}

func NewNodeConfig() (*NodeConfig, error) {
	// Use viper to load all configs from the config file, env, etc.
	v, err := loadViperConfig()
	if err != nil {
		return nil, err
	}

	// Then, use the loaded Viper instance to create the NodeConfig struct
	config, err := NewNodeConfigFromViper(v)
	if err != nil {
		return nil, err
	}

	// Finally, load the certs from disk into the NodeConfig struct
	rootCertPath := config.RootUserCertPath()
	if rootCertPath != "" {
		// Read cert files
		rootCert, err := decodePemCert(rootCertPath)
		if err != nil {
			return nil, err
		}
		config.RootUserCert = rootCert
	}

	if config.NodeCertPath() != "" {
		nodeCert, err := decodePemCert(config.NodeCertPath())
		if err != nil {
			return nil, err
		}
		config.NodeCert = nodeCert
	}

	log.Debug().Msgf("Loaded node config: node cert: %s root cert: %s node key: %s", config.NodeCertPath(), config.RootUserCertPath(), config.NodeKeyPath())

	return config, nil
}

func NewNodeConfigFromViper(v *viper.Viper) (*NodeConfig, error) {

	var config NodeConfig
	err := v.Unmarshal(&config)
	if err != nil {
		return nil, err
	}
	config.viper = v

	return &config, nil
}

// NewTestNodeConfig returns a NodeConfig suitable for testing.
// Besides setting up the Viper instance, it also sets a fake root user cert.
func NewTestNodeConfig(optionalViper *viper.Viper) (*NodeConfig, error) {

	// Create a new Viper instance if none was provided
	v := optionalViper
	if v == nil {
		v = viper.New()
	}

	config, err := NewNodeConfigFromViper(v)
	if err != nil {
		return nil, err
	}
	config.RootUserCert = &x509.Certificate{
		Raw: []byte("root_cert"),
	}
	return config, nil
}

func (n *NodeConfig) Environment() string {
	return n.viper.GetString("environment")
}

func (n *NodeConfig) LogLevel() zerolog.Level {
	isDebug := n.viper.GetBool("debug")
	if isDebug {
		return zerolog.DebugLevel
	}
	return zerolog.InfoLevel
}

func (n *NodeConfig) HabitatPath() string {
	return n.viper.GetString("habitat_path")
}

func (n *NodeConfig) HabitatAppPath() string {
	// Note that in dev mode, this should point to a path on the host machine rather than in the Docker container.
	path := n.viper.GetString("habitat_app_path")
	if path == "" {
		return filepath.Join(n.HabitatPath(), "apps")
	}
	return path
}

func (n *NodeConfig) HDBPath() string {
	return filepath.Join(n.HabitatPath(), "hdb")
}

// WebBundlePath returns the path to the directory where web bundles for various applications are stored.
func (n *NodeConfig) WebBundlePath() string {
	return filepath.Join(n.HabitatPath(), "web")
}

func (n *NodeConfig) NodeCertPath() string {
	habitatPath := n.HabitatPath()
	if habitatPath == "" {
		return ""
	}
	return filepath.Join(n.HabitatPath(), "certificates", "dev_node_cert.pem")
}

func (n *NodeConfig) NodeKeyPath() string {
	habitatPath := n.HabitatPath()
	if habitatPath == "" {
		return ""
	}
	return filepath.Join(habitatPath, "certificates", "dev_node_key.pem")
}

func (n *NodeConfig) RootUserCertPath() string {
	return filepath.Join(n.HabitatPath(), "certificates", "dev_root_user_cert.pem")
}

func (n *NodeConfig) RootUserCertB64() string {
	return base64.StdEncoding.EncodeToString(n.RootUserCert.Raw)
}

func (n *NodeConfig) TLSConfig() (*tls.Config, error) {
	if !n.UseTLS() {
		return nil, nil
	}

	rootCertBytes, err := os.ReadFile(n.RootUserCertPath())
	if err != nil {
		return nil, err
	}

	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(rootCertBytes)

	return &tls.Config{
		ClientCAs:  caCertPool,
		ClientAuth: tls.RequireAndVerifyClientCert,
	}, nil
}

func (n *NodeConfig) UseTLS() bool {
	return n.viper.GetBool("use_tls")
}

// Hostname that the node listens on. This may be updated dynamically because Tailscale may add a suffix
func (n *NodeConfig) Hostname() string {
	if n.TailscaleAuthkey() != "" {
		if n.Environment() == constants.EnvironmentDev {
			parts := strings.Split(n.Domain(), ".")
			if len(parts) > 0 {
				return parts[0]
			} else {
				log.Fatal().Msgf("Failed to parse domain: %s", n.Domain())
			}
		} else {
			return constants.TSNetHostnameDefault
		}
	}
	return "localhost"
}

// Domain name that hosts this Habitat node, if tailscale funnel is enabled.
func (n *NodeConfig) Domain() string {
	if n.TailScaleFunnelEnabled() {
		domain := n.viper.GetString("domain")
		if domain == "" {
			return "localhost"
		}
		return domain
	}
	return ""
}

func (n *NodeConfig) ReverseProxyPort() string {
	if n.TailScaleFunnelEnabled() {
		return constants.PortReverseProxyTSFunnel
	}
	return constants.DefaultPortReverseProxy
}

// Currently unused, but may be necessary to implement adding members to the community.
func (n *NodeConfig) TailnetName() string {
	return n.viper.GetString("tailnet")
}

func (n *NodeConfig) TailscaleAuthkey() string {
	return n.viper.GetString("tailscale_authkey")
}

func (n *NodeConfig) TailScaleStatePath() string {
	// Note: this is intentionally not configurable for simplicity's sake.
	return filepath.Join(n.HabitatPath(), "tailscale_state")
}

func (n *NodeConfig) TailScaleFunnelEnabled() bool {
	if n.TailscaleAuthkey() != "" {
		return n.viper.GetBool("tailscale_funnel_enabled")
	} else {
		return false
	}
}

// TODO @eagraf we probably will eventually need a better secret management system.
func (n *NodeConfig) PDSAdminUsername() string {
	return "admin"
}

func (n *NodeConfig) PDSAdminPassword() string {
	return "password"
}

func (n *NodeConfig) FrontendDev() bool {
	return n.viper.GetBool("frontend_dev")
}

func (n *NodeConfig) DefaultApps() ([]*node.AppInstallation, []*node.ReverseProxyRule, error) {
	var appRequestsMap map[string]*controller.InstallAppRequest
	err := n.viper.UnmarshalKey("default_apps", &appRequestsMap, viper.DecoderConfigOption(
		func(decoderConfig *mapstructure.DecoderConfig) {
			decoderConfig.TagName = "yaml"
			decoderConfig.Squash = true
		},
	))
	if err != nil {
		log.Error().Err(err).Msg("Failed to unmarshal default apps")
		return nil, nil, err
	}

	apps := make([]*node.AppInstallation, 0)
	rules := make([]*node.ReverseProxyRule, 0)
	for _, appRequest := range appRequestsMap {
		apps = append(apps, appRequest.AppInstallation)
		rules = append(rules, appRequest.ReverseProxyRules...)
	}
	return apps, rules, nil
}

// Helper functions

func homedir() (string, error) {
	usr, err := user.Current()
	if err != nil {
		return "", err
	}
	dir := usr.HomeDir
	return dir, nil
}

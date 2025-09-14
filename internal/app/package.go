package app

type DriverType string

const (
	DriverTypeUnknown DriverType = "unknown"
	DriverTypeNoop    DriverType = "noop"
	DriverTypeDocker  DriverType = "docker"
	DriverTypeWeb     DriverType = "web"
)

func (d DriverType) String() string {
	return string(d)
}

var (
	driverTypes = map[string]DriverType{
		"unknown": DriverTypeUnknown,
		"noop":    DriverTypeNoop,
		"docker":  DriverTypeDocker,
		"web":     DriverTypeWeb,
	}
)

func DriverTypeFromString(s string) DriverType {
	t, ok := driverTypes[s]
	if !ok {
		return DriverTypeUnknown
	}
	return t
}

type Package struct {
	Driver             DriverType             `json:"driver" yaml:"driver"`
	DriverConfig       map[string]interface{} `json:"driver_config" yaml:"driver_config"`
	RegistryURLBase    string                 `json:"registry_url_base" yaml:"registry_url_base"`
	RegistryPackageID  string                 `json:"registry_app_id" yaml:"registry_app_id"`
	RegistryPackageTag string                 `json:"registry_tag" yaml:"registry_tag"`
}

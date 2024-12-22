package web

import (
	"github.com/eagraf/habitat-new/internal/node/package_manager"
)

type BundleInstallationConfig struct {
	DownloadURL         string `json:"download_url"`          // Where to download the bundle from. Assume it's in a .tar.gz file.
	BundleDirectoryName string `json:"bundle_directory_name"` // The directory under $HABITAT_PATH/web/ where the bundle will be extracted into.
}

type Driver struct {
	PackageManager package_manager.PackageManager
	ProcessDriver  *ProcessDriver
}

func NewDriver(webBundlePath string) (*Driver, error) {
	return &Driver{
		PackageManager: &AppDriver{
			webBundlePath: webBundlePath,
		},
	}, nil
}

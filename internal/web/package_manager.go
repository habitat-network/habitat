package web

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/eagraf/habitat-new/internal/app"
	"github.com/eagraf/habitat-new/util"
	"github.com/rs/zerolog/log"
)

type webPackageManager struct {
	webBundlePath string
}

// webPackageManager implements PackageManager
var _ app.PackageManager = &webPackageManager{}

func NewPackageManager(webBundlePath string) app.PackageManager {
	return &webPackageManager{
		webBundlePath: webBundlePath,
	}
}

type BundleInstallationConfig struct {
	DownloadURL         string `json:"download_url"`          // Where to download the bundle from. Assume it's in a .tar.gz file.
	BundleDirectoryName string `json:"bundle_directory_name"` // The directory under $HABITAT_PATH/web/ where the bundle will be extracted into.
}

func (d *webPackageManager) Driver() app.DriverType {
	return app.DriverTypeWeb
}

func (m *webPackageManager) IsInstalled(pkg *app.Package, version string) (bool, error) {
	// Check for the existence of the bundle directory with the right version.
	bundleConfig, err := getWebBundleConfigFromPackage(pkg)
	if err != nil {
		return false, err
	}
	log.Info().Msgf("Installing web package %s@%s", bundleConfig.DownloadURL, version)
	bundlePath := m.getBundlePath(bundleConfig, version)

	if _, err := os.Stat(bundlePath); os.IsNotExist(err) {
		return false, nil
	}

	// TODO this doesn't verify the installed bundle is actually for the right application.
	// i.e. there is no guard against name conflicts right now.

	return true, nil
}

// Implement the package manager interface

func (m *webPackageManager) InstallPackage(packageSpec *app.Package, version string) error {
	if packageSpec.Driver != app.DriverTypeWeb {
		return fmt.Errorf("invalid package driver: %s, expected 'web' driver", packageSpec.Driver)
	}

	// Make sure the $HABITAT_PATH/web/ directory is created
	err := os.MkdirAll(m.webBundlePath, 0o755)
	if err != nil {
		return err
	}

	// Download the bundle into a temp directory.
	bundleConfig, err := getWebBundleConfigFromPackage(packageSpec)
	if err != nil {
		return err
	}

	log.Info().Msgf("Installing web package %s@%s", bundleConfig.DownloadURL, version)

	bundlePath := m.getBundlePath(bundleConfig, version)
	err = downloadAndExtractWebBundle(bundleConfig.DownloadURL, bundlePath)
	if err != nil {
		return err
	}

	return nil
}

func (m *webPackageManager) UninstallPackage(pkg *app.Package, version string) error {
	bundleConfig, err := getWebBundleConfigFromPackage(pkg)
	if err != nil {
		return err
	}
	bundlePath := m.getBundlePath(bundleConfig, version)

	if _, err := os.Stat(bundlePath); os.IsNotExist(err) {
		return nil
	}

	return os.RemoveAll(bundlePath)
}

func (m *webPackageManager) RestoreFromState(
	ctx context.Context,
	apps map[string]*app.Installation,
) error {
	var err error
	for _, app := range apps {
		if app.Driver == m.Driver() {
			perr := m.InstallPackage(app.Package, app.Version)
			if perr != nil {
				// Set the returned error to the last one we run into, but keep iterating
				err = perr
			}
		}
	}
	return err
}

func (m *webPackageManager) getBundlePath(
	bundleConfig *BundleInstallationConfig,
	version string,
) string {
	return filepath.Join(m.webBundlePath, bundleConfig.BundleDirectoryName, version)
}

func getWebBundleConfigFromPackage(pkg *app.Package) (*BundleInstallationConfig, error) {
	configBytes, err := json.Marshal(pkg.DriverConfig)
	if err != nil {
		return nil, err
	}

	var bundleConfig BundleInstallationConfig
	err = json.Unmarshal(configBytes, &bundleConfig)
	if err != nil {
		return nil, err
	}

	return &bundleConfig, nil
}

func downloadAndExtractWebBundle(downloadURL string, bundlePath string) error {
	// Create a temporary directory to store the bundle
	tempDir, err := os.MkdirTemp("", "habitat-web-bundle-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Path to temporary file we download.
	tmpFile := filepath.Join(tempDir, "bundle.tar.gz")

	// Create the destination directory
	err = os.MkdirAll(bundlePath, 0o755)
	if err != nil {
		return err
	}

	// Download the bundle to a temp dir.
	err = downloadWebBundle(downloadURL, tmpFile)
	if err != nil {
		return err
	}

	// Extract the bundle into the specified directory
	err = extractTarGz(tmpFile, bundlePath)
	if err != nil {
		return err
	}

	return nil
}

// Download a .tar.gz file from the specified URL.
func downloadWebBundle(downloadURL string, tmpFile string) error {
	log.Debug().Msgf("Downloading bundle from %s to %s", downloadURL, tmpFile)
	resp, err := http.Get(downloadURL)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	// Create a file to save the downloaded bundle
	bundleFile, err := os.Create(tmpFile)
	if err != nil {
		return err
	}
	defer util.Close(bundleFile)

	// Copy the downloaded bundle to the file
	_, err = io.Copy(bundleFile, resp.Body)
	if err != nil {
		return err
	}

	return nil
}

func extractTarGz(tarPath, destPath string) error {
	r, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer util.Close(r)

	gzr, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer util.Close(gzr)

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()

		switch {

		// if no more files are found return
		case err == io.EOF:
			return nil

		// return any other error
		case err != nil:
			return err

		// if the header is nil, just skip it (not sure how this happens)
		case header == nil:
			continue
		}

		// the target location where the dir/file should be created
		target := filepath.Join(destPath, header.Name)

		// the following switch could also be done using fi.Mode(), not sure if there
		// a benefit of using one vs. the other.
		// fi := header.FileInfo()

		// check the file type
		switch header.Typeflag {

		// if its a dir and it doesn't exist create it
		case tar.TypeDir:
			if _, err := os.Stat(target); err != nil {
				if err := os.MkdirAll(target, 0o755); err != nil {
					return err
				}
			}

		// if it's a file create it
		case tar.TypeReg:
			f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				return err
			}

			// copy over contents
			if _, err := io.Copy(f, tr); err != nil {
				return err
			}

			// manually close here after each file operation; defering would cause each file close
			// to wait until all operations have completed.
			err = f.Close()
			if err != nil {
				return err
			}
		}
	}
}

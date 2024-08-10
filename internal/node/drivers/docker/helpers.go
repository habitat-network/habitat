package docker

import (
	"fmt"

	"github.com/eagraf/habitat-new/core/state/node"
)

func repoURLFromPackage(packageSpec *node.Package, version string) string {
	return fmt.Sprintf("%s/%s:%s", packageSpec.RegistryURLBase, packageSpec.RegistryPackageID, version)
}

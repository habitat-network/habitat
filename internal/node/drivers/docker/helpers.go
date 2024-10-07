package docker

import (
	"fmt"

	"github.com/eagraf/habitat-new/core/state/node"
)

func repoURLFromPackage(packageSpec *node.Package) string {
	return fmt.Sprintf("%s/%s:%s", packageSpec.RegistryURLBase, packageSpec.RegistryPackageID, packageSpec.RegistryPackageTag)
}

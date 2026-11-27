// Package lexicons exposes the local Atmosphere Groups declarations needed
// to evaluate group permissions, without fetching mutable lexicons at runtime.
package lexicons

import "embed"

// AtgrpPermissions contains the standard group permission sets and space
// declarations. The permission evaluator reads these exact wire contracts.
//
//go:embed org/atgrp/auth*.json org/atgrp/meta.json org/atgrp/members.json org/atgrp/inbox.json
var AtgrpPermissions embed.FS

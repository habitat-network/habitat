package frontend

import (
	"embed"
)

//go:embed build/*
var EmbeddedFrontendBundle embed.FS

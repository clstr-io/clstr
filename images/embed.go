package images

import "embed"

//go:embed */Dockerfile
var Images embed.FS

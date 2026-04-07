package images

import (
	"embed"
	"strings"
)

//go:embed */Dockerfile
var fs embed.FS

var aliases = map[string]string{
	"golang": "go",
	"py":     "python",
}

// Dockerfile returns the Dockerfile for the given language, falling back to
// the generic template if no built-in exists for that language.
func Dockerfile(lang string) ([]byte, error) {
	lang = strings.ToLower(strings.TrimSpace(lang))

	mapped, ok := aliases[lang]
	if ok {
		lang = mapped
	}

	data, err := fs.ReadFile(lang + "/Dockerfile")
	if err == nil {
		return data, nil
	}

	return fs.ReadFile("template/Dockerfile")
}

package images

import "embed"

//go:embed */Dockerfile
var FS embed.FS

// Dockerfile returns the Dockerfile for the given language, falling back to
// the generic template if no built-in exists for that language.
func Dockerfile(lang string) ([]byte, error) {
	data, err := FS.ReadFile(lang + "/Dockerfile")
	if err == nil {
		return data, nil
	}

	return FS.ReadFile("template/Dockerfile")
}

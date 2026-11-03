package config

import (
	"fmt"
	"strings"
	"unicode"
)

const DefaultMediaLibraryPrefix = "assets/media-library/"

// NormalizeMediaLibraryPrefix returns a dedicated, relative object-key directory.
// It never cleans dot segments: silently rewriting them could expand the OSS scan scope.
func NormalizeMediaLibraryPrefix(prefix string) (string, error) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return DefaultMediaLibraryPrefix, nil
	}
	invalid := func() (string, error) {
		return "", fmt.Errorf("storage.media_library_prefix must be a dedicated relative directory of at most 120 bytes, without empty/dot segments, URL characters or overlap with uploads/ and assets/users/")
	}
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	if len(prefix) > 120 || strings.ContainsAny(prefix, `\%?#:*`) {
		return invalid()
	}
	for _, r := range prefix {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return invalid()
		}
	}
	for _, segment := range strings.Split(strings.TrimSuffix(prefix, "/"), "/") {
		if segment == "" || segment == "." || segment == ".." {
			return invalid()
		}
	}
	for _, reserved := range []string{"uploads/", "assets/users/"} {
		if strings.HasPrefix(prefix, reserved) || strings.HasPrefix(reserved, prefix) {
			return invalid()
		}
	}
	return prefix, nil
}

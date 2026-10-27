package main

import (
	_ "crypto/sha256"
	_ "crypto/sha512"
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/distribution/reference"
)

func main() {
	if len(os.Args) != 2 || !validImageReference(os.Args[1]) {
		fmt.Fprintln(os.Stderr, "invalid container image reference")
		os.Exit(1)
	}
}

func validImageReference(image string) bool {
	if image == "" || image != strings.TrimSpace(image) || strings.IndexFunc(image, unicode.IsSpace) >= 0 {
		return false
	}
	_, err := reference.ParseNormalizedNamed(image)
	return err == nil
}

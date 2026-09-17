package proto

import (
	"bytes"
	"encoding/base64"
	"strings"
	"unicode/utf8"
)

const escapedPrefix = "="

// PathComponent encodes a name for a slash-separated index path. Plain
// UTF-8 names stay readable; anything else, including name tokens of an
// encrypted store, is base64url with a leading "=".
func PathComponent(name []byte) string {
	if utf8.Valid(name) && !bytes.ContainsAny(name, "/\x00") && !bytes.HasPrefix(name, []byte(escapedPrefix)) {
		return string(name)
	}

	return escapedPrefix + base64.RawURLEncoding.EncodeToString(name)
}

// NameFromComponent reverses PathComponent.
func NameFromComponent(component string) []byte {
	if !strings.HasPrefix(component, escapedPrefix) {
		return []byte(component)
	}

	name, err := base64.RawURLEncoding.DecodeString(component[len(escapedPrefix):])
	if err != nil {
		return []byte(component)
	}

	return name
}

// JoinPath appends a name to an index path.
func JoinPath(prefix string, name []byte) string {
	component := PathComponent(name)
	if prefix == "" {
		return component
	}

	return prefix + "/" + component
}

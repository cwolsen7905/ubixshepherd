// Package runtimefile reads the daemon's runtime file (daemon.json). It is separate from
// package api so the wire types stay free of file I/O, and from package daemon so a client
// can find the daemon without importing it.
package runtimefile

import (
	"encoding/json"
	"os"

	"github.com/ubixsys/ubixshepherd/internal/api"
)

// Read reads a daemon's runtime file.
func Read(path string) (api.Runtime, error) {
	var rt api.Runtime
	b, err := os.ReadFile(path)
	if err != nil {
		return rt, err
	}
	return rt, json.Unmarshal(b, &rt)
}

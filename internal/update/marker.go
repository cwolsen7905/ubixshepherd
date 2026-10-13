package update

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Marker records an upgrade that has not yet proved itself. The daemon that installs
// writes it; the new daemon counts its launches in it and clears it once it has served
// for a while. A launch that finds it already counted once never proved itself, so it
// restores the previous binary instead (docs/update-check.md).
type Marker struct {
	From string `json:"from"`
	To   string `json:"to"`
	// Launches is how many times the new binary has started.
	Launches int       `json:"launches"`
	At       time.Time `json:"at"`
}

// ReadMarker reads the marker at path; ok is false when there is none.
func ReadMarker(path string) (Marker, bool) {
	var m Marker
	b, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(b, &m) != nil || m.To == "" {
		return Marker{}, false
	}
	return m, true
}

// WriteMarker replaces the marker whole.
func WriteMarker(path string, m Marker) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".upgrade-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// ClearMarker removes the marker; none is not an error.
func ClearMarker(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Startup decides what a daemon of version running does about a marker at path:
// proceed (clearing a stale one, counting a first launch) or roll back (a second launch
// of a version that never proved itself).
func Startup(path, running string) (rollback bool, m Marker) {
	m, ok := ReadMarker(path)
	if !ok {
		return false, Marker{}
	}
	if m.To != running {
		ClearMarker(path)
		return false, Marker{}
	}
	if m.Launches >= 1 {
		return true, m
	}
	m.Launches++
	WriteMarker(path, m)
	return false, m
}

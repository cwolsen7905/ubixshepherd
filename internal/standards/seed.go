package standards

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed seed/house/*.md
var seedFS embed.FS

// SeedHouse writes the example `house` pack to root/house, only if that directory is not
// there yet, and reports whether it wrote. A pack the person edited or deleted rules
// from is never touched.
func SeedHouse(root string) (bool, error) {
	dst := filepath.Join(root, "house")
	if _, err := os.Stat(dst); err == nil {
		return false, nil
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return false, err
	}
	err := fs.WalkDir(seedFS, "seed/house", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := seedFS.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, filepath.Base(p)), b, 0o644)
	})
	return err == nil, err
}

// Seed returns the seed pack's files by name, for checking examples/ against it.
func Seed() (map[string][]byte, error) {
	out := map[string][]byte{}
	entries, err := seedFS.ReadDir("seed/house")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		b, err := seedFS.ReadFile("seed/house/" + e.Name())
		if err != nil {
			return nil, err
		}
		out[e.Name()] = b
	}
	return out, nil
}

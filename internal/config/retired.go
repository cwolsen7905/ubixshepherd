package config

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// RetiredKeys are config keys that were renamed or removed, by dotted path to a note
// saying what to use instead. Strict decoding rejects a key it does not know, which is
// what catches a typo; a key listed here is not a typo, so a file that still has it
// loads with the key ignored and a warning, rather than leaving a daemon that cannot
// start after an upgrade. A segment of "*" matches any key at that level, so
// "repos.*.old_key" covers every repo's profile.
//
// Add an entry in the same change that retires the key. It is empty until then; the
// tests set it for themselves.
var RetiredKeys = map[string]string{}

// stripRetired removes every RetiredKeys entry found in the YAML document b. It
// returns b unchanged, with no warnings, when none is present.
func stripRetired(b []byte) ([]byte, []string, error) {
	if len(RetiredKeys) == 0 {
		return b, nil, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, nil, err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return b, nil, nil
	}
	paths := make([]string, 0, len(RetiredKeys))
	for p := range RetiredKeys {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var warns []string
	for _, p := range paths {
		for _, at := range removePath(doc.Content[0], strings.Split(p, "."), nil) {
			warns = append(warns, fmt.Sprintf("%s: retired key, ignored: %s", at, RetiredKeys[p]))
		}
	}
	if len(warns) == 0 {
		return b, nil, nil
	}
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	if err := enc.Encode(&doc); err != nil {
		return nil, nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, nil, err
	}
	return out.Bytes(), warns, nil
}

// removePath deletes the keys of mapping n that path names, and returns the concrete
// dotted path of each one removed.
func removePath(n *yaml.Node, path, trail []string) []string {
	if n.Kind != yaml.MappingNode {
		return nil
	}
	var found []string
	kept := n.Content[:0:0]
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if path[0] != "*" && path[0] != k.Value {
			kept = append(kept, k, v)
			continue
		}
		at := append(append([]string{}, trail...), k.Value)
		if len(path) == 1 {
			found = append(found, strings.Join(at, "."))
			continue
		}
		found = append(found, removePath(v, path[1:], at)...)
		kept = append(kept, k, v)
	}
	n.Content = kept
	return found
}

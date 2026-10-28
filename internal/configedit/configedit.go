// Package configedit changes Shepherd's config file the way a careful person would: it
// edits the YAML in place, so comments, key order and sections it was not asked about
// stay as they were; it validates the result with config.Parse before anything is
// written; it refuses to write over a file that changed since it was read; and it
// writes atomically, keeping a backup of what it replaced.
//
// The round trip is yaml.v3's Node tree, which keeps comments and order but not every
// byte: indentation is normalised to two spaces, and blank lines between entries may
// move. Values are never reinterpreted.
package configedit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/ubixsys/ubixshepherd/internal/config"
)

// ErrConflict means the file changed on disk after it was read.
var ErrConflict = errors.New("config.yaml changed on disk since it was read; reload and apply the change again")

// Keep is how many backups of a replaced config are kept beside it.
const Keep = 10

// maxDepth bounds an op's path.
const maxDepth = 8

// Op is one change: set Path to Value, or remove it so the value is inherited or
// defaulted again. Path is the keys from the file's top, ["repos", "api", "gate"].
type Op struct {
	Path  []string `json:"path"`
	Value any      `json:"value,omitempty"`
	Unset bool     `json:"unset,omitempty"`
}

// ValidationError is a config the edit would produce that config.Parse refuses.
type ValidationError struct{ Problems []string }

func (e *ValidationError) Error() string { return strings.Join(e.Problems, "; ") }

// Doc is a config file as it was read.
type Doc struct {
	Path string
	// Raw is the file's bytes (the commented template for a file not written yet).
	Raw []byte
	// ETag identifies Raw; Save refuses when the file's no longer has it.
	ETag string
	// Missing says the file did not exist.
	Missing bool
}

// ETagOf is the etag of a file's contents.
func ETagOf(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:16])
}

const missingETag = "missing"

// Read reads the config file. A file not there yet reads as the template, with the etag
// "missing": saving then creates it, unless something else did first.
func Read(path string) (*Doc, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Doc{Path: path, Raw: config.Template(), ETag: missingETag, Missing: true}, nil
	}
	if err != nil {
		return nil, err
	}
	return &Doc{Path: path, Raw: b, ETag: ETagOf(b)}, nil
}

// Apply returns the file's bytes with ops applied, validated: the result must parse and
// validate as a config, or the error is a *ValidationError and nothing is returned.
func (d *Doc) Apply(ops []Op) ([]byte, error) {
	out, err := Edit(d.Raw, ops)
	if err != nil {
		return nil, err
	}
	if _, err := config.Parse(out); err != nil {
		return nil, &ValidationError{Problems: Problems(err)}
	}
	return out, nil
}

// Problems splits a validation error into one line per problem.
func Problems(err error) []string {
	var out []string
	var walk func(error)
	walk = func(e error) {
		if j, ok := e.(interface{ Unwrap() []error }); ok {
			for _, c := range j.Unwrap() {
				walk(c)
			}
			return
		}
		for _, l := range strings.Split(e.Error(), "\n") {
			if l = strings.TrimSpace(l); l != "" {
				out = append(out, l)
			}
		}
	}
	walk(err)
	return out
}

// Edit applies ops to YAML text, without validating it as a config. A file with no
// content but comments (the template) keeps them all and gets the new keys appended.
func Edit(src []byte, ops []Op) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return nil, fmt.Errorf("config.yaml is not valid YAML: %w", err)
	}
	appendOnly := doc.Kind == 0
	if appendOnly {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("config.yaml is not a mapping at its top")
	}
	root := doc.Content[0]
	for _, op := range ops {
		if err := apply(root, op); err != nil {
			return nil, err
		}
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	if !appendOnly {
		return buf.Bytes(), nil
	}
	if len(root.Content) == 0 {
		return src, nil
	}
	out := bytes.TrimRight(src, "\n")
	if len(out) > 0 {
		out = append(out, '\n', '\n')
	}
	return append(out, buf.Bytes()...), nil
}

func apply(root *yaml.Node, op Op) error {
	if n := len(op.Path); n == 0 || n > maxDepth {
		return fmt.Errorf("path %q: need 1 to %d keys", strings.Join(op.Path, "."), maxDepth)
	}
	for _, k := range op.Path {
		if strings.TrimSpace(k) == "" {
			return fmt.Errorf("path %q has an empty key", strings.Join(op.Path, "."))
		}
	}
	if op.Unset {
		unset(root, op.Path, 0)
		return nil
	}
	val := &yaml.Node{}
	if err := val.Encode(normalize(op.Value)); err != nil {
		return fmt.Errorf("%s: %w", strings.Join(op.Path, "."), err)
	}
	cur := root
	for i, k := range op.Path {
		last := i == len(op.Path)-1
		idx := find(cur, k)
		if last {
			if idx < 0 {
				cur.Content = append(cur.Content, scalarKey(k), val)
				return nil
			}
			old := cur.Content[idx+1]
			val.HeadComment, val.LineComment, val.FootComment = old.HeadComment, old.LineComment, old.FootComment
			cur.Content[idx+1] = val
			return nil
		}
		if idx < 0 {
			m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			cur.Content = append(cur.Content, scalarKey(k), m)
			cur = m
			continue
		}
		next := cur.Content[idx+1]
		if next.Kind == yaml.ScalarNode && next.Tag == "!!null" {
			next.Kind, next.Tag, next.Value, next.Style = yaml.MappingNode, "!!map", "", 0
		}
		if next.Kind != yaml.MappingNode {
			return fmt.Errorf("%s: %q is not a section", strings.Join(op.Path, "."), strings.Join(op.Path[:i+1], "."))
		}
		cur = next
	}
	return nil
}

// unset removes the key at path and prunes the sections it empties, but never the
// file's own sections or a repo's entry: a repo with no overrides is still configured.
func unset(n *yaml.Node, path []string, depth int) {
	idx := find(n, path[0])
	if idx < 0 {
		return
	}
	if len(path) == 1 {
		n.Content = slices.Delete(n.Content, idx, idx+2)
		return
	}
	child := n.Content[idx+1]
	if child.Kind != yaml.MappingNode {
		return
	}
	unset(child, path[1:], depth+1)
	if len(child.Content) == 0 && depth >= 2 {
		n.Content = slices.Delete(n.Content, idx, idx+2)
	}
}

func find(m *yaml.Node, key string) int {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return i
		}
	}
	return -1
}

func scalarKey(k string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k}
}

// normalize turns JSON's numbers into integers where they are whole, so 4 is written 4.
func normalize(v any) any {
	switch t := v.(type) {
	case float64:
		if t == math.Trunc(t) && math.Abs(t) < 1<<53 {
			return int64(t)
		}
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = normalize(e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[k] = normalize(e)
		}
		return out
	}
	return v
}

// saveMu serialises writes in this process; the etag check covers other writers.
var saveMu sync.Mutex

// Save writes data (from Apply) over the file, if the file still has the etag it was
// read with: it keeps a backup of what it replaces, writes a temporary file beside the
// config and renames it over, so a reader sees the old file or the new, never half.
// It returns the new etag.
func Save(d *Doc, data []byte) (string, error) {
	saveMu.Lock()
	defer saveMu.Unlock()
	cur, err := os.ReadFile(d.Path)
	mode := os.FileMode(0o600)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if d.ETag != missingETag {
			return "", ErrConflict
		}
	case err != nil:
		return "", err
	default:
		if ETagOf(cur) != d.ETag {
			return "", ErrConflict
		}
		if st, err := os.Stat(d.Path); err == nil {
			mode = st.Mode().Perm()
		}
		if err := backup(d.Path, cur, mode); err != nil {
			return "", fmt.Errorf("keep a backup: %w", err)
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(d.Path), ".config-*.yaml")
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	fail := func(err error) (string, error) {
		tmp.Close()
		os.Remove(name)
		return "", err
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	if err := os.Chmod(name, mode); err != nil {
		os.Remove(name)
		return "", err
	}
	if err := os.Rename(name, d.Path); err != nil {
		os.Remove(name)
		return "", err
	}
	return ETagOf(data), nil
}

func backup(path string, cur []byte, mode os.FileMode) error {
	// A save within the same millisecond gets a zero-padded counter suffix, which
	// sorts after the bare stamp, so names stay unique and sort oldest first.
	stamp := fmt.Sprintf("%s.bak.%s", path, time.Now().UTC().Format("20060102T150405.000"))
	name := stamp
	for n := 1; ; n++ {
		f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if err == nil {
			_, werr := f.Write(cur)
			if cerr := f.Close(); werr == nil {
				werr = cerr
			}
			if werr != nil {
				os.Remove(name)
				return werr
			}
			break
		}
		if !os.IsExist(err) {
			return err
		}
		name = fmt.Sprintf("%s.%04d", stamp, n)
	}
	old, _ := filepath.Glob(path + ".bak.*")
	sort.Strings(old)
	for len(old) > Keep {
		os.Remove(old[0])
		old = old[1:]
	}
	return nil
}

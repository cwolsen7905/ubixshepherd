package crook

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"slices"
	"sort"
	"strings"
)

// The layers, earliest first. A later layer's rule with the same id replaces an earlier
// one.
const (
	LayerBuiltin   = "builtin"
	LayerWorkspace = "workspace"
	LayerProject   = "project"
	LayerRepo      = "repo"
)

// LayerOrder is the order layers are applied in.
var LayerOrder = []string{LayerBuiltin, LayerWorkspace, LayerProject, LayerRepo}

// LayerFiles is the raw rule files of one layer: file name (<id>.md) to its text.
type LayerFiles struct {
	Layer string
	Files map[string][]byte
}

// ReadLayerDir reads <dir>/rules/*.md. A layer folder with no rules folder has none.
func ReadLayerDir(layer, dir string) (LayerFiles, error) {
	return ReadLayerFS(layer, os.DirFS(dir))
}

// ReadLayerFS reads rules/*.md from a layer folder held in fsys. The caller chooses the
// source: a directory, or a repo's base branch rather than a working tree.
func ReadLayerFS(layer string, fsys fs.FS) (LayerFiles, error) {
	out := LayerFiles{Layer: layer, Files: map[string][]byte{}}
	entries, err := fs.ReadDir(fsys, "rules")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return out, nil
		}
		return out, fmt.Errorf("%s layer: %w", layer, err)
	}
	for _, e := range entries {
		if e.IsDir() || path.Ext(e.Name()) != ".md" {
			continue
		}
		b, err := fs.ReadFile(fsys, path.Join("rules", e.Name()))
		if err != nil {
			return out, fmt.Errorf("%s layer: %w", layer, err)
		}
		out.Files[e.Name()] = b
	}
	return out, nil
}

// LoadError is a rule file that was not used, and why.
type LoadError struct {
	Layer, File string
	Err         error
}

func (e LoadError) Error() string { return fmt.Sprintf("%s/rules/%s: %v", e.Layer, e.File, e.Err) }

// Override records a later layer taking the place of, or switching off, an earlier rule.
type Override struct {
	ID string
	// From is the layer whose rule was replaced; By the layer that replaced or disabled it.
	From, By string
	Disabled bool
	// Loosening: the replacing rule gives a less restrictive verdict than the one it
	// replaced, or the switched-off rule would have held something back.
	Loosening bool
}

// RuleSet is the effective rules after layering.
type RuleSet struct {
	// Rules are in layer order, then by id.
	Rules     []*FileRule
	Overrides []Override
	Errors    []LoadError
}

// Warnings lists each rule's warnings with its id.
func (s RuleSet) Warnings() []string {
	var out []string
	for _, r := range s.Rules {
		for _, w := range r.Warnings {
			out = append(out, fmt.Sprintf("%s (%s): %s", r.ID, r.Layer, w))
		}
	}
	return out
}

func layerIndex(l string) int { return slices.Index(LayerOrder, l) }

// Merge layers the files. Files may come in any order; layers apply in LayerOrder. A
// file that fails to load is reported and left out, and does not remove an earlier
// layer's rule of the same id.
func Merge(layers ...LayerFiles) RuleSet {
	var set RuleSet
	byLayer := map[string]LayerFiles{}
	for _, l := range layers {
		if layerIndex(l.Layer) < 0 {
			set.Errors = append(set.Errors, LoadError{Layer: l.Layer, Err: fmt.Errorf("unknown layer: use %s", strings.Join(LayerOrder, ", "))})
			continue
		}
		byLayer[l.Layer] = l
	}
	eff := map[string]*FileRule{}
	for _, name := range LayerOrder {
		l, ok := byLayer[name]
		if !ok {
			continue
		}
		files := make([]string, 0, len(l.Files))
		for f := range l.Files {
			files = append(files, f)
		}
		sort.Strings(files)
		for _, file := range files {
			id, ok := strings.CutSuffix(file, ".md")
			if !ok {
				set.Errors = append(set.Errors, LoadError{name, file, fmt.Errorf("a rule file ends in .md")})
				continue
			}
			r, err := ParseRule(name, id, l.Files[file])
			if err != nil {
				set.Errors = append(set.Errors, LoadError{name, file, err})
				continue
			}
			old := eff[id]
			switch {
			case r.Disabled:
				if old != nil {
					set.Overrides = append(set.Overrides, Override{ID: id, From: old.Layer, By: name, Disabled: true, Loosening: old.restrictiveness() > 0})
					delete(eff, id)
				}
			default:
				if old != nil {
					set.Overrides = append(set.Overrides, Override{ID: id, From: old.Layer, By: name, Loosening: r.restrictiveness() < old.restrictiveness()})
				}
				eff[id] = r
			}
		}
	}
	for _, r := range eff {
		set.Rules = append(set.Rules, r)
	}
	sort.Slice(set.Rules, func(i, j int) bool {
		a, b := set.Rules[i], set.Rules[j]
		if li, lj := layerIndex(a.Layer), layerIndex(b.Layer); li != lj {
			return li < lj
		}
		return a.ID < b.ID
	})
	return set
}

// restrictiveness is the rank of the verdict a rule gives; rules that give none
// (classifiers) rank as proceed.
func (r *FileRule) restrictiveness() int { return rank(r.Then) }

// rank orders verdicts: refuse above ask above hold above proceed.
func rank(k Kind) int {
	switch k {
	case Refuse:
		return 3
	case Ask:
		return 2
	case Hold:
		return 1
	}
	return 0
}

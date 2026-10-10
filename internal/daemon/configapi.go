package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/configedit"
)

// The config editor's routes (api/config.go). Reading and previewing are for a browser
// session; saving is the operator's, and a browser's only when its sign-in link asked
// for it (api.WebSigninRequest). The front desk is refused all of it: it could widen its
// own autonomy.

// canWriteConfig says whether the caller may save config changes, and when not, what to
// do about it.
func canWriteConfig(p principal) (bool, string) {
	switch p.Role {
	case RoleOperator:
		return true, ""
	case RoleWeb:
		if p.web != nil && p.web.allowSettings {
			return true, ""
		}
		return false, "this browser session may read settings but not save them; sign in again with shepherd web --allow-settings"
	}
	return false, "only the operator may save settings"
}

func (s *Server) configDoc(w http.ResponseWriter, r *http.Request) {
	doc, ok := s.readConfig(w)
	if !ok {
		return
	}
	out := api.ConfigDoc{Path: doc.Path, ETag: doc.ETag, Missing: doc.Missing, Sections: configSections()}
	out.CanWrite, out.Why = canWriteConfig(principalOf(r.Context()))
	var raw map[string]any
	if err := yaml.Unmarshal(doc.Raw, &raw); err != nil {
		out.Raw = map[string]any{}
	} else if raw != nil {
		out.Raw = raw
	} else {
		out.Raw = map[string]any{}
	}
	cfg, err := config.Parse(doc.Raw)
	if err != nil {
		// A hand edit left the file unloadable: show the config in force, and say so.
		cfg = s.LiveConfig()
		out.Invalid = configedit.Problems(err)
	}
	out.Effective = toMap(cfg)
	out.Defaults = api.ConfigProfile{Raw: subMap(out.Raw, "defaults"), Effective: toMap(cfg.Defaults), Overridden: leaves(subMap(out.Raw, "defaults"))}
	out.Repos = map[string]api.ConfigProfile{}
	rawRepos := subMap(out.Raw, "repos")
	for name := range cfg.Repos {
		rr := subMap(rawRepos, name)
		out.Repos[name] = api.ConfigProfile{Raw: rr, Effective: toMap(cfg.Profile(name)), Overridden: leaves(rr)}
	}
	out.Projects = map[string]any{}
	for name := range cfg.Projects {
		out.Projects[name] = map[string]any{"raw": subMap(subMap(out.Raw, "projects"), name), "effective": toMap(cfg.Project(name))}
	}
	writeJSON(w, http.StatusOK, out)
}

// readConfig reads the file, or says the daemon has none to edit.
func (s *Server) readConfig(w http.ResponseWriter) (*configedit.Doc, bool) {
	if s.ConfigPath == "" {
		writeError(w, http.StatusServiceUnavailable, errors.New("this daemon has no config file"))
		return nil, false
	}
	doc, err := configedit.Read(s.ConfigPath)
	if err != nil {
		s.fail(w, err)
		return nil, false
	}
	return doc, true
}

// planConfig applies a change to the file as it is now and reports what it would do. ok
// is false when it has already answered with an error.
type configPlan struct {
	doc  *configedit.Doc
	out  []byte
	risk []api.ConfigRisk
	diff string
}

func (s *Server) planConfig(w http.ResponseWriter, r *http.Request, ch api.ConfigChange) (configPlan, bool) {
	var p configPlan
	doc, ok := s.readConfig(w)
	if !ok {
		return p, false
	}
	if ch.ETag != doc.ETag {
		writeJSON(w, http.StatusConflict, api.ConfigRefusal{Error: configedit.ErrConflict.Error()})
		return p, false
	}
	if len(ch.Ops) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("no changes"))
		return p, false
	}
	ops := make([]configedit.Op, len(ch.Ops))
	for i, o := range ch.Ops {
		ops[i] = configedit.Op{Path: o.Path, Value: o.Value, Unset: o.Unset}
	}
	out, err := doc.Apply(ops)
	if err != nil {
		var ve *configedit.ValidationError
		if errors.As(err, &ve) {
			writeJSON(w, http.StatusUnprocessableEntity, api.ConfigRefusal{Error: "the change makes an invalid config: " + ve.Problems[0], Problems: ve.Problems})
		} else {
			writeError(w, http.StatusBadRequest, err)
		}
		return p, false
	}
	p = configPlan{doc: doc, out: out, diff: configedit.Diff(string(doc.Raw), string(out))}
	// Both parse: the file as it is now was read by this daemon's own loader.
	prev, perr := config.Parse(doc.Raw)
	next, _ := config.Parse(out)
	if perr != nil {
		prev = config.Default()
	}
	for _, rk := range configedit.Risks(prev, next) {
		p.risk = append(p.risk, api.ConfigRisk(rk))
	}
	return p, true
}

func (s *Server) configPreview(w http.ResponseWriter, r *http.Request) {
	var ch api.ConfigChange
	if !decode(w, r, &ch) {
		return
	}
	doc, ok := s.readConfig(w)
	if !ok {
		return
	}
	p, ok := s.planConfig(w, r, ch)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, api.ConfigPreview{ETag: doc.ETag, Diff: p.diff, Risky: p.risk})
}

func (s *Server) configSave(w http.ResponseWriter, r *http.Request) {
	pr := principalOf(r.Context())
	if ok, why := canWriteConfig(pr); !ok {
		writeError(w, http.StatusForbidden, errors.New(why))
		return
	}
	var ch api.ConfigChange
	if !decode(w, r, &ch) {
		return
	}
	p, ok := s.planConfig(w, r, ch)
	if !ok {
		return
	}
	var unconfirmed []api.ConfigRisk
	for _, rk := range p.risk {
		if !slices.Contains(ch.Confirm, rk.Path) {
			unconfirmed = append(unconfirmed, rk)
		}
	}
	if len(unconfirmed) > 0 {
		writeJSON(w, http.StatusPreconditionRequired, api.ConfigRefusal{
			Error: fmt.Sprintf("this change gives agents more power (%s) and needs confirming", unconfirmed[0].Path), Risky: unconfirmed})
		return
	}
	etag, err := configedit.Save(p.doc, p.out)
	if errors.Is(err, configedit.ErrConflict) {
		writeJSON(w, http.StatusConflict, api.ConfigRefusal{Error: err.Error()})
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	paths := make([]string, len(ch.Ops))
	for i, o := range ch.Ops {
		paths[i] = strings.Join(o.Path, ".")
	}
	s.Log.Info("config saved from the API", "by", pr.Role, "paths", paths, "confirmed", ch.Confirm)
	out := api.ConfigSaved{ETag: etag, Diff: p.diff}
	// The same reload a SIGHUP does, in process, so it needs no signal and works on Windows.
	if err := s.ReloadConfig(r.Context()); err != nil {
		out.ReloadError = err.Error()
	} else {
		out.Reloaded = true
	}
	writeJSON(w, http.StatusOK, out)
}

// toMap is v as the generic JSON object a client reads.
func toMap(v any) map[string]any {
	b, err := json.Marshal(v)
	m := map[string]any{}
	if err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

func subMap(m map[string]any, key string) map[string]any {
	if sub, ok := m[key].(map[string]any); ok {
		return sub
	}
	return map[string]any{}
}

// leaves lists the dotted paths of the values a map sets; a list or scalar is a leaf.
func leaves(m map[string]any) []string {
	out := []string{}
	var walk func(prefix string, m map[string]any)
	walk = func(prefix string, m map[string]any) {
		for _, k := range slices.Sorted(maps.Keys(m)) {
			if sub, ok := m[k].(map[string]any); ok && len(sub) > 0 {
				walk(prefix+k+".", sub)
				continue
			}
			out = append(out, prefix+k)
		}
	}
	walk("", m)
	return out
}

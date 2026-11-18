package crook

import (
	"fmt"
	"strings"
)

// LayerView is one layer's rules as they stand after layering.
type LayerView struct {
	Layer string     `json:"layer"`
	Rules []RuleView `json:"rules"`
}

// RuleView is one effective rule, flattened for display.
type RuleView struct {
	ID        string   `json:"id"`
	Kind      string   `json:"kind"`
	Mode      string   `json:"mode"`
	Then      string   `json:"then,omitempty"`
	Actions   []string `json:"actions,omitempty"`
	Statement string   `json:"statement,omitempty"`
	// Replaces names the layer whose rule of this id this one took the place of.
	Replaces  string   `json:"replaces,omitempty"`
	Loosening bool     `json:"loosening,omitempty"`
	Warnings  []string `json:"warnings,omitempty"`
}

// RulesView is the effective rules by layer, with what was switched off and what failed.
type RulesView struct {
	Layers   []LayerView `json:"layers"`
	Disabled []Override  `json:"disabled,omitempty"`
	Errors   []string    `json:"errors,omitempty"`
}

// View groups the effective rules by layer, every layer in LayerOrder shown even when it
// has none.
func (s RuleSet) View() RulesView {
	v := RulesView{}
	repl := map[string]Override{} // by the replacing layer and id
	for _, o := range s.Overrides {
		if o.Disabled {
			v.Disabled = append(v.Disabled, o)
			continue
		}
		repl[o.By+"/"+o.ID] = o
	}
	for _, name := range LayerOrder {
		lv := LayerView{Layer: name, Rules: []RuleView{}}
		for _, r := range s.Rules {
			if r.Layer != name {
				continue
			}
			mode := string(r.Mode)
			if mode == "" {
				mode = string(ModeLive)
			}
			rv := RuleView{ID: r.ID, Kind: string(r.Kind), Mode: mode, Then: string(r.Then),
				Actions: r.Actions, Statement: r.Statement, Warnings: r.Warnings}
			if o, ok := repl[name+"/"+r.ID]; ok {
				rv.Replaces, rv.Loosening = o.From, o.Loosening
			}
			lv.Rules = append(lv.Rules, rv)
		}
		v.Layers = append(v.Layers, lv)
	}
	for _, e := range s.Errors {
		v.Errors = append(v.Errors, e.Error())
	}
	return v
}

// Text renders the view for a terminal: a heading per layer, a line per rule, then what
// was switched off and what did not load.
func (v RulesView) Text() string {
	var b strings.Builder
	for _, l := range v.Layers {
		if l.Layer == LayerBuiltin && len(l.Rules) == 0 {
			b.WriteString("builtin: compiled into Shepherd, not rule files; see crook why\n")
			continue
		}
		fmt.Fprintf(&b, "%s (%d)\n", l.Layer, len(l.Rules))
		if len(l.Rules) == 0 {
			b.WriteString("  none\n")
		}
		for _, r := range l.Rules {
			fmt.Fprintf(&b, "  %-28s %s", r.ID, r.Kind)
			if r.Then != "" {
				fmt.Fprintf(&b, " -> %s", r.Then)
			}
			if r.Mode != string(ModeLive) {
				fmt.Fprintf(&b, " [%s]", r.Mode)
			}
			if r.Replaces != "" {
				fmt.Fprintf(&b, "  replaces %s", r.Replaces)
				if r.Loosening {
					b.WriteString(" (loosens)")
				}
			}
			b.WriteString("\n")
			for _, w := range r.Warnings {
				fmt.Fprintf(&b, "    warning: %s\n", w)
			}
		}
	}
	for _, o := range v.Disabled {
		fmt.Fprintf(&b, "switched off: %s (%s) by %s", o.ID, o.From, o.By)
		if o.Loosening {
			b.WriteString(" (loosens)")
		}
		b.WriteString("\n")
	}
	for _, e := range v.Errors {
		fmt.Fprintf(&b, "not used: %s\n", e)
	}
	return b.String()
}

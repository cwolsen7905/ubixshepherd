package desklog

import (
	"regexp"
	"sort"
	"strings"
)

type needItem struct {
	text string
	keys []string
}

var (
	itemRe = regexp.MustCompile(`^\s*\d+[.)]\s+(.*)$`)
	linkRe = regexp.MustCompile(`\]\((https?://[^)\s]+)\)|(https?://[^\s)\]>]+)`)
	idRe   = regexp.MustCompile(`(?:^|[^\w])([!#]\d+)\b`)
)

// needsYou lists the numbered items under a reply's "Needs you:" line, up to the
// "Status:" line or the blank line after the list. Each item carries the ids (!12, #7)
// and links it names, which is how a repeat is recognised.
func needsYou(reply string) []needItem {
	lines := strings.Split(reply, "\n")
	i := 0
	for ; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "Needs you:") {
			break
		}
	}
	var out []needItem
	for i++; i < len(lines); i++ {
		l := lines[i]
		if strings.HasPrefix(strings.TrimSpace(l), "Status:") {
			break
		}
		if m := itemRe.FindStringSubmatch(l); m != nil {
			out = append(out, needItem{text: m[1]})
		} else if strings.TrimSpace(l) == "" {
			if len(out) > 0 {
				break
			}
		} else if len(out) > 0 && (strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t")) {
			out[len(out)-1].text += " " + strings.TrimSpace(l)
		} else if len(out) > 0 {
			break
		}
	}
	for n := range out {
		out[n].keys = keysOf(out[n].text)
	}
	return out
}

func keysOf(text string) []string {
	set := map[string]bool{}
	for _, m := range linkRe.FindAllStringSubmatch(text, -1) {
		u := m[1]
		if u == "" {
			u = m[2]
		}
		set[strings.TrimRight(u, "/.,;")] = true
	}
	for _, m := range idRe.FindAllStringSubmatch(text, -1) {
		set[m[1]] = true
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

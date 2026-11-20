// Package reviewbot counts what a review bot did on a forge's merge requests: the jobs it
// ran, what it proposed, what a person dismissed, what waits on a person, the fixes it
// pushed and what it spent. It reads the forge only, through internal/forge and the
// person's login in the forge's CLI, and stores nothing.
//
// A bot is configuration (config.ReviewBot): its account and the prefix of its status
// labels. Nothing here names a product; the markers it reads are the generic ones in
// docs/review-bots.md.
package reviewbot

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/config"
	"github.com/ubixsys/ubixshepherd/internal/forge"
)

// The status label suffixes a bot's LabelPrefix is followed by.
const (
	LabelWorking  = "working"
	LabelPushed   = "pushed"
	LabelProposed = "proposed"
	LabelNeedsYou = "needs-you"
)

// MaxMRs is how many merge requests per project are read, newest update first. A project
// with more says so (Project.Truncated) and counts the newest.
const MaxMRs = 60

// DefaultTTL is how long a report is reused before the forge is read again.
const DefaultTTL = 5 * time.Minute

// Counts are what one bot did in one project, or in all of them.
//
// Jobs counts merge requests the bot touched (a status label of its own, or a note by its
// account). Working, Proposals, NeedsYou and Fixes count merge requests by the status
// label they carry now: the bot replaces its labels each round, so a request counts once,
// under the last one. Dismissals count threads the bot opened that someone other than the
// bot resolved. SpendUSD sums the cost stamped on the bot's notes; a bot that edits one
// summary note per request leaves one stamp, so this is the latest round's cost.
type Counts struct {
	Jobs       int     `json:"jobs"`
	Working    int     `json:"working"`
	Proposals  int     `json:"proposals"`
	Dismissals int     `json:"dismissals"`
	NeedsYou   int     `json:"needs_you"`
	Fixes      int     `json:"fixes"`
	SpendUSD   float64 `json:"spend_usd"`
	// Stamped is how many merge requests carried a cost stamp.
	Stamped int `json:"stamped"`
}

func (c *Counts) add(o Counts) {
	c.Jobs += o.Jobs
	c.Working += o.Working
	c.Proposals += o.Proposals
	c.Dismissals += o.Dismissals
	c.NeedsYou += o.NeedsYou
	c.Fixes += o.Fixes
	c.SpendUSD += o.SpendUSD
	c.Stamped += o.Stamped
}

// Item is a merge request that waits on a person, for the page to link.
type Item struct {
	IID   int    `json:"iid"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// Project is one repo's counts, or why they could not be read.
type Project struct {
	Name string `json:"name"`
	Counts
	// NeedsYouItems are the open requests carrying the needs-you label.
	NeedsYouItems []Item `json:"needs_you_items,omitempty"`
	// Truncated says the repo had more than MaxMRs requests in the window.
	Truncated bool `json:"truncated,omitempty"`
	// Error is why the repo could not be read; its counts are zero then.
	Error string `json:"error,omitempty"`
}

// Bot is one configured bot's report.
type Bot struct {
	Name        string    `json:"name"`
	User        string    `json:"user"`
	LabelPrefix string    `json:"label_prefix"`
	WindowDays  int       `json:"window_days"`
	Projects    []Project `json:"projects"`
	Total       Counts    `json:"total"`
}

// Report is every configured bot's counts as of a time.
type Report struct {
	AsOf time.Time `json:"as_of"`
	Bots []Bot     `json:"bots"`
}

// Collector reads the forge for bots and keeps the answer for TTL.
type Collector struct {
	// Forge is the forge for a repo name, as Shepherd knows it.
	Forge func(repo string) (forge.Forge, error)
	// Now and TTL are for tests; the zero values are time.Now and DefaultTTL.
	Now func() time.Time
	TTL time.Duration

	mu   sync.Mutex
	key  string
	at   time.Time
	last Report
}

func (c *Collector) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

// Report answers for the bots, from the cache when it is the same configuration and
// younger than TTL, unless refresh. Reads of one repo that fail are in that repo's
// Error: the report is always whole.
func (c *Collector) Report(ctx context.Context, bots []config.ReviewBot, refresh bool) Report {
	c.mu.Lock()
	defer c.mu.Unlock()
	raw, _ := json.Marshal(bots)
	key := string(raw)
	ttl := c.TTL
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	if !refresh && key == c.key && !c.at.IsZero() && c.now().Sub(c.at) < ttl {
		return c.last
	}
	rep := Report{AsOf: c.now(), Bots: []Bot{}}
	for _, b := range bots {
		rep.Bots = append(rep.Bots, c.bot(ctx, b))
	}
	c.key, c.at, c.last = key, rep.AsOf, rep
	return rep
}

func (c *Collector) bot(ctx context.Context, b config.ReviewBot) Bot {
	out := Bot{Name: b.Name, User: b.User, LabelPrefix: b.LabelPrefix, WindowDays: b.Window(), Projects: make([]Project, len(b.Projects))}
	since := c.now().AddDate(0, 0, -b.Window())
	var wg sync.WaitGroup
	for i, name := range b.Projects {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out.Projects[i] = c.project(ctx, b, name, since)
		}()
	}
	wg.Wait()
	for _, p := range out.Projects {
		out.Total.add(p.Counts)
	}
	return out
}

func (c *Collector) project(ctx context.Context, b config.ReviewBot, name string, since time.Time) Project {
	p := Project{Name: name}
	f, err := c.Forge(name)
	if err != nil {
		p.Error = err.Error()
		return p
	}
	l, ok := f.(forge.MRLister)
	if !ok {
		p.Error = fmt.Sprintf("%s cannot be read for review bots yet", f.Name())
		return p
	}
	mrs, more, err := l.MergeRequests(ctx, since, MaxMRs)
	if err != nil {
		p.Error = err.Error()
		return p
	}
	p.Truncated = more
	results := make([]mrResult, len(mrs))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, m := range mrs {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			ds, err := l.Discussions(ctx, m.IID)
			results[i] = readMR(b, m, ds, err)
		}()
	}
	wg.Wait()
	var failed int
	var firstErr error
	for _, r := range results {
		if r.err != nil {
			failed++
			if firstErr == nil {
				firstErr = r.err
			}
		}
		p.Counts.add(r.Counts)
		if r.needsYouOpen {
			p.NeedsYouItems = append(p.NeedsYouItems, r.item)
		}
	}
	if failed > 0 {
		p.Error = fmt.Sprintf("%d of %d merge requests could not be read: %v", failed, len(mrs), firstErr)
	}
	sort.Slice(p.NeedsYouItems, func(i, j int) bool { return p.NeedsYouItems[i].IID > p.NeedsYouItems[j].IID })
	return p
}

type mrResult struct {
	Counts
	needsYouOpen bool
	item         Item
	err          error
}

// stamp is the usage line a review job appends to its note: the model, then the cost in
// dollars. Only an italic line that starts with the model is read, so a quoted dollar amount in a
// finding is not counted.
var stamp = regexp.MustCompile("(?m)^_Model `[^`]+`[^\\n]*?\\$(\\d+(?:\\.\\d+)?)")

func readMR(b config.ReviewBot, m forge.MRSummary, ds []forge.Discussion, err error) mrResult {
	var r mrResult
	if err != nil {
		r.err = fmt.Errorf("!%d: %w", m.IID, err)
		return r
	}
	has := map[string]bool{}
	labelled := false
	for _, l := range m.Labels {
		if rest, ok := strings.CutPrefix(l, b.LabelPrefix); ok {
			labelled = true
			has[rest] = true
		}
	}
	botNote := false
	for _, d := range ds {
		if len(d.Notes) == 0 {
			continue
		}
		first := d.Notes[0]
		opened := strings.EqualFold(first.Author, b.User)
		for _, n := range d.Notes {
			if !strings.EqualFold(n.Author, b.User) {
				continue
			}
			botNote = true
			if v, ok := stampedCost(n.Text); ok {
				r.SpendUSD += v
				r.Stamped = 1
			}
		}
		if opened && d.Resolvable && d.Resolved && d.ResolvedBy != "" && !strings.EqualFold(d.ResolvedBy, b.User) {
			r.Dismissals++
		}
	}
	if !labelled && !botNote {
		return r
	}
	r.Jobs = 1
	if has[LabelWorking] {
		r.Working = 1
	}
	if has[LabelProposed] {
		r.Proposals = 1
	}
	if has[LabelPushed] {
		r.Fixes = 1
	}
	if has[LabelNeedsYou] {
		r.NeedsYou = 1
		if m.State == "opened" {
			r.needsYouOpen = true
			r.item = Item{IID: m.IID, Title: m.Title, URL: m.URL}
		}
	}
	return r
}

func stampedCost(text string) (float64, bool) {
	m := stamp.FindStringSubmatch(text)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(m[1], 64)
	return v, err == nil
}

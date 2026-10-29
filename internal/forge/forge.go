// Package forge reads what a forge knows about a lane's branch: its merge request, the
// request's pipeline, the jobs that failed and their logs. Proof comes from here: a lane
// is merged when the forge has a merge commit for it, not when an agent says so.
//
// Forges are reached through their own CLIs (glab, gh) and the person's login in them,
// so Shepherd keeps no forge credentials.
package forge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// MR is a merge (or pull) request.
type MR struct {
	IID   int    `json:"iid"`
	State string `json:"state"` // opened, merged, closed, locked
	// SHA is the request's head; MergeSHA the commit that landed it (merge or squash).
	SHA      string    `json:"sha"`
	MergeSHA string    `json:"merge_sha,omitempty"`
	URL      string    `json:"url"`
	Pipeline *Pipeline `json:"pipeline,omitempty"`
	// Author is the request's author's username; AuthorBot says the forge marks that
	// account a bot. Draft is a draft request. MergeStatus is the forge's own verdict on
	// mergeability ("mergeable" on GitLab) and is empty when the forge did not say.
	Author      string `json:"author,omitempty"`
	AuthorBot   bool   `json:"author_bot,omitempty"`
	Draft       bool   `json:"draft,omitempty"`
	MergeStatus string `json:"merge_status,omitempty"`
	// Created and Updated are when the forge made the request and last changed it
	// (comments and pushes count); zero when the forge did not say.
	Created, Updated time.Time
}

// Pipeline is a request's head pipeline.
type Pipeline struct {
	ID     int64  `json:"id"`
	Status string `json:"status"` // running, pending, success, failed, canceled, skipped, ...
	SHA    string `json:"sha"`
	URL    string `json:"url"`
	// Started is when a runner picked the pipeline up, Finished when it ended; zero
	// while not begun or still running.
	Started, Finished time.Time
}

// Job is one failed job of a pipeline.
type Job struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Thread is an unresolved review discussion on a merge request that someone has to act
// on: resolvable, not resolved, and not a system note.
type Thread struct {
	// ID is the forge's discussion id, stable for the thread's life.
	ID string `json:"id"`
	// Author is the first note's author's username.
	Author string `json:"author,omitempty"`
	// File and Line say where on the diff it was left; empty and 0 for a general thread.
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`
	// Body is the thread's notes in order, one per line, cleaned and cut short.
	Body string `json:"body"`
	// NoteID is the first note's id and URL the link to it (<mr>#note_<id>); GitLab
	// cannot link a discussion id, only a note.
	NoteID int64  `json:"note_id,omitempty"`
	URL    string `json:"url,omitempty"`
	// Notes is how many notes the thread has, system notes aside: it grows when anyone
	// replies, so a count above the one handed back says the thread was answered.
	Notes int `json:"notes,omitempty"`
}

// Title is a short link text for the thread: its first note's first line, cut.
func (t Thread) Title() string {
	body := t.Body
	if i := strings.Index(body, "\n"); i >= 0 {
		body = body[:i]
	}
	if i := strings.Index(body, ": "); i >= 0 && i < 40 {
		body = body[i+2:] // drop the "author: " prefix
	}
	body = strings.Join(strings.Fields(body), " ")
	if r := []rune(body); len(r) > 60 {
		body = string(r[:60]) + "..."
	}
	if body == "" {
		return "thread"
	}
	return body
}

// Link is the thread as a markdown link with its short id in brackets, or a bare
// description when no URL is known.
func (t Thread) Link() string {
	id := t.ID
	if len(id) > 8 {
		id = id[:8]
	}
	title := strings.NewReplacer("[", "(", "]", ")").Replace(t.Title())
	if t.URL == "" {
		return fmt.Sprintf("%s [%s]", title, id)
	}
	return fmt.Sprintf("[%s](%s) [%s]", title, t.URL, id)
}

// Linker is a Forge that can build links to its merge requests, pipelines and jobs.
type Linker interface {
	MRURL(iid int) string
	PipelineURL(id int64) string
	JobURL(id int64) string
	// ThreadURL is the link to the first note of a merge request's discussion.
	ThreadURL(ctx context.Context, mr int, discussion string) (string, error)
}

// ThreadCommander is a Forge that can say, as shell commands for an agent, how to reply
// on a review thread and resolve it. The agent runs them with its own login; Shepherd
// holds no token.
type ThreadCommander interface {
	// ReplyCommand posts a note on the discussion; the agent fills in the body.
	ReplyCommand(mr int, discussion string) string
	// ResolveCommand resolves the discussion.
	ResolveCommand(mr int, discussion string) string
}

// ThreadReader is a Forge that can list a request's review threads. GitLab can.
type ThreadReader interface {
	// Threads is the merge request's unresolved review threads, oldest first.
	Threads(ctx context.Context, mr int) ([]Thread, error)
}

// Forge answers for one repository.
type Forge interface {
	Name() string
	// MRForBranch is the most recently updated request from the branch, or nil.
	MRForBranch(ctx context.Context, branch string) (*MR, error)
	FailedJobs(ctx context.Context, pipeline int64) ([]Job, error)
	// JobLog is the end of a job's log, cleaned for reading.
	JobLog(ctx context.Context, job int64, lines int) (string, error)
	// CreateMR opens a merge request from source into target.
	CreateMR(ctx context.Context, source, target, title, description string) (*MR, error)
	// RefPipeline is the latest pipeline for a branch or tag, or nil.
	RefPipeline(ctx context.Context, ref string) (*Pipeline, error)
}

// Remote is a git remote URL split into host and repository path.
type Remote struct {
	Host, Path string
}

var scpLike = regexp.MustCompile(`^(?:[^@/]+@)?([^:/]+):(.+)$`)

// ParseRemote splits a remote URL: git@host:group/repo.git, ssh://git@host:22/group/repo,
// https://host/group/repo.git.
func ParseRemote(remote string) (Remote, error) {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return Remote{}, fmt.Errorf("no remote")
	}
	var r Remote
	if strings.Contains(remote, "://") {
		u, err := url.Parse(remote)
		if err != nil {
			return r, err
		}
		r = Remote{Host: u.Hostname(), Path: strings.TrimPrefix(u.Path, "/")}
	} else if m := scpLike.FindStringSubmatch(remote); m != nil {
		r = Remote{Host: m[1], Path: m[2]}
	} else {
		return r, fmt.Errorf("remote %q: not a URL Shepherd can read", remote)
	}
	r.Path = strings.TrimSuffix(strings.TrimSuffix(r.Path, "/"), ".git")
	if r.Host == "" || r.Path == "" {
		return r, fmt.Errorf("remote %q: no host or path", remote)
	}
	return r, nil
}

// For returns the forge for a remote. GitHub is recognised but not read yet (it is the
// mirror's forge in v1); any other host is taken to be GitLab.
func For(remote string) (Forge, error) {
	r, err := ParseRemote(remote)
	if err != nil {
		return nil, err
	}
	if r.Host == "github.com" {
		return nil, fmt.Errorf("GitHub as a lane's forge is not supported yet (%s)", r.Path)
	}
	return &GitLab{Host: r.Host, Project: r.Path}, nil
}

// GitLab reads a GitLab project through glab.
type GitLab struct {
	Host, Project string
	// Run runs glab and returns its stdout; tests replace it.
	Run func(ctx context.Context, args ...string) ([]byte, error)
}

func (g *GitLab) Name() string { return "gitlab" }

func (g *GitLab) api(ctx context.Context, path string, v any) error {
	run := g.Run
	if run == nil {
		run = glab
	}
	out, err := run(ctx, "api", "--hostname", g.Host, path)
	if err != nil {
		return err
	}
	if v == nil {
		return nil
	}
	if err := json.Unmarshal(out, v); err != nil {
		return fmt.Errorf("glab api %s: %w", path, err)
	}
	return nil
}

func glab(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "glab", args...)
	var errb strings.Builder
	cmd.Stderr = &errb
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(errb.String()); msg != "" {
			return nil, fmt.Errorf("glab %s: %s", strings.Join(args, " "), msg)
		}
		return nil, fmt.Errorf("glab %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}

// ProjectURL is the project's web address.
func (g *GitLab) ProjectURL() string { return "https://" + g.Host + "/" + g.Project }

func (g *GitLab) MRURL(iid int) string {
	return fmt.Sprintf("%s/-/merge_requests/%d", g.ProjectURL(), iid)
}
func (g *GitLab) PipelineURL(id int64) string {
	return fmt.Sprintf("%s/-/pipelines/%d", g.ProjectURL(), id)
}
func (g *GitLab) JobURL(id int64) string { return fmt.Sprintf("%s/-/jobs/%d", g.ProjectURL(), id) }

// ThreadURL asks the forge for the discussion and links its first note.
func (g *GitLab) ThreadURL(ctx context.Context, mr int, discussion string) (string, error) {
	var d glDiscussion
	q := fmt.Sprintf("%s/merge_requests/%d/discussions/%s", g.project(), mr, url.PathEscape(discussion))
	if err := g.api(ctx, q, &d); err != nil {
		return "", err
	}
	if len(d.Notes) == 0 || d.Notes[0].ID == 0 {
		return "", fmt.Errorf("discussion %s on !%d has no notes", discussion, mr)
	}
	return fmt.Sprintf("%s#note_%d", g.MRURL(mr), d.Notes[0].ID), nil
}

func (g *GitLab) ReplyCommand(mr int, discussion string) string {
	return fmt.Sprintf("glab api -X POST '%s/merge_requests/%d/discussions/%s/notes' -f body='...'", g.project(), mr, url.PathEscape(discussion))
}

func (g *GitLab) ResolveCommand(mr int, discussion string) string {
	return fmt.Sprintf("glab api -X PUT '%s/merge_requests/%d/discussions/%s' -f resolved=true", g.project(), mr, url.PathEscape(discussion))
}

func (g *GitLab) project() string { return "projects/" + url.PathEscape(g.Project) }

type glMR struct {
	IID          int         `json:"iid"`
	State        string      `json:"state"`
	SHA          string      `json:"sha"`
	MergeCommit  string      `json:"merge_commit_sha"`
	SquashCommit string      `json:"squash_commit_sha"`
	WebURL       string      `json:"web_url"`
	CreatedAt    string      `json:"created_at"`
	UpdatedAt    string      `json:"updated_at"`
	HeadPipeline *glPipeline `json:"head_pipeline"`
	Draft        bool        `json:"draft"`
	MergeStatus  string      `json:"detailed_merge_status"`
	Author       struct {
		Username string `json:"username"`
		Bot      bool   `json:"bot"`
	} `json:"author"`
}

type glPipeline struct {
	ID         int64  `json:"id"`
	Status     string `json:"status"`
	SHA        string `json:"sha"`
	WebURL     string `json:"web_url"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
}

func (p glPipeline) pipeline() *Pipeline {
	return &Pipeline{ID: p.ID, Status: p.Status, SHA: p.SHA, URL: p.WebURL,
		Started: glTime(p.StartedAt), Finished: glTime(p.FinishedAt)}
}

// glTime reads one of GitLab's timestamps, in UTC; zero for null, empty or unreadable.
func glTime(v string) time.Time {
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

func (g *GitLab) MRForBranch(ctx context.Context, branch string) (*MR, error) {
	var list []glMR
	q := fmt.Sprintf("%s/merge_requests?source_branch=%s&state=all&order_by=updated_at&per_page=1", g.project(), url.QueryEscape(branch))
	if err := g.api(ctx, q, &list); err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	// The list leaves the head pipeline out; the request itself has it.
	var one glMR
	if err := g.api(ctx, fmt.Sprintf("%s/merge_requests/%d", g.project(), list[0].IID), &one); err != nil {
		return nil, err
	}
	mr := &MR{IID: one.IID, State: one.State, SHA: one.SHA, URL: one.WebURL, MergeSHA: one.SquashCommit,
		Created: glTime(one.CreatedAt), Updated: glTime(one.UpdatedAt),
		Author: one.Author.Username, AuthorBot: one.Author.Bot, Draft: one.Draft, MergeStatus: one.MergeStatus}
	if one.MergeCommit != "" {
		mr.MergeSHA = one.MergeCommit
	}
	if p := one.HeadPipeline; p != nil {
		mr.Pipeline = p.pipeline()
	}
	return mr, nil
}

func (g *GitLab) CreateMR(ctx context.Context, source, target, title, description string) (*MR, error) {
	run := g.Run
	if run == nil {
		run = glab
	}
	out, err := run(ctx, "api", "--hostname", g.Host, "--method", "POST", g.project()+"/merge_requests",
		"--raw-field", "source_branch="+source, "--raw-field", "target_branch="+target,
		"--raw-field", "title="+title, "--raw-field", "description="+description,
		"--raw-field", "remove_source_branch=true")
	if err != nil {
		return nil, err
	}
	var m glMR
	if err := json.Unmarshal(out, &m); err != nil {
		return nil, fmt.Errorf("create merge request: %w", err)
	}
	return &MR{IID: m.IID, State: m.State, SHA: m.SHA, URL: m.WebURL}, nil
}

func (g *GitLab) RefPipeline(ctx context.Context, ref string) (*Pipeline, error) {
	var list []glPipeline
	if err := g.api(ctx, fmt.Sprintf("%s/pipelines?ref=%s&order_by=id&sort=desc&per_page=1", g.project(), url.QueryEscape(ref)), &list); err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	return list[0].pipeline(), nil
}

func (g *GitLab) FailedJobs(ctx context.Context, pipeline int64) ([]Job, error) {
	var jobs []struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		WebURL string `json:"web_url"`
	}
	if err := g.api(ctx, fmt.Sprintf("%s/pipelines/%d/jobs?scope[]=failed", g.project(), pipeline), &jobs); err != nil {
		return nil, err
	}
	var out []Job
	for _, j := range jobs {
		out = append(out, Job{ID: j.ID, Name: j.Name, URL: j.WebURL})
	}
	return out, nil
}

func (g *GitLab) JobLog(ctx context.Context, job int64, lines int) (string, error) {
	run := g.Run
	if run == nil {
		run = glab
	}
	out, err := run(ctx, "api", "--hostname", g.Host, fmt.Sprintf("%s/jobs/%d/trace", g.project(), job))
	if err != nil {
		return "", err
	}
	return CleanLog(string(out), lines), nil
}

var (
	ansi      = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]|\x1b\][^\x07]*\x07`)
	runnerTS  = regexp.MustCompile(`^\d{4}-\d\d-\d\dT[\d:.]+Z \d+[OE]\+? ?`)
	sectionMk = regexp.MustCompile(`section_(start|end):\d+:\S+`)
)

// CleanLog strips terminal colours, the runner's timestamps and section markers from a
// job log and keeps its last lines: where the failure usually is.
func CleanLog(raw string, lines int) string {
	var keep []string
	for _, l := range strings.Split(raw, "\n") {
		l = ansi.ReplaceAllString(l, "")
		l = strings.ReplaceAll(l, "\r", "")
		l = runnerTS.ReplaceAllString(l, "")
		l = sectionMk.ReplaceAllString(l, "")
		if strings.TrimSpace(l) == "" {
			continue
		}
		keep = append(keep, l)
	}
	if len(keep) > lines {
		keep = keep[len(keep)-lines:]
	}
	return strings.Join(keep, "\n")
}

// unreachable are the words of errors that mean the host did not answer: DNS, TCP and
// TLS failures and timeouts, as Go's net package and glab (which prints them) word them.
var unreachable = regexp.MustCompile(`(?i)no such host|server misbehaving|temporary failure in name resolution|` +
	`connection refused|connection reset|network is unreachable|no route to host|host is down|` +
	`i/o timeout|tls handshake timeout|client\.timeout exceeded|deadline exceeded|` +
	`unexpected eof|broken pipe|dial tcp`)

// serverError and clientError are an HTTP 5xx or 4xx as glab reports it:
// "502 Bad Gateway (HTTP 502)".
var (
	serverError = regexp.MustCompile(`\bHTTP 5\d\d\b`)
	clientError = regexp.MustCompile(`\bHTTP 4\d\d\b`)
)

// Unreachable says whether err means the forge's host could not be reached or could not
// answer (a network failure, or a 5xx), as opposed to an answer that refused the request
// (a 4xx: no access, no such project), which waiting would not change.
func Unreachable(err error) bool {
	if err == nil {
		return false
	}
	var ne net.Error
	if errors.As(err, &ne) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	msg := err.Error()
	if clientError.MatchString(msg) {
		return false // an answer, whatever else the message says
	}
	return unreachable.MatchString(msg) || serverError.MatchString(msg)
}

// MaxThreadBody caps a thread's text, in bytes, so one long review cannot fill a prompt.
const MaxThreadBody = 2000

type glNote struct {
	ID         int64  `json:"id"`
	Body       string `json:"body"`
	System     bool   `json:"system"`
	Resolvable bool   `json:"resolvable"`
	Resolved   bool   `json:"resolved"`
	Author     struct {
		Username string `json:"username"`
	} `json:"author"`
	Position *struct {
		NewPath string `json:"new_path"`
		OldPath string `json:"old_path"`
		NewLine int    `json:"new_line"`
		OldLine int    `json:"old_line"`
	} `json:"position"`
}

type glDiscussion struct {
	ID    string   `json:"id"`
	Notes []glNote `json:"notes"`
}

// Threads reads the request's discussions (GitLab pages them; reads up to 10 pages of 100)
// and keeps the unresolved, resolvable ones that are not system notes.
func (g *GitLab) Threads(ctx context.Context, mr int) ([]Thread, error) {
	var out []Thread
	for page := 1; page <= 10; page++ {
		var ds []glDiscussion
		q := fmt.Sprintf("%s/merge_requests/%d/discussions?per_page=100&page=%d", g.project(), mr, page)
		if err := g.api(ctx, q, &ds); err != nil {
			return nil, err
		}
		for _, d := range ds {
			if t, ok := d.thread(); ok {
				if t.NoteID != 0 {
					t.URL = fmt.Sprintf("%s#note_%d", g.MRURL(mr), t.NoteID)
				}
				out = append(out, t)
			}
		}
		if len(ds) < 100 {
			break
		}
	}
	return out, nil
}

func (d glDiscussion) thread() (Thread, bool) {
	if d.ID == "" || len(d.Notes) == 0 || d.Notes[0].System {
		return Thread{}, false
	}
	// A thread is open when any of its resolvable notes is unresolved.
	open := false
	for _, n := range d.Notes {
		if n.Resolvable && !n.Resolved {
			open = true
		}
	}
	if !open {
		return Thread{}, false
	}
	first := d.Notes[0]
	t := Thread{ID: d.ID, Author: first.Author.Username, NoteID: first.ID}
	for _, n := range d.Notes {
		if !n.System {
			t.Notes++
		}
	}
	if p := first.Position; p != nil {
		t.File, t.Line = p.NewPath, p.NewLine
		if t.File == "" {
			t.File = p.OldPath
		}
		if t.Line == 0 {
			t.Line = p.OldLine
		}
	}
	var b strings.Builder
	for _, n := range d.Notes {
		if n.System || strings.TrimSpace(n.Body) == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s: %s", n.Author.Username, strings.TrimSpace(n.Body))
	}
	t.Body = b.String()
	if len(t.Body) > MaxThreadBody {
		t.Body = strings.ToValidUTF8(t.Body[:MaxThreadBody], "") + " [cut]"
	}
	return t, true
}

package fold

import (
	"context"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/ubixsys/ubixshepherd/internal/redact"
	"github.com/ubixsys/ubixshepherd/internal/scope"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// Caps on an artifact's content, here so every client gets the same bounds. A larger file
// is listed with its size and not served.
const (
	ArtifactMaxText  = 1 << 20
	ArtifactMaxImage = 10 << 20
)

// Artifact kinds.
const (
	KindMarkdown = "markdown"
	KindText     = "text"
	KindCode     = "code"
	KindImage    = "image"
	KindOther    = "other"
)

// ArtifactInfo is one file of a lane that the person may want to read.
type ArtifactInfo struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
	// Source: committed, uncommitted (modified) or untracked.
	Source string `json:"source"`
	// Status is the file's change status in the lane's diff.
	Status string `json:"status"`
	Size   int64  `json:"size"`
	// Readable is false for a file over its cap, a symlink or a missing file (a deleted
	// one): it is listed, not served.
	Readable bool `json:"readable"`
	// Mime is the type the content endpoint answers with.
	Mime string `json:"mime,omitempty"`
}

// LaneArtifacts is the artifact list of a lane at one moment.
type LaneArtifacts struct {
	LaneID    int64          `json:"lane_id"`
	Lane      string         `json:"lane"`
	Artifacts []ArtifactInfo `json:"artifacts"`
	Globs     []string       `json:"globs"`
	Truncated bool           `json:"truncated,omitempty"`
}

var imageMime = map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".gif": "image/gif", ".webp": "image/webp", ".svg": "image/svg+xml"}

func artifactKind(p string) string {
	switch ext := strings.ToLower(path.Ext(p)); ext {
	case ".md", ".markdown":
		return KindMarkdown
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg":
		return KindImage
	case ".txt", ".log", ".csv", ".json", ".yaml", ".yml", ".toml", ".html", ".htm":
		return KindText
	case ".go", ".ts", ".tsx", ".js", ".py", ".sh", ".rs", ".c", ".h", ".java", ".sql":
		return KindCode
	}
	return KindOther
}

// Artifacts lists the files of an open lane that match the repo's artifact globs: the
// untracked, uncommitted and committed files of its diff, each once, the worktree's
// state first. It changes nothing and stores nothing.
func (f *Fold) Artifacts(ctx context.Context, laneID int64) (LaneArtifacts, error) {
	lane, err := f.Store.Lane(ctx, laneID)
	if err != nil {
		return LaneArtifacts{}, err
	}
	var globs []string
	if repo, err := f.Store.Repo(ctx, lane.RepoID); err == nil {
		globs = f.Conf().Profile(repo.Name).Artifacts.Globs()
	}
	d, err := f.Diff(ctx, laneID, DiffRequest{})
	if err != nil {
		return LaneArtifacts{}, err
	}
	out := LaneArtifacts{LaneID: lane.ID, Lane: lane.Name, Artifacts: []ArtifactInfo{}, Globs: globs,
		Truncated: d.Truncated}
	seen := map[string]bool{}
	add := func(files []DiffFileInfo, source string) {
		for _, fi := range files {
			if seen[fi.Path] || !scope.Any(globs, fi.Path) {
				continue
			}
			seen[fi.Path] = true
			a := ArtifactInfo{Path: fi.Path, Kind: artifactKind(fi.Path), Source: source, Status: fi.Status}
			if st, err := os.Lstat(filepath.Join(lane.Worktree, filepath.FromSlash(fi.Path))); err == nil && st.Mode().IsRegular() {
				a.Size = st.Size()
				a.Readable = a.Size <= artifactCap(a.Kind)
				a.Mime = artifactMime(a.Kind, fi.Path)
			}
			out.Artifacts = append(out.Artifacts, a)
		}
	}
	// A file changed in the worktree is listed once, as its live state.
	add(d.Untracked, "untracked")
	add(d.Uncommitted, "uncommitted")
	add(d.Committed, "committed")
	return out, nil
}

func artifactCap(kind string) int64 {
	if kind == KindImage {
		return ArtifactMaxImage
	}
	return ArtifactMaxText
}

// artifactMime: text of every kind, HTML and SVG source included, is plain text; a raster
// image keeps its type, and an SVG is served as an image only by its extension.
func artifactMime(kind, p string) string {
	if kind == KindImage {
		return imageMime[strings.ToLower(path.Ext(p))]
	}
	return "text/plain; charset=utf-8"
}

// ArtifactContent is a live file's bytes, ready to serve.
type ArtifactContent struct {
	Path string
	Mime string
	Data []byte
	// Redacted is how many spans of a text file were masked.
	Redacted int
}

// ArtifactContent reads one file of an open lane from its worktree. The path must be
// relative, inside the worktree, a regular file (never a symlink, also not through a
// symlinked directory), and match the repo's artifact globs; text is redacted.
func (f *Fold) ArtifactContent(ctx context.Context, laneID int64, rel string) (ArtifactContent, error) {
	lane, err := f.Store.Lane(ctx, laneID)
	if err != nil {
		return ArtifactContent{}, err
	}
	clean := path.Clean(filepath.ToSlash(rel))
	if rel == "" || filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") || strings.Contains(rel, `\`) ||
		clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || clean != rel {
		return ArtifactContent{}, refuse("%q is not a path inside the lane's worktree", rel)
	}
	if lane.State != store.LaneOpen {
		return ArtifactContent{}, refuse("lane %s is %s: there is no worktree to read", lane.Name, lane.State)
	}
	var globs []string
	if repo, err := f.Store.Repo(ctx, lane.RepoID); err == nil {
		globs = f.Conf().Profile(repo.Name).Artifacts.Globs()
	}
	if !scope.Any(globs, clean) {
		return ArtifactContent{}, refuse("%s is not an artifact of the repo (artifacts.discover)", clean)
	}
	root, err := filepath.EvalSymlinks(lane.Worktree)
	if err != nil {
		return ArtifactContent{}, refuse("lane %s has no worktree on disk at %s", lane.Name, lane.Worktree)
	}
	p := filepath.Join(root, filepath.FromSlash(clean))
	// Resolving every component refuses a symlink anywhere on the way, as well as at the end.
	if real, err := filepath.EvalSymlinks(p); err != nil || real != p {
		return ArtifactContent{}, refuse("%s is not a plain file inside the worktree", clean)
	}
	st, err := os.Lstat(p)
	if err != nil || !st.Mode().IsRegular() {
		return ArtifactContent{}, refuse("%s is not a regular file in the worktree", clean)
	}
	kind := artifactKind(clean)
	if st.Size() > artifactCap(kind) {
		return ArtifactContent{}, refuse("%s is %d bytes, over the %d byte cap: open it in the worktree", clean, st.Size(), artifactCap(kind))
	}
	fh, err := os.Open(p)
	if err != nil {
		return ArtifactContent{}, err
	}
	defer fh.Close()
	data, err := io.ReadAll(io.LimitReader(fh, artifactCap(kind)+1))
	if err != nil {
		return ArtifactContent{}, err
	}
	if int64(len(data)) > artifactCap(kind) {
		return ArtifactContent{}, refuse("%s grew past the cap while read", clean)
	}
	c := ArtifactContent{Path: clean, Mime: artifactMime(kind, clean), Data: data}
	if kind != KindImage {
		if !utf8.Valid(data) {
			return ArtifactContent{}, refuse("%s is binary: not served as text", clean)
		}
		s := string(data)
		out := redact.String(s)
		if out != s {
			c.Redacted = strings.Count(out, redact.Mask) - strings.Count(s, redact.Mask)
		}
		c.Data = []byte(out)
	}
	return c, nil
}

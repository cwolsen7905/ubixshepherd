package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ubixsys/ubixshepherd/internal/fold"
	"github.com/ubixsys/ubixshepherd/internal/redact"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// LaneFileNode is one file or directory in a managed repo, at a lane's worktree or a ref.
type LaneFileNode struct {
	Path     string         `json:"path"`
	Name     string         `json:"name"`
	Kind     string         `json:"kind"` // "dir" or "file"
	Size     int64          `json:"size,omitempty"`
	Mime     string         `json:"mime,omitempty"`
	Binary   bool           `json:"binary,omitempty"`
	Data     string         `json:"data,omitempty"`
	Redacted int            `json:"redacted,omitempty"`
	Children []LaneFileNode `json:"children,omitempty"`
}

const (
	fileKindDir  = "dir"
	fileKindFile = "file"
)

// MaxFileBytes is how large a file the endpoint will return inline. Larger files are listed
// but their data is omitted and binary is set true.
const MaxFileBytes = 1 << 20

// PathLaneFiles is GET /v1/lanes/{id}/files?path=...&ref=... Read-only; for the desk and
// browser sessions. Without ref it reads the lane's worktree. With ref it reads that ref in
// the repo. The response is one LaneFileNode: a directory with children, or a file with data.
func PathLaneFiles(id int64, req string) string {
	p := fmt.Sprintf("%s/%d/files", PathLanes, id)
	if req != "" {
		p += "?" + req
	}
	return p
}

// LaneFiles serves PathLaneFiles from f.
func LaneFiles(f *fold.Fold) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			conversationError(w, http.StatusBadRequest, errors.New("bad lane id"))
			return
		}

		ctx := r.Context()
		lane, err := f.Store.Lane(ctx, id)
		if err != nil {
			code := http.StatusInternalServerError
			if errors.Is(err, store.ErrNotFound) {
				code = http.StatusNotFound
			}
			conversationError(w, code, err)
			return
		}

		repo, err := f.Store.Repo(ctx, lane.RepoID)
		if err != nil {
			conversationError(w, http.StatusInternalServerError, err)
			return
		}

		q := r.URL.Query()
		rel := path.Clean(filepath.ToSlash(q.Get("path")))
		if rel == "." || rel == "/" {
			rel = ""
		}
		if !isSafePath(rel) {
			conversationError(w, http.StatusBadRequest, errors.New("path must be inside the repo"))
			return
		}
		rel = strings.TrimPrefix(rel, "/")

		ref := strings.TrimSpace(q.Get("ref"))
		if !isSafeRef(ref) {
			conversationError(w, http.StatusBadRequest, errors.New("bad ref"))
			return
		}

		var node LaneFileNode
		if ref == "" {
			node, err = filesWorktree(ctx, repo.Path, lane, rel)
		} else {
			node, err = filesRef(ctx, repo.Path, ref, rel)
		}
		if err != nil {
			if errors.Is(err, fold.ErrRefused) {
				conversationError(w, http.StatusConflict, err)
				return
			}
			if errors.Is(err, os.ErrNotExist) {
				conversationError(w, http.StatusNotFound, err)
				return
			}
			conversationError(w, http.StatusInternalServerError, err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(node)
	}
}

func isSafePath(rel string) bool {
	if rel == "" {
		return true
	}
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "\\") || strings.Contains(rel, "\x00") {
		return false
	}
	if strings.HasPrefix(rel, "../") || rel == ".." || strings.HasPrefix(rel, "/") {
		return false
	}
	return path.Clean(rel) == rel
}

func isSafeRef(ref string) bool {
	if ref == "" {
		return true
	}
	if strings.Contains(ref, "..") || strings.Contains(ref, "\\") || strings.Contains(ref, "\x00") || strings.ContainsAny(ref, "\n\r") {
		return false
	}
	if strings.HasPrefix(ref, "-") || strings.Contains(ref, "@{") {
		return false
	}
	return true
}

func filesWorktree(ctx context.Context, repoPath string, lane store.Lane, rel string) (LaneFileNode, error) {
	if lane.State != store.LaneOpen {
		return LaneFileNode{}, refusef("lane %s is %s: there is no worktree to read", lane.Name, lane.State)
	}
	root, err := filepath.Abs(lane.Worktree)
	if err != nil {
		return LaneFileNode{}, refusef("lane %s has no worktree on disk", lane.Name)
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return LaneFileNode{}, refusef("lane %s has no worktree on disk", lane.Name)
	}

	abs, err := resolveInside(root, rel)
	if err != nil {
		return LaneFileNode{}, err
	}

	info, err := os.Lstat(abs)
	if err != nil {
		return LaneFileNode{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return LaneFileNode{}, os.ErrNotExist
	}

	if info.IsDir() {
		children, err := listWorktreeDir(root, abs, rel)
		if err != nil {
			return LaneFileNode{}, err
		}
		return LaneFileNode{Path: rel, Name: filepath.Base(abs), Kind: fileKindDir, Children: children}, nil
	}

	if !info.Mode().IsRegular() {
		return LaneFileNode{}, os.ErrNotExist
	}
	file, err := readWorktreeFile(root, abs, rel)
	if err != nil {
		return LaneFileNode{}, err
	}
	return file, nil
}

func filesRef(ctx context.Context, repoPath, ref, rel string) (LaneFileNode, error) {
	resolved, err := gitRun(repoPath, "rev-parse", "--verify", "--quiet", ref)
	if err != nil {
		return LaneFileNode{}, refusef("ref %q not found", ref)
	}

	ty := "tree" // the root of a ref is its tree
	if rel != "" {
		var err error
		if ty, err = gitRun(repoPath, "cat-file", "-t", resolved+":"+rel); err != nil {
			return LaneFileNode{}, os.ErrNotExist
		}
	}
	switch ty {
	case "tree":
		name := rel
		if i := strings.LastIndex(rel, "/"); i >= 0 {
			name = rel[i+1:]
		}
		if name == "" {
			name = path.Base(repoPath)
		}
		children, err := listRefDir(repoPath, resolved, rel)
		if err != nil {
			return LaneFileNode{}, err
		}
		return LaneFileNode{Path: rel, Name: name, Kind: fileKindDir, Children: children}, nil
	case "blob":
		return readRefFile(repoPath, resolved, rel)
	}
	return LaneFileNode{}, refusef("%s is not a file or directory", rel)
}

func resolveInside(root, rel string) (string, error) {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	p := filepath.Join(realRoot, filepath.FromSlash(rel))
	realP, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", err
	}
	if realP != realRoot && !strings.HasPrefix(realP, realRoot+string(filepath.Separator)) {
		return "", os.ErrNotExist
	}
	return realP, nil
}

func listWorktreeDir(root, abs, rel string) ([]LaneFileNode, error) {
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}
	var out []LaneFileNode
	for _, de := range entries {
		name := de.Name()
		if name == ".git" {
			continue
		}
		if de.Type()&os.ModeSymlink != 0 {
			continue
		}
		childRel := joinRel(rel, name)
		info, err := de.Info()
		if err != nil {
			continue
		}
		n := LaneFileNode{Path: childRel, Name: name, Size: info.Size()}
		if de.IsDir() {
			n.Kind = fileKindDir
		} else if info.Mode().IsRegular() {
			n.Kind = fileKindFile
			n.Mime = mimeFor(name)
			if info.Size() > MaxFileBytes {
				n.Binary = true
			}
		} else {
			continue
		}
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind == fileKindDir
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func readWorktreeFile(root, abs, rel string) (LaneFileNode, error) {
	info, err := os.Stat(abs)
	if err != nil {
		return LaneFileNode{}, err
	}
	if !info.Mode().IsRegular() {
		return LaneFileNode{}, os.ErrNotExist
	}
	node := LaneFileNode{Path: rel, Name: info.Name(), Kind: fileKindFile, Size: info.Size(), Mime: mimeFor(info.Name())}
	if info.Size() > MaxFileBytes {
		node.Binary = true
		return node, nil
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return LaneFileNode{}, err
	}
	if !utf8.Valid(data) {
		node.Binary = true
		return node, nil
	}
	s := string(data)
	node.Data = redact.String(s)
	if node.Data != s {
		node.Redacted = countMask(node.Data) - countMask(s)
	}
	return node, nil
}

func listRefDir(repoPath, ref, rel string) ([]LaneFileNode, error) {
	var arg string
	if rel == "" {
		arg = ""
	} else {
		arg = rel + "/"
	}
	var args []string
	if arg == "" {
		args = []string{"ls-tree", "-z", ref}
	} else {
		args = []string{"ls-tree", "-z", ref, "--", arg}
	}
	out, err := gitRun(repoPath, args...)
	if err != nil {
		return nil, err
	}
	prefix := rel
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	var nodes []LaneFileNode
	seen := map[string]bool{}
	for _, rec := range strings.Split(out, "\x00") {
		if rec == "" {
			continue
		}
		// "<mode> <type> <object>\t<path>"; -z keeps the path unquoted.
		meta, full, ok := strings.Cut(rec, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) < 3 {
			continue
		}
		ty := fields[1]
		if !strings.HasPrefix(full, prefix) {
			continue
		}
		suffix := strings.TrimPrefix(full, prefix)
		if suffix == "" {
			continue
		}
		name := suffix
		if i := strings.Index(suffix, "/"); i >= 0 {
			name = suffix[:i]
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		childRel := joinRel(rel, name)
		if ty == "tree" {
			nodes = append(nodes, LaneFileNode{Path: childRel, Name: name, Kind: fileKindDir})
		} else if ty == "blob" {
			sizeStr, _ := gitRun(repoPath, "cat-file", "-s", ref+":"+childRel)
			size, _ := strconv.ParseInt(strings.TrimSpace(sizeStr), 10, 64)
			nodes = append(nodes, LaneFileNode{Path: childRel, Name: name, Kind: fileKindFile, Size: size, Mime: mimeFor(name), Binary: size > MaxFileBytes})
		}
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Kind != nodes[j].Kind {
			return nodes[i].Kind == fileKindDir
		}
		return nodes[i].Name < nodes[j].Name
	})
	return nodes, nil
}

func readRefFile(repoPath, ref, rel string) (LaneFileNode, error) {
	sizeStr, err := gitRun(repoPath, "cat-file", "-s", ref+":"+rel)
	if err != nil {
		return LaneFileNode{}, err
	}
	size, _ := strconv.ParseInt(strings.TrimSpace(sizeStr), 10, 64)
	node := LaneFileNode{Path: rel, Name: path.Base(rel), Kind: fileKindFile, Size: size, Mime: mimeFor(rel), Binary: size > MaxFileBytes}
	if size > MaxFileBytes {
		return node, nil
	}
	data, err := gitShowBytes(repoPath, ref+":"+rel)
	if err != nil {
		return LaneFileNode{}, err
	}
	if !utf8.Valid(data) {
		node.Binary = true
		return node, nil
	}
	s := string(data)
	node.Data = redact.String(s)
	if node.Data != s {
		node.Redacted = countMask(node.Data) - countMask(s)
	}
	return node, nil
}

func gitRun(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var out, errb strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errb
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if os.Getenv("GIT_SSH_COMMAND") == "" {
		cmd.Env = append(cmd.Env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	}
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), redact.String(msg))
	}
	return strings.TrimSpace(out.String()), nil
}

func gitShowBytes(dir, spec string) ([]byte, error) {
	cmd := exec.Command("git", "-C", dir, "cat-file", "-p", spec)
	var errb strings.Builder
	cmd.Stderr = &errb
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if os.Getenv("GIT_SSH_COMMAND") == "" {
		cmd.Env = append(cmd.Env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	}
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("git cat-file -p %s: %s", spec, redact.String(msg))
	}
	if len(out) > MaxFileBytes {
		out = out[:MaxFileBytes]
	}
	return out, nil
}

func joinRel(rel, name string) string {
	if rel == "" {
		return name
	}
	return rel + "/" + name
}

func mimeFor(p string) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".md", ".markdown":
		return "text/markdown"
	case ".go", ".ts", ".tsx", ".js", ".py", ".sh", ".rs", ".c", ".h", ".java", ".sql":
		return "text/plain; charset=utf-8"
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".json":
		return "application/json"
	case ".yaml", ".yml":
		return "application/yaml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	}
	return "text/plain; charset=utf-8"
}

func countMask(s string) int {
	return strings.Count(s, redact.Mask)
}

func refusef(format string, a ...any) error {
	return fmt.Errorf("%w: %s", fold.ErrRefused, fmt.Sprintf(format, a...))
}

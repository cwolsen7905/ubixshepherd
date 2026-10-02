package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/client"
	"github.com/ubixsys/ubixshepherd/internal/convo"
)

// shepherd session import | list | attach | ask: conversations had outside Shepherd.
func runSession(ctx context.Context, env Env, args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	fs := flags("session "+args[0], env)
	repo := fs.String("repo", "", "repo, by its name in the workspace (default: every repo)")
	pos, err := parse(fs, args[1:])
	if err != nil {
		return err
	}
	c, err := dial(ctx, env)
	if err != nil {
		return err
	}
	var repoID int64
	if *repo != "" {
		h, err := locate(ctx, env, c, *repo)
		if err != nil {
			return err
		}
		repoID = h.Repo.ID
	}
	w := env.Stdout
	switch args[0] {
	case "import":
		if len(pos) > 0 {
			return errUsage
		}
		got, err := c.ImportSessions(ctx, repoID)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "Adopted %d conversation(s):\n", len(got))
		printSessions(env, got)
		return nil
	case "list", "ls":
		if len(pos) > 0 {
			return errUsage
		}
		got, err := c.Sessions(ctx, repoID)
		if err != nil {
			return err
		}
		if len(got) == 0 {
			fmt.Fprintln(w, "No conversations adopted yet. Bring yours in with: shepherd session import")
			return nil
		}
		printSessions(env, got)
		return nil
	case "attach":
		if len(pos) != 1 {
			return errUsage
		}
		s, err := findSession(ctx, c, repoID, pos[0])
		if err != nil {
			return err
		}
		if !env.Interactive {
			return errors.New("attach needs a terminal")
		}
		if s.InUse {
			fmt.Fprintf(env.Stderr, "note: conversation %s changed in the last %s; if it is open in another terminal, use that one\n", s.ID[:8], convo.InUseWindow)
		}
		bin, err := exec.LookPath("claude")
		if err != nil {
			return errors.New("claude is not on PATH")
		}
		fmt.Fprintf(w, "reopening %s (%s) in %s\n\n", s.ID[:8], s.Title, s.Dir)
		cmd := exec.CommandContext(ctx, bin, "--resume", s.ID)
		cmd.Dir = s.Dir
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				return errSilent
			}
			return err
		}
		return nil
	case "ask":
		if len(pos) != 2 {
			return errUsage
		}
		s, err := findSession(ctx, c, repoID, pos[0])
		if err != nil {
			return err
		}
		fmt.Fprintf(env.Stderr, "asking %s (%s)…\n", s.ID[:8], s.Title)
		a, err := c.AskSession(ctx, s.ID, pos[1])
		if err != nil {
			return err
		}
		fmt.Fprintln(w, a.Text)
		if a.USD > 0 {
			fmt.Fprintf(env.Stderr, "(%s, $%.2f)\n", s.ID[:8], a.USD)
		}
		return nil
	}
	return errUsage
}

func printSessions(env Env, ss []api.SessionView) {
	for _, s := range ss {
		mark := ""
		if s.InUse {
			mark = "  (may be open now)"
		}
		branches := strings.Join(first(s.Branches, 3), ", ")
		if len(s.Branches) > 3 {
			branches += fmt.Sprintf(" +%d", len(s.Branches)-3)
		}
		fmt.Fprintf(env.Stdout, "  %s  %s  %-10s %s%s\n      %s → %s, branches: %s\n",
			s.ID[:8], s.Agent, s.Repo, s.Title, mark, s.Started.Local().Format("Jan 2"), s.Last.Local().Format("Jan 2 15:04"), orNone(branches))
	}
}

func first(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// findSession takes a conversation id or its first characters.
func findSession(ctx context.Context, c *client.Client, repoID int64, prefix string) (api.SessionView, error) {
	ss, err := c.Sessions(ctx, repoID)
	if err != nil {
		return api.SessionView{}, err
	}
	var hit []api.SessionView
	for _, s := range ss {
		if strings.HasPrefix(s.ID, prefix) {
			hit = append(hit, s)
		}
	}
	switch len(hit) {
	case 0:
		return api.SessionView{}, fmt.Errorf("no adopted conversation starts with %s (shepherd session list)", prefix)
	case 1:
		return hit[0], nil
	}
	return api.SessionView{}, fmt.Errorf("%d conversations start with %s; give more of the id", len(hit), prefix)
}

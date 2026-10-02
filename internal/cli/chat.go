package cli

import (
	"context"
	"errors"
	"fmt"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ubixsys/ubixshepherd/internal/chat"
)

// shepherd chat: the one conversation, with the front desk and the swarm's events.
func runChat(ctx context.Context, env Env, args []string) error {
	fs := flags("chat", env)
	model := fs.String("model", "", "the front desk's model, if not Claude Code's default")
	if pos, err := parse(fs, args); err != nil {
		return err
	} else if len(pos) > 0 {
		return errUsage
	}
	if !env.Interactive {
		return errors.New("shepherd chat needs a terminal")
	}
	bin, err := exec.LookPath("claude")
	if err != nil {
		return errors.New("shepherd chat's front desk runs on Claude Code, and claude is not on PATH")
	}
	if env.Exe == "" {
		return errors.New("cannot find this binary's path")
	}
	c, err := dial(ctx, env)
	if err != nil {
		return err
	}
	h, err := locate(ctx, env, c, "")
	if err != nil {
		return err
	}
	desk := chat.ClaudeDesk{Bin: bin, Shepherd: env.Exe, Dir: h.Workspace.Path, Model: *model}
	m := chat.New(ctx, c, desk, h.Workspace)
	if _, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithContext(ctx)).Run(); err != nil && !errors.Is(err, tea.ErrProgramKilled) {
		return fmt.Errorf("chat: %w", err)
	}
	return nil
}

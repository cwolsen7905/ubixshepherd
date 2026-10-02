// Package cli is the shepherd command. Every command except daemon is a client of the
// HTTP API, so the CLI behaves the same against a local or a remote daemon.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"

	"golang.org/x/term"

	"github.com/ubixsys/ubixshepherd/internal/client"
	"github.com/ubixsys/ubixshepherd/internal/paths"
)

// Env is what a command runs against, so tests can supply their own.
type Env struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	// Interactive is true when Stdin is a terminal a person can answer prompts on.
	Interactive bool
	Layout      paths.Layout
	Cwd         string
}

type command struct {
	name    string
	summary string
	usage   string
	run     func(ctx context.Context, env Env, args []string) error
}

// errUsage asks Main to print the command's usage and exit 2.
var errUsage = errors.New("usage")

func commands() []command {
	return []command{
		{"daemon", "Run the daemon in the foreground", "shepherd daemon", runDaemon},
		{"init", "Register a workspace and choose which of its repos Shepherd manages",
			"shepherd init [dir] [--name NAME] [--yes | --all | --only a,b]", runInit},
		{"status", "Show the daemon, its workspaces, and where you are", "shepherd status [--json]", runStatus},
		{"where", "Show the workspace, repo and lane for a directory", "shepherd where [dir] [--json]", runWhere},
		{"version", "Print the version", "shepherd version", runVersion},
	}
}

// Main runs the shepherd command and returns its exit code.
func Main(args []string) int {
	home, err := paths.Home()
	if err != nil {
		fmt.Fprintln(os.Stderr, "shepherd:", err)
		return 1
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "shepherd:", err)
		return 1
	}
	env := Env{
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
		Interactive: term.IsTerminal(int(os.Stdin.Fd())),
		Layout:      paths.Layout{Home: home}, Cwd: cwd,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return Run(ctx, env, args)
}

// Run dispatches args to a command.
func Run(ctx context.Context, env Env, args []string) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		usage(env.Stdout)
		return 0
	}
	for _, c := range commands() {
		if c.name != args[0] {
			continue
		}
		err := c.run(ctx, env, args[1:])
		switch {
		case err == nil:
			return 0
		case errors.Is(err, flag.ErrHelp):
			fmt.Fprintln(env.Stdout, "usage:", c.usage)
			return 0
		case errors.Is(err, errUsage):
			fmt.Fprintln(env.Stderr, "usage:", c.usage)
			return 2
		default:
			fmt.Fprintln(env.Stderr, "shepherd:", err)
			return 1
		}
	}
	fmt.Fprintf(env.Stderr, "shepherd: unknown command %q\n\n", args[0])
	usage(env.Stderr)
	return 2
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "shepherd: one voice to direct a swarm of AI agents")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	cs := commands()
	sort.Slice(cs, func(i, j int) bool { return cs[i].name < cs[j].name })
	for _, c := range cs {
		fmt.Fprintf(w, "  %-9s %s\n", c.name, c.summary)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Files live in $%s, or the OS user config directory under shepherd/.\n", paths.HomeEnv)
}

// flags returns a FlagSet that reports errors instead of exiting, and parses
// interspersed flags and positional arguments (shepherd where ~/git --json).
func flags(name string, env Env) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	return fs
}

func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil, err
			}
			return nil, errUsage
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

func dial(env Env) (*client.Client, error) {
	return client.FromRuntime(env.Layout.Runtime())
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func joinOr(s []string, empty string) string {
	if len(s) == 0 {
		return empty
	}
	return strings.Join(s, ", ")
}

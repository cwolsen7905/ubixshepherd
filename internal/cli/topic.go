package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/client"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// shepherd topic: the matters the front desk's thread is about (docs/topics.md). Every
// command goes through the daemon's API; renaming, merging, moving, clearing and
// reopening are the person's, and the daemon refuses them from the front desk outside a
// turn the person started.
func runTopic(ctx context.Context, env Env, args []string) error {
	sub := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	fs := flags("topic "+sub, env)
	asJSON := fs.Bool("json", false, "print JSON")
	all := fs.Bool("all", false, "list: include cleared topics")
	summary := fs.String("summary", "", "new or set: a line saying what the topic is about")
	repo := fs.String("repo", "", "the repo, by its name in the workspace (for --lane by name)")
	var refs [7]*string
	for i, k := range store.TopicKinds {
		flagName := strings.ReplaceAll(k, "_", "-")
		if k == store.TopicNextItem {
			flagName = "item"
		}
		refs[i] = fs.String(flagName, "", "set or move: tie a "+strings.ReplaceAll(k, "_", "-")+" (its id) to the topic")
	}
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	c, err := dial(ctx, env)
	if err != nil {
		return err
	}
	// ref is the one thing named by flags, if any.
	ref := func() (*api.TopicRef, error) {
		var got *api.TopicRef
		for i, k := range store.TopicKinds {
			v := strings.TrimSpace(*refs[i])
			if v == "" {
				continue
			}
			if got != nil {
				return nil, errors.New("tie one thing at a time")
			}
			if k == store.TopicLane {
				if n, err := strconv.ParseInt(v, 10, 64); err != nil || n <= 0 {
					h, err := locate(ctx, env, c, *repo)
					if err != nil {
						return nil, err
					}
					l, err := findLane(ctx, c, h, v)
					if err != nil {
						return nil, err
					}
					v = strconv.FormatInt(l.ID, 10)
				}
			}
			got = &api.TopicRef{Kind: k, Ref: v}
		}
		return got, nil
	}
	switch sub {
	case "list", "ls":
		if len(pos) > 0 {
			return errUsage
		}
		l, err := c.Topics(ctx, *all)
		if err != nil {
			return err
		}
		if *asJSON {
			return printJSON(env, l)
		}
		writeTopicList(env.Stdout, l)
		return nil
	case "show":
		id, err := topicArg(ctx, c, pos, 1)
		if err != nil {
			return err
		}
		v, err := c.Topic(ctx, id)
		if err != nil {
			return err
		}
		if *asJSON {
			return printJSON(env, v)
		}
		writeTopic(env.Stdout, v)
		return nil
	case "new":
		if len(pos) != 1 {
			return errUsage
		}
		r, err := ref()
		if err != nil {
			return err
		}
		v, err := c.AddTopic(ctx, api.TopicAdd{Title: pos[0], Summary: *summary, Link: r})
		if err != nil {
			return err
		}
		return printTopicChange(env, v, *asJSON, "added")
	case "set", "move":
		id, err := topicArg(ctx, c, pos, 1)
		if err != nil {
			return err
		}
		r, err := ref()
		if err != nil {
			return err
		}
		req := api.TopicUpdate{Link: r}
		if *summary != "" {
			req.Summary = summary
		}
		// With nothing named the daemon files the desk's own turn under the topic, and
		// refuses a caller that has no turn.
		v, err := c.UpdateTopic(ctx, id, req)
		if err != nil {
			return err
		}
		return printTopicChange(env, v, *asJSON, "updated")
	case "rename":
		if len(pos) != 2 {
			return errUsage
		}
		id, err := topicArg(ctx, c, pos[:1], 1)
		if err != nil {
			return err
		}
		v, err := c.UpdateTopic(ctx, id, api.TopicUpdate{Title: &pos[1]})
		if err != nil {
			return err
		}
		return printTopicChange(env, v, *asJSON, "renamed")
	case "merge":
		if len(pos) != 2 {
			return errUsage
		}
		from, err := topicArg(ctx, c, pos[:1], 1)
		if err != nil {
			return err
		}
		into, err := topicArg(ctx, c, pos[1:], 1)
		if err != nil {
			return err
		}
		v, err := c.UpdateTopic(ctx, from, api.TopicUpdate{MergeInto: into})
		if err != nil {
			return err
		}
		return printTopicChange(env, v, *asJSON, "merged")
	case "clear", "reopen":
		id, err := topicArg(ctx, c, pos, 1)
		if err != nil {
			return err
		}
		to := store.TopicCleared
		if sub == "reopen" {
			to = store.TopicOpen
		}
		v, err := c.UpdateTopic(ctx, id, api.TopicUpdate{Status: &to})
		if err != nil {
			return err
		}
		return printTopicChange(env, v, *asJSON, to)
	}
	return errUsage
}

// topicArg is a topic named by id, or by its title when the argument is not a number.
func topicArg(ctx context.Context, c *client.Client, pos []string, n int) (int64, error) {
	if len(pos) != n {
		return 0, errUsage
	}
	if id, err := strconv.ParseInt(pos[0], 10, 64); err == nil && id > 0 {
		return id, nil
	}
	l, err := c.Topics(ctx, true)
	if err != nil {
		return 0, err
	}
	var found int64
	for _, t := range l.Topics {
		if strings.EqualFold(t.Title, pos[0]) {
			if found != 0 {
				return 0, fmt.Errorf("two topics are called %q: use the id", pos[0])
			}
			found = t.ID
		}
	}
	if found == 0 {
		return 0, fmt.Errorf("no topic is called %q: see shepherd topic list --all", pos[0])
	}
	return found, nil
}

func printTopicChange(env Env, v api.TopicView, asJSON bool, what string) error {
	if asJSON {
		return printJSON(env, v)
	}
	fmt.Fprintf(env.Stdout, "topic %d %s: %s\n", v.ID, what, v.Title)
	return nil
}

func topicMark(v api.TopicView) string {
	switch {
	case v.Status == store.TopicCleared && v.Reopen:
		return "cleared, reopen?"
	case v.Status == store.TopicCleared:
		return "cleared"
	case v.Settled:
		return "settled?"
	}
	return "open"
}

func writeTopicList(w io.Writer, l api.TopicList) {
	if len(l.Topics) == 0 {
		fmt.Fprintln(w, "No topics. Make one with: shepherd topic new \"title\"")
		return
	}
	for _, v := range l.Topics {
		waits := ""
		if v.Waits > 0 {
			waits = fmt.Sprintf("  waits on you: %d", v.Waits)
		}
		fmt.Fprintf(w, "%3d  %-16s %s%s  (%s ago)\n", v.ID, topicMark(v), v.Title, waits, age(v.Activity))
	}
}

func writeTopic(w io.Writer, v api.TopicView) {
	fmt.Fprintf(w, "topic %d  %s\n  %s\n", v.ID, topicMark(v), v.Title)
	if v.Summary != "" {
		fmt.Fprintf(w, "  %s\n", v.Summary)
	}
	fmt.Fprintf(w, "  active %s ago, waits on you: %d, source %s\n", age(v.Activity), v.Waits, v.Source)
	for _, l := range v.Links {
		mark := " "
		if l.Waits {
			mark = "!"
		}
		fmt.Fprintf(w, "  %s %-9s %-6s %-10s %s\n", mark, l.Kind, l.Ref, l.State, l.Label)
	}
}

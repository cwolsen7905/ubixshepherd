package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// shepherd question: what the front desk asked the person, tracked until answered or
// skipped (docs/desk-questions.md). The desk opens a question when a reply asks the
// person something and marks it answered when a main-chat reply covers it. Skipping is
// the person's: the daemon refuses it from the desk outside a turn they started.
func runQuestion(ctx context.Context, env Env, args []string) error {
	sub := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	fs := flags("question "+sub, env)
	asJSON := fs.Bool("json", false, "print JSON")
	all := fs.Bool("all", false, "list: include answered and skipped questions")
	topic := fs.Int64("topic", 0, "open: the topic the question belongs to (the turn's topic by default)")
	turn := fs.Int64("turn", 0, "open: the desk turn it is asked in (the turn in progress by default)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	c, err := dial(ctx, env)
	if err != nil {
		return err
	}
	switch sub {
	case "list", "ls":
		if len(pos) > 0 {
			return errUsage
		}
		state := store.QuestionOpen
		if *all {
			state = ""
		}
		qs, err := c.Questions(ctx, 0, state)
		if err != nil {
			return err
		}
		if *asJSON {
			return printJSON(env, qs)
		}
		if len(qs) == 0 {
			fmt.Fprintln(env.Stdout, "No open questions.")
		}
		for _, q := range qs {
			writeQuestion(env.Stdout, q)
		}
		return nil
	case "open":
		if len(pos) != 1 {
			return errUsage
		}
		q, err := c.OpenQuestion(ctx, api.QuestionOpen{Text: pos[0], Turn: *turn, TopicID: *topic})
		if err != nil {
			return err
		}
		return printQuestion(env, q, *asJSON, "opened")
	case "answer", "skip":
		id, err := nextID(pos, 1)
		if err != nil {
			return err
		}
		var q store.Question
		if sub == "answer" {
			q, err = c.AnswerQuestion(ctx, id, api.QuestionAnswer{})
		} else {
			q, err = c.SkipQuestion(ctx, id)
		}
		if err != nil {
			return err
		}
		return printQuestion(env, q, *asJSON, q.State)
	}
	return errUsage
}

func writeQuestion(w io.Writer, q store.Question) {
	fmt.Fprintf(w, "%d  %s  turn %d  %s\n", q.ID, q.State, q.Turn, q.Text)
}

func printQuestion(env Env, q store.Question, asJSON bool, what string) error {
	if asJSON {
		return printJSON(env, q)
	}
	fmt.Fprintf(env.Stdout, "Question %d %s.\n", q.ID, what)
	return nil
}

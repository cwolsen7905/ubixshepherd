package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ubixsys/ubixshepherd/internal/api"
	"github.com/ubixsys/ubixshepherd/internal/client"
	"github.com/ubixsys/ubixshepherd/internal/desklog"
	"github.com/ubixsys/ubixshepherd/internal/store"
)

// runDesk is `shepherd desk`: the front desk's own tools. Its one subcommand is log
// (docs/desk-log.md).
func runDesk(ctx context.Context, env Env, args []string) error {
	if len(args) == 0 || args[0] != "log" {
		return errUsage
	}
	return runDeskLog(ctx, env, args[1:])
}

// runDeskLog is `shepherd desk log`: one JSONL line per desk turn, or with --summary a
// short read of the log. It only reads, through the daemon's API like every client.
func runDeskLog(ctx context.Context, env Env, args []string) error {
	fs := flags("desk log", env)
	since := fs.String("since", "", "from when: a day (2026-11-01), RFC 3339, or a number of days like 7d")
	until := fs.String("until", "", "up to when: a day (that whole day is included), RFC 3339, or a number of days ago like 1d")
	summary := fs.Bool("summary", false, "print totals instead of the turns")
	asJSON := fs.Bool("json", false, "with --summary, print JSON")
	workspace := fs.Int64("workspace", 0, "the workspace's id (default: the only one)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return errUsage
	}
	now := time.Now()
	from, err := parseLogTime(*since, now, false)
	if err != nil {
		return fmt.Errorf("--since: %w", err)
	}
	to, err := parseLogTime(*until, now, true)
	if err != nil {
		return fmt.Errorf("--until: %w", err)
	}
	if !from.IsZero() && !to.IsZero() && to.Before(from) {
		return fmt.Errorf("--until is before --since")
	}
	c, err := dial(ctx, env)
	if err != nil {
		return err
	}
	in, err := gatherDeskLog(ctx, c, *workspace)
	if err != nil {
		return err
	}
	in.Since, in.Until = from, to
	recs := desklog.Derive(in)
	if *summary {
		s := desklog.Summarise(recs)
		if *asJSON {
			return printJSON(env, s)
		}
		writeDeskSummary(env.Stdout, s)
		return nil
	}
	enc := json.NewEncoder(env.Stdout)
	for _, r := range recs {
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	return nil
}

// parseLogTime reads a --since or --until. A bare day is its start, or with end its
// last instant; "7d" is that many days before now.
func parseLogTime(v string, now time.Time, end bool) (time.Time, error) {
	v = strings.TrimSpace(v)
	switch {
	case v == "":
		return time.Time{}, nil
	case strings.HasSuffix(v, "d"):
		n, err := strconv.Atoi(strings.TrimSuffix(v, "d"))
		if err != nil || n < 0 {
			break
		}
		return now.AddDate(0, 0, -n), nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation(dayFormat, v, time.Local); err == nil {
		if end {
			return t.AddDate(0, 0, 1).Add(-time.Nanosecond), nil
		}
		return t, nil
	}
	return time.Time{}, fmt.Errorf("%q is not a time (want YYYY-MM-DD, RFC 3339 or a number of days like 7d)", v)
}

// gatherDeskLog reads what the log is derived from: the whole kept conversation, the
// feed, the questions and decisions, and the topics the turns are filed under.
func gatherDeskLog(ctx context.Context, c *client.Client, ws int64) (desklog.Input, error) {
	var in desklog.Input
	var before int64
	for {
		h, err := c.DeskHistory(ctx, ws, before, 500)
		if err != nil {
			return in, err
		}
		in.Events = append(h.Events, in.Events...)
		if !h.More || len(h.Events) == 0 {
			break
		}
		before = h.Events[0].Seq
	}
	for after := int64(0); ; {
		f, err := c.Feed(ctx, after)
		if err != nil {
			return in, err
		}
		if len(f.Items) == 0 {
			break
		}
		in.Feed = append(in.Feed, f.Items...)
		after = f.Last
	}
	var err error
	if in.Logged, err = c.DeskTurnLogs(ctx, ws); err != nil {
		return in, err
	}
	if in.Questions, err = c.Questions(ctx, ws, ""); err != nil {
		return in, err
	}
	dv, err := c.Decisions(ctx, "")
	if err != nil {
		return in, err
	}
	decisionRun := map[int64]int64{}
	for _, d := range dv {
		in.Decisions = append(in.Decisions, d.Decision)
		decisionRun[d.ID] = d.RunID
	}
	tl, err := c.Topics(ctx, true)
	if err != nil {
		return in, err
	}
	in.TurnTopic = map[int64]string{}
	for _, t := range tl.Topics {
		for _, l := range t.Links {
			if l.Kind == store.TopicTurn {
				if id, err := strconv.ParseInt(l.Ref, 10, 64); err == nil {
					in.TurnTopic[id] = t.Title
				}
			}
		}
	}
	in.Resolve = deskLogResolver(ctx, c, ws, decisionRun)
	return in, nil
}

// deskLogResolver names the repo, lane and run a feed item is about from its ref, which
// is a run, a decision or a lane depending on the kind. A lookup that fails leaves the
// field empty: the log is a record to study, not a check.
func deskLogResolver(ctx context.Context, c *client.Client, ws int64, decisionRun map[int64]int64) desklog.Resolver {
	type laneName struct{ repo, lane string }
	lanes := map[int64]laneName{}
	if ls, err := c.Lanes(ctx, ws, 0); err == nil {
		for _, l := range ls {
			lanes[l.ID] = laneName{l.Repo, l.Name}
		}
	}
	if hs, err := c.LaneHistory(ctx, ws, store.HistoryAll, time.Time{}); err == nil {
		for _, l := range hs {
			lanes[l.ID] = laneName{l.Repo, l.Name}
		}
	}
	runs := map[int64]api.RunView{}
	run := func(id int64) (api.RunView, bool) {
		if v, ok := runs[id]; ok {
			return v, v.ID != 0
		}
		v, err := c.Run(ctx, id)
		if err != nil {
			v = api.RunView{}
		}
		runs[id] = v
		return v, v.ID != 0
	}
	return func(kind string, ref int64) (repo, lane string, runID int64) {
		switch kind {
		case store.FeedDecision, store.FeedDecisionAnswer:
			ref = decisionRun[ref]
			fallthrough
		case store.FeedRunStarted, store.FeedRunEnded, store.FeedRunPassed, store.FeedRunFailed,
			store.FeedRunInterrupted, store.FeedRunQuota, store.FeedCommit, store.FeedReport:
			if v, ok := run(ref); ok {
				return v.Repo, v.Lane, v.ID
			}
		case store.FeedGate, store.FeedMR, store.FeedPipeline, store.FeedLaneOpened, store.FeedLaneClosed:
			if l, ok := lanes[ref]; ok {
				return l.repo, l.lane, 0
			}
		}
		return "", "", 0
	}
}

func writeDeskSummary(w io.Writer, s desklog.Summary) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "DAY\tTURNS\tPERSON\tEVENT\tSHEPHERD\tREPLY CHARS")
	for _, d := range s.Days {
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%d\n", d.Day, d.Turns, d.PersonTurns, d.EventTurns, d.OtherTurns, d.ReplyChars)
	}
	tw.Flush()
	fmt.Fprintf(w, "\nEvent turns: %d, of which %d have a next message from you to judge by.\n", s.EventTurns, s.Judged)
	if s.Judged > 0 {
		fmt.Fprintf(w, "No action by you after %d of them (%.0f%%).\n", s.NoAction, s.NoActionShare*100)
	}
	fmt.Fprintln(w, "\nMost repeated Needs-you items:")
	if len(s.MostRepeated) == 0 {
		fmt.Fprintln(w, "  none")
	}
	for _, r := range s.MostRepeated {
		fmt.Fprintf(w, "  %3d turns  %s  %s\n", r.Turns, r.Key, r.Sample)
	}
	fmt.Fprintln(w, "\nEvent kinds most often followed by no action:")
	if len(s.NoActionKinds) == 0 {
		fmt.Fprintln(w, "  none")
	}
	for _, k := range s.NoActionKinds {
		fmt.Fprintf(w, "  %-20s %3d of %d turns\n", k.Kind, k.NoAction, k.Turns)
	}
}

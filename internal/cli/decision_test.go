package cli

import (
	"slices"
	"testing"
)

// An option with commas stays one option from the ask_human tool through the worker's
// --option flag; --scope still splits on commas.
func TestAskHumanOptionKeepsCommas(t *testing.T) {
	want := []string{"Go with the recommended order (1, then 2 and 3 in parallel)", "Wait, ask again"}
	for _, tl := range workerTools() {
		if tl.Name != "ask_human" {
			continue
		}
		args, err := tl.args(map[string]any{"question": "which?", "recommendation": "1", "options": []any{want[0], want[1]}})
		if err != nil {
			t.Fatal(err)
		}
		var got textFlag
		fs := flags("worker ask-human", Env{})
		fs.Var(&got, "option", "")
		fs.String("recommendation", "", "")
		fs.String("why", "", "")
		if _, err := parse(fs, args[2:]); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, want) {
			t.Errorf("options = %q, want %q", got, want)
		}
		var scope listFlag
		scope.Set("a/**,b/**")
		if !slices.Equal(scope, []string{"a/**", "b/**"}) {
			t.Errorf("scope = %q", scope)
		}
		return
	}
	t.Fatal("no ask_human tool")
}

package desk

import (
	"strings"
	"testing"
)

func TestDigestEndsWithTheWaitingLine(t *testing.T) {
	line := "[Waiting on you (2)] decision 4 (app:a): ask; MR !9 (app:b): approve it"
	got := digest([]string{"run 3 ended"}, 0, line)
	if !strings.Contains(got, "\n"+line+"\n") || !strings.HasSuffix(got, "repeating this line, so the person sees what waits on them.") {
		t.Errorf("digest: %q", got)
	}
	if none := digest([]string{"run 3 ended"}, 0, ""); strings.Contains(none, "Waiting on you") {
		t.Errorf("digest with nothing waiting: %q", none)
	}
}

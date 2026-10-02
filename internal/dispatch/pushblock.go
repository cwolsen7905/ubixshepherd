package dispatch

import (
	"context"
	"fmt"
	"strings"

	"github.com/ubixsys/ubixshepherd/internal/git"
)

// pushBlock is the environment that stops an agent pushing the lane's repo anywhere it
// knows: every push to one of the repo's remote URLs is rewritten to noPush, which
// cannot connect, whatever flags the agent passes, --no-verify included.
//
// It names the repo's URLs, not a remote name, so git run by the agent on any other
// repo (a test suite's temporary repos, which call their remote origin too) pushes as
// usual. A remote's url is caught by pushInsteadOf, which also covers pushing to the URL
// written out; git does not apply pushInsteadOf to an explicit pushurl, so a pushurl is
// caught by insteadOf, which also stops fetches from it (fetches use url, so this only
// bites a remote whose pushurl repeats its url).
func pushBlock(ctx context.Context, dir string) []string {
	// Exits 1 when no remote has a URL: then there is nothing to rewrite.
	out, _ := git.Run(ctx, dir, "config", "--get-regexp", `^remote\..*\.(url|pushurl)$`)
	type rule struct{ key, url string }
	var rules []rule
	seen := map[rule]bool{}
	for _, line := range strings.Split(out, "\n") {
		k, url, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok || url == "" {
			continue
		}
		r := rule{"url." + noPush + ".pushInsteadOf", url}
		if strings.HasSuffix(k, ".pushurl") {
			r.key = "url." + noPush + ".insteadOf"
		}
		if !seen[r] {
			seen[r] = true
			rules = append(rules, r)
		}
	}
	env := []string{fmt.Sprintf("GIT_CONFIG_COUNT=%d", len(rules))}
	for i, r := range rules {
		env = append(env, fmt.Sprintf("GIT_CONFIG_KEY_%d=%s", i, r.key), fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s", i, r.url))
	}
	return env
}

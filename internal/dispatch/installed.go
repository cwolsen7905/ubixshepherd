package dispatch

import (
	"os/exec"
	"sort"
)

// installedHost caches OpenCode's version probe for a nil Runner, so a caller that asks
// often does not run the probe each time.
var installedHost = &OpenCodeHost{}

// InstalledAgents names the agents this machine can start, sorted: each adapter's
// executable found the way a run finds it (OpenCode's completed and checked as for a
// run). A nil Runner looks on the PATH.
func (r *Runner) InstalledAgents() []string {
	look := exec.LookPath
	host := installedHost
	if r != nil {
		if r.lookPath != nil {
			look = r.lookPath
		}
		host = r.openCodeHost()
	}
	var out []string
	for _, name := range AgentNames() {
		ad, err := adapterFor(name, host)
		if err != nil || ad.Bin == "" {
			continue
		}
		if _, err := look(ad.Bin); err == nil {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

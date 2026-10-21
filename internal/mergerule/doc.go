// Package mergerule runs a repo's merge rules in shadow mode (docs/merge-rules.md). It
// reads the rule files from the repo's base branch, gathers the facts a merge rule
// reads for a lane's open merge request, asks crook.Evaluate what each shadow rule would
// have done and records that. It never merges, approves or retries: a shadow rule is
// evidence for the person, who decides whether to make it live.
//
// The package does no forge or store reads of its own. The caller passes what it found
// in an Input, and the verdict log and feed are reached through small interfaces.
package mergerule

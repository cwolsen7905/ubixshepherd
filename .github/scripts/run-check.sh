#!/usr/bin/env bash
# Run `make check` and, when it fails, say why where a person looks first.
#
# GitHub shows only "Process completed with exit code 2" on the check run's page, because
# make's own message is a log line, not an annotation. So this keeps the full output in the
# log, and on failure adds the failing tests and errors as annotations and the tail of the
# output to the job summary. The exit status is make's.
set -uo pipefail

log="${RUNNER_TEMP:-${TMPDIR:-/tmp}}/make-check.log"

make check 2>&1 | tee "$log"
status=${PIPESTATUS[0]}
[ "$status" -eq 0 ] && exit 0

# The lines that name the failure: Go test failures and panics, the failing make target,
# TypeScript and vitest errors, shell errors. Strip CRs (Windows) and cap at the 10
# annotations GitHub shows per step.
pattern='^(--- FAIL|FAIL|panic:|make: \*\*\*)|^[[:space:]]+[a-z_]+_test\.go:[0-9]+:|error TS[0-9]+|AssertionError|^[[:space:]]*(FAIL|×|✗) |ERR!|gofmt needed|^go: |: command not found|^[^ ]+\.go:[0-9]+:[0-9]+: |^# [a-zA-Z]'
grep -E "$pattern" "$log" | tr -d '\r' | cut -c1-400 | awk '!seen[$0]++' | head -10 |
	while IFS= read -r line; do
		# Workflow commands use % and newlines as escapes.
		line=${line//'%'/'%25'}
		echo "::error title=make check failed (${RUNNER_OS:-local})::$line"
	done

if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
	{
		echo "### make check failed on ${RUNNER_OS:-local} (exit $status)"
		echo
		echo "Failing lines:"
		echo '```'
		grep -E "$pattern" "$log" | tr -d '\r' | cut -c1-400 | awk '!seen[$0]++' | head -60
		echo '```'
		echo
		echo "Last 60 lines of output:"
		echo '```'
		tail -n 60 "$log" | tr -d '\r'
		echo '```'
	} >> "$GITHUB_STEP_SUMMARY"
fi

exit "$status"

---
level: must
when: [commit]
check: '! git log "$SHEPHERD_BASE_REF"..HEAD --format=%B | grep -iE "co-authored-by:.*(claude|anthropic|copilot|openai|gpt|gemini|cursor)|generated (with|by) .*(claude|ai|copilot|gpt)"'
---
Never credit an AI tool in commits, merge requests or code: no Co-Authored-By trailers for it, no "generated with" lines.

## Why

The person who merges the work owns it; attribution lines add noise and are not wanted in the history.

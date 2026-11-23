---
level: must
when: [commit]
check: '! git log "$SHEPHERD_BASE_REF"..HEAD --format=%B | grep -iqE "^shepherd-run:" || git log "$SHEPHERD_BASE_REF"..HEAD --format=%B | grep -iqE "^co-authored-by:"'
---
Commits an agent made in a Shepherd run keep their attribution: the Co-Authored-By, Shepherd-Run and Shepherd-Model trailers Shepherd adds. Do not strip them.

## Why

The person decided that knowing which agent and model did the work is useful, so the old rule against crediting an AI tool is reversed. Shepherd adds the trailers itself; see docs/attribution.md.

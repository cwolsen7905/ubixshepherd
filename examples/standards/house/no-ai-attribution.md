---
level: must
when: [commit]
check: 'bad=0; for c in $(git rev-list "$SHEPHERD_BASE_REF"..HEAD); do m=$(git log -1 --format=%B "$c"); if printf "%s\n" "$m" | grep -iqE "^shepherd-run:" && ! printf "%s\n" "$m" | grep -iqE "^co-authored-by:"; then bad=1; fi; done; [ "$bad" = 0 ]'
---
Commits an agent made in a Shepherd run keep their attribution: the Co-Authored-By, Shepherd-Run and Shepherd-Model trailers Shepherd adds. Do not strip them.

## Why

The person decided that knowing which agent and model did the work is useful, so the old rule against crediting an AI tool is reversed. Shepherd adds the trailers itself; see docs/attribution.md.

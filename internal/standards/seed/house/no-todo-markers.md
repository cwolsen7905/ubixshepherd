---
level: must
check: '! git diff "$SHEPHERD_BASE_REF"...HEAD -U0 | grep -E "^\+[^+].*(TODO|FIXME)"'
---
Mark deferred work `NOT_IMPLEMENTED:` (not done yet) or `BY_DESIGN:` (deliberately left out), never TODO or FIXME.

## Why

A TODO says nothing about whether the gap is a bug or a decision. The two markers can be searched for and tell the next reader which it is.

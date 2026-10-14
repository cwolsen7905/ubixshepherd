# Links in reports

GitLab does not link a discussion id, and a bare `!12` or `pipeline 345` is not clickable
from a terminal. Every report that names a forge thing links it:

| Thing | URL |
| --- | --- |
| Merge request | `<project_url>/-/merge_requests/<iid>` |
| Pipeline | `<project_url>/-/pipelines/<id>` |
| Job | `<project_url>/-/jobs/<id>` |
| Review thread | `<project_url>/-/merge_requests/<iid>#note_<first_note_id>` |

A thread's link text is its title or first line. The short discussion id may follow in
brackets, never on its own.

## Who follows it

- Agents: the rule is in the brief Shepherd adds to every run (`dispatch.LinkRule`).
- The front desk: it is in the desk's standing instruction.
- Shepherd's own messages (failed-pipeline and review-thread hand-offs, feed lines) emit
  the URLs themselves, from the forge's own IDs.

## The helper

`shepherd links thread <mr_iid> <discussion_id> [--repo R]` asks the forge for the
discussion (`GET /projects/:id/merge_requests/:iid/discussions/:discussion_id`) and prints
the first note's URL. `links mr|pipeline|job <id>` print the others. Agents and the desk
reach it as the `links_thread` MCP tool. It reads through `glab`, like the rest of
`internal/forge`, so it needs the person's GitLab login on the machine it runs on.

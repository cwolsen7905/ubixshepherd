# OpenCode

`shepherd lane run --agent opencode` starts [OpenCode](https://opencode.ai) headless in a
lane's worktree. It exists for **small mechanical edits on a local model**: typos,
renames, one-line fixes, where a hosted model is more than the job needs.

**Do not use it to write tests in a typed codebase.** On the model it was checked with
(a 30B coder model behind an OpenAI-compatible host) it was fine for typos, renames and
tiny bug fixes and unusable for tests. Use Claude Code, Copilot or Cursor for anything
that needs judgment.

## Setup

1. Install OpenCode. Shepherd looks for the executable in this order: `opencode.bin` in
   `config.yaml`, `opencode` on the daemon's `PATH`, then `~/.opencode/bin/opencode`
   (where its standalone installer puts it).
2. Tell Shepherd where the model host is, and which model each run uses:

   ```yaml
   opencode:
     # bin: /opt/opencode/bin/opencode
     endpoint: http://localhost:11434/v1   # any OpenAI-compatible host
     # idle_timeout: 10m                   # stop a run that prints nothing this long
     # request_timeout: 60m                # one provider request; at least 1m, or off
     # chunk_timeout: 10m                  # gap between streamed chunks; at least 10s
     # context: 32768                      # tokens; when OpenCode compacts
     # output: 8192                        # tokens; must be less than context
   defaults:
     agent:
       model: {opencode: "local/qwen3-coder:30b"}
   ```

   With an `endpoint`, Shepherd defines a provider named `local` for the run when the
   model is meant for it: a bare name (`qwen3-coder:30b`) or `local/<model>`. Any other
   `provider/model` is passed to OpenCode untouched, with no provider block, so it uses
   its own provider of that name and its own login (see "Using OpenRouter alongside a
   local host"). With no `endpoint`, every model comes from your own OpenCode
   configuration and `agent.model.opencode` names a model in it.
   `request_timeout` (default 60 minutes, at least 1 minute, or `off`) is how long
   one request to that host may take. `off` writes `timeout: false`, which OpenCode
   treats as no limit. `chunk_timeout` (default 10 minutes, at least 10 seconds) is
   how long the stream may go without a chunk. Leaving either key out keeps these
   defaults. OpenCode accepts only a positive number of milliseconds for the chunk
   limit: a missing or false value in its own config becomes 5 minutes, so there is
   no `off`. These bound one request. `idle_timeout` bounds a whole run that prints
   nothing, and raising it does not raise these.
   `context` and `output` (default 32768 and 8192) are the limits Shepherd tells
   OpenCode about the model under `endpoint`; `output` must be less than `context`.
   They only tell OpenCode when to compact. The model server must load at least that
   much context itself: for Ollama, set `OLLAMA_CONTEXT_LENGTH` or a Modelfile
   `num_ctx`, because the OpenAI-compatible endpoint cannot set it per request.
3. `shepherd lane run <lane> --agent opencode "task"`. `shepherd run attach <id>` opens
   the session in OpenCode's own interface (`opencode -s <session>`).

A config change applies to the next run; no daemon restart is needed.

## Using OpenRouter alongside a local host

The model name picks the provider for each run, so a local host and a hosted provider
can both be in use without one taking over the other:

| Model | Runs on |
| --- | --- |
| `qwen3-coder:30b` or `local/qwen3-coder:30b` | the `opencode.endpoint` host |
| `openrouter/qwen/qwen3-coder` | OpenRouter, through OpenCode's own `openrouter` provider |
| `anthropic/claude-sonnet-4-5` and any other `provider/model` | that provider, as OpenCode itself has it |

Everything after the first `/` of a hosted model is its id, so OpenRouter's
`vendor/model` ids work as written: `openrouter/anthropic/claude-sonnet-4-5`. Only
`local/` is Shepherd's. A prefix such as `mybox/` used to define a provider at the local
endpoint; it no longer does, so write `local/<model>`.

1. Log in once, as the user the daemon runs as: `opencode auth login`, choose
   OpenRouter and paste the key. OpenCode keeps it in its own data directory, and
   Shepherd never reads it.
2. Choose the model for one run: `shepherd lane run <lane> --agent opencode --model
   openrouter/qwen/qwen3-coder "task"`. Or per repo, in its profile:
   `agent: {model: {opencode: "openrouter/qwen/qwen3-coder"}}`. The run's flag wins
   over the profile.
3. A hosted model is billed. OpenCode reports a cost with each step; Shepherd sums it
   per run, shows it in the run log's closing line, and records it as `opencode`'s spend,
   so it counts in the daily total and in budgets. A local model has no rates and
   records 0.

`--pure` only disables external plugins, and `OPENCODE_CONFIG_CONTENT` is inline
config layered over your files; neither is documented as changing where OpenCode reads
its logins, and Shepherd does not set a variable that moves OpenCode's data directory.
Run a hosted model once to confirm the login is found: a run that answers, rather
than failing with an authentication error, shows it is.

## What a run does

- **The prompt is on standard input**, never on the command line. `opencode run` reads
  the message from standard input when it has no message argument.
- **The run's config travels in `OPENCODE_CONFIG_CONTENT`.** OpenCode has no config-path
  flag. Shepherd writes nothing to the worktree and nothing to your home, and the
  variable is in that run's environment alone: it is not set in the daemon and no other
  agent sees it. Your own OpenCode config and the repo's `opencode.json` still load
  underneath it.
- **Permissions** are explicit, and `--auto` is not used. Files may be edited. Shell
  commands are denied except `go test`, `go build`, `go vet`, `gofmt`, `git diff`,
  `git status`, `git log`, `git show`, `git add`, `git commit`, `ls`, `cat`, `git fetch`,
  `git rebase origin/*`, `git merge-base`, `git rev-parse` and the repo's
  gate. `git push` is denied unless the repo sets `autonomy.push: agent`, and the runner's
  push block applies as for every agent. Questions, web fetch and web search are denied,
  and so is access outside the worktree: `external_directory` is `deny` except for the
  lane's own worktree path, which is allowed by exact path (the path as given and its
  symlink-resolved form). OpenCode 1.18.35 refuses the files of a linked git worktree as
  external (observed with the worktree as its working directory), so without that allow
  it refuses even the worktree's own files, and the model falls back to editing the main checkout. The run also gets
  `--dir <worktree>`, and the process starts there. A chained command such as `ls && touch x` is judged
  per command, so `touch` is refused.
- **Sessions**: every JSON event carries the session id, which Shepherd records; the next
  run of the lane resumes it with `-s`.
- **Cost**: tokens are logged. Cost is recorded as 0 for a local model, which OpenCode
  has no rates for. A priced provider's per-step cost is summed per run, logged at the end
  and recorded as `opencode`'s spend.
- **Output** is read from `--format json` and goes through the same redaction as every
  other agent's.

## The version pin

The adapter was checked against OpenCode 1.18.35 and accepts major version 1. Any other
major is refused at run start, with a message naming the version found, because the flags
and JSON events the adapter reads may differ. Shepherd checks again on every run, a
resumed one too. The version is printed at the end of each run's log.

## Limits

These come from the model and from OpenCode, and Shepherd does not hide them:

- **Exit 0 with no change is a failure.** The model sometimes prints a tool call as plain
  text and ends the turn, or finishes without editing, and OpenCode exits 0 either way.
  A run that exits 0 with no commit, no file changed in the worktree, and nothing asked
  or reported through Shepherd's tools is recorded `failed` ("opencode exited 0 but
  changed nothing"), so it shows in your thread as a failed run and is never shipped.
  The run log says whether the model printed a tool call as text. Uncommitted edits
  count as a change.
- **Hangs.** The model sometimes calls a tool that does not exist, and the run can stall.
  A run that prints nothing for `opencode.idle_timeout` (default 10 minutes, at least
  1 minute, or `off`) is stopped and recorded `failed` ("hung: printed nothing for ...").
  Any output resets the clock. That clock is the run going quiet. When `endpoint` is
  set, each request to the model host is also limited by `opencode.request_timeout`
  (default 60 minutes, at least 1 minute, or `off`) and by `opencode.chunk_timeout`
  (default 10 minutes, at least 10 seconds). `chunk_timeout` is the gap between
  streamed chunks, and it also covers the wait for the first one: OpenCode starts
  that clock on the first read of the event stream, before any chunk has arrived.
  A local server that sends the stream headers and then spends a long time on the
  prompt, with no token yet, can hit it. The time before those headers arrive is
  `request_timeout`. A slow model can fail with a provider timeout while the run is
  still printing, or before it has been quiet for `idle_timeout`. Raising
  `idle_timeout` does not raise these.
  `request_timeout: off` disables the request limit. `chunk_timeout` cannot be turned
  off: OpenCode only accepts a positive number, and a missing or false value in its
  own config becomes a 5 minute default, so Shepherd always writes a number.
- **Tool names.** Shepherd's worker tools reach the model as `shepherd_ask_human`,
  `shepherd_ask_shepherd` and `shepherd_report`; a small model sometimes calls them by
  the short names in the brief and is told the tool is unavailable.
- **Your own and the repo's OpenCode config load under Shepherd's.** A rule there that
  Shepherd's permission block does not override still applies. Keep the global config to
  a provider, and review a repo's `opencode.json` as you would any file that changes what
  an agent may run.
- **Sessions live in OpenCode's own data directory**, not in Shepherd's.

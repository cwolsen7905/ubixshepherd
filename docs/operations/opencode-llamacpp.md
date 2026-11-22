# OpenCode on a llama.cpp server in Kubernetes

How to point Shepherd's OpenCode runs at a llama.cpp server that runs in a cluster
behind llama-swap. Setup in general is in [../opencode.md](../opencode.md).

## The endpoint

llama-swap fronts llama.cpp and serves an OpenAI-compatible API. Example address:

```
http://10.50.18.65:8080/v1
```

Use the address your cluster gives the service. If it moves, `opencode.endpoint` is the
one value to change; a connection error (`curl: (7)` or a timeout) from a run is the
sign that it is stale.

## Models

`GET /v1/models` lists them, with `status.value` of `loaded` or `unloaded`.

| Model | Notes |
| --- | --- |
| `qwen3.8-27b` | Recommended default. 128k context, 3 parallel slots. |
| `qwen3.8-27b-xhigh` | 1 slot, MTP, long reasoning. Slower per task. |
| `qwen3.6-35b-a3b`, `qwen3.6-35b-a3b-mtp` | Fast mixture-of-experts models. |
| `gemma-4-26b-a4b`, `gemma-4-31b` | The 31b has a smaller window (64k). |
| `qwen3.8-flash-next`, `qwen3.8-flash-next-coder` | Large model split over GPU and RAM, 64k window. |

Check the window each model has on the server before raising `context`.

## Shepherd config

In `~/.shepherd/config.yaml`:

```yaml
opencode:
  endpoint: http://10.50.18.65:8080/v1
  context: 131072     # the server's window for qwen3.8-27b
  output: 16384
  idle_timeout: 30m
  request_timeout: off
defaults:
  agent:
    model: {claude: sonnet, opencode: "local/qwen3.8-27b"}
```

A repo profile with its own `agent.model.opencode` overrides the default, so change it
there too if it names another model. The `local/` prefix selects the provider Shepherd
defines from `opencode.endpoint`; a name such as `openrouter/...` goes to that hosted
provider instead and never reaches the cluster. A config change applies to the next
run.

Keep `context` no higher than the loaded model's window: `context` only tells OpenCode
when to compact, and the server does not grow to match. Use `65536` and `8192` for the
64k models.

## Verify

1. The server answers and the model is listed:

   ```
   curl -s http://10.50.18.65:8080/v1/models | grep -o '"id":"[^"]*"'
   ```

2. A small completion works. qwen3 models reason first, so allow enough tokens for an
   answer to appear after the reasoning:

   ```
   curl -s http://10.50.18.65:8080/v1/chat/completions \
     -H 'content-type: application/json' \
     -d '{"model":"qwen3.8-27b","messages":[{"role":"user","content":"Say ok"}],"max_tokens":256}'
   ```

   `reasoning_content` holds the thinking and `content` the answer. With a tiny
   `max_tokens`, `content` is empty and `finish_reason` is `length`: that is not a fault.

3. A Shepherd run reaches it:

   ```
   shepherd lane run <lane> --agent opencode "Fix the typo in README.md"
   ```

   The run log's closing line names the model and says "local, no cost".

## Caveats

- **One GPU.** The server holds one model at a time. Requests for another model make
  llama-swap unload the loaded one and load the new one, which takes minutes for a large
  model, and the requests of anyone using the first model wait or fail meanwhile. Keep
  runs on one model.
- **Swap delay.** The first request after a swap can look hung. `request_timeout: off`
  and a generous `idle_timeout` keep Shepherd from killing a run during a load.
- **Slots.** `qwen3.8-27b` has 3 parallel slots, so no more than 3 OpenCode runs at once
  make progress; the rest queue. The `xhigh` model has 1.
- **Small-model limits.** Per [../opencode.md](../opencode.md), a local model suits small
  mechanical edits, not tests in a typed codebase.

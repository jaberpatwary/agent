# Agentic news desk

The portal's newsroom is a desk of AI agents that a run takes through fixed
stages. Code lives in `internal/agentic` (the stages are `stages.go`); the
provider layer with tool calling is `internal/ai/chat.go`.

## Flow of one run

Every stage is one HTTP call, `POST /api/agent/stage/<name>` (X-API-Key), and
one node of the n8n workflow (`n8n/autonews-agent.json`), so the workflow
shows which agent is working and what it returned. Each reply carries
`next`, the stage to call now; the workflow's IF nodes only follow it.

```
n8n, every 15 min
  │
  ▼
plan     Editor agent    fetch feeds, dedupe, download photos, re-read the
  │                      source pages; pick the story to cover + 2 reserves
  ▼
verify   Reporter agent  the desk first gathers evidence without a model call
  │                      (news search on the headline + the archive); the
  │                      reporter then chooses its own searches and ends with
  │                      record_verification, reject_story or ask_editor
  ▼
write    Writer          rewrites the whole source in our own words; after a
  │  ▲                   failed review it fixes that draft instead
  ▼  │ failed
review   Reviewer        hard rules in code + an independent AI editor
  │
  ▼
decide   Reporter agent  sees the draft and its review, and ends with
  │                      submit_article (publish / hold / update),
  │                      reject_story or ask_editor
  ▼
more stories? ──▶ verify (story held or rejected and a reserve is left)
  │
  ▼
finish   closes the run and returns its summary
```

The order of the stages is fixed; the judgement is inside them. The reporter
is the one agent that works in a loop (model chooses a tool ─▶ tool runs ─▶
result goes back to the model), and its decide stage continues the same
conversation as its verify stage. The editor, the writer and the reviewer
are single-purpose model calls.

A failed draft goes back to the writer for up to 3 reviews and until 10
minutes into the run; the 4th review is kept for the reporter, who may
correct the draft once itself (`review_draft`) before deciding. No new story
starts after 9 minutes and the run stops at 14, because the next one is due;
a run whose driver disappears is closed by the desk a minute later.

`POST /api/agent/run` runs the same stages inside the app (the "run now"
button on `/admin/agent`, and the `news-worker` command).

Every model turn re-sends the conversation, so the engine shortens tool
results the model has already read (`compactHistory`). Free quotas are per
model, so the gateway walks a list of models (`GEMINI_EXTRA_MODELS`,
`GROQ_EXTRA_MODELS`) and skips a model whose daily quota is used up until
the reset time the provider names.

A story goes live with the time it was published on the portal (or approved
by an editor), so new news is always at the top and the open homepage's
"নতুন খবর এসেছে" bar notices it.

## Reporter agent tools

| Tool | Stage | What it does | Guardrail |
|---|---|---|---|
| `search_news` | verify | Google News RSS search (free, no key) | — |
| `web_search` | verify | Groq web search via the gateway | falls back to telling the agent to use `search_news` |
| `read_source`, `read_page` | verify | The full source text / another outlet's article text | blocks localhost/private URLs |
| `find_similar_stories`, `get_article` | verify | Search / read the portal's own archive | — |
| `remember_fact` | verify | Saves a current fact (who holds an office now) for later stories | needs 2 outlets from this story's searches |
| `record_verification` | verify | Records the fact-check verdict, the corroborating outlets and notes for the writer | refused until there is search evidence |
| `review_draft` | decide | The reporter's own correction of a failed draft: a new title/summary, or flagged numbers that are not facts of the story | only while a review is left |
| `submit_article` | decide | Publish, hold for an editor, or update an existing article (old text kept as a version) | needs a positive verification and, to publish, a passed review; sensitive categories and the autopublish switch force a hold |
| `reject_story` | verify, decide | fake / duplicate / not_news / low_quality | "fake" needs search evidence |
| `ask_editor` | verify, decide | Pauses the story with a question for a human | answer is given to the agent next time |

The writer rewrites the whole source passage by passage (≤ 7 calls, nothing
dropped) and writes a new headline. After a failed review of a source of
≤ 4000 characters it fixes only the reported problems in that draft, so a fix
does not bring new problems; a held story keeps the newest draft.

The reviewer runs the hard rules in code (length ≥ 85% of source, no copied
sentences of 7+ words, no outlet names, numbers kept, headline not the
source's) and an independent AI editor that is given the CURRENT FACTS (it
must not "correct" today's office-holders from memory).

The engine (`engine.go`) also stops the agent at a turn limit, a per-story
token budget, or if it stops acting; a verified draft is then held for an
editor instead of being lost.

## Memory

Before each story the agent is told the editors' recent decisions and
reasons (`editor_feedback`, recorded from the admin panel), the source's
track record (from `news_journeys`), and, on a retry, its previous attempt
and any editor answer.

## Watching and controlling it

* `/admin/agent` — status, recent runs, the editor agent's choices, and the
  limits: on/off switch, autopublish, turns, stories per run, token budget.
* `/admin/journey/<news_id>/trace` — every thought, tool call and result for a story.
* n8n (http://localhost:5678) — each execution shows every stage, its agent and its result.
* `GET /api/agent/status` (X-API-Key) — the same status as `/admin/agent`.

Providers are chosen by `ai.Gateway.Chat`: Gemini → Gemini fallback → Groq →
Cohere, skipping any that are rate-limited.

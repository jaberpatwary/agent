# Agentic news desk

The portal's newsroom is an AI agent, not a fixed pipeline. Code lives in
`internal/agentic`; the provider layer with tool calling is `internal/ai/chat.go`.

## Flow of one run

```
n8n (every 15 min) ──POST /api/agent/run──▶ Desk.Start
                                              │
          fetch feeds, dedupe, download photos (cron.Worker.PendingItems)
          re-read each story's source page for its full text (fullStories)
                                              │
       Editor agent: picks the story to cover first, plus 2 reserves
                                              │
       Desk gathers evidence without a model call: news search on the
       headline + the portal's archive (story.observe)
                                              │
       Reporter agent — loop until a final decision:
         model chooses a tool ─▶ tool runs ─▶ result goes back to the model
         (usually: record_verification + write_draft, then submit_article)
                                              │
     submit_article (publish / hold / update)  ·  reject_story  ·  ask_editor
                                              │
       not published (held, rejected)? next reserve; no new story after
       9 minutes, and the story in progress stops at 14 (the next run is due)
```

Every model turn re-sends the conversation, so the engine shortens tool
results the model has already read (`compactHistory`). Together with the
pre-gathered evidence and `write_draft` reviewing its own draft, a story
takes about 3 agent turns instead of ~10. Free quotas are per model, so the
gateway walks a list of models (`GEMINI_EXTRA_MODELS`, `GROQ_EXTRA_MODELS`)
and skips a model whose daily quota is used up until the reset time the
provider names.

A story goes live with the time it was published on the portal (or approved
by an editor), so new news is always at the top and the open homepage's
"নতুন খবর এসেছে" bar notices it.

## Reporter agent tools

| Tool | What it does | Guardrail |
|---|---|---|
| `search_news` | Google News RSS search (free, no key) | — |
| `web_search` | Groq web search via the gateway | falls back to telling the agent to use `search_news` |
| `read_page` | Extract another outlet's article text | blocks localhost/private URLs |
| `find_similar_stories`, `get_article` | Search / read the portal's own archive | — |
| `record_verification` | Records the fact-check verdict and corroborating outlets | refused until there is search evidence |
| `write_draft` | The desk writer rewrites the whole source passage by passage (≤ 7 calls, nothing dropped), writes a new headline, then reviews the draft like `review_draft`. After a failed review (sources ≤ 4000 characters) it instead fixes only the reported problems in that draft, so a fix does not bring new problems | counts toward the 4 reviews; a held story keeps the newest draft |
| `review_draft` | Hard rules in code (length ≥ 85% of source, no copied sentences of 7+ words, no outlet names, numbers kept, headline not the source's) + an independent AI editor that is given the CURRENT FACTS (it must not "correct" today's office-holders from memory) | max 4 reviews |
| `submit_article` | Publish, hold for an editor, or update an existing article (old text kept as a version) | needs a positive verification and, to publish, a passed review; sensitive categories and the autopublish switch force a hold |
| `reject_story` | fake / duplicate / not_news / low_quality | "fake" needs search evidence |
| `ask_editor` | Pauses the story with a question for a human | answer is given to the agent next time |

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
* `GET /api/agent/status` (X-API-Key) — the same status for n8n.

Providers are chosen by `ai.Gateway.Chat`: Gemini → Gemini fallback → Groq →
Cohere, skipping any that are rate-limited.

# Agentic news desk

The portal's newsroom is an AI agent, not a fixed pipeline. Code lives in
`internal/agentic`; the provider layer with tool calling is `internal/ai/chat.go`.

## Flow of one run

```
n8n (every 15 min) ──POST /api/agent/run──▶ Desk.Start
                                              │
          fetch feeds, dedupe, download photos (cron.Worker.PendingItems)
                                              │
               Editor agent: picks which stories to cover first
                                              │
       Reporter agent, per story — loop until a final decision:
         model chooses a tool ─▶ tool runs ─▶ result goes back to the model
                                              │
     submit_article (publish / hold / update)  ·  reject_story  ·  ask_editor
```

## Reporter agent tools

| Tool | What it does | Guardrail |
|---|---|---|
| `search_news` | Google News RSS search (free, no key) | — |
| `web_search` | Groq web search via the gateway | falls back to telling the agent to use `search_news` |
| `read_page` | Extract another outlet's article text | blocks localhost/private URLs |
| `find_similar_stories`, `get_article` | Search / read the portal's own archive | — |
| `record_verification` | Records the fact-check verdict and corroborating outlets | refused until the agent has searched |
| `review_draft` | Hard rules in code (length ≥ 80% of source, no copied sentences, no outlet names, numbers kept) + an independent AI editor | max 4 reviews |
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

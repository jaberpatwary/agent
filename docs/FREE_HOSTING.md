# Free hosting setup

This repository is prepared for a hobby deployment using a Render Free web service, Supabase Free PostgreSQL, and a scheduled GitHub Actions worker.

## Set up the database and web app

1. Create a Supabase Free PostgreSQL project and import a backup of the local database. The local database is currently about 30 MB. Keep a separate backup before changing `DATABASE_URL`.
2. Connect this repository to Render and deploy with `render.yaml`.
3. In Render environment settings, set `DATABASE_URL` to the Supabase connection string and set a strong `ADMIN_DEFAULT_PASS`.

Render sets `DISABLE_BACKGROUND_WORKER=true`, so the web service does not run a second copy of the feed scheduler.

## Enable scheduled news updates

In GitHub repository Settings → Secrets and variables → Actions:

- Add the `DATABASE_URL` secret with the Supabase connection string.
- Add `OPENROUTER_API_KEY` if you want AI summaries through the free OpenRouter model router.
- Add the repository variable `NEWS_WORKER_ENABLED` with value `true` only after the database secret is configured.

The scheduled worker runs every 10 minutes and stays disabled until `NEWS_WORKER_ENABLED=true`. It connects to the existing schema without running migrations or seeding default accounts/feeds.

## Optional: SEO, your real domain, and image caching

Set `PUBLIC_BASE_URL` (e.g. `https://your-site.onrender.com` or your custom domain) in Render's environment settings to enable `/sitemap.xml`, `/sitemap-news.xml`, and correct canonical/OpenGraph links — without it those are skipped rather than emitting broken relative URLs.

`IMAGE_STORAGE_DIR` defaults to blank on Render in `render.yaml` because the free web service's disk is not persistent across deploys, so caching images there would just churn. If you add a Render persistent disk, set `IMAGE_STORAGE_DIR` to a path on it (e.g. `/data/images`) to enable it.

`AI_DAILY_TOKEN_BUDGET` (0 = unlimited) caps AI spend per day; set it as a Render env var and, separately, as the `AI_DAILY_TOKEN_BUDGET` repository **variable** (not secret) if you also want the GitHub Actions worker to respect the same cap.

## Free tier limits

Render Free web services sleep after inactivity, so the first page request can be slow. Supabase Free databases can pause after low activity. GitHub scheduled runs may be delayed, so this setup is suitable for a hobby portal rather than a strict always-on news service. Never commit `.env` or API keys.

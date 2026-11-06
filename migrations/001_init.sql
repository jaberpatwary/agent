-- This file documents the schema GORM's AutoMigrate creates automatically
-- on server startup (see internal/repository/postgres.go). You normally
-- don't need to run this by hand — it's provided for reference, for manual
-- inspection, or if you prefer to manage migrations outside GORM.

CREATE EXTENSION IF NOT EXISTS pgcrypto; -- for gen_random_uuid()

CREATE TABLE IF NOT EXISTS admin_users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username      VARCHAR(100) UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ DEFAULT now(),
    updated_at    TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE IF NOT EXISTS feed_sources (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        VARCHAR(255) NOT NULL,
    url         TEXT UNIQUE NOT NULL,
    category    VARCHAR(100),
    active      BOOLEAN DEFAULT true,
    last_fetch  TIMESTAMPTZ,
    last_error  TEXT,
    created_at  TIMESTAMPTZ DEFAULT now(),
    updated_at  TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE IF NOT EXISTS articles (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title        TEXT NOT NULL,
    content      TEXT NOT NULL,
    summary      TEXT,
    source_url   TEXT UNIQUE NOT NULL,
    source_name  VARCHAR(255),
    image_url    TEXT,
    category     VARCHAR(100),
    status       VARCHAR(20) DEFAULT 'published',
    published_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ DEFAULT now(),
    updated_at   TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_articles_category ON articles(category);
CREATE INDEX IF NOT EXISTS idx_articles_status ON articles(status);
CREATE INDEX IF NOT EXISTS idx_articles_published_at ON articles(published_at);

CREATE TABLE IF NOT EXISTS ai_usages (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider    VARCHAR(50) NOT NULL,
    model       VARCHAR(100),
    article_id  UUID,
    tokens_used INTEGER,
    success     BOOLEAN,
    error_msg   TEXT,
    duration_ms BIGINT,
    created_at  TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_ai_usages_article_id ON ai_usages(article_id);

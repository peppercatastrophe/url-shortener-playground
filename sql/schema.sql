-- urls table: maps a short code to the original URL.
CREATE TABLE IF NOT EXISTS urls (
    code         VARCHAR(16) PRIMARY KEY,
    original_url TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- clicks table: one row per redirect, written by the worker.
CREATE TABLE IF NOT EXISTS clicks (
    id         BIGSERIAL PRIMARY KEY,
    code       VARCHAR(16) NOT NULL,
    clicked_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
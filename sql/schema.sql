-- urls table: maps a short code to the original URL.
CREATE TABLE IF NOT EXISTS urls (
    code        VARCHAR(16) PRIMARY KEY,
    original_url TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
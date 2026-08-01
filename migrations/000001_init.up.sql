-- Initial schema: URL shortener tables.
-- urls: maps a short code to the original URL (write path of the API).
-- clicks: one row per redirect, written by the worker from the RabbitMQ queue.

CREATE TABLE IF NOT EXISTS urls (
    code         VARCHAR(16) PRIMARY KEY,
    original_url TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS clicks (
    id         BIGSERIAL PRIMARY KEY,
    code       VARCHAR(16) NOT NULL,
    clicked_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
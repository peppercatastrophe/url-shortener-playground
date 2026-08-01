-- Indexes to support the click analytics read path.
-- Most analytics queries filter by code and/or a clicked_at time range,
-- e.g. "click count for code X in the last 24h" or "latest clicks for code X".
-- Without these, every analytics query scans the whole clicks table (Seq Scan).
--
-- 1) Composite index on (code, clicked_at): serves per-code time-range
--    aggregations and "latest clicks for code X" (index scan, sorted by time).
-- 2) Partial index on (clicked_at) WHERE code IS NOT NULL: a narrower,
--    cheaper index for global time-range analytics over rows that have a code.
--    (Defensive: clicked_at is NOT NULL by schema, but the partial predicate
--    keeps the index small if future nullable codes are introduced.)

CREATE INDEX IF NOT EXISTS idx_clicks_code_clicked_at
    ON clicks (code, clicked_at);

CREATE INDEX IF NOT EXISTS idx_clicks_clicked_at
    ON clicks (clicked_at)
    WHERE code IS NOT NULL;
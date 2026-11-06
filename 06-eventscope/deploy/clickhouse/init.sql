CREATE TABLE IF NOT EXISTS events (
 tenant String, id String, user_id String, name String,
 occurred_at DateTime64(3, 'UTC'), properties String
) ENGINE=ReplacingMergeTree ORDER BY (tenant,id);
-- Query with FINAL for idempotent analytics after a retried export:
-- SELECT name, count(), uniqExact(user_id) FROM events FINAL
-- WHERE tenant='demo' GROUP BY name;

-- internal/db/migrations/0011_v0.5.0_semantic_cache.postgres.sql
-- +goose Up
CREATE TABLE semantic_index (
  service_id        TEXT NOT NULL REFERENCES services(id) ON DELETE CASCADE,
  exact_key_hash    TEXT NOT NULL,
  prompt_fingerprint TEXT NOT NULL,
  embedding_dim     INTEGER NOT NULL,
  embedding_blob    BYTEA NOT NULL,
  created_at        TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
  PRIMARY KEY (service_id, exact_key_hash)
);
CREATE INDEX idx_semantic_service_ts ON semantic_index(service_id, created_at DESC);

-- +goose Down
DROP TABLE semantic_index;

CREATE TABLE idempotency_keys (
  user_id TEXT NOT NULL,
  operation TEXT NOT NULL,
  key VARCHAR(128) NOT NULL,
  fingerprint TEXT NOT NULL,
  response BYTEA,
  expires_at TIMESTAMPTZ,
  PRIMARY KEY (user_id, operation, key),
  CHECK ((response IS NULL) = (expires_at IS NULL))
);
CREATE INDEX idempotency_keys_expiration_idx ON idempotency_keys (expires_at);

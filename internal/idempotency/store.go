// Package idempotency stores successful command snapshots in the caller's transaction.
package idempotency

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrMismatch = errors.New("idempotency key was already used with different input")
var keyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func ValidKey(key string) bool { return keyPattern.MatchString(key) }

func Fingerprint(input any) (string, error) {
	payload, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

// Claim locks the scoped key until commit. A placeholder is never committed without
// Complete. Separate READ COMMITTED statements see a concurrent winner's snapshot.
func Claim(ctx context.Context, tx pgx.Tx, user, operation, key, fingerprint string) ([]byte, error) {
	for {
		tag, err := tx.Exec(ctx, `INSERT INTO idempotency_keys (user_id, operation, key, fingerprint)
   VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`, user, operation, key, fingerprint)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() == 1 {
			return nil, nil
		}
		var stored string
		var response []byte
		var expired bool
		err = tx.QueryRow(ctx, `SELECT fingerprint, response, expires_at <= clock_timestamp()
   FROM idempotency_keys WHERE user_id=$1 AND operation=$2 AND key=$3 FOR UPDATE`, user, operation, key).Scan(&stored, &response, &expired)
		// Cleanup may have deleted the row between statements; retry the claim.
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if expired {
			_, err = tx.Exec(ctx, `UPDATE idempotency_keys SET fingerprint=$4, response=NULL, expires_at=NULL
    WHERE user_id=$1 AND operation=$2 AND key=$3`, user, operation, key, fingerprint)
			return nil, err
		}
		if stored != fingerprint {
			return nil, ErrMismatch
		}
		return response, nil
	}
}

func Complete(ctx context.Context, tx pgx.Tx, user, operation, key string, result any) error {
	response, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE idempotency_keys SET response=$4, expires_at=clock_timestamp() + interval '24 hours'
  WHERE user_id=$1 AND operation=$2 AND key=$3`, user, operation, key, response)
	return err
}

// Cleanup removes at most 1000 unlocked expired records per call.
func Cleanup(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `WITH expired AS (
  SELECT user_id, operation, key FROM idempotency_keys
  WHERE expires_at <= statement_timestamp() ORDER BY expires_at LIMIT 1000 FOR UPDATE SKIP LOCKED
 ) DELETE FROM idempotency_keys k USING expired e
 WHERE k.user_id=e.user_id AND k.operation=e.operation AND k.key=e.key`)
	return err
}

func RunCleanup(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		cleanupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := Cleanup(cleanupCtx, pool)
		cancel()
		if err != nil && ctx.Err() == nil {
			logger.Error("idempotency cleanup failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

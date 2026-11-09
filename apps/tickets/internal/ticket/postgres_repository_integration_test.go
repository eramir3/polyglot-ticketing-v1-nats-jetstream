package ticket

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"polyglot-ticketing-v1/internal/idempotency"
)

func startTicketRepositoryPostgres(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	container, err := postgres.Run(ctx, "postgres:17-alpine", postgres.BasicWaitStrategies(), postgres.WithDatabase("tickets"), postgres.WithUsername("tickets"), postgres.WithPassword("tickets-test-password"))
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("start tickets PostgreSQL container: %v", err)
	}
	databaseURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get tickets PostgreSQL connection string: %v", err)
	}
	applyTicketRepositoryMigrations(t, databaseURL)
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create tickets database pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func applyTicketRepositoryMigrations(t *testing.T, databaseURL string) {
	t.Helper()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get test working directory: %v", err)
	}
	migrationsPath := filepath.Join(workingDirectory, "..", "..", "migrations")
	migrator, err := migrate.New(fmt.Sprintf("file://%s", migrationsPath), databaseURL)
	if err != nil {
		t.Fatalf("initialize tickets migrations: %v", err)
	}
	t.Cleanup(func() { _, _ = migrator.Close() })
	if err := migrator.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("apply tickets migrations: %v", err)
	}
}

func TestCreationIdempotency(t *testing.T) {
	ctx := context.Background()
	pool := startTicketRepositoryPostgres(t, ctx)
	repository := NewPostgresRepository(pool)
	input := CreateInput{Title: "Original", Price: 100, UserID: "user-1", IdempotencyKey: uuid.NewString()}
	const callers = 8
	results := make([]Ticket, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Go(func() { results[i], errs[i] = repository.Create(ctx, input) })
	}
	wg.Wait()
	first := results[0]
	for i := range callers {
		if errs[i] != nil || !reflect.DeepEqual(first, results[i]) {
			t.Fatalf("concurrent retry %d: result=%+v err=%v", i, results[i], errs[i])
		}
	}
	var count int
	for _, table := range []string{"tickets", "outbox_events", "idempotency_keys"} {
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%s count=%d err=%v", table, count, err)
		}
	}
	// The retention deadline is anchored to the original success.
	var deadline string
	if err := pool.QueryRow(ctx, `SELECT expires_at::text FROM idempotency_keys`).Scan(&deadline); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Create(ctx, input); err != nil {
		t.Fatal(err)
	}
	var replayDeadline string
	if err := pool.QueryRow(ctx, `SELECT expires_at::text FROM idempotency_keys`).Scan(&replayDeadline); err != nil || deadline != replayDeadline {
		t.Fatalf("retry changed deadline: %q -> %q (%v)", deadline, replayDeadline, err)
	}
	changed := input
	changed.Title = "Changed"
	if _, err := repository.Create(ctx, changed); !errors.Is(err, idempotency.ErrMismatch) {
		t.Fatalf("expected mismatch, got %v", err)
	}
	// Replays must not read current resource state.
	if _, err := pool.Exec(ctx, `UPDATE tickets SET title='Edited', price=200 WHERE id=$1`, first.ID); err != nil {
		t.Fatal(err)
	}
	replay, err := repository.Create(ctx, input)
	if err != nil || !reflect.DeepEqual(first, replay) {
		t.Fatalf("snapshot replay=%+v err=%v", replay, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE idempotency_keys SET expires_at=clock_timestamp()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	fresh, err := repository.Create(ctx, input)
	if err != nil || reflect.DeepEqual(first, fresh) {
		t.Fatalf("expired key did not execute: %+v %v", fresh, err)
	}
	if err := idempotency.Cleanup(ctx, pool); err != nil {
		t.Fatal(err)
	}
	replay, err = repository.Create(ctx, input)
	if err != nil || !reflect.DeepEqual(fresh, replay) {
		t.Fatalf("cleanup removed active snapshot: %+v %v", replay, err)
	}
	// A different user has an independent key scope.
	input.UserID = "user-2"
	if _, err := repository.Create(ctx, input); err != nil {
		t.Fatalf("user-scoped key: %v", err)
	}

	input.IdempotencyKey = uuid.NewString()
	competing := input
	competing.Price++
	conflictErrors := make([]error, 2)
	wg.Go(func() { _, conflictErrors[0] = repository.Create(ctx, input) })
	wg.Go(func() { _, conflictErrors[1] = repository.Create(ctx, competing) })
	wg.Wait()
	successes, conflicts := 0, 0
	for _, err := range conflictErrors {
		if err == nil {
			successes++
		} else if errors.Is(err, idempotency.ErrMismatch) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent mismatch results: %v", conflictErrors)
	}
}

func TestCreationRollbackDoesNotConsumeKey(t *testing.T) {
	ctx := context.Background()
	pool := startTicketRepositoryPostgres(t, ctx)
	repository := NewPostgresRepository(pool)
	input := CreateInput{Title: "Original", Price: 100, UserID: "user-1", IdempotencyKey: uuid.NewString()}
	// Force failure after inserting the resource, when the outbox write runs.
	if _, err := pool.Exec(ctx, `ALTER TABLE outbox_events ADD CONSTRAINT reject_test_event CHECK (false) NOT VALID`); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Create(ctx, input); err == nil {
		t.Fatal("expected outbox failure")
	}
	for _, table := range []string{"tickets", "idempotency_keys"} {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("rollback %s count=%d err=%v", table, count, err)
		}
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE outbox_events DROP CONSTRAINT reject_test_event`); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Create(ctx, input); err != nil {
		t.Fatalf("retry after rollback: %v", err)
	}
}

func TestIdempotencyCleanupAndOperationScope(t *testing.T) {
	ctx := context.Background()
	pool := startTicketRepositoryPostgres(t, ctx)
	// Same user/key, different operations have independent fingerprints/results.
	for _, op := range []string{"create_ticket", "other_operation"} {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		snapshot, err := idempotency.Claim(ctx, tx, "user-1", op, "shared-key", op)
		if err != nil || snapshot != nil {
			t.Fatalf("operation claim: %s %v", snapshot, err)
		}
		if err := idempotency.Complete(ctx, tx, "user-1", op, "shared-key", op); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE idempotency_keys SET expires_at=clock_timestamp()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT 1 FROM idempotency_keys WHERE operation='create_ticket' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	if err := idempotency.Cleanup(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM idempotency_keys`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("cleanup must skip locked row, count=%d err=%v", count, err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := idempotency.Cleanup(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM idempotency_keys`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("cleanup must remove unlocked expired row, count=%d err=%v", count, err)
	}
}

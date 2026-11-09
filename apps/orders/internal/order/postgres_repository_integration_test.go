package order

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"google.golang.org/protobuf/proto"

	orderevents "polyglot-ticketing-v1/contracts/orders"
	ordersv1 "polyglot-ticketing-v1/protogen/go/orders/v1"

	"polyglot-ticketing-v1/internal/idempotency"
)

func TestPostgresRepositoryCancelByIDAndUser(t *testing.T) {
	ctx := context.Background()
	pool := startOrderRepositoryPostgres(t, ctx)
	repository := NewPostgresRepository(pool)

	t.Run("cancels a created order and publishes one cancellation event", func(t *testing.T) {
		orderID := seedOrderForCancellation(t, ctx, pool, StatusCreated, 0)

		canceled, err := repository.CancelByIDAndUser(ctx, orderID, "user-1")
		if err != nil {
			t.Fatalf("cancel created order: %v", err)
		}
		if canceled.Status != StatusCanceled || canceled.AggregateVersion != 1 {
			t.Fatalf("unexpected canceled order: %+v", canceled)
		}

		var payload []byte
		if err := pool.QueryRow(ctx, `SELECT payload FROM outbox_events WHERE subject = $1`, orderevents.OrderCanceledSubject).Scan(&payload); err != nil {
			t.Fatalf("read cancellation outbox event: %v", err)
		}
		var event ordersv1.OrderCanceled
		if err := proto.Unmarshal(payload, &event); err != nil {
			t.Fatalf("unmarshal cancellation outbox event: %v", err)
		}
		if event.GetOrderId() != orderID || event.GetAggregateVersion() != 1 {
			t.Fatalf("unexpected cancellation event: %+v", &event)
		}
	})

	for _, testCase := range []struct {
		name    string
		status  Status
		version int64
	}{
		{name: "rejects an awaiting payment order", status: StatusAwaitingPayment, version: 1},
		{name: "rejects a complete order", status: StatusComplete, version: 2},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			orderID := seedOrderForCancellation(t, ctx, pool, testCase.status, testCase.version)

			_, err := repository.CancelByIDAndUser(ctx, orderID, "user-1")
			if !errors.Is(err, ErrOrderNotCancelable) {
				t.Fatalf("expected not-cancelable error, got %v", err)
			}
			assertCancellationUnchanged(t, ctx, pool, orderID, testCase.status, testCase.version)
		})
	}

	t.Run("keeps an already canceled order idempotent", func(t *testing.T) {
		orderID := seedOrderForCancellation(t, ctx, pool, StatusCanceled, 1)

		canceled, err := repository.CancelByIDAndUser(ctx, orderID, "user-1")
		if err != nil {
			t.Fatalf("cancel already canceled order: %v", err)
		}
		if canceled.Status != StatusCanceled || canceled.AggregateVersion != 1 {
			t.Fatalf("unexpected idempotent cancellation result: %+v", canceled)
		}
		assertCancellationUnchanged(t, ctx, pool, orderID, StatusCanceled, 1)
	})
}

func TestPostgresRepositoryReservationBlockingLifecycle(t *testing.T) {
	ctx := context.Background()
	pool := startOrderRepositoryPostgres(t, ctx)
	repository := NewPostgresRepository(pool)

	t.Run("expired created order no longer blocks a new reservation", func(t *testing.T) {
		ticketID, oldOrderID := seedOrderWithExpiration(t, ctx, pool, StatusCreated, time.Now().UTC().Add(-time.Minute))

		result, err := repository.ReserveTicket(ctx, TicketReservationInput{IdempotencyKey: uuid.NewString(),
			ExpiresAt: time.Now().UTC().Add(ExpirationWindow),
			TicketID:  ticketID,
			UserID:    "user-2",
		})
		if err != nil {
			t.Fatalf("reserve after created order expires: %v", err)
		}
		if !result.Created || result.Order.ID == oldOrderID {
			t.Fatalf("expected a new reservation, got %+v", result)
		}
	})

	t.Run("expired awaiting payment order blocks a new reservation", func(t *testing.T) {
		ticketID, _ := seedOrderWithExpiration(t, ctx, pool, StatusAwaitingPayment, time.Now().UTC().Add(-time.Minute))

		_, err := repository.ReserveTicket(ctx, TicketReservationInput{IdempotencyKey: uuid.NewString(),
			ExpiresAt: time.Now().UTC().Add(ExpirationWindow),
			TicketID:  ticketID,
			UserID:    "user-2",
		})
		if !errors.Is(err, ErrReserved) {
			t.Fatalf("expected ticket to remain reserved, got %v", err)
		}
	})

	t.Run("payment failure releases an expired awaiting payment reservation", func(t *testing.T) {
		ticketID, orderID := seedOrderWithExpiration(t, ctx, pool, StatusAwaitingPayment, time.Now().UTC().Add(-time.Minute))
		if err := repository.ApplyPaymentFailed(ctx, uuid.NewString(), orderID); err != nil {
			t.Fatalf("apply payment failure: %v", err)
		}

		result, err := repository.ReserveTicket(ctx, TicketReservationInput{IdempotencyKey: uuid.NewString(),
			ExpiresAt: time.Now().UTC().Add(ExpirationWindow),
			TicketID:  ticketID,
			UserID:    "user-2",
		})
		if err != nil {
			t.Fatalf("reserve after payment failure: %v", err)
		}
		if !result.Created {
			t.Fatalf("expected payment failure to release ticket, got %+v", result)
		}
	})

	t.Run("payment success permanently blocks an expired awaiting payment reservation", func(t *testing.T) {
		ticketID, orderID := seedOrderWithExpiration(t, ctx, pool, StatusAwaitingPayment, time.Now().UTC().Add(-time.Minute))
		if err := repository.ApplyPaymentSucceeded(ctx, uuid.NewString(), orderID); err != nil {
			t.Fatalf("apply payment success: %v", err)
		}

		_, err := repository.ReserveTicket(ctx, TicketReservationInput{IdempotencyKey: uuid.NewString(),
			ExpiresAt: time.Now().UTC().Add(ExpirationWindow),
			TicketID:  ticketID,
			UserID:    "user-2",
		})
		if !errors.Is(err, ErrReserved) {
			t.Fatalf("expected completed order to block ticket, got %v", err)
		}
	})
}

func assertCancellationUnchanged(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orderID string, wantStatus Status, wantVersion int64) {
	t.Helper()
	var status string
	var version int64
	if err := pool.QueryRow(ctx, `SELECT status::text, aggregate_version FROM orders WHERE id = $1`, orderID).Scan(&status, &version); err != nil {
		t.Fatalf("read order after cancellation attempt: %v", err)
	}
	if Status(status) != wantStatus || version != wantVersion {
		t.Fatalf("unexpected persisted order: status=%q version=%d", status, version)
	}
	var eventCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM outbox_events WHERE subject = $1 AND payload::text LIKE '%' || $2 || '%'`, orderevents.OrderCanceledSubject, orderID).Scan(&eventCount); err != nil {
		t.Fatalf("count cancellation events: %v", err)
	}
	if eventCount != 0 {
		t.Fatalf("expected no cancellation event, got %d", eventCount)
	}
}

func seedOrderForCancellation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, status Status, version int64) string {
	t.Helper()
	ticketID := uuid.NewString()
	orderID := uuid.NewString()
	if _, err := pool.Exec(ctx, `INSERT INTO tickets (id, title, price, aggregate_version) VALUES ($1, $2, $3, $4)`, ticketID, "Projected ticket", 10_000, 0); err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO orders (id, expires_at, user_id, ticket_id, status, aggregate_version) VALUES ($1, $2, $3, $4, $5, $6)`, orderID, time.Now().UTC().Add(time.Minute), "user-1", ticketID, status, version); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	return orderID
}

func seedOrderWithExpiration(t *testing.T, ctx context.Context, pool *pgxpool.Pool, status Status, expiresAt time.Time) (string, string) {
	t.Helper()
	ticketID := uuid.NewString()
	orderID := uuid.NewString()
	if _, err := pool.Exec(ctx, `INSERT INTO tickets (id, title, price, aggregate_version) VALUES ($1, $2, $3, $4)`, ticketID, "Projected ticket", 10_000, 0); err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO orders (id, expires_at, user_id, ticket_id, status, aggregate_version) VALUES ($1, $2, $3, $4, $5, $6)`, orderID, expiresAt, "user-1", ticketID, status, 1); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	return ticketID, orderID
}

func startOrderRepositoryPostgres(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	container, err := postgres.Run(ctx, "postgres:17-alpine", postgres.BasicWaitStrategies(), postgres.WithDatabase("orders"), postgres.WithUsername("orders"), postgres.WithPassword("orders-test-password"))
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("start orders PostgreSQL container: %v", err)
	}
	databaseURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get orders PostgreSQL connection string: %v", err)
	}
	applyOrderRepositoryMigrations(t, databaseURL)
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create orders database pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func applyOrderRepositoryMigrations(t *testing.T, databaseURL string) {
	t.Helper()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get test working directory: %v", err)
	}
	migrationsPath := filepath.Join(workingDirectory, "..", "..", "migrations")
	migrator, err := migrate.New(fmt.Sprintf("file://%s", migrationsPath), databaseURL)
	if err != nil {
		t.Fatalf("initialize orders migrations: %v", err)
	}
	t.Cleanup(func() { _, _ = migrator.Close() })
	if err := migrator.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("apply orders migrations: %v", err)
	}
}

func TestCreationIdempotency(t *testing.T) {
	ctx := context.Background()
	pool := startOrderRepositoryPostgres(t, ctx)
	repository := NewPostgresRepository(pool)
	ticketID := uuid.NewString()
	if _, err := pool.Exec(ctx, `INSERT INTO tickets(id,title,price,aggregate_version) VALUES ($1,'Original',100,0)`, ticketID); err != nil {
		t.Fatal(err)
	}
	input := TicketReservationInput{TicketID: ticketID, UserID: "user-1", IdempotencyKey: uuid.NewString(), ExpiresAt: time.Now().UTC().Add(ExpirationWindow)}
	const callers = 8
	results := make([]ReservationResult, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Go(func() { results[i], errs[i] = repository.ReserveTicket(ctx, input) })
	}
	wg.Wait()
	first := results[0]
	for i := range callers {
		if errs[i] != nil || !reflect.DeepEqual(first, results[i]) {
			t.Fatalf("concurrent retry %d: result=%+v err=%v", i, results[i], errs[i])
		}
	}
	var count int
	for _, table := range []string{"orders", "outbox_events", "idempotency_keys"} {
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%s count=%d err=%v", table, count, err)
		}
	}
	// The retention deadline is anchored to the original success.
	var deadline string
	if err := pool.QueryRow(ctx, `SELECT expires_at::text FROM idempotency_keys`).Scan(&deadline); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ReserveTicket(ctx, input); err != nil {
		t.Fatal(err)
	}
	var replayDeadline string
	if err := pool.QueryRow(ctx, `SELECT expires_at::text FROM idempotency_keys`).Scan(&replayDeadline); err != nil || deadline != replayDeadline {
		t.Fatalf("retry changed deadline: %q -> %q (%v)", deadline, replayDeadline, err)
	}
	changed := input
	changed.TicketID = uuid.NewString()
	if _, err := repository.ReserveTicket(ctx, changed); !errors.Is(err, idempotency.ErrMismatch) {
		t.Fatalf("expected mismatch, got %v", err)
	}
	// Replays must not read current resource state.
	if _, err := pool.Exec(ctx, `UPDATE orders SET status='Canceled' WHERE id=$1`, first.Order.ID); err != nil {
		t.Fatal(err)
	}
	replay, err := repository.ReserveTicket(ctx, input)
	if err != nil || !reflect.DeepEqual(first, replay) {
		t.Fatalf("snapshot replay=%+v err=%v", replay, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE idempotency_keys SET expires_at=clock_timestamp()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	fresh, err := repository.ReserveTicket(ctx, input)
	if err != nil || reflect.DeepEqual(first, fresh) {
		t.Fatalf("expired key did not execute: %+v %v", fresh, err)
	}
	if err := idempotency.Cleanup(ctx, pool); err != nil {
		t.Fatal(err)
	}
	replay, err = repository.ReserveTicket(ctx, input)
	if err != nil || !reflect.DeepEqual(fresh, replay) {
		t.Fatalf("cleanup removed active snapshot: %+v %v", replay, err)
	}
	// A different user has an independent key scope.
	input.UserID = "user-2"
	input.TicketID = uuid.NewString()
	if _, err := pool.Exec(ctx, `INSERT INTO tickets(id,title,price,aggregate_version) VALUES ($1,'Other',100,0)`, input.TicketID); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ReserveTicket(ctx, input); err != nil {
		t.Fatalf("user-scoped key: %v", err)
	}

	input.IdempotencyKey = uuid.NewString()
	competing := input
	competing.TicketID = uuid.NewString()
	if _, err := pool.Exec(ctx, `INSERT INTO tickets(id,title,price,aggregate_version) VALUES ($1,'Competing',100,0)`, competing.TicketID); err != nil {
		t.Fatal(err)
	}
	conflictErrors := make([]error, 2)
	wg.Go(func() { _, conflictErrors[0] = repository.ReserveTicket(ctx, input) })
	wg.Go(func() { _, conflictErrors[1] = repository.ReserveTicket(ctx, competing) })
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
	pool := startOrderRepositoryPostgres(t, ctx)
	repository := NewPostgresRepository(pool)
	ticketID := uuid.NewString()
	if _, err := pool.Exec(ctx, `INSERT INTO tickets(id,title,price,aggregate_version) VALUES ($1,'Original',100,0)`, ticketID); err != nil {
		t.Fatal(err)
	}
	input := TicketReservationInput{TicketID: ticketID, UserID: "user-1", IdempotencyKey: uuid.NewString(), ExpiresAt: time.Now().UTC().Add(ExpirationWindow)}
	// Force failure after inserting the resource, when the outbox write runs.
	if _, err := pool.Exec(ctx, `ALTER TABLE outbox_events ADD CONSTRAINT reject_test_event CHECK (false) NOT VALID`); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ReserveTicket(ctx, input); err == nil {
		t.Fatal("expected outbox failure")
	}
	for _, table := range []string{"orders", "idempotency_keys"} {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("rollback %s count=%d err=%v", table, count, err)
		}
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE outbox_events DROP CONSTRAINT reject_test_event`); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ReserveTicket(ctx, input); err != nil {
		t.Fatalf("retry after rollback: %v", err)
	}
}

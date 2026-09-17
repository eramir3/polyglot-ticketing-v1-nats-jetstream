package order

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

		result, err := repository.ReserveTicket(ctx, TicketReservationInput{
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

		_, err := repository.ReserveTicket(ctx, TicketReservationInput{
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

		result, err := repository.ReserveTicket(ctx, TicketReservationInput{
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

		_, err := repository.ReserveTicket(ctx, TicketReservationInput{
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

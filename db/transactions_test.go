package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/jmoiron/sqlx"
	"gotest.tools/v3/assert"

	"github.com/circleci/ex/o11y"
)

func TestNoEffectError(t *testing.T) {
	var err error

	err = ErrNop
	assert.Assert(t, o11y.IsWarning(err))

	err = fmt.Errorf("some other error: %w", err)
	assert.Assert(t, o11y.IsWarning(err))

	err = fmt.Errorf("another error: %w", err)
	assert.Assert(t, errors.Is(err, ErrNop))
	assert.Assert(t, o11y.IsWarning(err))
}

func TestTransactionManager_ContextCancelled_WithError(t *testing.T) {
	ourError := errors.New("our error")
	tests := []struct {
		returnError error
		cancel      bool
		commits     int
		rollbacks   int
		expectError error
	}{
		{returnError: nil, cancel: false, expectError: nil, commits: 1},
		{returnError: nil, cancel: true, expectError: context.Canceled, rollbacks: 1},
		{returnError: ourError, cancel: false, expectError: ourError, rollbacks: 1},
		// the sqlx transaction wrapper sees the context cancel so does not call commit
		// but if the commit is called in our tx manager it will return context.Canceled and not ourError
		{returnError: ourError, cancel: true, expectError: ourError, rollbacks: 1},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("err-%v-cancel-%t", tt.returnError, tt.cancel), func(t *testing.T) {
			ttx := &fakeTx{}
			tx := NewTransactionManager(sqlx.NewDb(sql.OpenDB(fakeConnector{tx: ttx}), "fake"))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			err := tx.WithTransaction(ctx, func(ctx context.Context, _ Querier) error {
				if tt.cancel {
					cancel()
				}
				if tt.returnError != nil {
					return tt.returnError
				}
				return nil
			})
			if tt.expectError != nil {
				assert.Assert(t, errors.Is(err, tt.expectError), "got:%v wanted:%v", err, tt.expectError)
			} else {
				assert.NilError(t, err)
			}
			ttx.mu.Lock()
			defer ttx.mu.Unlock()
			assert.Equal(t, ttx.rollBackCount, tt.rollbacks)
			assert.Equal(t, ttx.commitCount, tt.commits)
		})
	}
}

type fakeConnector struct {
	driver.Connector
	tx *fakeTx
}

func (c fakeConnector) Connect(context.Context) (driver.Conn, error) {
	return fakeConn{tx: c.tx}, nil
}

type fakeConn struct {
	tx *fakeTx
	driver.Conn
}

func (c fakeConn) Begin() (driver.Tx, error) {
	// to simulate the transaction lifecycle
	// will be unlocked in Commit or Rollback
	c.tx.mu.Lock()
	return c.tx, nil
}

func (c fakeConn) Close() error {
	return nil
}

type fakeTx struct {
	// to simulate a transaction a bit and because the
	// actual rollback calls are async in the stdlib (or sqlx) code
	mu            sync.Mutex
	commitCount   int
	rollBackCount int
}

func (tx *fakeTx) Commit() error {
	tx.commitCount++
	defer tx.mu.Unlock()
	return nil
}

func (tx *fakeTx) Rollback() error {
	tx.rollBackCount++
	tx.mu.Unlock()
	return nil
}

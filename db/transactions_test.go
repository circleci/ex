package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/o11y"
)

func TestNoEffectError(t *testing.T) {
	var err error

	err = ErrNop
	assert.Check(t, o11y.IsWarning(err))

	err = fmt.Errorf("some other error: %w", err)
	assert.Check(t, o11y.IsWarning(err))

	err = fmt.Errorf("another error: %w", err)
	assert.Check(t, errors.Is(err, ErrNop))
	assert.Check(t, o11y.IsWarning(err))
}

func TestTxManager_WithTx_ContextCancelledWithError(t *testing.T) {
	ourError := errors.New("our error")
	tests := []struct {
		returnError error
		cancel      bool
		timeout     bool
		commits     int
		rollbacks   int
		expectError error
	}{
		{returnError: nil, cancel: false, timeout: false, expectError: nil, commits: 1},
		{returnError: nil, cancel: true, timeout: false, expectError: context.Canceled, rollbacks: 1},
		{returnError: nil, cancel: false, timeout: true, expectError: context.DeadlineExceeded, rollbacks: 1},
		{returnError: ourError, cancel: false, timeout: false, expectError: ourError, rollbacks: 1},
		// the sqlx transaction wrapper sees the context cancel so does not call commit
		// but if the commit is called in our tx manager it will return context.Canceled and not ourError
		{returnError: ourError, cancel: true, timeout: false, expectError: ourError, rollbacks: 1},
		{returnError: ourError, cancel: false, timeout: true, expectError: ourError, rollbacks: 1},
	}
	for _, tt := range tests {
		name := fmt.Sprintf("err-%v-cancel-%t-timeout-%t", tt.returnError, tt.cancel, tt.timeout)
		t.Run(name, func(t *testing.T) {
			ttx := &fakeTx{}
			tx := NewTxManager(sqlx.NewDb(sql.OpenDB(fakeConnector{tx: ttx}), "fake"))

			var cancel func()
			ctx := context.Background()
			if tt.timeout {
				ctx, cancel = context.WithTimeout(ctx, time.Millisecond*50)
			} else {
				ctx, cancel = context.WithCancel(ctx)
			}
			defer cancel()

			err := tx.WithTx(ctx, func(ctx context.Context, _ Querier) error {
				if tt.cancel {
					cancel()
				} else if tt.timeout {
					<-ctx.Done()
				}
				if tt.returnError != nil {
					return tt.returnError
				}
				return nil
			})
			if tt.expectError != nil {
				assert.Assert(t, errors.Is(err, tt.expectError), "got:%v wanted:%v", err, tt.expectError)
			} else {
				assert.Assert(t, err)
			}
			ttx.mu.Lock()
			defer ttx.mu.Unlock()
			assert.Check(t, cmp.Equal(ttx.rollBackCount, tt.rollbacks))
			assert.Check(t, cmp.Equal(ttx.commitCount, tt.commits))
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

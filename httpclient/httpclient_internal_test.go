package httpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	gocmp "github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"golang.org/x/sync/errgroup"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/httpserver"
	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/testing/testcontext"
)

func TestNewRequest_Formats(t *testing.T) {
	req := NewRequest("POST", "/%s.txt",
		RouteParams("the-path"),
	)
	assert.Check(t, cmp.Equal(req.url, "/the-path.txt"))
	assert.Check(t, cmp.Equal(req.route, "/%s.txt"))
	assert.Check(t, cmp.Equal(req.method, "POST"))
}

func TestNewRequest_NoParams(t *testing.T) {
	req := NewRequest("POST", "/api/foo")
	assert.Check(t, cmp.Equal(req.url, "/api/foo"))
	assert.Check(t, cmp.Equal(req.route, "/api/foo"))
	assert.Check(t, cmp.Equal(req.method, "POST"))
}

func TestHTTPError_Is(t *testing.T) {
	tests := []struct {
		code int
		is   bool
	}{
		{code: 100, is: false},
		{code: 101, is: false},
		{code: 400, is: false},
		{code: 401, is: true},
		{code: 403, is: true},
		{code: 404, is: true},
		{code: 405, is: false},
		{code: 500, is: false},
		{code: 503, is: false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("code-:%d", tt.code), func(t *testing.T) {
			// all errors start off as warnings - since they default to retrying
			var err error
			err = &HTTPError{code: tt.code}
			assert.Check(t, cmp.Equal(o11y.IsWarning(err), true))

			err = doneRetrying(err)
			assert.Check(t, cmp.Equal(o11y.IsWarning(err), tt.is))

			// confirm wrapped it is still checked as a do not trace
			wErr := fmt.Errorf("foo :%w", err)
			assert.Check(t, cmp.Equal(o11y.IsWarning(err), tt.is))

			// and check the wrapped err it still is an HTTPError and that we can get the code back
			ne := &HTTPError{}
			assert.Check(t, errors.As(wErr, &ne))
			assert.Check(t, cmp.Equal(ne.code, tt.code))
			// ne should be equivalent to wErr now
			assert.Check(t, !errors.Is(err, wErr))

			// check that no two instances are Is-quivalent
			err2 := &HTTPError{}
			// and confirm they are not equivalent
			assert.Check(t, !errors.Is(err, err2))
		})
	}
}

func TestHasStatusCode(t *testing.T) {
	tests := []struct {
		name  string
		err   error
		codes []int
		want  bool
	}{
		{
			name: "With matching code",
			err: &HTTPError{
				code: 400,
			},
			codes: []int{400, 500},
			want:  true,
		},
		{
			name: "With different code",
			err: &HTTPError{
				code: 200,
			},
			codes: []int{400, 500},
			want:  false,
		},
		{
			name:  "Empty error",
			err:   &HTTPError{},
			codes: []int{400},
			want:  false,
		},
		{
			name:  "Nil error",
			err:   nil,
			codes: []int{400},
			want:  false,
		},
		{
			name:  "Other kind of error",
			err:   errors.New("some other error"),
			codes: []int{400},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Check(t, cmp.Equal(HasStatusCode(tt.err, tt.codes...), tt.want))
		})
	}
}

func TestIsRequestProblem(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "With problem code",
			err: &HTTPError{
				code: 400,
			},
			want: true,
		},
		{
			name: "With non-Request error code",
			err: &HTTPError{
				code: 500,
			},
			want: false,
		},
		{
			name: "With good code",
			err: &HTTPError{
				code: 200,
			},
			want: false,
		},
		{
			name: "Empty error",
			err:  &HTTPError{},
			want: false,
		},
		{
			name: "Nil error",
			err:  nil,
			want: false,
		},
		{
			name: "Other kind of error",
			err:  errors.New("some other error"),
			want: false,
		},
		{
			name: "No content error",
			err:  ErrNoContent,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Check(t, cmp.Equal(IsRequestProblem(tt.err), tt.want))
		})
	}
}

func TestClient_ExplicitBackoff(t *testing.T) {
	testcases := []struct {
		Name             string
		RetryAfterHeader bool
	}{
		{Name: "without a Retry-After header", RetryAfterHeader: false},
		{Name: "with a Retry-After header", RetryAfterHeader: true},
	}
	for _, tt := range testcases {
		t.Run(tt.Name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(testcontext.Background())

			var mu sync.RWMutex
			send429 := false
			handlerCount := 0
			now := time.Now()
			nowFn := func() time.Time {
				mu.RLock()
				defer mu.RUnlock()
				return now
			}
			// start our server with a handler that writes a response and in a certain range of
			// requests returns 429's
			h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				doSend := send429
				handlerCount++
				mu.Unlock()
				if doSend {
					if tt.RetryAfterHeader {
						w.Header().Add("Retry-After", "30")
					}
					w.WriteHeader(http.StatusTooManyRequests)
					return
				}
				_, _ = io.WriteString(w, `{"hello": "world!"} ...`)
				// to help the client have the full number of concurrent requests in flight
				time.Sleep(2 * time.Millisecond)
			})

			srv, err := httpserver.New(ctx, httpserver.Config{
				Name:    "test server",
				Addr:    "localhost:0",
				Handler: h,
			})
			assert.Assert(t, err)

			g, ctx := errgroup.WithContext(ctx)
			t.Cleanup(func() {
				cancel()
				assert.Check(t, g.Wait())
			})
			g.Go(func() error {
				return srv.Serve(ctx)
			})

			t.Run("backoff", func(t *testing.T) {
				client := New(Config{
					Name:    "keep-alive",
					BaseURL: "http://" + srv.Addr(),
					Timeout: time.Second,
				})
				client.now = nowFn
				req := NewRequest("POST", "/")

				// Making concurrent calls in this test to increase the chance of
				// flushing out any race in the client
				const numReq = 50
				var wg sync.WaitGroup
				wg.Add(numReq)
				for n := 0; n < numReq; n++ {
					go func() {
						err := client.Call(context.Background(), req)
						assert.Assert(t, err)
						wg.Done()
					}()
				}
				wg.Wait()

				// At some random point start sending 429's (and explicitly stop setting once we have)
				ctx429, cancel429 := context.WithCancel(ctx)
				go func() {
					for {
						if ctx429.Err() != nil {
							return
						}
						// start sending 429's
						mu.Lock()
						if handlerCount > numReq+5 {
							send429 = true
							cancel429()
						}
						mu.Unlock()
						time.Sleep(time.Microsecond * 10)
					}
				}()

				// hopefully during these concurrent calls we will see the 429 and the explicit backoff
				// It is not critical that we do, it is just statistically likely, and in that case
				// we can be confident that we would see if the client was racy.
				wg.Add(numReq)
				for n := 0; n < numReq; n++ {
					go func() {
						// these calls may see a mix of nil error, explicit backoff
						// and 429's most likely all 429's, so no point testing the error
						_ = client.Call(context.Background(), req)
						wg.Done()
					}()
				}
				wg.Wait()

				// make sure we stop sending 429's after the backoff time has elapsed
				// wait until we are sure the 429 setting loop above is complete
				<-ctx429.Done()
				send429 = false

				// confirm the server may have seen all or none of the calls whilst the 429 was being set
				assert.Check(t, handlerCount > numReq && handlerCount <= numReq*2, handlerCount)

				// there is a v slim chance this call is the first one to see the 429
				_ = client.Call(context.Background(), req)

				// but this one will definitely be an explicit backoff
				curHandlerCount := handlerCount
				err = client.Call(context.Background(), req)
				assert.Check(t, cmp.ErrorContains(err, "explicit backoff"))
				if tt.RetryAfterHeader {
					assert.Check(t, gocmp.Equal(client.lastRetryAfterTime, time.Now().Add(time.Second*30), cmpopts.EquateApproxTime(time.Second)))
				}
				// and will not have called the server
				assert.Check(t, cmp.Equal(curHandlerCount, handlerCount))

				// during some concurrent calls set the time to have elapsed past the 10s last 429 time
				// to close the circuit - these calls may not see the close, or they may all see it
				// or something in between, so there is not much we can assert.
				wg.Add(numReq)
				for n := 0; n < numReq; n++ {
					// at some random point boost the time to close the circuit
					if n == 10 {
						go func() {
							mu.Lock()
							now = now.Add(time.Second * 20)
							mu.Unlock()
						}()
					}
					go func() {
						err := client.Call(context.Background(), req)
						if err != nil {
							assert.Check(t, cmp.ErrorContains(err, "explicit backoff"))
						}
						wg.Done()
					}()
				}
				wg.Wait()

				if !tt.RetryAfterHeader {
					// this call will definitely nt see the explicit backoff
					err = client.Call(context.Background(), req)
					assert.Assert(t, err)
				} else {
					// during some concurrent calls set the time to have elapsed past the 10s last 429 time
					// to close the circuit - these calls may not see the close, or they may all see it
					// or something in between, so there is not much we can assert.
					wg.Add(numReq)
					for n := 0; n < numReq; n++ {
						// at some random point boost the time to close the circuit
						if n == 10 {
							go func() {
								mu.Lock()
								now = now.Add(time.Second * 11)
								mu.Unlock()
							}()
						}
						go func() {
							err := client.Call(context.Background(), req)
							if err != nil {
								assert.Check(t, cmp.ErrorContains(err, "explicit backoff"))
							}
							wg.Done()
						}()
					}
					wg.Wait()

					// this call will definitely not see the explicit backoff
					err = client.Call(context.Background(), req)
					assert.Assert(t, err)
				}
			})

		})
	}
}

func Test_parseRetryAfterHeader(t *testing.T) {
	testcases := []struct {
		Name         string
		HeaderValue  string
		ExpectedTime time.Time
		ExpectedOK   bool
	}{
		{
			Name:         "invalid header returns false",
			HeaderValue:  "uasdfaisdf90asudf90asuid90fuas0d9uf0s",
			ExpectedTime: time.Time{},
			ExpectedOK:   false,
		},
		{
			Name:         "http-time returns a time and true",
			HeaderValue:  "Tue, 10 Nov 2009 23:00:00 GMT",
			ExpectedTime: time.Date(2009, 11, 10, 23, 00, 00, 00, time.UTC),
			ExpectedOK:   true,
		},
		{
			Name:         "int values returns a time and true",
			HeaderValue:  "30",
			ExpectedTime: time.Now().Add(30 * time.Second),
			ExpectedOK:   true,
		},
	}
	for _, tt := range testcases {
		t.Run(tt.Name, func(t *testing.T) {
			retryHeaderTime, ok := parseRetryAfterHeader(tt.HeaderValue)
			assert.Check(t, gocmp.Equal(retryHeaderTime, tt.ExpectedTime, cmpopts.EquateApproxTime(time.Second)))
			assert.Check(t, cmp.Equal(ok, tt.ExpectedOK))
		})
	}
}

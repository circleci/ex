package metrics

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptrace"
	"sync"
	"sync/atomic"
	"time"

	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/system"
)

type Metrics struct {
	prov o11y.MetricsProvider
	rt   http.RoundTripper
	name string

	mu            sync.Mutex
	poolAvailable int64 // an estimate based on reference counting
	inFlight      int64
	inFlightMax   int64
}

// New creates a new Metrics for capturing metrics from http trace.
// the context is needed to extract the metrics provider.
func New(ctx context.Context) *Metrics {
	var prov o11y.MetricsProvider
	p := o11y.FromContext(ctx)
	if p != nil {
		prov = p.MetricsProvider()
	}

	return &Metrics{
		prov: prov,
	}
}

// Wrap wraps r with the Metrics reference counted round tripper.
func (m *Metrics) Wrap(name string, r http.RoundTripper) http.RoundTripper {
	m.name = name
	m.rt = r
	return m
}

// RoundTrip makes Metrics a http.RoundTripper. It adds reference counting to
// round trips to be able to count inflight requests.
func (m *Metrics) RoundTrip(req *http.Request) (*http.Response, error) {
	func() {
		m.mu.Lock()
		defer m.mu.Unlock()

		m.inFlight++
		if m.inFlight > m.inFlightMax {
			m.inFlightMax = m.inFlight
		}
	}()

	defer func() {
		m.mu.Lock()
		m.inFlight--
		m.mu.Unlock()
	}()

	// *important* that this is done outside the lock
	return m.rt.RoundTrip(req)
}

func (m *Metrics) GaugeName() string {
	return "httpclient"
}

// Gauges are instantaneous name value pairs
func (m *Metrics) Gauges(_ context.Context) map[string][]system.TaggedValue {
	m.mu.Lock()
	defer m.mu.Unlock()

	tags := []string{"client:" + m.name}

	poolAvail := m.poolAvailable
	if poolAvail < 0 {
		poolAvail = 0
	}

	return map[string][]system.TaggedValue{
		"in_flight": {
			{
				Val:  float64(m.inFlight),
				Tags: append(tags, "type:instant"),
			},
			{
				Val:  float64(m.inFlightMax),
				Tags: append(tags, "type:max"),
			},
		},
		"pool_avail_estimate": {
			{
				Val:  float64(poolAvail),
				Tags: tags,
			},
		},
	}
}

// WithTracer adds the tracer onto the context for this request.
//nolint:funlen
func (m *Metrics) WithTracer(ctx context.Context, route string) context.Context {
	r := &request{
		m:   m,
		ctx: ctx,
		commonTags: []string{
			"client:" + m.name,
			"route:" + route,
		},
	}

	trace := &httptrace.ClientTrace{
		GetConn: func(hostPort string) {
			r.con = &con{
				host:  hostPort,
				getAt: time.Now(),
			}
		},
		GotConn: r.gotCon,
		PutIdleConn: func(err error) {
			if err != nil {
				return
			}
			atomic.AddInt64(&r.m.poolAvailable, 1)
		},
		GotFirstResponseByte: func() {
			if r.requestDoneAt.IsZero() {
				return
			}
			duration := time.Since(r.requestDoneAt)
			_ = m.prov.TimeInMilliseconds("httpclient.req.first_byte",
				float64(duration.Milliseconds()), r.commonTags, 1)
			o11y.AddField(ctx, "req.first_byte", duration)
		},
		DNSStart: func(info httptrace.DNSStartInfo) {
			r.con.dns = &dns{
				host:    info.Host,
				startAt: time.Now(),
			}
		},
		DNSDone: func(info httptrace.DNSDoneInfo) {
			if r.con.dns == nil {
				return
			}
			r.con.dns.doneAt = time.Now()

			r.con.dns.addrCount = len(info.Addrs)
			r.con.dns.coalesced = info.Coalesced
			r.con.dns.error = info.Err != nil
		},
		ConnectStart: func(network, addr string) {
			r.con.dialStartAt = time.Now()
		},
		ConnectDone: func(network, addr string, err error) {
			r.con.dialDoneAt = time.Now()
		},
		TLSHandshakeStart: func() {
			r.con.tlsStartAt = time.Now()
		},
		TLSHandshakeDone: func(state tls.ConnectionState, err error) {
			r.con.tlsDoneAt = time.Now()
		},
		//WroteHeaders:
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			r.requestDoneAt = time.Now()
			duration := r.requestDoneAt.Sub(r.conDoneAt)
			o11y.AddField(ctx, "req.wrote_request", duration)
			_ = m.prov.TimeInMilliseconds("httpclient.req.wrote",
				float64(duration.Milliseconds()), r.commonTags, 1)
		},
	}
	return httptrace.WithClientTrace(ctx, trace)
}

type request struct {
	m *Metrics
	// this is the per-request context
	ctx        context.Context
	commonTags []string

	con           *con
	conDoneAt     time.Time
	requestDoneAt time.Time
}

type dns struct {
	startAt   time.Time
	doneAt    time.Time
	host      string
	addrCount int
	coalesced bool
	error     bool
}

type con struct {
	host  string
	getAt time.Time

	dialStartAt time.Time
	dialDoneAt  time.Time
	tlsStartAt  time.Time
	tlsDoneAt   time.Time

	dns *dns
}

//nolint:funlen
func (r *request) gotCon(info httptrace.GotConnInfo) {
	if r.con == nil {
		panic("cant have gotCon after getCon")
	}
	r.conDoneAt = time.Now()

	commonTags := append(r.commonTags, "host:"+r.con.host)

	tags := map[string]string{
		"reused":  "false",
		"starved": "false",
		"idle":    "false",
		"delayed": "false",
	}

	// internalConnDelay is the delay getting a connection, because of internal client reasons
	// such as enforced by various internal Max limits. We explicitly remove actual dial delays
	// from the dialing and tls handshaking.
	internalConnDelay := time.Since(r.con.getAt)

	if info.Reused {
		// Update the approximation to pool available depth
		atomic.AddInt64(&r.m.poolAvailable, -1)

		tags["reused"] = "true"

		// If it is reused then there will have been no time taken to form a new connection
		// any delay is due to limiting - very noteworthy
		if internalConnDelay > time.Millisecond*10 {
			tags["delayed"] = "true"
		}

		if info.WasIdle {
			tags["idle"] = "true"
			// this is the time this connection was idle - if this is low the pool is filling
			_ = r.m.prov.TimeInMilliseconds("httpclient.con.idle",
				float64(info.IdleTime.Milliseconds()), r.commonTags, 1)
			o11y.AddField(r.ctx, "req.con_idle", info.IdleTime)
		} else {
			// Reused connections might not have been made idle, they can be used immediately.

			// this means the connection was never returned to the pool so a request was
			// waiting for the connection, which effectively means the pool was exhausted.
			tags["starved"] = "true" // let's see if this is congruent with delayed
		}
	} else {
		// Getting here means we must have established a new connection.
		// (We never see WasIdle here: a new connection can never have been idle.)

		// Isolate the actual connection forming...
		makeConnectionDuration := r.con.dialDoneAt.Sub(r.con.dialStartAt)
		// ...from the internal queueing delays to acquired a connection.
		internalConnDelay = internalConnDelay - makeConnectionDuration

		// This is the stand-alone metric that tells us how long it took to physically form a
		// connection including DNS, and TLS handshake, but excluding any internal delays
		// (due to enforced maximum connections etc.)
		_ = r.m.prov.TimeInMilliseconds("httpclient.con.new",
			float64(makeConnectionDuration.Milliseconds()), commonTags, 1)
		o11y.AddField(r.ctx, "req.con_new", makeConnectionDuration)

		// this separate metric for dns resolution should always be less than makeConnectionDuration
		if r.con.dns != nil {
			dur := r.con.dns.doneAt.Sub(r.con.dns.startAt)

			dnsTags := append(commonTags,
				fmt.Sprintf("adresses:%d", r.con.dns.addrCount),
				fmt.Sprintf("coalesced:%t", r.con.dns.coalesced),
				fmt.Sprintf("error:%t", r.con.dns.error),
			)
			_ = r.m.prov.TimeInMilliseconds("httpclient.con.dns",
				float64(dur.Milliseconds()), dnsTags, 1)
			o11y.AddField(r.ctx, "req.con_dns", dur)
		}
		if !r.con.tlsDoneAt.IsZero() {
			dur := r.con.tlsDoneAt.Sub(r.con.tlsStartAt)
			_ = r.m.prov.TimeInMilliseconds("httpclient.con.tls", float64(dur.Milliseconds()), commonTags, 1)
			o11y.AddField(r.ctx, "req.con_tls", dur)
		}
	}

	tagList := commonTags
	for k, v := range tags {
		o11y.AddField(r.ctx, "req.con_"+k, v)
		tagList = append(tagList, k+":"+v)
	}

	o11y.AddField(r.ctx, "req.con_wait", internalConnDelay)
	// this metric measures delays in fully internal logic, and is a strong measure of client contention.
	_ = r.m.prov.TimeInMilliseconds("httpclient.con.wait", float64(internalConnDelay.Milliseconds()), tagList, 1)
}

package server

import (
	"context"
	"net"
	"net/url"
	"sync"
)

// trackedListener includes an embedded anonymous net.Listener but overrides Accept so that it can count the
// number of connections. All Accepted connections are stored until the connection is Closed.
type trackedListener struct {
	net.Listener

	mu         sync.RWMutex
	name       string
	accepted   int
	activeConn int
	remotes    map[string]int
}

// Accept waits for and returns the next connection to the listener. It returns trackedConnection which
// takes l as a field, so that the Close call can remove this connection from l's list of active connections.
func (l *trackedListener) Accept() (net.Conn, error) {
	con, err := l.Listener.Accept()
	if err != nil {
		return con, err
	}
	tracked := &trackedConnection{
		l:    l,
		Conn: con,
	}
	l.trackConn(tracked, true)

	return tracked, err
}

// MetricName returns the name for the metrics the listener will produce. (satisfies MetricProducer)
func (l *trackedListener) MetricName() string {
	return l.name + "-listener"
}

// Gauges returns a set of key value pairs representing gauge metrics. (satisfies MetricProducer)
func (l *trackedListener) Gauges(_ context.Context) map[string]float64 {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var (
		max int
		min int
	)
	// we use this guard so that if there is nothing active then the min will show as zero
	if l.activeConn > 0 {
		min = 100000
		// count min and max connections per remote host
		for _, c := range l.remotes {
			if c > max {
				max = c
			}
			if c < min {
				min = c
			}
		}
	}
	return map[string]float64{
		"number_of_remotes":  float64(len(l.remotes)),
		"total_connections":  float64(l.accepted),
		"active_connections": float64(l.activeConn),
		// useful to see if clients are balancing us well
		"max_connections_per_remote": float64(max),
		"min_connections_per_remote": float64(min),
	}
}

// trackConn adds or removes a connection from our tracking list.
func (l *trackedListener) trackConn(c *trackedConnection, add bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.remotes == nil {
		l.remotes = make(map[string]int)
	}
	// use the remote host name (probably an ip) excluding port
	host := (&url.URL{Host: c.RemoteAddr().String()}).Hostname()
	if add {
		l.accepted++
		l.activeConn++
		l.remotes[host]++
	} else {
		l.activeConn--
		l.remotes[host]--
		if l.remotes[host] == 0 {
			delete(l.remotes, host)
		}
	}
}

// trackedConnection embeds an anonymous net.Conn and overrides Close so we can update the
// trackedListener.
type trackedConnection struct {
	net.Conn

	l *trackedListener
}

// Close updates the trackedConnection and closes the underlying connection.
func (c *trackedConnection) Close() error {
	c.l.trackConn(c, false)
	return c.Conn.Close()
}

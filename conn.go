package grpcp

import (
	"context"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
)

type connState int

const (
	connecting connState = iota
	ready
	idle
	shutdown
)

var (
	errNoReady = fmt.Errorf("no ready")
)

type trackedConn struct {
	sync.RWMutex
	addr    string
	conn    *grpc.ClientConn
	tracker *ConnectionTracker
	state   connState
	expires time.Time
	retry   int
}

func (tc *trackedConn) tryconn() error {
	tc.Lock()
	defer tc.Unlock()
	if tc.conn != nil {
		if tc.state == ready {
			return nil
		}
		if tc.state == idle {
			return errNoReady
		}
	}
	conn, err := tc.tracker.dial(tc.addr)
	if err != nil {
		return err
	}
	if tc.conn != nil {
		tc.conn.Close()
	}
	tc.conn = conn

	ctx, cancel := context.WithTimeout(context.Background(), checkReadyTimeout)
	defer cancel()

	if ok := tc.isReady(ctx); !ok {
		return errNoReady
	}
	go tc.heartbeat(context.Background())
	return nil
}

func (tc *trackedConn) getState() connState {
	tc.RLock()
	defer tc.RUnlock()
	return tc.state
}

func (tc *trackedConn) healthCheck(ctx context.Context) {
	tc.Lock()
	defer tc.Unlock()
	ctx, cancel := context.WithTimeout(ctx, checkReadyTimeout)
	defer cancel()

	if ok := tc.isReady(ctx); !ok && tc.expired() {
		tc.shutdown()
	}
}

func (tc *trackedConn) isReady(ctx context.Context) bool {
	for {
		s := tc.conn.GetState()
		if s == connectivity.Ready {
			tc.ready()
			return true
		} else if s == connectivity.Shutdown {
			tc.shutdown()
			return false
		}
		if !tc.conn.WaitForStateChange(ctx, s) {
			tc.idle()
			return false
		}
	}
}

func (tc *trackedConn) ready() {
	tc.state = ready
	tc.expires = time.Now().Add(tc.tracker.timeout)
	tc.retry = 0
}

func (tc *trackedConn) idle() {
	tc.state = idle
	tc.retry++
	tc.tracker.connShutdown(tc.addr)
}

func (tc *trackedConn) shutdown() {
	tc.state = shutdown
	tc.conn.Close()
	tc.tracker.connShutdown(tc.addr)
}

func (tc *trackedConn) expired() bool {
	return tc.expires.Before(time.Now())
}

func (tc *trackedConn) heartbeat(ctx context.Context) {
	ticker := time.NewTicker(heartbeatInterval)
	for tc.getState() != shutdown {
		select {
		case <-ctx.Done():
			tc.shutdown()
			break
		case <-ticker.C:
			tc.healthCheck(ctx)
		}
	}
}

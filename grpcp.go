/*
 *
 * Created by 0x5010 on 2018/06/20.
 * grpcp
 * https://github.com/0x5010/grpcp
 *
 * Copyright 2018 0x5010.
 * Licensed under the MIT license.
 *
 */
package grpcp

import (
	"context"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
)

const (
	defaultTimeout    = 100 * time.Second
	checkReadyTimeout = 5 * time.Second
	heartbeatInterval = 20 * time.Second
)

// DialFunc dial function
type DialFunc func(addr string) (*grpc.ClientConn, error)

// ConnectionTracker keep connections and maintain their status
type ConnectionTracker struct {
	sync.RWMutex
	dial              DialFunc
	connections       map[string]*trackedConn
	alives            map[string]*trackedConn
	timeout           time.Duration
	checkReadyTimeout time.Duration
	heartbeatInterval time.Duration

	ctx    context.Context
	cannel context.CancelFunc
}

// TrackerOption initialization options
type TrackerOption func(*ConnectionTracker)

// SetTimeout custom timeout
func SetTimeout(timeout time.Duration) TrackerOption {
	return func(o *ConnectionTracker) {
		o.timeout = timeout
	}
}

// SetCheckReadyTimeout custom checkReadyTimeout
func SetCheckReadyTimeout(timeout time.Duration) TrackerOption {
	return func(o *ConnectionTracker) {
		o.checkReadyTimeout = timeout
	}
}

// SetHeartbeatInterval custom heartbeatInterval
func SetHeartbeatInterval(interval time.Duration) TrackerOption {
	return func(o *ConnectionTracker) {
		o.heartbeatInterval = interval
	}
}

// New initialization ConnectionTracker
func New(dial DialFunc, opts ...TrackerOption) *ConnectionTracker {
	ctx, cannel := context.WithCancel(context.Background())
	ct := &ConnectionTracker{
		dial:              dial,
		connections:       make(map[string]*trackedConn),
		alives:            make(map[string]*trackedConn),
		timeout:           defaultTimeout,
		checkReadyTimeout: checkReadyTimeout,
		heartbeatInterval: heartbeatInterval,

		ctx:    ctx,
		cannel: cannel,
	}

	for _, opt := range opts {
		opt(ct)
	}

	return ct
}

// GetConn create or get an existing connection
func (ct *ConnectionTracker) GetConn(addr string) (*grpc.ClientConn, error) {
	ct.Lock()
	tc, ok := ct.connections[addr]
	if !ok {
		tc = &trackedConn{
			addr:    addr,
			tracker: ct,
		}
		ct.connections[addr] = tc
	}
	ct.Unlock()

	err := tc.tryconn(ct.ctx)
	if err != nil {
		return nil, err
	}
	return tc.conn, nil
}

func (ct *ConnectionTracker) connReady(tc *trackedConn) {
	ct.Lock()
	defer ct.Unlock()
	ct.alives[tc.addr] = tc
}

func (ct *ConnectionTracker) connUnReady(addr string) {
	ct.Lock()
	defer ct.Unlock()
	delete(ct.alives, addr)
}

// Alives current live connections
func (ct *ConnectionTracker) Alives() []string {
	ct.RLock()
	defer ct.RUnlock()
	alives := []string{}
	for addr := range ct.alives {
		alives = append(alives, addr)
	}
	return alives
}

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
	cannel  context.CancelFunc
}

func (tc *trackedConn) tryconn(ctx context.Context) error {
	tc.Lock()
	defer tc.Unlock()
	if tc.conn != nil { // another goroutine got the write lock first
		if tc.state == ready {
			return nil
		}
		if tc.state == idle {
			return errNoReady
		}
	}

	if tc.conn != nil { // close shutdown conn
		tc.conn.Close()
	}
	conn, err := tc.tracker.dial(tc.addr)
	if err != nil {
		return err
	}
	tc.conn = conn

	readyCtx, cancel := context.WithTimeout(ctx, tc.tracker.checkReadyTimeout)
	defer cancel()

	if ok := tc.isReady(readyCtx); !ok {
		return errNoReady
	}

	hbCtx, cancel := context.WithCancel(ctx)
	tc.cannel = cancel
	go tc.heartbeat(hbCtx)
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
	ctx, cancel := context.WithTimeout(ctx, tc.tracker.checkReadyTimeout)
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
	tc.tracker.connReady(tc)
}

func (tc *trackedConn) idle() {
	tc.state = idle
	tc.retry++
	tc.tracker.connUnReady(tc.addr)
}

func (tc *trackedConn) shutdown() {
	tc.state = shutdown
	tc.conn.Close()
	tc.cannel()
	tc.tracker.connUnReady(tc.addr)
}

func (tc *trackedConn) expired() bool {
	return tc.expires.Before(time.Now())
}

func (tc *trackedConn) heartbeat(ctx context.Context) {
	ticker := time.NewTicker(tc.tracker.heartbeatInterval)
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

var (
	// Pool default pool
	Pool  *ConnectionTracker
	dialF = func(addr string) (*grpc.ClientConn, error) {
		return grpc.Dial(
			addr,
			grpc.WithInsecure(),
		)
	}
)

func init() {
	Pool = New(dialF)
}

// GetConn create or get an existing connection from default pool
func GetConn(addr string) (*grpc.ClientConn, error) {
	return Pool.GetConn(addr)
}

// Alives current live connections from default pool
func Alives() []string {
	return Pool.Alives()
}

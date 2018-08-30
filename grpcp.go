package grpcp

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

var (
	errNoReady = fmt.Errorf("no ready")
)

// DialFunc dial function
type DialFunc func(addr string) (*grpc.ClientConn, error)

// ReadyCheckFunc check conn is ready function
type ReadyCheckFunc func(ctx context.Context, conn *grpc.ClientConn) connectivity.State

// ConnectionTracker keep connections and maintain their status
type ConnectionTracker struct {
	sync.RWMutex
	dial              DialFunc
	readyCheck        ReadyCheckFunc
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

// CustomReadyCheck custom ready check function
func CustomReadyCheck(f ReadyCheckFunc) TrackerOption {
	return func(o *ConnectionTracker) {
		o.readyCheck = f
	}
}

// New initialization ConnectionTracker
func New(dial DialFunc, opts ...TrackerOption) *ConnectionTracker {
	ctx, cannel := context.WithCancel(context.Background())
	ct := &ConnectionTracker{
		dial:              dial,
		readyCheck:        defaultReadyCheck,
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
	return ct.getConn(addr, false)
}

// Dial force to create new connection, this operation will close old connection!
func (ct *ConnectionTracker) Dial(addr string) (*grpc.ClientConn, error) {
	return ct.getConn(addr, true)
}

func (ct *ConnectionTracker) getConn(addr string, force bool) (*grpc.ClientConn, error) {
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

	err := tc.tryconn(ct.ctx, force)
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

type trackedConn struct {
	sync.RWMutex
	addr    string
	conn    *grpc.ClientConn
	tracker *ConnectionTracker
	state   connectivity.State
	expires time.Time
	retry   int
	cannel  context.CancelFunc
}

func (tc *trackedConn) tryconn(ctx context.Context, force bool) error {
	tc.Lock()
	defer tc.Unlock()
	if !force && tc.conn != nil { // another goroutine got the write lock first
		if tc.state == connectivity.Ready {
			return nil
		}
		if tc.state == connectivity.Idle {
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

	checkStatus := tc.tracker.readyCheck(readyCtx, tc.conn)
	hbCtx, hbCancel := context.WithCancel(ctx)
	tc.cannel = hbCancel
	go tc.heartbeat(hbCtx)

	if checkStatus != connectivity.Ready {
		return errNoReady
	}
	tc.ready()
	return nil
}

func (tc *trackedConn) getState() connectivity.State {
	tc.RLock()
	defer tc.RUnlock()
	return tc.state
}

func (tc *trackedConn) healthCheck(ctx context.Context) {
	tc.Lock()
	defer tc.Unlock()
	ctx, cancel := context.WithTimeout(ctx, tc.tracker.checkReadyTimeout)
	defer cancel()

	switch tc.tracker.readyCheck(ctx, tc.conn) {
	case connectivity.Ready:
		tc.ready()
	case connectivity.Shutdown:
		tc.shutdown()
	case connectivity.Idle:
		if tc.expired() {
			tc.shutdown()
		} else {
			tc.idle()
		}
	}
}

func defaultReadyCheck(ctx context.Context, conn *grpc.ClientConn) connectivity.State {
	for {
		s := conn.GetState()
		if s == connectivity.Ready || s == connectivity.Shutdown {
			return s
		}
		if !conn.WaitForStateChange(ctx, s) {
			return connectivity.Idle
		}
	}
}

func (tc *trackedConn) ready() {
	tc.state = connectivity.Ready
	tc.expires = time.Now().Add(tc.tracker.timeout)
	tc.retry = 0
	tc.tracker.connReady(tc)
}

func (tc *trackedConn) idle() {
	tc.state = connectivity.Idle
	tc.retry++
	tc.tracker.connUnReady(tc.addr)
}

func (tc *trackedConn) shutdown() {
	tc.state = connectivity.Shutdown
	tc.conn.Close()
	tc.cannel()
	tc.tracker.connUnReady(tc.addr)
}

func (tc *trackedConn) expired() bool {
	return tc.expires.Before(time.Now())
}

func (tc *trackedConn) heartbeat(ctx context.Context) {
	ticker := time.NewTicker(tc.tracker.heartbeatInterval)
	for tc.getState() != connectivity.Shutdown {
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
	// defaultPool default pool
	defaultPool *ConnectionTracker
	once        sync.Once
	dialF       = func(addr string) (*grpc.ClientConn, error) {
		return grpc.Dial(
			addr,
			grpc.WithInsecure(),
		)
	}
)

func pool() *ConnectionTracker {
	once.Do(func() {
		defaultPool = New(dialF)
	})
	return defaultPool
}

// GetConn create or get an existing connection from default pool
func GetConn(addr string) (*grpc.ClientConn, error) {
	return pool().GetConn(addr)
}

// Dial force to create new connection from default pool, this operation will close old connection!
func Dial(addr string) (*grpc.ClientConn, error) {
	return pool().Dial(addr)
}

// Alives current live connections from default pool
func Alives() []string {
	return pool().Alives()
}

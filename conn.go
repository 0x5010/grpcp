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

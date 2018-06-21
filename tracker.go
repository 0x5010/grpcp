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
	"sync"
	"time"

	"google.golang.org/grpc"
)

const (
	defaultTimeout    = 100 * time.Second
	checkReadyTimeout = 5 * time.Second
	heartbeatInterval = 20 * time.Second
)

// DialFunc 建联方式
type DialFunc func(addr string) (*grpc.ClientConn, error)

// ConnectionTracker 连接池
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

// TrackerOption 选项
type TrackerOption func(*ConnectionTracker)

// SetTimeout 自定义超时
func SetTimeout(timeout time.Duration) TrackerOption {
	return func(o *ConnectionTracker) {
		o.timeout = timeout
	}
}

// SetCheckReadyTimeout 自定义检测超时时间
func SetCheckReadyTimeout(timeout time.Duration) TrackerOption {
	return func(o *ConnectionTracker) {
		o.checkReadyTimeout = timeout
	}
}

// SetHeartbeatInterval 自定义心跳间隔
func SetHeartbeatInterval(interval time.Duration) TrackerOption {
	return func(o *ConnectionTracker) {
		o.heartbeatInterval = interval
	}
}

// New 初始化连接池
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

// GetConn 创建或获取已有连接
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

// Alives 当前存活连接
func (ct *ConnectionTracker) Alives() []string {
	ct.RLock()
	defer ct.RUnlock()
	alives := []string{}
	for addr := range ct.alives {
		alives = append(alives, addr)
	}
	return alives
}

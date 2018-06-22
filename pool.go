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
	"google.golang.org/grpc"
)

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

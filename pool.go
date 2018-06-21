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
	// Pool 默认连接池
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

// GetConn 创建或获取已有连接
func GetConn(addr string) (*grpc.ClientConn, error) {
	return Pool.GetConn(addr)
}

// Alives 当前存活连接
func Alives() []string {
	return Pool.Alives()
}

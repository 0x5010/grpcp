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

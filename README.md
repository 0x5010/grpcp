grpcp is a Grpc Persistent Connection Pool.

[![LICENSE](https://img.shields.io/badge/license-MIT-orange.svg)](LICENSE)
[![Build Status](https://travis-ci.org/0x5010/grpcp.png?branch=master)](https://travis-ci.org/0x5010/grpcp)
[![codecov](https://codecov.io/gh/0x5010/grpcp/branch/master/graph/badge.svg)](https://codecov.io/gh/0x5010/grpcp/)
[![Go Report Card](https://goreportcard.com/badge/github.com/0x5010/grpcp)](https://goreportcard.com/report/github.com/0x5010/grpcp)
[![Godoc](http://img.shields.io/badge/go-documentation-blue.svg?style=flat-square)](https://godoc.org/github.com/0x5010/grpcp)

Installation
-----------

```bash
go get -u github.com/0x5010/grpcp
```

Usage
-----------

default

```go
import (
    "context"
    "fmt"

    "google.golang.org/grpc"
    pb "google.golang.org/grpc/examples/helloworld/helloworld"
)

func main() {
    var addr, name string
    conn, _ := grpc.Dial(addr, grpc.WithInsecure())
    defer conn.Close()
    client := pb.NewGreeterClient(conn)
    r, _ := client.SayHello(context.Background(), &pb.HelloRequest{Name: name})
    fmt.Println(r.GetMessage())
}
```

with grpcp

```go
import (
    "context"
    "fmt"

    "google.golang.org/grpc"
    pb "google.golang.org/grpc/examples/helloworld/helloworld"
)

func main() {
    var addr, name string

    conn, _ := grpcp.GetConn(addr)  // get conn with grpcp default pool
    // defer conn.Close()  // no close, close will disconnect
    client := pb.NewGreeterClient(conn)
    r, _ := client.SayHello(context.Background(), &pb.HelloRequest{Name: name})
    fmt.Println(r.GetMessage())
}
```

custom dial function

```go
import (
    "context"
    "fmt"

    "github.com/0x5010/grpcp"
    "google.golang.org/grpc"
    pb "google.golang.org/grpc/examples/helloworld/helloworld"
)

func main() {
    var addr, name string

    pool := grpcp.New(func(addr string) (*grpc.ClientConn, error) {
        return grpc.Dial(
            addr,
            grpc.WithInsecure(),
        )
    })
    conn, _ := pool.GetConn(addr)

    client := pb.NewGreeterClient(conn)
    r, _ := client.SayHello(context.Background(), &pb.HelloRequest{Name: name})
    fmt.Println(r.GetMessage())
}
```

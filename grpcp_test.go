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
	"net"
	"sort"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	pb "google.golang.org/grpc/examples/helloworld/helloworld"
	"google.golang.org/grpc/reflection"
)

func TestNewWithOption(t *testing.T) {
	type want struct {
		timeout           time.Duration
		checkReadyTimeout time.Duration
		heartbeatInterval time.Duration
		readyCheckFunc    ReadyCheckFunc
	}

	td := 123 * time.Second

	tests := []struct {
		name string
		args []TrackerOption
		want want
	}{
		{
			name: "default",
			args: []TrackerOption{},
			want: want{
				timeout:           defaultTimeout,
				checkReadyTimeout: checkReadyTimeout,
				heartbeatInterval: heartbeatInterval,
				readyCheckFunc:    defaultReadyCheck,
			},
		},
		{
			name: "SetTimeout",
			args: []TrackerOption{SetTimeout(td)},
			want: want{
				timeout:           td,
				checkReadyTimeout: checkReadyTimeout,
				heartbeatInterval: heartbeatInterval,
				readyCheckFunc:    defaultReadyCheck,
			},
		},
		{
			name: "SetCheckReadyTimeout",
			args: []TrackerOption{SetCheckReadyTimeout(td)},
			want: want{
				timeout:           defaultTimeout,
				checkReadyTimeout: td,
				heartbeatInterval: heartbeatInterval,
				readyCheckFunc:    defaultReadyCheck,
			},
		},
		{
			name: "SetHeartbeatInterval",
			args: []TrackerOption{SetHeartbeatInterval(td)},
			want: want{
				timeout:           defaultTimeout,
				checkReadyTimeout: checkReadyTimeout,
				heartbeatInterval: td,
				readyCheckFunc:    defaultReadyCheck,
			},
		},
		{
			name: "CustomReadyCheck",
			args: []TrackerOption{CustomReadyCheck(myReadyCheck)},
			want: want{
				timeout:           defaultTimeout,
				checkReadyTimeout: checkReadyTimeout,
				heartbeatInterval: heartbeatInterval,
				readyCheckFunc:    myReadyCheck,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := New(dialF, tt.args...)
			if tc.timeout != tt.want.timeout {
				t.Errorf("tracker timeout is %d, expected %d.", tc.timeout, tt.want.timeout)
			}
			if tc.checkReadyTimeout != tt.want.checkReadyTimeout {
				t.Errorf("tracker checkReadyTimeout is %d, expected %d.", tc.timeout, tt.want.checkReadyTimeout)
			}
			if tc.heartbeatInterval != tt.want.heartbeatInterval {
				t.Errorf("tracker heartbeatInterval is %d, expected %d.", tc.timeout, tt.want.heartbeatInterval)
			}
			if fmt.Sprintf("%v", tc.readyCheck) != fmt.Sprintf("%v", tt.want.readyCheckFunc) {
				t.Errorf("tracker readyCheckFunc is %d, expected %d.", tc.timeout, tt.want.heartbeatInterval)
			}
		})
	}
}

type server struct{}

func (s *server) SayHello(ctx context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	return &pb.HelloReply{Message: "Hello " + in.Name}, nil
}

func startHWServer(t *testing.T) (*grpc.Server, string) {
	lis, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterGreeterServer(s, &server{})
	reflection.Register(s)

	go func() {
		if err := s.Serve(lis); err != nil {
			t.Fatalf("failed to serve: %v", err)
		}
	}()

	return s, lis.Addr().String()
}

func TestHWServer(t *testing.T) {
	s, addr := startHWServer(t)
	defer s.GracefulStop()

	conn, err := dialF(addr)
	if err != nil {
		t.Fatal(err)
	}
	testHelloworld(t, conn)
}

func TestHWServerWithGrpcp(t *testing.T) {
	s, addr := startHWServer(t)
	defer s.GracefulStop()

	conn, err := GetConn(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	testHelloworld(t, conn)
	alives := Alives()
	testAlives(t, alives, []string{addr})
}

func TestHWServerWithCustomReadyCheck(t *testing.T) {
	s, addr := startHWServer(t)
	defer s.GracefulStop()

	pool := New(dialF, CustomReadyCheck(myReadyCheck))
	conn, err := pool.GetConn(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	testHelloworld(t, conn)
	alives := pool.Alives()
	testAlives(t, alives, []string{addr})
}

func TestConnErrAddr(t *testing.T) {
	s, addr := startHWServer(t)
	defer s.GracefulStop()
	dialF := func(addr string) (*grpc.ClientConn, error) {
		return grpc.Dial(
			addr,
			grpc.WithInsecure(),
			grpc.WithBlock(),
			grpc.WithTimeout(1*time.Second),
		)
	}
	pool := New(dialF, SetCheckReadyTimeout(1*time.Second))

	conn, err := pool.GetConn(addr)
	if err != nil {
		t.Fatal(err)
	}
	testHelloworld(t, conn)
	errAddr := "xxxx"

	_, err = pool.GetConn(errAddr)
	if err == nil {
		t.Fatal("conn err addr no raise error")
	}

	alives := pool.Alives()
	testAlives(t, alives, []string{addr})
}

func TestStopServer(t *testing.T) {
	s, addr := startHWServer(t)

	pool := New(
		dialF,
		SetTimeout(2*time.Second),
		SetCheckReadyTimeout(1*time.Second),
		SetHeartbeatInterval(1*time.Second),
	)

	conn, err := pool.GetConn(addr)
	if err != nil {
		t.Fatal(err)
	}
	testHelloworld(t, conn)
	alives := pool.Alives()
	testAlives(t, alives, []string{addr})
	time.Sleep(5 * time.Second)
	conn.Close()
	s.Stop()
	time.Sleep(10 * time.Second)

	alives = pool.Alives()
	testAlives(t, alives, []string{})

	_, err = pool.GetConn(addr)
	if err == nil {
		t.Fatal("conn err addr no raise error")
	}
}

func TestClosePool(t *testing.T) {
	s, addr := startHWServer(t)
	defer s.GracefulStop()

	pool := New(
		dialF,
		SetTimeout(2*time.Second),
		SetCheckReadyTimeout(1*time.Second),
		SetHeartbeatInterval(1*time.Second),
	)

	conn, err := pool.GetConn(addr)
	if err != nil {
		t.Fatal(err)
	}
	testHelloworld(t, conn)
	alives := pool.Alives()
	testAlives(t, alives, []string{addr})

	pool.cannel()
	time.Sleep(2 * time.Second)
	alives = pool.Alives()
	testAlives(t, alives, []string{})
}

func TestRaceGetConn(t *testing.T) {
	s, addr := startHWServer(t)
	defer s.GracefulStop()

	pool := New(dialF)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			_, err := pool.GetConn(addr)
			if err != nil {
				t.Fatal(err)
			}
			wg.Done()
		}()
	}
	wg.Wait()
}

func TestConnIdle(t *testing.T) {
	s, addr := startHWServer(t)
	defer s.GracefulStop()

	pool := New(dialF)

	_, err := pool.GetConn(addr)
	if err != nil {
		t.Fatal(err)
	}
	tc := pool.connections[addr]
	tc.cannel()
	tc.idle()
	_, err = pool.GetConn(addr)
	if err != errNoReady {
		t.Fatalf("GetConn when state idle, raise %v, want errNoReady.", err)
	}
}

func TestConnExpired(t *testing.T) {
	s, addr := startHWServer(t)
	defer s.GracefulStop()

	first := true
	mockCheckFunc := func(ctx context.Context, conn *grpc.ClientConn) connectivity.State {
		if first == true {
			first = false
			return connectivity.Ready
		}
		return connectivity.Idle
	}

	pool := New(
		dialF,
		SetTimeout(2*time.Second),
		SetCheckReadyTimeout(1*time.Second),
		SetHeartbeatInterval(1*time.Second),
		CustomReadyCheck(mockCheckFunc),
	)

	conn, err := pool.GetConn(addr)
	if err != nil {
		t.Fatal(err)
	}
	testHelloworld(t, conn)
	alives := pool.Alives()
	testAlives(t, alives, []string{addr})

	time.Sleep(5 * time.Second)

	// conn no ready, expired.
	alives = pool.Alives()
	testAlives(t, alives, []string{})

}

func testHelloworld(t *testing.T, conn *grpc.ClientConn) bool {
	name := "test"
	client := pb.NewGreeterClient(conn)
	r, err := client.SayHello(context.Background(), &pb.HelloRequest{Name: name})
	if err != nil {
		t.Fatal(err)
		return false
	}
	expected := fmt.Sprintf("Hello %s", name)
	if r.Message != expected {
		t.Fatalf("server reply is \"%s\", expected \"%s\".", r.Message, expected)
		return false
	}
	return true
}

func testAlives(t *testing.T, alives, expected []string) {
	if len(alives) != len(expected) {
		t.Fatalf("Alives() does not contain %d addr. got=%d, %v", len(expected), len(alives), alives)
	}
	sort.Strings(alives)
	sort.Strings(expected)
	for i, addr := range expected {
		if alives[i] != addr {
			t.Fatalf("alives addr wrong value. got=%s, want=%s", alives[0], addr)
		}
	}
}

func myReadyCheck(ctx context.Context, conn *grpc.ClientConn) connectivity.State {
	name := "test"
	client := pb.NewGreeterClient(conn)
	_, err := client.SayHello(context.Background(), &pb.HelloRequest{Name: name})
	if err != nil {
		return connectivity.Idle
	}
	return connectivity.Ready
}

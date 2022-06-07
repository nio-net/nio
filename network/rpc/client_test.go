package rpc

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/neatlab/neatio/chain/log"
)

func TestClientRequest(t *testing.T) {
	server := newTestServer()
	defer server.Stop()
	client := DialInProc(server)
	defer client.Close()

	var resp Result
	if err := client.Call(&resp, "test_echo", "hello", 10, &Args{"world"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resp, Result{"hello", 10, &Args{"world"}}) {
		t.Errorf("incorrect result %#v", resp)
	}
}

func TestClientErrorData(t *testing.T) {
	server := newTestServer()
	defer server.Stop()
	client := DialInProc(server)
	defer client.Close()

	var resp interface{}
	err := client.Call(&resp, "test_returnError")
	if err == nil {
		t.Fatal("expected error")
	}

	if e, ok := err.(Error); !ok {
		t.Fatalf("client did not return rpc.Error, got %#v", e)
	} else if e.ErrorCode() != (testError{}.ErrorCode()) {
		t.Fatalf("wrong error code %d, want %d", e.ErrorCode(), testError{}.ErrorCode())
	}
	if e, ok := err.(DataError); !ok {
		t.Fatalf("client did not return rpc.DataError, got %#v", e)
	} else if e.ErrorData() != (testError{}.ErrorData()) {
		t.Fatalf("wrong error data %#v, want %#v", e.ErrorData(), testError{}.ErrorData())
	}
}

func TestClientBatchRequest(t *testing.T) {
	server := newTestServer()
	defer server.Stop()
	client := DialInProc(server)
	defer client.Close()

	batch := []BatchElem{
		{
			Method: "test_echo",
			Args:   []interface{}{"hello", 10, &Args{"world"}},
			Result: new(Result),
		},
		{
			Method: "test_echo",
			Args:   []interface{}{"hello2", 11, &Args{"world"}},
			Result: new(Result),
		},
		{
			Method: "no_such_method",
			Args:   []interface{}{1, 2, 3},
			Result: new(int),
		},
	}
	if err := client.BatchCall(batch); err != nil {
		t.Fatal(err)
	}
	wantResult := []BatchElem{
		{
			Method: "test_echo",
			Args:   []interface{}{"hello", 10, &Args{"world"}},
			Result: &Result{"hello", 10, &Args{"world"}},
		},
		{
			Method: "test_echo",
			Args:   []interface{}{"hello2", 11, &Args{"world"}},
			Result: &Result{"hello2", 11, &Args{"world"}},
		},
		{
			Method: "no_such_method",
			Args:   []interface{}{1, 2, 3},
			Result: new(int),
			Error:  &jsonError{Code: -32601, Message: "the method no_such_method does not exist/is not available"},
		},
	}
	if !reflect.DeepEqual(batch, wantResult) {
		t.Errorf("batch results mismatch:\ngot %swant %s", spew.Sdump(batch), spew.Sdump(wantResult))
	}
}

func TestClientNotify(t *testing.T) {
	server := newTestServer()
	defer server.Stop()
	client := DialInProc(server)
	defer client.Close()

	if err := client.Notify(context.Background(), "test_echo", "hello", 10, &Args{"world"}); err != nil {
		t.Fatal(err)
	}
}

func TestClientCancelWebsocket(t *testing.T) { testClientCancel("ws", t) }
func TestClientCancelHTTP(t *testing.T)      { testClientCancel("http", t) }
func TestClientCancelIPC(t *testing.T)       { testClientCancel("ipc", t) }

func testClientCancel(transport string, t *testing.T) {
	t.Parallel()

	server := newTestServer()
	defer server.Stop()

	maxContextCancelTimeout := 300 * time.Millisecond
	fl := &flakeyListener{
		maxAcceptDelay: 1 * time.Second,
		maxKillTimeout: 600 * time.Millisecond,
	}

	var client *Client
	switch transport {
	case "ws", "http":
		c, hs := httpTestClient(server, transport, fl)
		defer hs.Close()
		client = c
	case "ipc":
		c, l := ipcTestClient(server, fl)
		defer l.Close()
		client = c
	default:
		panic("unknown transport: " + transport)
	}

	var (
		wg       sync.WaitGroup
		nreqs    = 10
		ncallers = 6
	)
	caller := func(index int) {
		defer wg.Done()
		for i := 0; i < nreqs; i++ {
			var (
				ctx     context.Context
				cancel  func()
				timeout = time.Duration(rand.Int63n(int64(maxContextCancelTimeout)))
			)
			if index < ncallers/2 {
				ctx, cancel = context.WithCancel(context.Background())
				time.AfterFunc(timeout, cancel)
			} else {
				ctx, cancel = context.WithTimeout(context.Background(), timeout)
			}
			sleepTime := maxContextCancelTimeout + 20*time.Millisecond
			err := client.CallContext(ctx, nil, "test_sleep", sleepTime)
			if err != nil {
				log.Debug(fmt.Sprint("got expected error:", err))
			} else {
				t.Errorf("no error for call with %v wait time", timeout)
			}
			cancel()
		}
	}
	wg.Add(ncallers)
	for i := 0; i < ncallers; i++ {
		go caller(i)
	}
	wg.Wait()
}

func TestClientSubscribeInvalidArg(t *testing.T) {
	server := newTestServer()
	defer server.Stop()
	client := DialInProc(server)
	defer client.Close()

	check := func(shouldPanic bool, arg interface{}) {
		defer func() {
			err := recover()
			if shouldPanic && err == nil {
				t.Errorf("EthSubscribe should've panicked for %#v", arg)
			}
			if !shouldPanic && err != nil {
				t.Errorf("EthSubscribe shouldn't have panicked for %#v", arg)
				buf := make([]byte, 1024*1024)
				buf = buf[:runtime.Stack(buf, false)]
				t.Error(err)
				t.Error(string(buf))
			}
		}()
		client.EthSubscribe(context.Background(), arg, "foo_bar")
	}
	check(true, nil)
	check(true, 1)
	check(true, (chan int)(nil))
	check(true, make(<-chan int))
	check(false, make(chan int))
	check(false, make(chan<- int))
}

func TestClientSubscribe(t *testing.T) {
	server := newTestServer()
	defer server.Stop()
	client := DialInProc(server)
	defer client.Close()

	nc := make(chan int)
	count := 10
	sub, err := client.Subscribe(context.Background(), "nftest", nc, "someSubscription", count, 0)
	if err != nil {
		t.Fatal("can't subscribe:", err)
	}
	for i := 0; i < count; i++ {
		if val := <-nc; val != i {
			t.Fatalf("value mismatch: got %d, want %d", val, i)
		}
	}

	sub.Unsubscribe()
	select {
	case v := <-nc:
		t.Fatal("received value after unsubscribe:", v)
	case err := <-sub.Err():
		if err != nil {
			t.Fatalf("Err returned a non-nil error after explicit unsubscribe: %q", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("subscription not closed within 1s after unsubscribe")
	}
}

func TestClientSubscribeClose(t *testing.T) {
	server := newTestServer()
	service := &notificationTestService{
		gotHangSubscriptionReq:  make(chan struct{}),
		unblockHangSubscription: make(chan struct{}),
	}
	if err := server.RegisterName("nftest2", service); err != nil {
		t.Fatal(err)
	}

	defer server.Stop()
	client := DialInProc(server)
	defer client.Close()

	var (
		nc   = make(chan int)
		errc = make(chan error)
		sub  *ClientSubscription
		err  error
	)
	go func() {
		sub, err = client.Subscribe(context.Background(), "nftest2", nc, "hangSubscription", 999)
		errc <- err
	}()

	<-service.gotHangSubscriptionReq
	client.Close()
	service.unblockHangSubscription <- struct{}{}

	select {
	case err := <-errc:
		if err == nil {
			t.Errorf("Subscribe returned nil error after Close")
		}
		if sub != nil {
			t.Error("Subscribe returned non-nil subscription after Close")
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("Subscribe did not return within 1s after Close")
	}
}

func TestClientCloseUnsubscribeRace(t *testing.T) {
	server := newTestServer()
	defer server.Stop()

	for i := 0; i < 20; i++ {
		client := DialInProc(server)
		nc := make(chan int)
		sub, err := client.Subscribe(context.Background(), "nftest", nc, "someSubscription", 3, 1)
		if err != nil {
			t.Fatal(err)
		}
		go client.Close()
		go sub.Unsubscribe()
		select {
		case <-sub.Err():
		case <-time.After(5 * time.Second):
			t.Fatal("subscription not closed within timeout")
		}
	}
}

func TestClientNotificationStorm(t *testing.T) {
	server := newTestServer()
	defer server.Stop()

	doTest := func(count int, wantError bool) {
		client := DialInProc(server)
		defer client.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		nc := make(chan int)
		sub, err := client.Subscribe(ctx, "nftest", nc, "someSubscription", count, 0)
		if err != nil {
			t.Fatal("can't subscribe:", err)
		}
		defer sub.Unsubscribe()

		for i := 0; i < count; i++ {
			select {
			case val := <-nc:
				if val != i {
					t.Fatalf("(%d/%d) unexpected value %d", i, count, val)
				}
			case err := <-sub.Err():
				if wantError && err != ErrSubscriptionQueueOverflow {
					t.Fatalf("(%d/%d) got error %q, want %q", i, count, err, ErrSubscriptionQueueOverflow)
				} else if !wantError {
					t.Fatalf("(%d/%d) got unexpected error %q", i, count, err)
				}
				return
			}
			var r int
			err := client.CallContext(ctx, &r, "nftest_echo", i)
			if err != nil {
				if !wantError {
					t.Fatalf("(%d/%d) call error: %v", i, count, err)
				}
				return
			}
		}
	}

	doTest(8000, false)
	doTest(10000, true)
}

func TestClientHTTP(t *testing.T) {
	server := newTestServer()
	defer server.Stop()

	client, hs := httpTestClient(server, "http", nil)
	defer hs.Close()
	defer client.Close()

	var (
		results    = make([]Result, 100)
		errc       = make(chan error)
		wantResult = Result{"a", 1, new(Args)}
	)
	defer client.Close()
	for i := range results {
		i := i
		go func() {
			errc <- client.Call(&results[i], "test_echo",
				wantResult.String, wantResult.Int, wantResult.Args)
		}()
	}

	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for i := range results {
		select {
		case err := <-errc:
			if err != nil {
				t.Fatal(err)
			}
		case <-timeout.C:
			t.Fatalf("timeout (got %d/%d) results)", i+1, len(results))
		}
	}

	for i := range results {
		if !reflect.DeepEqual(results[i], wantResult) {
			t.Errorf("result %d mismatch: got %#v, want %#v", i, results[i], wantResult)
		}
	}
}

func TestClientReconnect(t *testing.T) {
	startServer := func(addr string) (*Server, net.Listener) {
		srv := newTestServer()
		l, err := net.Listen("tcp", addr)
		if err != nil {
			t.Fatal("can't listen:", err)
		}
		go http.Serve(l, srv.WebsocketHandler([]string{"*"}))
		return srv, l
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	s1, l1 := startServer("127.0.0.1:0")
	client, err := DialContext(ctx, "ws://"+l1.Addr().String())
	if err != nil {
		t.Fatal("can't dial", err)
	}

	var resp Result
	if err := client.CallContext(ctx, &resp, "test_echo", "", 1, nil); err != nil {
		t.Fatal(err)
	}

	l1.Close()
	s1.Stop()
	time.Sleep(2 * time.Second)

	if err := client.CallContext(ctx, &resp, "test_echo", "", 2, nil); err == nil {
		t.Error("successful call while the server is down")
		t.Logf("resp: %#v", resp)
	}

	s2, l2 := startServer(l1.Addr().String())
	defer l2.Close()
	defer s2.Stop()

	start := make(chan struct{})
	errors := make(chan error, 20)
	for i := 0; i < cap(errors); i++ {
		go func() {
			<-start
			var resp Result
			errors <- client.CallContext(ctx, &resp, "test_echo", "", 3, nil)
		}()
	}
	close(start)
	errcount := 0
	for i := 0; i < cap(errors); i++ {
		if err = <-errors; err != nil {
			errcount++
		}
	}
	t.Logf("%d errors, last error: %v", errcount, err)
	if errcount > 1 {
		t.Errorf("expected one error after disconnect, got %d", errcount)
	}
}

func httpTestClient(srv *Server, transport string, fl *flakeyListener) (*Client, *httptest.Server) {
	var hs *httptest.Server
	switch transport {
	case "ws":
		hs = httptest.NewUnstartedServer(srv.WebsocketHandler([]string{"*"}))
	case "http":
		hs = httptest.NewUnstartedServer(srv)
	default:
		panic("unknown HTTP transport: " + transport)
	}
	if fl != nil {
		fl.Listener = hs.Listener
		hs.Listener = fl
	}
	hs.Start()
	client, err := Dial(transport + "://" + hs.Listener.Addr().String())
	if err != nil {
		panic(err)
	}
	return client, hs
}

func ipcTestClient(srv *Server, fl *flakeyListener) (*Client, net.Listener) {
	endpoint := fmt.Sprintf("go-ethereum-test-ipc-%d-%d", os.Getpid(), rand.Int63())
	if runtime.GOOS == "windows" {
		endpoint = `\\.\pipe\` + endpoint
	} else {
		endpoint = os.TempDir() + "/" + endpoint
	}
	l, err := ipcListen(endpoint)
	if err != nil {
		panic(err)
	}
	if fl != nil {
		fl.Listener = l
		l = fl
	}
	go srv.ServeListener(l)
	client, err := Dial(endpoint)
	if err != nil {
		panic(err)
	}
	return client, l
}

type flakeyListener struct {
	net.Listener
	maxKillTimeout time.Duration
	maxAcceptDelay time.Duration
}

func (l *flakeyListener) Accept() (net.Conn, error) {
	delay := time.Duration(rand.Int63n(int64(l.maxAcceptDelay)))
	time.Sleep(delay)

	c, err := l.Listener.Accept()
	if err == nil {
		timeout := time.Duration(rand.Int63n(int64(l.maxKillTimeout)))
		time.AfterFunc(timeout, func() {
			log.Debug(fmt.Sprintf("killing conn %v after %v", c.LocalAddr(), timeout))
			c.Close()
		})
	}
	return c, err
}

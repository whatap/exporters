package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRunHTTPServerWaitsForInflightRequest(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		_, _ = io.WriteString(w, "complete\n")
	})}
	ctx, cancel := context.WithCancel(context.Background())
	serveReturned := make(chan struct{})
	runDone := make(chan struct{})
	var runErr error
	requestDone := make(chan struct{})
	var requestErr error
	var status int
	var body []byte
	transport := &http.Transport{DisableKeepAlives: true}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	t.Cleanup(func() {
		unblock()
		cancel()
		_ = server.Close()
		_ = listener.Close()
		transport.CloseIdleConnections()
		waitForHTTPEvent(t, runDone, "server runner cleanup")
		waitForHTTPEvent(t, requestDone, "HTTP client cleanup")
	})
	go func() {
		runErr = runHTTPServer(ctx, server, func() error {
			defer close(serveReturned)
			return server.Serve(listener)
		}, 3*time.Second)
		close(runDone)
	}()
	go func() {
		defer close(requestDone)
		response, err := client.Get("http://" + listener.Addr().String() + "/metrics")
		if err != nil {
			requestErr = err
			return
		}
		defer response.Body.Close()
		status = response.StatusCode
		body, requestErr = io.ReadAll(response.Body)
	}()
	waitForHTTPEvent(t, started, "in-flight handler")
	cancel()
	waitForHTTPEvent(t, serveReturned, "listener shutdown")
	select {
	case <-runDone:
		t.Fatal("server lifecycle returned before the in-flight handler finished")
	case <-time.After(100 * time.Millisecond):
	}
	unblock()
	waitForHTTPEvent(t, requestDone, "completed HTTP response")
	waitForHTTPEvent(t, runDone, "graceful shutdown completion")
	if !errors.Is(runErr, http.ErrServerClosed) {
		t.Fatalf("server returned %v, want http.ErrServerClosed", runErr)
	}
	if requestErr != nil || status != http.StatusOK || strings.TrimSpace(string(body)) != "complete" {
		t.Fatalf("in-flight response was interrupted: status=%d body=%q error=%v", status, body, requestErr)
	}
}

func TestRunHTTPServerWaitsForForcedCloseAfterTimeout(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closeStarted := make(chan struct{})
	allowClose := make(chan struct{})
	closeFinished := make(chan struct{})
	gatedListener := &closeGatedListener{
		Listener: listener, closeStarted: closeStarted, allowClose: allowClose, closeFinished: closeFinished,
	}
	var releaseOnce sync.Once
	releaseClose := func() { releaseOnce.Do(func() { close(allowClose) }) }
	started := make(chan struct{})
	handlerDone := make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(handlerDone)
	})}
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	var runErr error
	t.Cleanup(func() {
		releaseClose()
		cancel()
		_ = server.Close()
		_ = listener.Close()
		waitForHTTPEvent(t, runDone, "server runner cleanup")
	})
	go func() {
		runErr = runHTTPServer(ctx, server, func() error {
			return server.Serve(gatedListener)
		}, 50*time.Millisecond)
		close(runDone)
	}()
	connection, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	if err := connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(connection, "GET /metrics HTTP/1.1\r\nHost: localhost\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	waitForHTTPEvent(t, started, "in-flight handler")
	cancel()
	select {
	case <-closeStarted:
	case <-runDone:
		t.Fatal("server lifecycle returned before timeout forced the active connection closed")
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown timeout did not force the active connection closed")
	}
	select {
	case <-runDone:
		t.Fatal("server lifecycle returned before forced Close finished")
	case <-time.After(100 * time.Millisecond):
	}
	select {
	case <-handlerDone:
		t.Fatal("handler ended before the blocked connection Close was released")
	default:
	}
	releaseClose()
	waitForHTTPEvent(t, closeFinished, "forced connection Close completion")
	waitForHTTPEvent(t, runDone, "forced server Close completion")
	waitForHTTPEvent(t, handlerDone, "request context cancellation")
	if !errors.Is(runErr, http.ErrServerClosed) {
		t.Fatalf("server returned %v, want http.ErrServerClosed", runErr)
	}
	if n, err := connection.Read(make([]byte, 1)); n != 0 || err == nil {
		t.Fatalf("active connection survived forced Close: bytes=%d error=%v", n, err)
	} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		t.Fatalf("active connection timed out instead of closing: %v", err)
	}
}

func TestRunHTTPServerStartupErrorDoesNotLeaveShutdownWaiter(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	server := &http.Server{Addr: occupied.Addr().String()}
	defer server.Close()
	shutdownStarted := make(chan struct{})
	server.RegisterOnShutdown(func() { close(shutdownStarted) })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan struct{})
	var runErr error
	go func() {
		runErr = runHTTPServer(ctx, server, server.ListenAndServe, 50*time.Millisecond)
		close(runDone)
	}()
	waitForHTTPEvent(t, runDone, "startup error without a shutdown signal")
	if runErr == nil || errors.Is(runErr, http.ErrServerClosed) {
		t.Fatalf("server returned %v, want a listener startup error", runErr)
	}
	if ctx.Err() != nil {
		t.Fatal("startup error required context cancellation to return")
	}
	cancel()
	select {
	case <-shutdownStarted:
		t.Fatal("startup error left a shutdown goroutine waiting for cancellation")
	case <-time.After(100 * time.Millisecond):
	}
}

// Keep the real listener, socket and HTTP server, but hold the server-side
// connection Close open so returning before the fallback finishes is observable.
type closeGatedListener struct {
	net.Listener
	closeStarted  chan struct{}
	allowClose    chan struct{}
	closeFinished chan struct{}
}

func (l *closeGatedListener) Accept() (net.Conn, error) {
	connection, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &closeGatedConn{
		Conn: connection, closeStarted: l.closeStarted, allowClose: l.allowClose, closeFinished: l.closeFinished,
	}, nil
}

type closeGatedConn struct {
	net.Conn
	closeStarted  chan struct{}
	allowClose    chan struct{}
	closeFinished chan struct{}
	closeOnce     sync.Once
	closeErr      error
}

func (c *closeGatedConn) Close() error {
	c.closeOnce.Do(func() {
		close(c.closeStarted)
		<-c.allowClose
		c.closeErr = c.Conn.Close()
		close(c.closeFinished)
	})
	return c.closeErr
}

func waitForHTTPEvent(t *testing.T, event <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-event:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}

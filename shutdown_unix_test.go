//go:build unix

package owl

import (
	"io"
	"net"
	"net/http"
	"os"
	"syscall"
	"testing"
	"time"
)

// A request that is in flight when the signal arrives must still be answered: a
// graceful stop waits for it. Before, startServer treated the ErrServerClosed that
// Shutdown causes as a failure and ended the program with log.Fatalln.
func TestShutdownWaitsForTheRequestsInFlight(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()

	started := make(chan struct{})
	server := &http.Server{
		Addr: addr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			close(started)
			time.Sleep(300 * time.Millisecond)
			_, _ = io.WriteString(w, "done")
		}),
	}
	go startServer(server)
	stopped := make(chan struct{})
	go func() {
		waitAndStopServer(server)
		close(stopped)
	}()
	time.Sleep(200 * time.Millisecond) // listening, and the signal registered

	answer := make(chan string, 1)
	go func() {
		res, err := http.Get("http://" + addr)
		if err != nil {
			answer <- "error: " + err.Error()
			return
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		answer <- string(body)
	}()

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("the request never reached the handler")
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-answer:
		if got != "done" {
			t.Fatalf("the request in flight got %q, want done", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no answer")
	}
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("the server did not stop")
	}
}

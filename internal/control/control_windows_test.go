//go:build windows

package control

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"mapftpdrive/internal/config"
)

// startServer serves scope with handler in the background and waits until
// the pipe accepts connections. It skips the test when the pipe name is
// already taken, for example by a MapFTPDrive host on a developer machine.
func startServer(t *testing.T, scope config.Scope, handler Handler) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- Serve(ctx, scope, handler) }()
	t.Cleanup(func() {
		cancel()
		<-served
	})
	select {
	case err := <-served:
		t.Skipf("cannot serve the %s pipe: %v", scope, err)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestUserPipeRoundTrip(t *testing.T) {
	startServer(t, config.User, func(_ context.Context, request Request) (Response, error) {
		if request.Operation == Remove {
			return Response{Message: "kept"}, errors.New("refused")
		}
		return Response{Message: "pong"}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	response, err := Call(ctx, config.User, Request{Operation: Ping})
	if err != nil {
		t.Fatalf("ping: %v", err)
	}
	if response.Status != StatusDone || response.Message != "pong" {
		t.Fatalf("ping response %+v", response)
	}
	response, err = Call(ctx, config.User, Request{Operation: Remove, Drive: "X"})
	if err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("remove returned %v", err)
	}
	if response.Error != "refused" || response.Message != "kept" {
		t.Fatalf("remove response %+v", response)
	}
}

func TestUserPipeReportsProgressForSlowRequests(t *testing.T) {
	startServer(t, config.User, func(ctx context.Context, _ Request) (Response, error) {
		select {
		case <-time.After(ProgressInterval + time.Second):
			return Response{}, nil
		case <-ctx.Done():
			return Response{}, ctx.Err()
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), RequestTimeout)
	defer cancel()
	if err := Send(ctx, config.User, Request{Operation: Mount, Drive: "X"}); err != nil {
		t.Fatalf("slow request: %v", err)
	}
}

func TestShutdownMakesServeReturn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	served := make(chan error, 1)
	go func() {
		served <- Serve(ctx, config.User, func(context.Context, Request) (Response, error) {
			t.Error("the handler received the shutdown request")
			return Response{}, nil
		})
	}()
	select {
	case err := <-served:
		t.Skipf("cannot serve the user pipe: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	sendCtx, sendCancel := context.WithTimeout(ctx, 5*time.Second)
	defer sendCancel()
	if err := Send(sendCtx, config.User, Request{Operation: Shutdown}); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("Serve returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve kept running after a shutdown request")
	}
}

func TestSystemPipeServedByAnotherAccountIsRefused(t *testing.T) {
	user, err := currentSID()
	if err != nil {
		t.Fatal(err)
	}
	if user == "S-1-5-18" {
		t.Skip("the test runs as LocalSystem")
	}
	// This process plays a process that created the system pipe name
	// before the service did.
	startServer(t, config.System, func(context.Context, Request) (Response, error) {
		t.Error("the squatting server received a request")
		return Response{}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = Send(ctx, config.System, Request{Operation: SaveAndMount, Password: "secret"})
	if errors.Is(err, ErrUntrustedHost) {
		return
	}
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "access is denied") {
		t.Skip("the test process cannot open the system pipe without elevation")
	}
	t.Fatalf("Send returned %v, want ErrUntrustedHost", err)
}

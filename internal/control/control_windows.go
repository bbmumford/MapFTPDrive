//go:build windows

package control

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
	"mapftpdrive/internal/config"
)

const (
	// requestReadTimeout bounds how long a connected client may take to
	// send its request, so an idle connection cannot hold a handler.
	requestReadTimeout = 10 * time.Second
	// messageWriteTimeout bounds each message either side writes.
	messageWriteTimeout = 10 * time.Second
	// clientIdleTimeout is how long a client waits between host messages;
	// the host sends one every ProgressInterval while it works.
	clientIdleTimeout = 3 * ProgressInterval
)

func currentSID() (string, error) {
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		return "", err
	}
	return user.User.Sid.String(), nil
}

func currentSession() (uint32, error) {
	var session uint32
	err := windows.ProcessIdToSessionId(windows.GetCurrentProcessId(), &session)
	return session, err
}

// pipeName returns the scope's control pipe. User hosts are per account and
// per logon session: the same account signed in twice (for example on the
// console and over Remote Desktop) runs one host per session, each owning
// the drives visible in its session.
func pipeName(scope config.Scope) (string, error) {
	if scope == config.System {
		return `\\.\pipe\MapFTPDrive-system`, nil
	}
	sid, err := currentSID()
	if err != nil {
		return "", err
	}
	session, err := currentSession()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(sid))
	return fmt.Sprintf(`\\.\pipe\MapFTPDrive-user-%s-%d`, hex.EncodeToString(sum[:8]), session), nil
}

func pipeSecurity(scope config.Scope) (string, error) {
	if scope == config.System {
		return "D:P(A;;GA;;;SY)(A;;GA;;;BA)", nil
	}
	sid, err := currentSID()
	if err != nil {
		return "", err
	}
	return "D:P(A;;GA;;;SY)(A;;GA;;;" + sid + ")", nil
}

func serve(ctx context.Context, scope config.Scope, handler Handler) error {
	name, err := pipeName(scope)
	if err != nil {
		return err
	}
	security, err := pipeSecurity(scope)
	if err != nil {
		return err
	}
	listener, err := winio.ListenPipe(name, &winio.PipeConfig{
		SecurityDescriptor: security,
		InputBufferSize:    64 * 1024,
		OutputBufferSize:   64 * 1024,
	})
	if err != nil {
		return err
	}
	defer listener.Close()
	serveCtx, shutdown := context.WithCancel(ctx)
	defer shutdown()
	go func() {
		<-serveCtx.Done()
		listener.Close()
	}()
	for {
		connection, err := listener.Accept()
		if err != nil {
			if serveCtx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		go handleConnection(ctx, connection, handler, shutdown)
	}
}

func handleConnection(ctx context.Context, connection net.Conn, handler Handler, shutdown context.CancelFunc) {
	defer connection.Close()
	encoder := json.NewEncoder(connection)
	write := func(response Response) error {
		_ = connection.SetWriteDeadline(time.Now().Add(messageWriteTimeout))
		return encoder.Encode(response)
	}
	_ = connection.SetReadDeadline(time.Now().Add(requestReadTimeout))
	var request Request
	if err := json.NewDecoder(io.LimitReader(connection, 128*1024)).Decode(&request); err != nil {
		_ = write(Response{Error: "invalid request"})
		return
	}
	if request.Operation == Shutdown {
		_ = write(Response{Status: StatusDone})
		shutdown()
		return
	}
	if err := write(Response{Status: StatusAccepted}); err != nil {
		return
	}
	requestCtx, cancel := context.WithTimeout(ctx, RequestTimeout)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- handler(requestCtx, request) }()
	ticker := time.NewTicker(ProgressInterval)
	defer ticker.Stop()
	for {
		select {
		case err := <-result:
			response := Response{Status: StatusDone}
			if err != nil {
				response = Response{Error: err.Error()}
			}
			_ = write(response)
			return
		case <-ticker.C:
			// A client that has gone away no longer needs progress; the
			// handler still runs to completion on its own.
			if write(Response{Status: StatusWorking}) != nil {
				return
			}
		case <-requestCtx.Done():
			_ = write(Response{Error: fmt.Sprintf("the request did not finish within %s", RequestTimeout)})
			return
		}
	}
}

func send(ctx context.Context, scope config.Scope, request Request) error {
	name, err := pipeName(scope)
	if err != nil {
		return err
	}
	connection, err := winio.DialPipeContext(ctx, name)
	if err != nil {
		return err
	}
	defer connection.Close()
	stop := context.AfterFunc(ctx, func() { connection.Close() })
	defer stop()
	if err := verifyServer(connection, scope); err != nil {
		return err
	}
	until := func(limit time.Duration) time.Time {
		deadline := time.Now().Add(limit)
		if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
			deadline = contextDeadline
		}
		return deadline
	}
	_ = connection.SetWriteDeadline(until(messageWriteTimeout))
	if err := json.NewEncoder(connection).Encode(request); err != nil {
		return err
	}
	decoder := json.NewDecoder(io.LimitReader(connection, 64*1024))
	for {
		_ = connection.SetReadDeadline(until(clientIdleTimeout))
		var response Response
		if err := decoder.Decode(&response); err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("the MapFTPDrive host did not finish in time: %w", ctx.Err())
			}
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				return errors.New("the MapFTPDrive host stopped responding")
			}
			return fmt.Errorf("read the MapFTPDrive host response: %w", err)
		}
		if !response.Final() {
			continue
		}
		if response.Error != "" {
			return fmt.Errorf("MapFTPDrive host: %s", response.Error)
		}
		return nil
	}
}

// verifyServer checks, before any request is written, that the process
// serving the pipe is the expected host. Pipe names are predictable and the
// first process to create one serves it, so without this check another
// local process could create the name first and receive passwords sent by
// the mapper. The system pipe must be served by LocalSystem; a user pipe by
// the caller's own account in the caller's logon session.
func verifyServer(connection net.Conn, scope config.Scope) error {
	pipe, ok := connection.(interface{ Fd() uintptr })
	if !ok {
		return fmt.Errorf("%w: the pipe handle is unavailable", ErrUntrustedHost)
	}
	var pid uint32
	if err := windows.GetNamedPipeServerProcessId(windows.Handle(pipe.Fd()), &pid); err != nil {
		return fmt.Errorf("%w: identify the pipe server: %v", ErrUntrustedHost, err)
	}
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return fmt.Errorf("%w: open pipe server process %d: %v", ErrUntrustedHost, pid, err)
	}
	defer windows.CloseHandle(process)
	var token windows.Token
	if err := windows.OpenProcessToken(process, windows.TOKEN_QUERY, &token); err != nil {
		return fmt.Errorf("%w: read the identity of process %d: %v", ErrUntrustedHost, pid, err)
	}
	defer token.Close()
	server, err := token.GetTokenUser()
	if err != nil {
		return fmt.Errorf("%w: read the identity of process %d: %v", ErrUntrustedHost, pid, err)
	}
	if scope == config.System {
		if !server.User.Sid.IsWellKnown(windows.WinLocalSystemSid) {
			return fmt.Errorf("%w: process %d runs as %s, not LocalSystem", ErrUntrustedHost, pid, server.User.Sid)
		}
		return nil
	}
	self, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	if !server.User.Sid.Equals(self.User.Sid) {
		return fmt.Errorf("%w: process %d runs as %s", ErrUntrustedHost, pid, server.User.Sid)
	}
	var serverSession uint32
	if err := windows.ProcessIdToSessionId(pid, &serverSession); err != nil {
		return fmt.Errorf("%w: read the session of process %d: %v", ErrUntrustedHost, pid, err)
	}
	session, err := currentSession()
	if err != nil {
		return err
	}
	if serverSession != session {
		return fmt.Errorf("%w: process %d belongs to logon session %d, not %d", ErrUntrustedHost, pid, serverSession, session)
	}
	return nil
}

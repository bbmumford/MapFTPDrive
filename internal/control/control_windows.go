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

func currentSID() (string, error) {
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		return "", err
	}
	return user.User.Sid.String(), nil
}

func pipeName(scope config.Scope) (string, error) {
	if scope == config.System {
		return `\\.\pipe\MapFTPDrive-system`, nil
	}
	sid, err := currentSID()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(sid))
	return `\\.\pipe\MapFTPDrive-user-` + hex.EncodeToString(sum[:8]), nil
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

func serve(ctx context.Context, scope config.Scope, handler Handler, ready func()) error {
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
	go func() {
		<-ctx.Done()
		listener.Close()
	}()
	if ready != nil {
		ready()
	}
	for {
		connection, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		go handleConnection(ctx, connection, handler)
	}
}

func handleConnection(ctx context.Context, connection net.Conn, handler Handler) {
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(30 * time.Second))
	decoder := json.NewDecoder(io.LimitReader(connection, 128*1024))
	encoder := json.NewEncoder(connection)
	var request Request
	if err := decoder.Decode(&request); err != nil {
		_ = encoder.Encode(Response{Error: "invalid request"})
		return
	}
	response, err := handler(ctx, request)
	if err != nil {
		response.Error = err.Error()
	}
	_ = encoder.Encode(response)
}

func send(ctx context.Context, scope config.Scope, request Request) (Response, error) {
	name, err := pipeName(scope)
	if err != nil {
		return Response{}, err
	}
	connection, err := winio.DialPipeContext(ctx, name)
	if err != nil {
		return Response{}, err
	}
	defer connection.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = connection.SetDeadline(deadline)
	}
	if err := json.NewEncoder(connection).Encode(request); err != nil {
		return Response{}, err
	}
	var response Response
	if err := json.NewDecoder(io.LimitReader(connection, 64*1024)).Decode(&response); err != nil {
		return Response{}, err
	}
	if response.Error != "" {
		return response, fmt.Errorf("MapFTPDrive host: %s", response.Error)
	}
	return response, nil
}

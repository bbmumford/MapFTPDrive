package host

import (
	"context"
	"errors"
	"fmt"
	"time"

	"mapftpdrive/internal/app"
	"mapftpdrive/internal/config"
	"mapftpdrive/internal/control"
)

// requestBudget bounds each request below the 30-second deadline the pipe
// server in internal/control puts on every connection, so the reply is
// always written before the server drops the client.
const requestBudget = 28 * time.Second

// NewHandler dispatches control requests to supervisor. Every request is
// checked against the host's scope; endpoint definitions in replies carry no
// secrets because secrets never enter config.Endpoint.
func NewHandler(scope config.Scope, supervisor *app.Supervisor) control.Handler {
	return func(ctx context.Context, request control.Request) (control.Response, error) {
		ctx, cancel := context.WithTimeout(ctx, requestBudget)
		defer cancel()
		return dispatch(ctx, scope, supervisor, request)
	}
}

func dispatch(ctx context.Context, scope config.Scope, supervisor *app.Supervisor, request control.Request) (control.Response, error) {
	switch request.Operation {
	case control.Ping:
		return control.Response{}, nil
	case control.Status:
		return control.Response{Status: statusReport(scope, supervisor.Status())}, nil
	case control.Map, control.SaveAndMount:
		endpoint, err := requestEndpoint(scope, request)
		if err != nil {
			return control.Response{}, err
		}
		mapped, err := supervisor.Map(ctx, endpoint, request.Password)
		if err != nil {
			return control.Response{}, err
		}
		return control.Response{Endpoint: &mapped}, nil
	case control.Update:
		endpoint, err := requestEndpoint(scope, request)
		if err != nil {
			return control.Response{}, err
		}
		if endpoint.ID == "" {
			return control.Response{}, errors.New("update needs the drive's id")
		}
		updated, err := supervisor.Update(ctx, endpoint, optionalSecret(request.Password))
		if err != nil {
			return control.Response{}, err
		}
		return control.Response{Endpoint: &updated}, nil
	case control.Test:
		if request.Endpoint != nil {
			endpoint, err := requestEndpoint(scope, request)
			if err != nil {
				return control.Response{}, err
			}
			secret := optionalSecret(request.Password)
			if secret == nil && endpoint.ID == "" {
				empty := ""
				secret = &empty
			}
			return control.Response{Message: "connection succeeded"}, supervisor.Test(ctx, endpoint, secret)
		}
		id, err := supervisor.ResolveID(request.ID, request.Drive)
		if err != nil {
			return control.Response{}, err
		}
		return control.Response{Message: "connection succeeded"}, supervisor.TestStored(ctx, id)
	}

	id, err := supervisor.ResolveID(request.ID, request.Drive)
	if err != nil {
		return control.Response{}, err
	}
	switch request.Operation {
	case control.Mount:
		message, err := supervisor.Enable(ctx, id)
		return control.Response{Message: message}, err
	case control.Unmount:
		return control.Response{}, supervisor.Disable(id)
	case control.Remove:
		return control.Response{}, supervisor.Remove(id)
	case control.Rename:
		message, err := supervisor.Rename(ctx, id, request.Name)
		return control.Response{Message: message}, err
	case control.Repin:
		message, err := supervisor.Repin(ctx, id, request.KnownHost, request.CertificatePin)
		return control.Response{Message: message}, err
	default:
		return control.Response{}, fmt.Errorf("unsupported operation %q", request.Operation)
	}
}

func requestEndpoint(scope config.Scope, request control.Request) (config.Endpoint, error) {
	if request.Endpoint == nil {
		return config.Endpoint{}, errors.New("endpoint is required")
	}
	endpoint := request.Endpoint.Normalized()
	if endpoint.Scope != scope {
		return config.Endpoint{}, fmt.Errorf("endpoint scope %q does not match host scope %q", endpoint.Scope, scope)
	}
	return endpoint, nil
}

// optionalSecret maps an empty password to "keep the stored secret".
func optionalSecret(password string) *string {
	if password == "" {
		return nil
	}
	return &password
}

func statusReport(scope config.Scope, report app.Report) *control.StatusReport {
	result := &control.StatusReport{Scope: scope, Problems: report.Problems, StoreError: report.StoreError}
	for _, status := range report.Endpoints {
		entry := control.EndpointStatus{
			Endpoint:  status.Endpoint,
			State:     control.State(status.State),
			LastError: status.LastError,
			RetryAt:   status.RetryAt,
			Since:     status.Since,
			Attempts:  status.Attempts,
		}
		if status.Trust != nil {
			entry.Trust = &control.TrustProblem{Kind: status.Trust.Kind, Pinned: status.Trust.Pinned, Presented: status.Trust.Presented}
		}
		result.Endpoints = append(result.Endpoints, entry)
	}
	return result
}

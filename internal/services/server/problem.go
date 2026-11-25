package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"charm.land/log/v2"

	v1 "github.com/tmih06/nightshift/api/v1"
	"github.com/tmih06/nightshift/internal/services/checkout"
)

// apiError is an error with the status and code it is reported under.
type apiError struct {
	status int
	code   string
	detail string
}

func (e *apiError) Error() string { return e.detail }

func invalid(detail string) error {
	return &apiError{status: http.StatusBadRequest, code: v1.CodeInvalid, detail: detail}
}

func notFound(detail string) error {
	return &apiError{status: http.StatusNotFound, code: v1.CodeNotFound, detail: detail}
}

func unavailable(detail string) error {
	return &apiError{status: http.StatusNotImplemented, code: v1.CodeUnavailable, detail: detail}
}

var titles = map[string]string{
	v1.CodeInvalid:      "Invalid request",
	v1.CodeUnauthorized: "Unauthorized",
	v1.CodeNotFound:     "Not found",
	v1.CodeConflict:     "Conflict",
	v1.CodeUnavailable:  "Unavailable",
	v1.CodeTimeout:      "Timed out",
	v1.CodeInternal:     "Internal error",
}

// classify turns anything a backend returned into the status and code the
// contract promises. The cluster's own words stay in the detail.
func classify(err error) *apiError {
	var ae *apiError
	var inv *checkout.InvalidError
	msg := err.Error()
	switch {
	case errors.As(err, &ae):
		return ae
	case errors.As(err, &inv):
		return &apiError{status: http.StatusUnprocessableEntity, code: v1.CodeInvalid, detail: msg}
	case errors.Is(err, checkout.ErrNotFound), strings.Contains(msg, "(NotFound)"):
		return &apiError{status: http.StatusNotFound, code: v1.CodeNotFound, detail: msg}
	case strings.Contains(msg, "(AlreadyExists)"), strings.Contains(msg, "(Conflict)"):
		return &apiError{status: http.StatusConflict, code: v1.CodeConflict, detail: msg}
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(msg, "did not finish within"), strings.Contains(msg, "adopted no sandbox within"):
		return &apiError{status: http.StatusGatewayTimeout, code: v1.CodeTimeout, detail: msg}
	case unreachable(msg):
		return &apiError{status: http.StatusServiceUnavailable, code: v1.CodeUnavailable, detail: msg}
	}
	return &apiError{status: http.StatusInternalServerError, code: v1.CodeInternal, detail: msg}
}

// unreachable is whether msg says the cluster, or a service in it, could not be
// reached.
func unreachable(msg string) bool {
	for _, s := range []string{"Unable to connect", "connection refused", "no such host", "i/o timeout", "Unauthorized", "Forbidden", "couldn't get current server API group list"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// writeProblem answers with err as application/problem+json.
func writeProblem(w http.ResponseWriter, err error) {
	ae := classify(err)
	if ae.status >= http.StatusInternalServerError && ae.status != http.StatusNotImplemented {
		log.Error("request failed", "status", ae.status, "err", ae.detail)
	}
	body := v1.Problem{
		Type:   "about:blank",
		Title:  titles[ae.code],
		Status: ae.status,
		Detail: ae.detail,
		Code:   ae.code,
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(ae.status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Debug("write problem", "err", err)
	}
}

// writeJSON answers with v as JSON.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Debug("write response", "err", err)
	}
}

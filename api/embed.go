// Package api carries the contract itself: the OpenAPI document that every
// frontend and the server agree on. The Go types of its bodies are in api/v1.
package api

import _ "embed"

// OpenAPI is api/openapi.yaml, the source of truth for the services API.
//
//go:embed openapi.yaml
var OpenAPI []byte

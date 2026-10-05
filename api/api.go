// Package api holds the OpenAPI description of the REST API. It is served at
// /api/openapi.json; internal/server's tests keep it in step with the router.
package api

import _ "embed"

// Spec is the OpenAPI 3.2 document (openapi.json).
//
//go:embed openapi.json
var Spec []byte

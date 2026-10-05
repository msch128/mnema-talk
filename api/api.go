// Package api holds the OpenAPI description of the REST API. It is served at
// /api/openapi.json; internal/server's tests keep it in step with the router.
package api

import _ "embed"

// Spec is the OpenAPI 3.1 document (openapi.json). It is generated from the
// handler annotations by `make openapi`; do not edit it by hand.
//
//go:embed openapi.json
var Spec []byte

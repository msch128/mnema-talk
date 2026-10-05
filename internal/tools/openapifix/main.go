// Command openapifix tidies the OpenAPI 3.1 document that swag v2 generates
// from the handler annotations, so api/openapi.json stays accurate and
// reproducible. It fixes what swag cannot express itself:
//
//   - fields tagged `extensions:"x-nullable"` become `type: [T, "null"]`;
//   - every response lists all of an operation's @Produce media types, so error
//     bodies are cut down to application/json and plain-string bodies to the
//     non-JSON media types;
//   - $ref request body schemas lose the summary/description siblings swag adds;
//   - operations without @Security are public, so they get an explicit
//     `security: []` (the document has no root-level security requirement);
//   - a request body whose schema has required properties (multipart forms)
//     is marked required;
//   - the empty externalDocs object and the `uniqueItems: false` noise go.
//
// Usage: openapifix <swag output> <target file>
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

type obj = map[string]any

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: openapifix <input.json> <output.json>")
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, "openapifix:", err)
		os.Exit(1)
	}
}

func run(in, out string) error {
	raw, err := os.ReadFile(in)
	if err != nil {
		return err
	}
	var doc obj
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("parse %s: %w", in, err)
	}
	if v, _ := doc["openapi"].(string); len(v) < 4 || v[:4] != "3.1." {
		return fmt.Errorf("openapi = %q, want 3.1.x", v)
	}
	if ed, ok := doc["externalDocs"].(obj); ok && ed["url"] == "" {
		delete(doc, "externalDocs")
	}
	if err := walk(doc); err != nil {
		return err
	}
	if paths, ok := doc["paths"].(obj); ok {
		for _, item := range paths {
			for _, op := range item.(obj) {
				if op, ok := op.(obj); ok {
					fixOperation(op)
				}
			}
		}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return err
	}
	return os.WriteFile(out, buf.Bytes(), 0o644)
}

// walk applies the schema-level fixes to every object in the document.
func walk(v any) error {
	switch v := v.(type) {
	case obj:
		delete(v, "uniqueItems")
		if n, _ := v["x-nullable"].(bool); n {
			delete(v, "x-nullable")
			t, ok := v["type"].(string)
			if !ok {
				return fmt.Errorf("x-nullable on a schema without a plain type: %v", v)
			}
			v["type"] = []any{t, "null"}
		}
		for _, c := range v {
			if err := walk(c); err != nil {
				return err
			}
		}
	case []any:
		for _, c := range v {
			if err := walk(c); err != nil {
				return err
			}
		}
	}
	return nil
}

func fixOperation(op obj) {
	if _, ok := op["security"]; !ok {
		op["security"] = []any{}
	}
	if rb, ok := op["requestBody"].(obj); ok {
		if content, ok := rb["content"].(obj); ok {
			for _, mt := range content {
				s, ok := mt.(obj)["schema"].(obj)
				if !ok {
					continue
				}
				if s["$ref"] != nil {
					delete(s, "summary")
					delete(s, "description")
				}
				if req, _ := s["required"].([]any); len(req) > 0 {
					rb["required"] = true
				}
			}
		}
	}
	responses, _ := op["responses"].(obj)
	for _, r := range responses {
		r, _ := r.(obj)
		content, _ := r["content"].(obj)
		if len(content) < 2 {
			if len(content) == 1 {
				fixErrorBody(content)
			}
			continue
		}
		fixErrorBody(content)
		if len(content) > 1 && plainString(content) {
			// A string body is never JSON; keep the non-JSON media types.
			delete(content, "application/json")
		}
	}
}

// fixErrorBody reduces an ErrorResponse body to application/json.
func fixErrorBody(content obj) {
	for _, mt := range content {
		s, _ := mt.(obj)["schema"].(obj)
		ref, _ := s["$ref"].(string)
		if ref != "#/components/schemas/httpx.ErrorResponse" {
			return
		}
		for k := range content {
			delete(content, k)
		}
		content["application/json"] = obj{"schema": obj{"$ref": ref}}
		return
	}
}

// plainString reports whether every body schema is a bare {"type": "string"}.
func plainString(content obj) bool {
	for _, mt := range content {
		s, _ := mt.(obj)["schema"].(obj)
		if len(s) != 1 || s["type"] != "string" {
			return false
		}
	}
	return true
}

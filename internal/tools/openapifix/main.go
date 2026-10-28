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
	"iter"
	"maps"
	"os"
	"slices"
	"strings"
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

func cleanExternalDocs(doc obj) {
	if ed, ok := doc["externalDocs"].(obj); ok && ed["url"] == "" {
		delete(doc, "externalDocs")
	}
}

func fixPathItem(item any) {
	itemObj, ok := item.(obj)
	if !ok {
		return
	}
	for _, op := range itemObj {
		if opObj, ok := op.(obj); ok {
			fixOperation(opObj)
		}
	}
}

func fixPaths(paths obj) {
	renameAtMe(paths)
	for _, item := range paths {
		fixPathItem(item)
	}
}

func run(in, out string) error {
	doc, err := readDocument(in)
	if err != nil {
		return err
	}
	cleanExternalDocs(doc)
	if err := walk(doc); err != nil {
		return err
	}
	if paths, ok := doc["paths"].(obj); ok {
		fixPaths(paths)
	}
	return writeDocument(out, doc)
}

// readDocument parses swag's output and checks that it is OpenAPI 3.1.
func readDocument(in string) (obj, error) {
	raw, err := os.ReadFile(in)
	if err != nil {
		return nil, err
	}
	var doc obj
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", in, err)
	}
	if v, _ := doc["openapi"].(string); len(v) < 4 || v[:4] != "3.1." {
		return nil, fmt.Errorf("openapi = %q, want 3.1.x", v)
	}
	return doc, nil
}

func writeDocument(out string, doc obj) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return err
	}
	return os.WriteFile(out, buf.Bytes(), 0o644)
}

// renameAtMe renames the paths swag could not spell: its route syntax has no
// '@', so routes with a literal "@me" segment are annotated as "/at_me".
func renameAtMe(paths obj) {
	for p, item := range paths {
		if renamed := strings.ReplaceAll(p, "/at_me", "/@me"); renamed != p {
			delete(paths, p)
			paths[renamed] = item
		}
	}
}

// walk applies the schema-level fixes to every object in the document.
func walk(v any) error {
	switch v := v.(type) {
	case obj:
		if u, ok := v["uniqueItems"].(bool); ok && !u {
			delete(v, "uniqueItems")
		}
		if err := makeNullable(v); err != nil {
			return err
		}
		return walkAll(maps.Values(v))
	case []any:
		return walkAll(slices.Values(v))
	}
	return nil
}

func walkAll(children iter.Seq[any]) error {
	for c := range children {
		if err := walk(c); err != nil {
			return err
		}
	}
	return nil
}

// makeNullable turns a schema tagged x-nullable into type: [T, "null"].
func makeNullable(schema obj) error {
	if n, _ := schema["x-nullable"].(bool); !n {
		return nil
	}
	delete(schema, "x-nullable")
	t, ok := schema["type"].(string)
	if !ok {
		return fmt.Errorf("x-nullable on a schema without a plain type: %v", schema)
	}
	schema["type"] = []any{t, "null"}
	return nil
}

func fixOperation(op obj) {
	if _, ok := op["security"]; !ok {
		op["security"] = []any{}
	}
	if rb, ok := op["requestBody"].(obj); ok {
		fixRequestBody(rb)
	}
	responses, _ := op["responses"].(obj)
	for _, r := range responses {
		r, _ := r.(obj)
		content, _ := r["content"].(obj)
		fixErrorBody(content)
		if len(content) > 1 && plainString(content) {
			// A string body is never JSON; keep the non-JSON media types.
			delete(content, "application/json")
		}
	}
}

// fixRequestBody drops the siblings swag adds to $ref schemas and marks a
// body with required properties (multipart forms) as required.
func fixRequestBody(rb obj) {
	content, _ := rb["content"].(obj)
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

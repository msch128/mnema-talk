package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func buildSampleOpenAPIDoc() obj {
	return obj{
		"openapi": "3.1.0",
		"info": obj{
			"title":   "Test API",
			"version": "1.0.0",
		},
		"externalDocs": obj{
			"url": "",
		},
		"paths": obj{
			"/api/users/at_me": obj{
				"get": obj{
					"summary": "Current user",
					"requestBody": obj{
						"content": obj{
							"multipart/form-data": obj{
								"schema": obj{
									"$ref":     "#/components/schemas/UploadReq",
									"summary":  "drop me",
									"required": []any{"file"},
								},
							},
						},
					},
					"responses": obj{
						"200": obj{
							"content": obj{
								"text/plain": obj{
									"schema": obj{"type": "string"},
								},
								"application/json": obj{
									"schema": obj{"type": "string"},
								},
							},
						},
						"400": obj{
							"content": obj{
								"application/json": obj{
									"schema": obj{"$ref": "#/components/schemas/httpx.ErrorResponse"},
								},
								"text/plain": obj{
									"schema": obj{"$ref": "#/components/schemas/httpx.ErrorResponse"},
								},
							},
						},
					},
				},
			},
		},
		"components": obj{
			"schemas": obj{
				"NullableField": obj{
					"type":       "string",
					"x-nullable": true,
				},
				"UniqueArray": obj{
					"uniqueItems": true,
					"type":        "array",
					"properties":  obj{"uniqueItems": obj{"type": "boolean"}},
				},
				"ArrayItem": obj{
					"uniqueItems": false,
					"type":        "array",
				},
			},
		},
	}
}

func assertNullableTypeArray(t *testing.T, raw any) {
	t.Helper()
	types, ok := raw.([]any)
	if !ok {
		t.Fatalf("expected []any type, got %T", raw)
	}
	if len(types) != 2 {
		t.Fatalf("expected length 2, got %d", len(types))
	}
	if types[0] != "string" {
		t.Fatalf("expected types[0] string, got %v", types[0])
	}
	if types[1] != "null" {
		t.Fatalf("expected types[1] null, got %v", types[1])
	}
}

func verifyFixedDocument(t *testing.T, res obj) {
	t.Helper()
	if _, ok := res["externalDocs"]; ok {
		t.Errorf("expected externalDocs to be deleted")
	}

	paths := res["paths"].(obj)
	if _, ok := paths["/api/users/@me"]; !ok {
		t.Errorf("expected /api/users/@me path to exist")
	}
	assertFixedOperation(t, paths["/api/users/@me"].(obj)["get"].(obj))

	schemas := res["components"].(obj)["schemas"].(obj)
	nf := schemas["NullableField"].(obj)
	if _, ok := nf["x-nullable"]; ok {
		t.Errorf("x-nullable should be deleted")
	}
	assertNullableTypeArray(t, nf["type"])

	arr := schemas["ArrayItem"].(obj)
	if _, ok := arr["uniqueItems"]; ok {
		t.Errorf("uniqueItems should be deleted")
	}
	// Only the false noise goes: true is a real constraint, and a property
	// that happens to be named uniqueItems is not a keyword.
	uniq := schemas["UniqueArray"].(obj)
	if uniq["uniqueItems"] != true {
		t.Errorf("uniqueItems: true was removed")
	}
	if _, ok := uniq["properties"].(obj)["uniqueItems"]; !ok {
		t.Errorf("a property named uniqueItems was removed")
	}
}

func TestOpenAPIFixRun(t *testing.T) {
	dir := t.TempDir()
	inPath := filepath.Join(dir, "input.json")
	outPath := filepath.Join(dir, "output.json")

	sampleDoc := buildSampleOpenAPIDoc()
	data, err := json.Marshal(sampleDoc)
	if err != nil {
		t.Fatalf("marshal sample: %v", err)
	}
	if err := os.WriteFile(inPath, data, 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	if err := run(inPath, outPath); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	outRaw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	var res obj
	if err := json.Unmarshal(outRaw, &res); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}

	verifyFixedDocument(t, res)
}

func TestOpenAPIFixErrors(t *testing.T) {
	dir := t.TempDir()
	badJSON := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(badJSON, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(badJSON, filepath.Join(dir, "out.json")); err == nil {
		t.Errorf("expected error for bad json")
	}

	badVersion := filepath.Join(dir, "badver.json")
	if err := os.WriteFile(badVersion, []byte(`{"openapi": "3.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(badVersion, filepath.Join(dir, "out.json")); err == nil {
		t.Errorf("expected error for openapi 3.0.0")
	}

	badNullable := filepath.Join(dir, "badnull.json")
	badNullDoc := obj{
		"openapi": "3.1.0",
		"components": obj{
			"schemas": obj{
				"Bad": obj{"x-nullable": true},
			},
		},
	}
	nullData, err := json.Marshal(badNullDoc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(badNullable, nullData, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(badNullable, filepath.Join(dir, "out.json")); err == nil {
		t.Errorf("expected error for bad nullable schema")
	}

	// Test non-object path item
	paths := obj{"/test": "not-an-obj"}
	fixPaths(paths)
}

// assertFixedOperation checks the per-operation fixes: an explicit empty
// security list, request bodies without the siblings swag adds to a $ref and
// marked required, error bodies reduced to JSON and string bodies to their
// non-JSON media types.
func assertFixedOperation(t *testing.T, op obj) {
	t.Helper()
	if sec, ok := op["security"].([]any); !ok || len(sec) != 0 {
		t.Errorf("security = %v; want an empty list", op["security"])
	}
	rb := op["requestBody"].(obj)
	if rb["required"] != true {
		t.Errorf("a body with required properties must be required: %v", rb)
	}
	schema := rb["content"].(obj)["multipart/form-data"].(obj)["schema"].(obj)
	if _, ok := schema["summary"]; ok {
		t.Errorf("the summary next to a $ref was kept: %v", schema)
	}
	responses := op["responses"].(obj)
	errBody := responses["400"].(obj)["content"].(obj)
	if len(errBody) != 1 || errBody["application/json"] == nil {
		t.Errorf("error body media types = %v; want application/json only", errBody)
	}
	okBody := responses["200"].(obj)["content"].(obj)
	if _, ok := okBody["application/json"]; ok || okBody["text/plain"] == nil {
		t.Errorf("string body media types = %v; want text/plain without application/json", okBody)
	}
}

func TestChatSubPackageSchemasKeepTheChatPrefix(t *testing.T) {
	doc := obj{"components": obj{"schemas": obj{
		"core.Message":       obj{"properties": obj{"poll": obj{"$ref": "#/components/schemas/core.Poll"}}},
		"core.Poll":          obj{"type": "object"},
		"messages.Pin":       obj{"type": "string"},
		"blocks.BlockedUser": obj{"type": "object"},
	}}}
	if err := renameChatSchemas(doc); err != nil {
		t.Fatal(err)
	}
	schemas := doc["components"].(obj)["schemas"].(obj)
	var names []string
	for name := range schemas {
		names = append(names, name)
	}
	slices.Sort(names)
	if want := []string{"blocks.BlockedUser", "chat.Message", "chat.Pin", "chat.Poll"}; !slices.Equal(names, want) {
		t.Errorf("schemas = %v, want %v", names, want)
	}
	ref := schemas["chat.Message"].(obj)["properties"].(obj)["poll"].(obj)["$ref"]
	if ref != "#/components/schemas/chat.Poll" {
		t.Errorf("$ref = %v", ref)
	}

	clash := obj{"components": obj{"schemas": obj{
		"core.Thing":     obj{"type": "string"},
		"messages.Thing": obj{"type": "integer"},
	}}}
	if err := renameChatSchemas(clash); err == nil {
		t.Error("two schemas of one name were merged")
	}
}

func TestRenameAtMeSpellsTheOriginalResponse(t *testing.T) {
	paths := obj{"/api/bot/v1/interactions/{id}/{token}/messages/at_original": obj{}, "/api/plain": obj{}}
	renameAtMe(paths)
	if _, ok := paths["/api/bot/v1/interactions/{id}/{token}/messages/@original"]; !ok {
		t.Fatalf("at_original not renamed: %v", paths)
	}
	if _, ok := paths["/api/plain"]; !ok || len(paths) != 2 {
		t.Fatalf("other paths changed: %v", paths)
	}
}

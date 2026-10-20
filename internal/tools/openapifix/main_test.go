package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenAPIFixRun(t *testing.T) {
	dir := t.TempDir()
	inPath := filepath.Join(dir, "input.json")
	outPath := filepath.Join(dir, "output.json")

	sampleDoc := obj{
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
				"ArrayItem": obj{
					"uniqueItems": false,
					"type":        "array",
				},
			},
		},
	}

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

	// Verify externalDocs dropped
	if _, ok := res["externalDocs"]; ok {
		t.Errorf("expected externalDocs to be deleted")
	}

	// Verify paths renamed
	paths := res["paths"].(obj)
	if _, ok := paths["/api/users/@me"]; !ok {
		t.Errorf("expected /api/users/@me path to exist")
	}

	// Verify nullable field transformed
	schemas := res["components"].(obj)["schemas"].(obj)
	nf := schemas["NullableField"].(obj)
	if _, ok := nf["x-nullable"]; ok {
		t.Errorf("x-nullable should be deleted")
	}
	types, ok := nf["type"].([]any)
	if !ok || len(types) != 2 || types[0] != "string" || types[1] != "null" {
		t.Errorf("type should be [string, null], got %v", nf["type"])
	}

	// Verify uniqueItems false deleted
	arr := schemas["ArrayItem"].(obj)
	if _, ok := arr["uniqueItems"]; ok {
		t.Errorf("uniqueItems should be deleted")
	}
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
	nullData, _ := json.Marshal(badNullDoc)
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

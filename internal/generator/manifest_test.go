package generator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func TestNormalizeOpenAPIBytesConvertsOpenAPI31Features(t *testing.T) {
	t.Parallel()

	spec := []byte(`{
	  "openapi": "3.1.0",
	  "info": { "title": "Example API", "version": "1.0.0" },
	  "paths": {
	    "/items": {
	      "post": {
	        "requestBody": {
	          "required": true,
	          "content": {
	            "application/json": {
	              "schema": {
	                "type": "object",
	                "properties": {
	                  "name": { "type": ["string", "null"] },
	                  "count": { "type": "integer", "exclusiveMinimum": 0 }
	                },
	                "required": ["name"]
	              }
	            }
	          }
	        },
	        "responses": {
	          "200": {
	            "description": "ok"
	          }
	        }
	      }
	    }
	  },
	  "webhooks": {}
	}`)

	normalized, err := normalizeOpenAPIBytes(spec)
	if err != nil {
		t.Fatalf("normalizeOpenAPIBytes returned error: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(normalized, &raw); err != nil {
		t.Fatalf("normalized output is not valid JSON: %v", err)
	}

	if _, exists := raw["webhooks"]; exists {
		t.Fatalf("expected top-level webhooks to be removed")
	}

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(normalized)
	if err != nil {
		t.Fatalf("loader rejected normalized document: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("normalized document did not validate: %v", err)
	}

	schema := doc.Paths.Map()["/items"].Post.RequestBody.Value.Content["application/json"].Schema.Value.Properties["name"].Value
	if !schema.Nullable {
		t.Fatalf("expected nullable to be set on string|null union")
	}
	if doc.Paths.Map()["/items"].Post.RequestBody.Value.Content["application/json"].Schema.Value.Properties["count"].Value.Min == nil || *doc.Paths.Map()["/items"].Post.RequestBody.Value.Content["application/json"].Schema.Value.Properties["count"].Value.Min != 0 {
		t.Fatalf("expected numeric exclusiveMinimum to become minimum")
	}
	if !doc.Paths.Map()["/items"].Post.RequestBody.Value.Content["application/json"].Schema.Value.Properties["count"].Value.ExclusiveMin {
		t.Fatalf("expected numeric exclusiveMinimum to become ExclusiveMin=true")
	}
}

func TestNormalizeOpenAPIBytesPreservesNonEmptyWebhooks(t *testing.T) {
	t.Parallel()

	spec := []byte(`{
	  "openapi": "3.1.0",
	  "info": { "title": "Webhook API", "version": "1.0.0" },
	  "paths": {},
	  "webhooks": {
	    "widget.created": {
	      "post": {
	        "requestBody": {
	          "required": true,
	          "content": {
	            "application/json": {
	              "schema": {
	                "type": "object",
	                "properties": {
	                  "id": { "type": "string" }
	                }
	              }
	            }
	          }
	        },
	        "responses": {
	          "200": { "description": "ok" }
	        }
	      }
	    }
	  }
	}`)

	normalized, err := normalizeOpenAPIBytes(spec)
	if err != nil {
		t.Fatalf("normalizeOpenAPIBytes returned error: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(normalized, &raw); err != nil {
		t.Fatalf("normalized output is not valid JSON: %v", err)
	}

	webhooks, ok := raw["webhooks"].(map[string]any)
	if !ok || len(webhooks) != 1 {
		t.Fatalf("expected non-empty webhooks to be preserved, got %#v", raw["webhooks"])
	}
}

func TestBuildManifestExpandsServerVariables(t *testing.T) {
	t.Parallel()

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData([]byte(`{
	  "openapi": "3.0.3",
	  "info": { "title": "Templated API", "version": "1.0.0" },
	  "servers": [{
	    "url": "https://{subdomain}.example.com/{version}",
	    "variables": {
	      "subdomain": {
	        "default": "api",
	        "description": "Tenant subdomain."
	      },
	      "version": {
	        "default": "v1"
	      }
	    }
	  }],
	  "paths": {}
	}`))
	if err != nil {
		t.Fatalf("load doc: %v", err)
	}

	manifest, err := BuildManifest(doc, "templated")
	if err != nil {
		t.Fatalf("BuildManifest returned error: %v", err)
	}

	if manifest.ServerTemplate != "https://{subdomain}.example.com/{version}" {
		t.Fatalf("unexpected server template: %q", manifest.ServerTemplate)
	}
	if manifest.DefaultServer != "https://api.example.com/v1" {
		t.Fatalf("unexpected default server: %q", manifest.DefaultServer)
	}
	if len(manifest.ServerVars) != 2 {
		t.Fatalf("expected 2 server variables, got %d", len(manifest.ServerVars))
	}
	if manifest.ServerVars[0].EnvVar != "TEMPLATED_SERVER_SUBDOMAIN" || manifest.ServerVars[0].Default != "api" {
		t.Fatalf("unexpected first server variable: %#v", manifest.ServerVars[0])
	}
	if manifest.ServerVars[1].EnvVar != "TEMPLATED_SERVER_VERSION" || manifest.ServerVars[1].Default != "v1" {
		t.Fatalf("unexpected second server variable: %#v", manifest.ServerVars[1])
	}
}

func TestBuildManifestCapturesClientCredentialsAndRequiredScopes(t *testing.T) {
	t.Parallel()

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData([]byte(`{
	  "openapi": "3.0.3",
	  "info": { "title": "OAuth API", "version": "1.0.0" },
	  "servers": [{ "url": "https://example.invalid" }],
	  "components": {
	    "securitySchemes": {
	      "oauth": {
	        "type": "oauth2",
	        "flows": {
	          "clientCredentials": {
	            "tokenUrl": "https://auth.example.invalid/oauth/token",
	            "scopes": {
	              "read:users": "Read users",
	              "write:users": "Write users"
	            }
	          }
	        }
	      }
	    }
	  },
	  "paths": {
	    "/users": {
	      "get": {
	        "operationId": "users.list",
	        "security": [{ "oauth": ["read:users"] }],
	        "x-scope": "read:users read:profiles",
	        "responses": {
	          "200": { "description": "ok" }
	        }
	      }
	    }
	  }
	}`))
	if err != nil {
		t.Fatalf("load doc: %v", err)
	}

	manifest, err := BuildManifest(doc, "oauthapi")
	if err != nil {
		t.Fatalf("BuildManifest returned error: %v", err)
	}
	if len(manifest.Auth) != 1 {
		t.Fatalf("expected one auth scheme, got %d", len(manifest.Auth))
	}
	auth := manifest.Auth[0]
	if auth.ClientCredentials == nil {
		t.Fatalf("expected client credentials config on auth scheme")
	}
	if auth.ClientCredentials.TokenURL != "https://auth.example.invalid/oauth/token" {
		t.Fatalf("unexpected token URL: %q", auth.ClientCredentials.TokenURL)
	}
	if auth.ClientCredentials.ClientIDEnv != "OAUTHAPI_CLIENT_ID" || auth.ClientCredentials.ClientSecretEnv != "OAUTHAPI_CLIENT_SECRET" {
		t.Fatalf("unexpected client credentials env vars: %#v", auth.ClientCredentials)
	}
	if got := strings.Join(auth.ClientCredentials.AvailableScopes, ","); got != "read:users,write:users" {
		t.Fatalf("unexpected available scopes: %s", got)
	}
	if len(manifest.Operations) != 1 {
		t.Fatalf("expected one operation, got %d", len(manifest.Operations))
	}
	if got := strings.Join(manifest.Operations[0].RequiredScopes, ","); got != "read:profiles,read:users" {
		t.Fatalf("unexpected required scopes: %s", got)
	}
}

func TestNormalizeOpenAPIBytesPromotesDescriptionsTypo(t *testing.T) {
	t.Parallel()

	spec := []byte(`{
	  "openapi": "3.1.0",
	  "info": { "title": "Typo API", "version": "1.0.0" },
	  "paths": {
	    "/keys": {
	      "get": {
	        "responses": {
	          "200": {
	            "description": "ok",
	            "content": {
	              "application/json": {
	                "schema": {
	                  "type": "object",
	                  "properties": {
	                    "permissions": {
	                      "type": "array",
	                      "descriptions": "Permissions assigned to the API key."
	                    }
	                  }
	                }
	              }
	            }
	          }
	        }
	      }
	    }
	  }
	}`)

	normalized, err := normalizeOpenAPIBytes(spec)
	if err != nil {
		t.Fatalf("normalizeOpenAPIBytes returned error: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(normalized, &raw); err != nil {
		t.Fatalf("normalized output is not valid JSON: %v", err)
	}

	property := raw["paths"].(map[string]any)["/keys"].(map[string]any)["get"].(map[string]any)["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)["properties"].(map[string]any)["permissions"].(map[string]any)
	if got := property["description"]; got != "Permissions assigned to the API key." {
		t.Fatalf("expected descriptions typo to become description, got %#v", got)
	}
	if _, exists := property["descriptions"]; exists {
		t.Fatalf("expected descriptions typo field to be removed")
	}

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(normalized)
	if err != nil {
		t.Fatalf("loader rejected normalized document: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("normalized document did not validate: %v", err)
	}
}

func TestNormalizeOpenAPIBytesMovesMisnestedSchemaProperties(t *testing.T) {
	t.Parallel()

	spec := []byte(`{
	  "openapi": "3.1.0",
	  "info": { "title": "Misnested Properties API", "version": "1.0.0" },
	  "paths": {
	    "/configs": {
	      "get": {
	        "responses": {
	          "200": {
	            "description": "ok",
	            "content": {
	              "application/json": {
	                "schema": {
	                  "type": "object",
	                  "properties": {
	                    "data": {
	                      "type": "object",
	                      "properties": {
	                        "name": { "type": "string" }
	                      },
	                      "ID": {
	                        "type": "string",
	                        "description": "The configuration ID."
	                      }
	                    }
	                  }
	                }
	              }
	            }
	          }
	        }
	      }
	    }
	  }
	}`)

	normalized, err := normalizeOpenAPIBytes(spec)
	if err != nil {
		t.Fatalf("normalizeOpenAPIBytes returned error: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(normalized, &raw); err != nil {
		t.Fatalf("normalized output is not valid JSON: %v", err)
	}

	dataSchema := raw["paths"].(map[string]any)["/configs"].(map[string]any)["get"].(map[string]any)["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)["properties"].(map[string]any)["data"].(map[string]any)
	properties := dataSchema["properties"].(map[string]any)
	if _, exists := properties["ID"]; !exists {
		t.Fatalf("expected misnested schema member to move under properties, got %#v", properties)
	}
	if _, exists := dataSchema["ID"]; exists {
		t.Fatalf("expected misnested schema member to be removed from parent schema")
	}

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(normalized)
	if err != nil {
		t.Fatalf("loader rejected normalized document: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("normalized document did not validate: %v", err)
	}
}

func TestNormalizeOpenAPIBytesUnwrapsResponseLikeSchemaValues(t *testing.T) {
	t.Parallel()

	spec := []byte(`{
	  "openapi": "3.1.0",
	  "info": { "title": "Wrapped Schema API", "version": "1.0.0" },
	  "paths": {
	    "/errors": {
	      "get": {
	        "responses": {
	          "400": {
	            "description": "bad request",
	            "content": {
	              "application/json": {
	                "schema": {
	                  "description": "Bad Request",
	                  "content": {
	                    "application/json": {
	                      "schema": {
	                        "type": "object",
	                        "properties": {
	                          "message": { "type": "string" }
	                        }
	                      }
	                    }
	                  }
	                }
	              }
	            }
	          }
	        }
	      }
	    }
	  }
	}`)

	normalized, err := normalizeOpenAPIBytes(spec)
	if err != nil {
		t.Fatalf("normalizeOpenAPIBytes returned error: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(normalized, &raw); err != nil {
		t.Fatalf("normalized output is not valid JSON: %v", err)
	}

	schema := raw["paths"].(map[string]any)["/errors"].(map[string]any)["get"].(map[string]any)["responses"].(map[string]any)["400"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	if schema["type"] != "object" {
		t.Fatalf("expected wrapped schema to unwrap to inner schema, got %#v", schema)
	}
	if _, exists := schema["content"]; exists {
		t.Fatalf("expected wrapped schema content to be removed after unwrapping")
	}

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(normalized)
	if err != nil {
		t.Fatalf("loader rejected normalized document: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("normalized document did not validate: %v", err)
	}
}

func TestNormalizeOpenAPIBytesMergesEmbeddedSchemaMetadata(t *testing.T) {
	t.Parallel()

	spec := []byte(`{
	  "openapi": "3.1.0",
	  "info": { "title": "Embedded Metadata API", "version": "1.0.0" },
	  "paths": {
	    "/bookings": {
	      "put": {
	        "responses": {
	          "200": {
	            "description": "ok",
	            "content": {
	              "application/json": {
	                "schema": {
	                  "oneOf": [
	                    {
	                      "title": "Booking cancelled",
	                      "type": "object",
	                      "required": ["request_id"],
	                      "content": {
	                        "application/json": {
	                          "schema": {
	                            "type": "object",
	                            "properties": {
	                              "request_id": { "type": "string" }
	                            }
	                          }
	                        }
	                      }
	                    }
	                  ]
	                }
	              }
	            }
	          }
	        }
	      }
	    }
	  }
	}`)

	normalized, err := normalizeOpenAPIBytes(spec)
	if err != nil {
		t.Fatalf("normalizeOpenAPIBytes returned error: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(normalized, &raw); err != nil {
		t.Fatalf("normalized output is not valid JSON: %v", err)
	}

	branch := raw["paths"].(map[string]any)["/bookings"].(map[string]any)["put"].(map[string]any)["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)["oneOf"].([]any)[0].(map[string]any)
	if branch["title"] != "Booking cancelled" {
		t.Fatalf("expected embedded schema title to be preserved, got %#v", branch["title"])
	}
	if _, exists := branch["properties"]; !exists {
		t.Fatalf("expected inner schema properties to be preserved, got %#v", branch)
	}
	if _, exists := branch["content"]; exists {
		t.Fatalf("expected embedded content wrapper to be removed, got %#v", branch)
	}

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(normalized)
	if err != nil {
		t.Fatalf("loader rejected normalized document: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("normalized document did not validate: %v", err)
	}
}

func TestNormalizeOpenAPIBytesSupportsOneOfObjectBranchValidation(t *testing.T) {
	t.Parallel()

	spec := []byte(`{
	  "openapi": "3.1.0",
	  "info": { "title": "OneOf Validation API", "version": "1.0.0" },
	  "paths": {
	    "/redirect-uris": {
	      "post": {
	        "requestBody": {
	          "required": true,
	          "content": {
	            "application/json": {
	              "schema": {
	                "type": "object",
	                "required": ["url"],
	                "oneOf": [
	                  {
	                    "properties": {
	                      "platform": {
	                        "type": "string",
	                        "enum": ["web", "desktop"]
	                      },
	                      "url": {
	                        "type": "string"
	                      }
	                    }
	                  },
	                  {
	                    "properties": {
	                      "platform": {
	                        "type": "string",
	                        "enum": ["js"]
	                      },
	                      "settings": {
	                        "type": "object",
	                        "properties": {
	                          "origin": { "type": "string" }
	                        },
	                        "required": ["origin"]
	                      },
	                      "url": {
	                        "type": "string"
	                      }
	                    }
	                  }
	                ]
	              }
	            }
	          }
	        },
	        "responses": {
	          "200": { "description": "ok" }
	        }
	      }
	    }
	  }
	}`)

	normalized, err := normalizeOpenAPIBytes(spec)
	if err != nil {
		t.Fatalf("normalizeOpenAPIBytes returned error: %v", err)
	}

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(normalized)
	if err != nil {
		t.Fatalf("loader rejected normalized document: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("normalized document did not validate: %v", err)
	}

	schema := doc.Paths.Map()["/redirect-uris"].Post.RequestBody.Value.Content["application/json"].Schema
	if err := schema.Value.VisitJSON(map[string]any{
		"url":      "http://localhost:3002/callback",
		"platform": "web",
	}, openapi3.MultiErrors()); err != nil {
		t.Fatalf("expected oneOf branch validation to accept web callback body, got: %v", err)
	}
	if err := schema.Value.VisitJSON(map[string]any{
		"url":      "http://localhost:3002/callback",
		"platform": "js",
		"settings": map[string]any{"origin": "http://localhost:3002"},
	}, openapi3.MultiErrors()); err != nil {
		t.Fatalf("expected oneOf branch validation to accept js callback body, got: %v", err)
	}
}

func TestFinalizeLoadedDocRemovesBrokenDiscriminatorMappings(t *testing.T) {
	t.Parallel()

	spec := []byte(`{
	  "openapi": "3.1.0",
	  "info": { "title": "Broken Discriminator API", "version": "1.0.0" },
	  "paths": {
	    "/redirect-uris": {
	      "post": {
	        "requestBody": {
	          "required": true,
	          "content": {
	            "application/json": {
	              "schema": {
	                "type": "object",
	                "required": ["url"],
	                "oneOf": [
	                  {
	                    "title": "Web",
	                    "properties": {
	                      "platform": {
	                        "type": "string",
	                        "enum": ["web"]
	                      },
	                      "url": {
	                        "type": "string"
	                      }
	                    }
	                  }
	                ],
	                "discriminator": {
	                  "propertyName": "platform",
	                  "mapping": {
	                    "web": "#/components/schemas/WebCallback"
	                  }
	                }
	              }
	            }
	          }
	        },
	        "responses": {
	          "200": { "description": "ok" }
	        }
	      }
	    }
	  }
	}`)

	normalized, err := normalizeOpenAPIBytes(spec)
	if err != nil {
		t.Fatalf("normalizeOpenAPIBytes returned error: %v", err)
	}

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(normalized)
	if err != nil {
		t.Fatalf("loader rejected normalized document: %v", err)
	}
	if err := finalizeLoadedDoc(doc); err != nil {
		t.Fatalf("finalizeLoadedDoc returned error: %v", err)
	}

	schema := doc.Paths.Map()["/redirect-uris"].Post.RequestBody.Value.Content["application/json"].Schema
	if schema.Value.Discriminator != nil {
		t.Fatalf("expected broken discriminator mapping to be removed, got %#v", schema.Value.Discriminator)
	}
	if err := schema.Value.VisitJSON(map[string]any{
		"url":      "http://localhost:3002/callback",
		"platform": "web",
	}, openapi3.MultiErrors()); err != nil {
		t.Fatalf("expected validation to succeed after discriminator cleanup, got: %v", err)
	}
}

func TestNormalizeAmbiguousScalarOneOfToAnyOf(t *testing.T) {
	t.Parallel()

	spec := []byte(`{
	  "openapi": "3.0.3",
	  "info": { "title": "Identifiers API", "version": "1.0.0" },
	  "paths": {
	    "/customers/{identifier}": {
	      "get": {
	        "parameters": [
	          {
	            "name": "identifier",
	            "in": "path",
	            "required": true,
	            "schema": {
	              "oneOf": [
	                { "type": "string", "description": "Any identifier" },
	                { "type": "string", "description": "Email address" },
	                { "type": "string", "format": "cio_[a-zA-Z0-9]*", "description": "cio id" }
	              ]
	            }
	          }
	        ],
	        "responses": {
	          "200": { "description": "ok" }
	        }
	      }
	    }
	  }
	}`)

	normalized, err := normalizeOpenAPIBytes(spec)
	if err != nil {
		t.Fatalf("normalizeOpenAPIBytes returned error: %v", err)
	}

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(normalized)
	if err != nil {
		t.Fatalf("loader rejected normalized document: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("normalized document did not validate: %v", err)
	}

	schema := doc.Paths.Map()["/customers/{identifier}"].Get.Parameters[0].Value.Schema
	if got := len(schema.Value.OneOf); got != 0 {
		t.Fatalf("expected oneOf to be removed, got %d branches", got)
	}
	if got := len(schema.Value.AnyOf); got != 3 {
		t.Fatalf("expected anyOf to preserve original branches, got %d", got)
	}
	if err := schema.Value.VisitJSON("agent-cli-generator@example.invalid", openapi3.MultiErrors()); err != nil {
		t.Fatalf("expected normalized identifier schema to accept string input, got: %v", err)
	}
}

func TestBuildManifestDerivesAgentOperationIDs(t *testing.T) {
	t.Parallel()

	spec := []byte(`{
	  "openapi": "3.0.3",
	  "info": { "title": "Example API", "version": "1.0.0" },
	  "paths": {
	    "/tasks": {
	      "get": { "responses": { "200": { "description": "ok" } } },
	      "post": { "responses": { "201": { "description": "created" } } }
	    },
	    "/tasks/{id}": {
	      "get": {
	        "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }],
	        "responses": { "200": { "description": "ok" } }
	      },
	      "patch": {
	        "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }],
	        "responses": { "200": { "description": "ok" } }
	      }
	    },
	    "/planning/fit-task": {
	      "post": {
	        "responses": { "200": { "description": "ok" } }
	      }
	    }
	  }
	}`)

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(spec)
	if err != nil {
		t.Fatalf("failed to parse test spec: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("test spec validation failed: %v", err)
	}

	manifest, err := BuildManifest(doc, "example")
	if err != nil {
		t.Fatalf("BuildManifest returned error: %v", err)
	}

	got := map[string]bool{}
	for _, op := range manifest.Operations {
		got[op.ID] = true
	}

	for _, want := range []string{
		"planning.fit-task",
		"tasks.create",
		"tasks.get",
		"tasks.list",
		"tasks.update",
	} {
		if !got[want] {
			t.Fatalf("expected operation ID %q to be generated; got %#v", want, manifest.Operations)
		}
	}
}

func TestBuildManifestRejectsDuplicateDerivedOperationIDs(t *testing.T) {
	t.Parallel()

	spec := []byte(`{
	  "openapi": "3.0.3",
	  "info": { "title": "Collision API", "version": "1.0.0" },
	  "paths": {
	    "/users/posts": {
	      "post": { "responses": { "200": { "description": "ok" } } }
	    },
	    "/users/{id}/posts": {
	      "post": {
	        "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }],
	        "responses": { "200": { "description": "ok" } }
	      }
	    }
	  }
	}`)

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(spec)
	if err != nil {
		t.Fatalf("failed to parse test spec: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("test spec validation failed: %v", err)
	}

	_, err = BuildManifest(doc, "collision")
	if err == nil {
		t.Fatalf("expected duplicate operation ID error")
	}
}

func TestBuildManifestQualifiesMethodWhenDerivedIDsCollide(t *testing.T) {
	t.Parallel()

	spec := []byte(`{
	  "openapi": "3.0.3",
	  "info": { "title": "RPC API", "version": "1.0.0" },
	  "paths": {
	    "/rpc/task_summary": {
	      "get": { "responses": { "200": { "description": "ok" } } },
	      "post": { "responses": { "200": { "description": "ok" } } }
	    }
	  }
	}`)

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(spec)
	if err != nil {
		t.Fatalf("failed to parse test spec: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("test spec validation failed: %v", err)
	}

	manifest, err := BuildManifest(doc, "rpc")
	if err != nil {
		t.Fatalf("BuildManifest returned error: %v", err)
	}

	opsByID := map[string]OperationManifest{}
	for _, op := range manifest.Operations {
		opsByID[op.ID] = op
	}

	if _, ok := opsByID["rpc.task-summary.get"]; !ok {
		t.Fatalf("expected rpc.task-summary.get operation, got %#v", manifest.Operations)
	}
	if _, ok := opsByID["rpc.task-summary.post"]; !ok {
		t.Fatalf("expected rpc.task-summary.post operation, got %#v", manifest.Operations)
	}
	if aliases := opsByID["rpc.task-summary.get"].Aliases; len(aliases) != 0 {
		t.Fatalf("expected ambiguous base alias to be skipped for GET, got %#v", aliases)
	}
	if aliases := opsByID["rpc.task-summary.post"].Aliases; len(aliases) != 0 {
		t.Fatalf("expected ambiguous base alias to be skipped for POST, got %#v", aliases)
	}
}

func TestBuildManifestAddsDerivedAliasesForNamedOperations(t *testing.T) {
	t.Parallel()

	spec := []byte(`{
	  "openapi": "3.0.3",
	  "info": { "title": "Alias API", "version": "1.0.0" },
	  "paths": {
	    "/users": {
	      "get": {
	        "operationId": "GetUserList",
	        "responses": { "200": { "description": "ok" } }
	      },
	      "post": {
	        "operationId": "CreateUser",
	        "responses": { "201": { "description": "created" } }
	      }
	    },
	    "/users/{user_id}": {
	      "get": {
	        "operationId": "GetUser",
	        "parameters": [{ "name": "user_id", "in": "path", "required": true, "schema": { "type": "string" } }],
	        "responses": { "200": { "description": "ok" } }
	      }
	    }
	  }
	}`)

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(spec)
	if err != nil {
		t.Fatalf("failed to parse test spec: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("test spec validation failed: %v", err)
	}

	manifest, err := BuildManifest(doc, "alias")
	if err != nil {
		t.Fatalf("BuildManifest returned error: %v", err)
	}

	aliasesByID := map[string][]string{}
	for _, op := range manifest.Operations {
		aliasesByID[op.ID] = op.Aliases
	}

	if got := aliasesByID["GetUserList"]; len(got) != 1 || got[0] != "users.list" {
		t.Fatalf("expected GetUserList alias users.list, got %#v", got)
	}
	if got := aliasesByID["CreateUser"]; len(got) != 1 || got[0] != "users.create" {
		t.Fatalf("expected CreateUser alias users.create, got %#v", got)
	}
	if got := aliasesByID["GetUser"]; len(got) != 1 || got[0] != "users.get" {
		t.Fatalf("expected GetUser alias users.get, got %#v", got)
	}
}

func TestBuildManifestDetectsWhoAmIOperation(t *testing.T) {
	t.Parallel()

	spec := []byte(`{
	  "openapi": "3.0.3",
	  "info": { "title": "Identity API", "version": "1.0.0" },
	  "paths": {
	    "/users": {
	      "get": {
	        "operationId": "users.list",
	        "responses": { "200": { "description": "ok" } }
	      }
	    },
	    "/me": {
	      "get": {
	        "operationId": "users.me",
	        "summary": "Get current user",
	        "responses": { "200": { "description": "ok" } }
	      }
	    },
	    "/whoami": {
	      "post": {
	        "operationId": "whoami.post",
	        "responses": { "200": { "description": "ok" } }
	      }
	    }
	  }
	}`)

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(spec)
	if err != nil {
		t.Fatalf("failed to parse test spec: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("test spec validation failed: %v", err)
	}

	manifest, err := BuildManifest(doc, "identity")
	if err != nil {
		t.Fatalf("BuildManifest returned error: %v", err)
	}

	if manifest.WhoAmIOperationID != "whoami.post" {
		t.Fatalf("expected whoami.post to be selected, got %q", manifest.WhoAmIOperationID)
	}
}

func TestBuildManifestSkipsConflictingAliases(t *testing.T) {
	t.Parallel()

	spec := []byte(`{
	  "openapi": "3.0.3",
	  "info": { "title": "Alias Conflict API", "version": "1.0.0" },
	  "paths": {
	    "/users/ban": {
	      "post": {
	        "operationId": "UsersBan",
	        "responses": { "200": { "description": "ok" } }
	      }
	    },
	    "/users/{user_id}/ban": {
	      "post": {
	        "operationId": "BanUser",
	        "parameters": [{ "name": "user_id", "in": "path", "required": true, "schema": { "type": "string" } }],
	        "responses": { "200": { "description": "ok" } }
	      }
	    }
	  }
	}`)

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(spec)
	if err != nil {
		t.Fatalf("failed to parse test spec: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("test spec validation failed: %v", err)
	}

	manifest, err := BuildManifest(doc, "alias-conflict")
	if err != nil {
		t.Fatalf("BuildManifest returned error: %v", err)
	}

	for _, op := range manifest.Operations {
		if len(op.Aliases) != 0 {
			t.Fatalf("expected conflicting alias to be skipped for %s, got %#v", op.ID, op.Aliases)
		}
	}
}

func TestNormalizeOpenAPIBytesKeepsStricterInclusiveMinimum(t *testing.T) {
	t.Parallel()

	spec := []byte(`{
	  "openapi": "3.1.0",
	  "info": { "title": "Bounds API", "version": "1.0.0" },
	  "paths": {
	    "/items": {
	      "post": {
	        "requestBody": {
	          "required": true,
	          "content": {
	            "application/json": {
	              "schema": {
	                "type": "object",
	                "properties": {
	                  "count": {
	                    "type": "integer",
	                    "minimum": 5,
	                    "exclusiveMinimum": 0
	                  }
	                }
	              }
	            }
	          }
	        },
	        "responses": { "200": { "description": "ok" } }
	      }
	    }
	  }
	}`)

	normalized, err := normalizeOpenAPIBytes(spec)
	if err != nil {
		t.Fatalf("normalizeOpenAPIBytes returned error: %v", err)
	}

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(normalized)
	if err != nil {
		t.Fatalf("loader rejected normalized document: %v", err)
	}

	schema := doc.Paths.Map()["/items"].Post.RequestBody.Value.Content["application/json"].Schema.Value.Properties["count"].Value
	if schema.Min == nil || *schema.Min != 5 {
		t.Fatalf("expected inclusive minimum of 5 to be preserved")
	}
	if schema.ExclusiveMin {
		t.Fatalf("expected exclusive minimum to be dropped when an existing minimum is stricter")
	}
}

func TestNormalizeOpenAPIBytesConvertsRefWithSchemaSiblingExample(t *testing.T) {
	t.Parallel()

	spec := []byte(`{
	  "openapi": "3.1.0",
	  "info": { "title": "Ref Example API", "version": "1.0.0" },
	  "paths": {
	    "/items": {
	      "get": {
	        "responses": {
	          "400": {
	            "description": "bad request",
	            "content": {
	              "application/json": {
	                "schema": {
	                  "$ref": "#/components/schemas/Error",
	                  "example": { "message": "bad request" }
	                }
	              }
	            }
	          }
	        }
	      }
	    }
	  },
	  "components": {
	    "schemas": {
	      "Error": {
	        "type": "object",
	        "properties": {
	          "message": { "type": "string" }
	        }
	      }
	    }
	  }
	}`)

	normalized, err := normalizeOpenAPIBytes(spec)
	if err != nil {
		t.Fatalf("normalizeOpenAPIBytes returned error: %v", err)
	}

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(normalized)
	if err != nil {
		t.Fatalf("loader rejected normalized document: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("normalized document did not validate: %v", err)
	}

	schema := doc.Paths.Map()["/items"].Get.Responses.Map()["400"].Value.Content["application/json"].Schema.Value
	if len(schema.AllOf) != 1 || schema.AllOf[0].Ref != "#/components/schemas/Error" {
		t.Fatalf("expected schema ref to be rewritten as allOf, got %#v", schema.AllOf)
	}
	if schema.Example == nil {
		t.Fatalf("expected example to be preserved on rewritten schema")
	}
}

func TestNormalizeOpenAPIBytesConvertsPureNullType(t *testing.T) {
	t.Parallel()

	spec := []byte(`{
	  "openapi": "3.1.0",
	  "info": { "title": "Null Type API", "version": "1.0.0" },
	  "paths": {
	    "/items": {
	      "get": {
	        "responses": {
	          "200": {
	            "description": "ok",
	            "content": {
	              "application/json": {
	                "schema": {
	                  "type": "null"
	                }
	              }
	            }
	          }
	        }
	      }
	    }
	  }
	}`)

	normalized, err := normalizeOpenAPIBytes(spec)
	if err != nil {
		t.Fatalf("normalizeOpenAPIBytes returned error: %v", err)
	}

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(normalized)
	if err != nil {
		t.Fatalf("loader rejected normalized document: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("normalized document did not validate: %v", err)
	}

	schema := doc.Paths.Map()["/items"].Get.Responses.Map()["200"].Value.Content["application/json"].Schema.Value
	if schema.Type != nil {
		t.Fatalf("expected null-only type to be removed, got %#v", schema.Type)
	}
	if !schema.Nullable {
		t.Fatalf("expected null-only type to become nullable")
	}
	if len(schema.Enum) != 1 || schema.Enum[0] != nil {
		t.Fatalf("expected null-only type to become enum [null], got %#v", schema.Enum)
	}
}

func TestNormalizeOpenAPIBytesStripsResponseRefDescriptionSibling(t *testing.T) {
	t.Parallel()

	spec := []byte(`{
	  "openapi": "3.1.0",
	  "info": { "title": "Response Ref API", "version": "1.0.0" },
	  "paths": {
	    "/items": {
	      "get": {
	        "responses": {
	          "404": {
	            "$ref": "#/components/responses/NotFound",
	            "description": "Item not found"
	          }
	        }
	      }
	    }
	  },
	  "components": {
	    "responses": {
	      "NotFound": {
	        "description": "missing"
	      }
	    }
	  }
	}`)

	normalized, err := normalizeOpenAPIBytes(spec)
	if err != nil {
		t.Fatalf("normalizeOpenAPIBytes returned error: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(normalized, &raw); err != nil {
		t.Fatalf("normalized output is not valid JSON: %v", err)
	}

	response := raw["paths"].(map[string]any)["/items"].(map[string]any)["get"].(map[string]any)["responses"].(map[string]any)["404"].(map[string]any)
	if _, exists := response["allOf"]; exists {
		t.Fatalf("expected response ref to remain a ref object, got %#v", response)
	}
	if _, exists := response["description"]; exists {
		t.Fatalf("expected response ref sibling description to be stripped, got %#v", response)
	}

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(normalized)
	if err != nil {
		t.Fatalf("loader rejected normalized document: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("normalized document did not validate: %v", err)
	}
}

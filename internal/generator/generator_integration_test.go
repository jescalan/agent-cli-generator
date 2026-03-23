package generator

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateProducesRunnableCLI(t *testing.T) {
	t.Parallel()

	specPath := filepath.Join(t.TempDir(), "openapi.json")
	spec := `{
	  "openapi": "3.0.3",
	  "info": { "title": "Widgets API", "version": "1.0.0" },
	  "servers": [{ "url": "https://example.invalid" }],
	  "components": {
	    "securitySchemes": {
	      "apiKeyAuth": {
	        "type": "apiKey",
	        "in": "header",
	        "name": "X-API-Key"
	      }
	    },
	    "schemas": {
	      "WidgetResponse": {
	        "type": "object",
	        "properties": {
	          "ok": { "type": "boolean" }
	        },
	        "required": ["ok"]
	      }
	    }
	  },
	  "paths": {
	    "/widgets/{id}": {
	      "get": {
	        "summary": "Get a widget",
	        "security": [{ "apiKeyAuth": [] }],
	        "parameters": [
	          { "name": "id", "in": "path", "required": true, "schema": { "type": "string" } },
	          { "name": "id", "in": "query", "required": false, "schema": { "type": "string" } }
	        ],
	        "responses": {
	          "200": {
	            "description": "ok",
	            "content": {
	              "application/json": {
	                "schema": { "$ref": "#/components/schemas/WidgetResponse" }
	              }
	            }
	          }
	        }
	      }
	    }
	  }
	}`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "out")
	if err := Generate(Options{
		SpecPath:  specPath,
		OutputDir: outputDir,
		Name:      "widgets",
		Build:     true,
	}); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	binary := filepath.Join(outputDir, "bin", "widgets")

	stdout, stderr, err := runCLI(t, outputDir, nil, binary, "operations", "--filter", "widgets.get")
	if err != nil {
		t.Fatalf("operations failed: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"id": "widgets.get"`) {
		t.Fatalf("operations output did not include widgets.get: %s", stdout)
	}

	stdout, stderr, err = runCLI(t, outputDir, nil, binary, "example", "widgets.get", "--kind", "params")
	if err != nil {
		t.Fatalf("example failed: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"path"`) || !strings.Contains(stdout, `"query"`) {
		t.Fatalf("example output did not include grouped params: %s", stdout)
	}

	_, stderr, err = runCLI(t, outputDir, nil, binary, "call", "widgets.get", "--base-url", "https://example.com", "--params", `{"id":"123"}`, "--dry-run")
	if err == nil {
		t.Fatalf("expected ambiguous flat parameter input to fail")
	}
	if !strings.Contains(stderr, `"code": "ambiguous_parameter"`) {
		t.Fatalf("expected ambiguous_parameter error, got: %s", stderr)
	}

	stdout, stderr, err = runCLI(t, outputDir, nil, binary, "call", "widgets.get", "--base-url", "https://example.com", "--params", `{"path":{"id":"123"},"query":{"id":"external"}}`, "--dry-run")
	if err != nil {
		t.Fatalf("dry-run failed: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"url": "https://example.com/widgets/123?id=external"`) {
		t.Fatalf("dry-run URL was wrong: %s", stdout)
	}

	_, stderr, err = runCLI(t, outputDir, nil, binary, "call", "widgets.get", "--base-url", "https://example.com", "--params", `{"path":{"id":"123"},"query":{"id":"external"}}`)
	if err == nil {
		t.Fatalf("expected missing auth to fail before network")
	}
	if !strings.Contains(stderr, `"code": "missing_auth"`) {
		t.Fatalf("expected missing_auth error, got: %s", stderr)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/widgets/123" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("id") != "external" {
			t.Errorf("unexpected query id: %s", r.URL.Query().Get("id"))
		}
		if r.Header.Get("X-API-Key") != "secret-token" {
			t.Errorf("missing API key header: %q", r.Header.Get("X-API-Key"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	stdout, stderr, err = runCLI(t, outputDir, []string{"WIDGETS_API_KEY=secret-token"}, binary, "call", "widgets.get", "--base-url", server.URL, "--params", `{"path":{"id":"123"},"query":{"id":"external"}}`)
	if err != nil {
		t.Fatalf("authenticated call failed: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"ok": true`) {
		t.Fatalf("expected response body in output, got: %s", stdout)
	}
}

func TestGenerateExpandsServerVariableDefaults(t *testing.T) {
	t.Parallel()

	specPath := filepath.Join(t.TempDir(), "openapi.json")
	spec := `{
	  "openapi": "3.0.3",
	  "info": { "title": "Templated API", "version": "1.0.0" },
	  "servers": [{
	    "url": "https://{subdomain}.example.com/{version}",
	    "variables": {
	      "subdomain": { "default": "api" },
	      "version": { "default": "v1" }
	    }
	  }],
	  "paths": {
	    "/ping": {
	      "get": {
	        "operationId": "ping.get",
	        "responses": {
	          "200": {
	            "description": "ok"
	          }
	        }
	      }
	    }
	  }
	}`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "out")
	if err := Generate(Options{
		SpecPath:  specPath,
		OutputDir: outputDir,
		Name:      "templated",
		Build:     true,
	}); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	binary := filepath.Join(outputDir, "bin", "templated")
	stdout, stderr, err := runCLI(t, outputDir, nil, binary, "call", "ping.get", "--dry-run")
	if err != nil {
		t.Fatalf("dry-run failed: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"url": "https://api.example.com/v1/ping"`) {
		t.Fatalf("unexpected dry-run URL: %s", stdout)
	}
}

func TestGenerateUsesOAuthClientCredentialsFromSpec(t *testing.T) {
	t.Parallel()

	tokenHits := 0
	apiHits := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			tokenHits++
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if got := r.Form.Get("grant_type"); got != "client_credentials" {
				t.Fatalf("unexpected grant_type: %q", got)
			}
			if got := r.Form.Get("client_id"); got != "client-id" {
				t.Fatalf("unexpected client_id: %q", got)
			}
			if got := r.Form.Get("client_secret"); got != "client-secret" {
				t.Fatalf("unexpected client_secret: %q", got)
			}
			if got := r.Form.Get("scope"); got != "read:items" {
				t.Fatalf("unexpected scope: %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"opaque-access-token","token_type":"bearer","scope":"read:items","expires_in":3600}`))
		case "/items":
			apiHits++
			if got := r.Header.Get("Authorization"); got != "Bearer opaque-access-token" {
				t.Fatalf("unexpected Authorization header: %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	specPath := filepath.Join(t.TempDir(), "openapi.json")
	spec := `{
	  "openapi": "3.0.3",
	  "info": { "title": "OAuth API", "version": "1.0.0" },
	  "servers": [{ "url": "` + server.URL + `" }],
	  "components": {
	    "securitySchemes": {
	      "oauth": {
	        "type": "oauth2",
	        "flows": {
	          "clientCredentials": {
	            "tokenUrl": "` + server.URL + `/oauth/token",
	            "scopes": {
	              "read:items": "Read items"
	            }
	          }
	        }
	      }
	    }
	  },
	  "paths": {
	    "/items": {
	      "get": {
	        "operationId": "items.list",
	        "security": [{ "oauth": ["read:items"] }],
	        "responses": {
	          "200": {
	            "description": "ok",
	            "content": {
	              "application/json": {
	                "schema": {
	                  "type": "object",
	                  "properties": {
	                    "ok": { "type": "boolean" }
	                  }
	                }
	              }
	            }
	          }
	        }
	      }
	    }
	  }
	}`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "out")
	if err := Generate(Options{
		SpecPath:  specPath,
		OutputDir: outputDir,
		Name:      "oauthapi",
		Build:     true,
	}); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	binary := filepath.Join(outputDir, "bin", "oauthapi")
	stdout, stderr, err := runCLI(t, outputDir, []string{
		"OAUTHAPI_CLIENT_ID=client-id",
		"OAUTHAPI_CLIENT_SECRET=client-secret",
	}, binary, "call", "items.list")
	if err != nil {
		t.Fatalf("call failed: %v\nstderr=%s", err, stderr)
	}
	if tokenHits != 1 || apiHits != 1 {
		t.Fatalf("expected one token request and one API request, got tokenHits=%d apiHits=%d", tokenHits, apiHits)
	}
	if !strings.Contains(stdout, `"ok": true`) {
		t.Fatalf("expected response body in output, got: %s", stdout)
	}
}

func TestGeneratePreflightsMissingScopesFromBearerClientCredentials(t *testing.T) {
	t.Parallel()

	tokenHits := 0
	apiHits := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			tokenHits++
			w.Header().Set("Content-Type", "application/json")
			token := unsignedJWT(map[string]any{"scope": "write:items"})
			_, _ = w.Write([]byte(`{"access_token":"` + token + `","token_type":"bearer","expires_in":3600}`))
		case "/items":
			apiHits++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	specPath := filepath.Join(t.TempDir(), "openapi.json")
	spec := `{
	  "openapi": "3.0.3",
	  "info": { "title": "Bearer API", "version": "1.0.0" },
	  "servers": [{ "url": "` + server.URL + `" }],
	  "components": {
	    "securitySchemes": {
	      "bearerAuth": {
	        "type": "http",
	        "scheme": "bearer"
	      }
	    }
	  },
	  "paths": {
	    "/items": {
	      "get": {
	        "operationId": "items.list",
	        "security": [{ "bearerAuth": [] }],
	        "x-scope": "read:items",
	        "responses": {
	          "200": { "description": "ok" }
	        }
	      }
	    }
	  }
	}`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "out")
	if err := Generate(Options{
		SpecPath:  specPath,
		OutputDir: outputDir,
		Name:      "bearerapi",
		Build:     true,
	}); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	binary := filepath.Join(outputDir, "bin", "bearerapi")
	_, stderr, err := runCLI(t, outputDir, []string{
		"BEARERAPI_TOKEN_URL=" + server.URL + "/oauth/token",
		"BEARERAPI_CLIENT_ID=client-id",
		"BEARERAPI_CLIENT_SECRET=client-secret",
	}, binary, "call", "items.list")
	if err == nil {
		t.Fatalf("expected missing_scope failure")
	}
	if !strings.Contains(stderr, `"code": "missing_scope"`) {
		t.Fatalf("expected missing_scope error, got: %s", stderr)
	}
	if !strings.Contains(stderr, `read:items`) {
		t.Fatalf("expected required scope in error, got: %s", stderr)
	}
	if tokenHits != 1 {
		t.Fatalf("expected one token request, got %d", tokenHits)
	}
	if apiHits != 0 {
		t.Fatalf("expected API request to be blocked before send, got %d", apiHits)
	}
}

func TestGenerateUsesServerVariableEnv(t *testing.T) {
	t.Parallel()

	specPath := filepath.Join(t.TempDir(), "openapi.json")
	spec := `{
	  "openapi": "3.0.3",
	  "info": { "title": "Templated Host API", "version": "1.0.0" },
	  "servers": [{
	    "url": "http://{host}",
	    "variables": {
	      "host": {
	        "default": "127.0.0.1:80",
	        "description": "API host and port."
	      }
	    }
	  }],
	  "paths": {
	    "/ping": {
	      "get": {
	        "operationId": "ping.get",
	        "responses": {
	          "200": {
	            "description": "ok",
	            "content": {
	              "application/json": {
	                "schema": {
	                  "type": "object",
	                  "properties": {
	                    "ok": { "type": "boolean" }
	                  },
	                  "required": ["ok"]
	                }
	              }
	            }
	          }
	        }
	      }
	    }
	  }
	}`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "out")
	if err := Generate(Options{
		SpecPath:  specPath,
		OutputDir: outputDir,
		Name:      "templated",
		Build:     true,
	}); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ping" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}

	binary := filepath.Join(outputDir, "bin", "templated")
	stdout, stderr, err := runCLI(t, outputDir, []string{"TEMPLATED_SERVER_HOST=" + parsed.Host}, binary, "call", "ping.get")
	if err != nil {
		t.Fatalf("call failed: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"ok": true`) {
		t.Fatalf("expected response body in output, got: %s", stdout)
	}
}

func TestGenerateStripsBrokenDiscriminatorMappingsFromRuntimeSpec(t *testing.T) {
	t.Parallel()

	specPath := filepath.Join(t.TempDir(), "openapi.json")
	spec := `{
	  "openapi": "3.1.0",
	  "info": { "title": "Broken Discriminator API", "version": "1.0.0" },
	  "paths": {
	    "/redirect-uris": {
	      "post": {
	        "operationId": "callbacks.create",
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
	}`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "out")
	if err := Generate(Options{
		SpecPath:  specPath,
		OutputDir: outputDir,
		Name:      "callbacks",
		Build:     true,
	}); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	generatedSpec, err := os.ReadFile(filepath.Join(outputDir, "openapi.json"))
	if err != nil {
		t.Fatalf("read generated spec: %v", err)
	}
	if strings.Contains(string(generatedSpec), `"discriminator"`) {
		t.Fatalf("expected broken discriminator to be stripped from generated spec: %s", string(generatedSpec))
	}

	binary := filepath.Join(outputDir, "bin", "callbacks")
	stdout, stderr, err := runCLI(t, outputDir, nil, binary, "call", "callbacks.create", "--base-url", "https://example.com", "--body", `{"url":"http://localhost:3002/callback","platform":"web"}`, "--dry-run")
	if err != nil {
		t.Fatalf("dry-run failed: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"url": "https://example.com/redirect-uris"`) {
		t.Fatalf("unexpected dry-run output: %s", stdout)
	}
}

func TestGenerateExecosSpecSmoke(t *testing.T) {
	execosSpecPath := "/Users/jeff/Sites/execos/apps/api/openapi.json"
	if _, err := os.Stat(execosSpecPath); err != nil {
		t.Skip("execos spec not available")
	}

	outputDir := filepath.Join(t.TempDir(), "out")
	if err := Generate(Options{
		SpecPath:  execosSpecPath,
		OutputDir: outputDir,
		Name:      "execos",
		Build:     true,
	}); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	binary := filepath.Join(outputDir, "bin", "execos")

	stdout, stderr, err := runCLI(t, outputDir, nil, binary, "operations", "--filter", "tasks")
	if err != nil {
		t.Fatalf("operations failed: %v\nstderr=%s", err, stderr)
	}
	var operationsPayload map[string]any
	if err := json.Unmarshal([]byte(stdout), &operationsPayload); err != nil {
		t.Fatalf("failed to parse operations output: %v", err)
	}
	operations, _ := operationsPayload["operations"].([]any)
	if len(operations) == 0 {
		t.Fatalf("expected task operations in execos output")
	}

	stdout, stderr, err = runCLI(t, outputDir, nil, binary, "schema", "tasks.create")
	if err != nil {
		t.Fatalf("schema failed: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"requestBody"`) || !strings.Contains(stdout, `"rawInput"`) {
		t.Fatalf("schema output did not include task body details: %s", stdout)
	}

	stdout, stderr, err = runCLI(t, outputDir, nil, binary, "call", "tasks.list", "--base-url", "http://localhost:3000", "--params", `{"query":{"status":["todo"],"includeLinks":"true"}}`, "--dry-run")
	if err != nil {
		t.Fatalf("dry-run failed: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `/tasks?includeLinks=true`) || !strings.Contains(stdout, `status=todo`) {
		t.Fatalf("unexpected execos dry-run URL: %s", stdout)
	}
}

func TestGenerateSupportsQueryAPIKeyAuth(t *testing.T) {
	t.Parallel()

	specPath := filepath.Join(t.TempDir(), "openapi.json")
	spec := `{
	  "openapi": "3.0.3",
	  "info": { "title": "Query Auth API", "version": "1.0.0" },
	  "components": {
	    "securitySchemes": {
	      "apiKeyAuth": {
	        "type": "apiKey",
	        "in": "query",
	        "name": "api_key"
	      }
	    },
	    "schemas": {
	      "Ok": {
	        "type": "object",
	        "properties": {
	          "ok": { "type": "boolean" }
	        },
	        "required": ["ok"]
	      }
	    }
	  },
	  "paths": {
	    "/secure": {
	      "get": {
	        "operationId": "secure.get",
	        "security": [{ "apiKeyAuth": [] }],
	        "responses": {
	          "200": {
	            "description": "ok",
	            "content": {
	              "application/json": {
	                "schema": { "$ref": "#/components/schemas/Ok" }
	              }
	            }
	          }
	        }
	      }
	    }
	  }
	}`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "out")
	if err := Generate(Options{
		SpecPath:  specPath,
		OutputDir: outputDir,
		Name:      "queryauth",
		Build:     true,
	}); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	binary := filepath.Join(outputDir, "bin", "queryauth")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api_key") != "secret-token" {
			t.Errorf("missing api_key query value: %q", r.URL.Query().Get("api_key"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	stdout, stderr, err := runCLI(t, outputDir, []string{"QUERYAUTH_API_KEY=secret-token"}, binary, "call", "secure.get", "--base-url", server.URL)
	if err != nil {
		t.Fatalf("query-auth call failed: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"ok": true`) {
		t.Fatalf("expected successful response, got: %s", stdout)
	}
}

func TestGenerateSupportsRuntimeAuthOverrides(t *testing.T) {
	t.Parallel()

	specPath := filepath.Join(t.TempDir(), "openapi.json")
	spec := `{
	  "openapi": "3.0.3",
	  "info": { "title": "Override Auth API", "version": "1.0.0" },
	  "paths": {
	    "/secure": {
	      "get": {
	        "operationId": "secure.get",
	        "responses": {
	          "200": {
	            "description": "ok",
	            "content": {
	              "application/json": {
	                "schema": {
	                  "type": "object",
	                  "properties": {
	                    "ok": { "type": "boolean" }
	                  },
	                  "required": ["ok"]
	                }
	              }
	            }
	          }
	        }
	      }
	    }
	  }
	}`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "out")
	if err := Generate(Options{
		SpecPath:  specPath,
		OutputDir: outputDir,
		Name:      "overrideauth",
		Build:     true,
	}); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	binary := filepath.Join(outputDir, "bin", "overrideauth")
	overrides := `{"auth":{"headers":[{"name":"Authorization","env":"OVERRIDEAUTH_TOKEN","prefix":"Bearer ","required":true,"secret":true,"description":"Bearer token"}]}}`

	stdout, stderr, err := runCLI(t, outputDir, []string{"OVERRIDEAUTH_OVERRIDES_JSON=" + overrides}, binary, "auth")
	if err != nil {
		t.Fatalf("auth failed: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"overridesJsonEnv": "OVERRIDEAUTH_OVERRIDES_JSON"`) || !strings.Contains(stdout, `"Authorization"`) {
		t.Fatalf("auth output did not include override metadata: %s", stdout)
	}

	_, stderr, err = runCLI(t, outputDir, []string{"OVERRIDEAUTH_OVERRIDES_JSON=" + overrides}, binary, "call", "secure.get", "--base-url", "https://example.com")
	if err == nil {
		t.Fatalf("expected missing override auth to fail")
	}
	if !strings.Contains(stderr, `"code": "missing_auth"`) {
		t.Fatalf("expected missing_auth error, got: %s", stderr)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret-token" {
			t.Errorf("unexpected Authorization header: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	stdout, stderr, err = runCLI(t, outputDir, []string{
		"OVERRIDEAUTH_OVERRIDES_JSON=" + overrides,
		"OVERRIDEAUTH_TOKEN=secret-token",
	}, binary, "call", "secure.get", "--base-url", server.URL)
	if err != nil {
		t.Fatalf("override-auth call failed: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"ok": true`) {
		t.Fatalf("expected successful override-auth response, got: %s", stdout)
	}
}

func TestGenerateSupportsConditionalRuntimeRequirements(t *testing.T) {
	t.Parallel()

	specPath := filepath.Join(t.TempDir(), "openapi.json")
	spec := `{
	  "openapi": "3.0.3",
	  "info": { "title": "Conditional API", "version": "1.0.0" },
	  "paths": {
	    "/machines/wait": {
	      "get": {
	        "operationId": "GetMachineWait",
	        "parameters": [
	          { "name": "state", "in": "query", "schema": { "type": "string" } },
	          { "name": "instance_id", "in": "query", "schema": { "type": "string" } }
	        ],
	        "responses": {
	          "200": {
	            "description": "ok",
	            "content": {
	              "application/json": {
	                "schema": {
	                  "type": "object",
	                  "properties": {
	                    "ok": { "type": "boolean" }
	                  }
	                }
	              }
	            }
	          }
	        }
	      }
	    }
	  }
	}`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "out")
	if err := Generate(Options{
		SpecPath:  specPath,
		OutputDir: outputDir,
		Name:      "waitapi",
		Build:     true,
	}); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	binary := filepath.Join(outputDir, "bin", "waitapi")
	overrides := `{"operations":{"machines.wait":{"requirements":[{"when":[{"location":"query","name":"state","oneOf":["stopped","destroyed"]}],"require":[{"location":"query","name":"instance_id"}],"message":"query.instance_id is required when query.state is stopped or destroyed"}]}}}`

	_, stderr, err := runCLI(t, outputDir, []string{"WAITAPI_OVERRIDES_JSON=" + overrides}, binary, "call", "machines.wait", "--base-url", "https://example.com", "--params", `{"query":{"state":"stopped"}}`, "--dry-run")
	if err == nil {
		t.Fatalf("expected conditional requirement to fail")
	}
	if !strings.Contains(stderr, `"code": "validation_failed"`) || !strings.Contains(stderr, `query.instance_id is required when query.state is stopped or destroyed`) {
		t.Fatalf("expected conditional requirement error, got: %s", stderr)
	}

	stdout, stderr, err := runCLI(t, outputDir, []string{"WAITAPI_OVERRIDES_JSON=" + overrides}, binary, "call", "machines.wait", "--base-url", "https://example.com", "--params", `{"query":{"state":"stopped","instance_id":"01HXYZ"}}`, "--dry-run")
	if err != nil {
		t.Fatalf("expected conditional requirement with instance_id to pass: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"operation": "GetMachineWait"`) || !strings.Contains(stdout, `instance_id=01HXYZ`) || !strings.Contains(stdout, `state=stopped`) {
		t.Fatalf("unexpected dry-run output: %s", stdout)
	}
}

func TestGenerateKeepsPaginationTokensInQueryBlock(t *testing.T) {
	t.Parallel()

	specPath := filepath.Join(t.TempDir(), "openapi.json")
	spec := `{
	  "openapi": "3.0.3",
	  "info": { "title": "Cursor API", "version": "1.0.0" },
	  "paths": {
	    "/items": {
	      "get": {
	        "operationId": "items.list",
	        "parameters": [
	          { "name": "cursor", "in": "query", "schema": { "type": "string" } },
	          { "name": "cursor", "in": "header", "schema": { "type": "string" } }
	        ],
	        "responses": {
	          "200": {
	            "description": "ok",
	            "content": {
	              "application/json": {
	                "schema": {
	                  "type": "object",
	                  "properties": {
	                    "next_cursor": { "type": "string" },
	                    "items": {
	                      "type": "array",
	                      "items": { "type": "integer" }
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
	}`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "out")
	if err := Generate(Options{
		SpecPath:  specPath,
		OutputDir: outputDir,
		Name:      "cursorapi",
		Build:     true,
	}); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	binary := filepath.Join(outputDir, "bin", "cursorapi")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("cursor") {
		case "":
			_, _ = w.Write([]byte(`{"next_cursor":"next-1","items":[1]}`))
		case "next-1":
			_, _ = w.Write([]byte(`{"items":[2]}`))
		default:
			t.Fatalf("unexpected cursor value: %q", r.URL.Query().Get("cursor"))
		}
	}))
	defer server.Close()

	stdout, stderr, err := runCLI(t, outputDir, nil, binary, "call", "items.list", "--base-url", server.URL, "--page-all", "--page-limit", "2")
	if err != nil {
		t.Fatalf("page-all failed: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"items":[1]`) || !strings.Contains(stdout, `"items":[2]`) {
		t.Fatalf("expected both paginated responses, got: %s", stdout)
	}
}

func TestGenerateSupportsAliasesAndPrioritizedParamExamples(t *testing.T) {
	t.Parallel()

	specPath := filepath.Join(t.TempDir(), "openapi.json")
	spec := `{
	  "openapi": "3.0.3",
	  "info": { "title": "Alias API", "version": "1.0.0" },
	  "servers": [{ "url": "https://example.invalid" }],
	  "paths": {
	    "/users": {
	      "get": {
	        "operationId": "GetUserList",
	        "parameters": [
	          { "name": "email_address", "in": "query", "schema": { "type": "array", "items": { "type": "string" } } },
	          { "name": "external_id", "in": "query", "schema": { "type": "array", "items": { "type": "string" } } },
	          { "name": "phone_number", "in": "query", "schema": { "type": "array", "items": { "type": "string" } } },
	          { "name": "user_id", "in": "query", "schema": { "type": "array", "items": { "type": "string" } } },
	          { "name": "username", "in": "query", "schema": { "type": "array", "items": { "type": "string" } } },
	          { "name": "web3_wallet", "in": "query", "schema": { "type": "array", "items": { "type": "string" } } },
	          { "name": "limit", "in": "query", "schema": { "type": "integer", "default": 10 } },
	          { "name": "order_by", "in": "query", "schema": { "type": "string", "default": "-created_at" } }
	        ],
	        "responses": {
	          "200": {
	            "description": "ok",
	            "content": {
	              "application/json": {
	                "schema": {
	                  "type": "object",
	                  "properties": {
	                    "ok": { "type": "boolean" }
	                  }
	                }
	              }
	            }
	          }
	        }
	      }
	    }
	  }
	}`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "out")
	if err := Generate(Options{
		SpecPath:  specPath,
		OutputDir: outputDir,
		Name:      "aliasapi",
		Build:     true,
	}); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	binary := filepath.Join(outputDir, "bin", "aliasapi")

	stdout, stderr, err := runCLI(t, outputDir, nil, binary, "operations", "--filter", "users.list")
	if err != nil {
		t.Fatalf("operations filter by alias failed: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"id": "GetUserList"`) || !strings.Contains(stdout, `"aliases": [`) || !strings.Contains(stdout, `"users.list"`) {
		t.Fatalf("operations output did not include alias metadata: %s", stdout)
	}

	stdout, stderr, err = runCLI(t, outputDir, nil, binary, "schema", "users.list")
	if err != nil {
		t.Fatalf("schema by alias failed: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"id": "GetUserList"`) {
		t.Fatalf("schema output did not resolve canonical operation: %s", stdout)
	}

	stdout, stderr, err = runCLI(t, outputDir, nil, binary, "example", "users.list", "--kind", "params")
	if err != nil {
		t.Fatalf("example by alias failed: %v\nstderr=%s", err, stderr)
	}
	var payload struct {
		Result map[string]map[string]any `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("failed to parse example output: %v", err)
	}
	query := payload.Result["query"]
	if _, ok := query["limit"]; !ok {
		t.Fatalf("expected prioritized params example to include limit, got: %s", stdout)
	}
	if _, ok := query["order_by"]; !ok {
		t.Fatalf("expected prioritized params example to include order_by, got: %s", stdout)
	}

	stdout, stderr, err = runCLI(t, outputDir, nil, binary, "call", "users.list", "--base-url", "https://example.com", "--params", `{"query":{"limit":2}}`, "--dry-run")
	if err != nil {
		t.Fatalf("call by alias failed: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"operation": "GetUserList"`) || !strings.Contains(stdout, `"url": "https://example.com/users?limit=2"`) {
		t.Fatalf("unexpected dry-run output for alias call: %s", stdout)
	}
}

func TestGenerateSupportsSwagger2Specs(t *testing.T) {
	t.Parallel()

	specPath := filepath.Join(t.TempDir(), "swagger.json")
	spec := `{
	  "swagger": "2.0",
	  "info": { "title": "Legacy API", "version": "1.0.0" },
	  "host": "example.invalid",
	  "basePath": "/v1",
	  "schemes": ["https"],
	  "securityDefinitions": {
	    "apiKeyAuth": {
	      "type": "apiKey",
	      "in": "header",
	      "name": "apikey"
	    }
	  },
	  "paths": {
	    "/items/{id}": {
	      "get": {
	        "operationId": "items.get",
	        "security": [{ "apiKeyAuth": [] }],
	        "parameters": [
	          { "name": "id", "in": "path", "required": true, "type": "string" }
	        ],
	        "responses": {
	          "200": {
	            "description": "ok",
	            "schema": {
	              "type": "object",
	              "required": ["ok"],
	              "properties": {
	                "ok": { "type": "boolean" }
	              }
	            }
	          }
	        }
	      }
	    }
	  }
	}`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "out")
	if err := Generate(Options{
		SpecPath:  specPath,
		OutputDir: outputDir,
		Name:      "legacy",
		Build:     true,
	}); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	binary := filepath.Join(outputDir, "bin", "legacy")

	stdout, stderr, err := runCLI(t, outputDir, nil, binary, "operations", "--filter", "items.get")
	if err != nil {
		t.Fatalf("operations failed: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"id": "items.get"`) {
		t.Fatalf("operations output did not include items.get: %s", stdout)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/items/123" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("apikey") != "secret-token" {
			t.Errorf("missing apikey header: %q", r.Header.Get("apikey"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	stdout, stderr, err = runCLI(t, outputDir, []string{"LEGACY_API_KEY=secret-token"}, binary, "call", "items.get", "--base-url", server.URL+"/v1", "--params", `{"path":{"id":"123"}}`)
	if err != nil {
		t.Fatalf("call failed: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"ok": true`) {
		t.Fatalf("expected response body in output, got: %s", stdout)
	}
}

func TestGenerateSupportsHTTPBasicAuth(t *testing.T) {
	t.Parallel()

	specPath := filepath.Join(t.TempDir(), "openapi.json")
	spec := `{
	  "openapi": "3.0.3",
	  "info": { "title": "Basic API", "version": "1.0.0" },
	  "servers": [{ "url": "https://example.invalid" }],
	  "components": {
	    "securitySchemes": {
	      "credentials": {
	        "type": "http",
	        "scheme": "basic"
	      }
	    }
	  },
	  "paths": {
	    "/whoami": {
	      "get": {
	        "operationId": "whoami.get",
	        "security": [{ "credentials": [] }],
	        "responses": {
	          "200": {
	            "description": "ok",
	            "content": {
	              "application/json": {
	                "schema": {
	                  "type": "object",
	                  "properties": {
	                    "ok": { "type": "boolean" }
	                  },
	                  "required": ["ok"]
	                }
	              }
	            }
	          }
	        }
	      }
	    }
	  }
	}`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "out")
	if err := Generate(Options{
		SpecPath:  specPath,
		OutputDir: outputDir,
		Name:      "basicauthapi",
		Build:     true,
	}); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	binary := filepath.Join(outputDir, "bin", "basicauthapi")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte("site-id:secret-key"))
		if r.Header.Get("Authorization") != want {
			t.Errorf("unexpected Authorization header: got %q want %q", r.Header.Get("Authorization"), want)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	stdout, stderr, err := runCLI(t, outputDir, []string{"BASICAUTHAPI_CREDENTIALS=site-id:secret-key"}, binary, "call", "whoami.get", "--base-url", server.URL)
	if err != nil {
		t.Fatalf("basic-auth call failed: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"ok": true`) {
		t.Fatalf("expected response body in output, got: %s", stdout)
	}
}

func TestGenerateNormalizesAmbiguousScalarOneOfParameters(t *testing.T) {
	t.Parallel()

	specPath := filepath.Join(t.TempDir(), "openapi.json")
	spec := `{
	  "openapi": "3.0.3",
	  "info": { "title": "Identifiers API", "version": "1.0.0" },
	  "servers": [{ "url": "https://example.invalid" }],
	  "paths": {
	    "/customers/{identifier}": {
	      "get": {
	        "operationId": "customers.get",
	        "parameters": [
	          {
	            "name": "identifier",
	            "in": "path",
	            "required": true,
	            "schema": {
	              "oneOf": [
	                { "type": "string", "description": "Any identifier" },
	                { "type": "string", "description": "Email address" }
	              ]
	            }
	          }
	        ],
	        "responses": {
	          "200": {
	            "description": "ok",
	            "content": {
	              "application/json": {
	                "schema": {
	                  "type": "object",
	                  "properties": {
	                    "ok": { "type": "boolean" }
	                  },
	                  "required": ["ok"]
	                }
	              }
	            }
	          }
	        }
	      }
	    }
	  }
	}`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "out")
	if err := Generate(Options{
		SpecPath:  specPath,
		OutputDir: outputDir,
		Name:      "identifierapi",
		Build:     true,
	}); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	binary := filepath.Join(outputDir, "bin", "identifierapi")
	stdout, stderr, err := runCLI(t, outputDir, nil, binary, "call", "customers.get", "--base-url", "https://example.com", "--params", `{"path":{"identifier":"agent-cli-generator@example.invalid"}}`, "--dry-run")
	if err != nil {
		t.Fatalf("expected ambiguous scalar oneOf param to validate: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"url": "https://example.com/customers/agent-cli-generator@example.invalid"`) {
		t.Fatalf("unexpected dry-run output: %s", stdout)
	}
}

func TestGenerateNormalizesSchemaExamplesArrays(t *testing.T) {
	t.Parallel()

	specPath := filepath.Join(t.TempDir(), "openapi.json")
	spec := `{
	  "openapi": "3.1.0",
	  "info": { "title": "Examples API", "version": "1.0.0" },
	  "paths": {
	    "/items": {
	      "get": {
	        "operationId": "items.list",
	        "responses": {
	          "200": {
	            "description": "ok",
	            "content": {
	              "application/json": {
	                "schema": {
	                  "type": "object",
	                  "properties": {
	                    "total_count": {
	                      "type": "integer",
	                      "examples": [2]
	                    },
	                    "items": {
	                      "type": "array",
	                      "items": {
	                        "type": "object",
	                        "properties": {
	                          "name": {
	                            "type": "string",
	                            "examples": ["widget"]
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
	  }
	}`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "out")
	if err := Generate(Options{
		SpecPath:  specPath,
		OutputDir: outputDir,
		Name:      "examplesapi",
		Build:     true,
	}); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	binary := filepath.Join(outputDir, "bin", "examplesapi")

	stdout, stderr, err := runCLI(t, outputDir, nil, binary, "example", "items.list", "--kind", "response")
	if err != nil {
		t.Fatalf("example failed: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"total_count": 2`) || !strings.Contains(stdout, `"name": "widget"`) {
		t.Fatalf("expected normalized examples to drive response example, got: %s", stdout)
	}
}

func TestGenerateAddsItemsForArrayUnionSchemas(t *testing.T) {
	t.Parallel()

	specPath := filepath.Join(t.TempDir(), "openapi.json")
	spec := `{
	  "openapi": "3.1.0",
	  "info": { "title": "Array Union API", "version": "1.0.0" },
	  "paths": {
	    "/properties": {
	      "get": {
	        "operationId": "properties.get",
	        "responses": {
	          "200": {
	            "description": "ok",
	            "content": {
	              "application/json": {
	                "schema": {
	                  "type": "object",
	                  "properties": {
	                    "default_value": {
	                      "type": ["null", "string", "array"],
	                      "oneOf": [
	                        { "type": "string" },
	                        { "type": "array", "items": { "type": "string" } }
	                      ]
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
	}`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "out")
	if err := Generate(Options{
		SpecPath:  specPath,
		OutputDir: outputDir,
		Name:      "arrayunionapi",
		Build:     true,
	}); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	binary := filepath.Join(outputDir, "bin", "arrayunionapi")

	stdout, stderr, err := runCLI(t, outputDir, nil, binary, "schema", "properties.get")
	if err != nil {
		t.Fatalf("schema failed: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"default_value"`) {
		t.Fatalf("expected schema output to include default_value, got: %s", stdout)
	}
}

func TestGenerateSupportsRemoteSpecsWithRelativeRefs(t *testing.T) {
	t.Parallel()

	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/openapi.yaml":
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write([]byte(`
openapi: 3.0.3
info:
  title: Remote Ref API
  version: 1.0.0
servers:
  - url: ` + serverURL + `
paths:
  /ping:
    get:
      operationId: ping.get
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                $ref: './components.yaml#/components/schemas/Ok'
`))
		case "/components.yaml":
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write([]byte(`
components:
  schemas:
    Ok:
      type: object
      required:
        - ok
      properties:
        ok:
          type: boolean
`))
		case "/ping":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	outputDir := filepath.Join(t.TempDir(), "out")
	if err := Generate(Options{
		SpecPath:  server.URL + "/openapi.yaml",
		OutputDir: outputDir,
		Name:      "remoteapi",
		Build:     true,
	}); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	binary := filepath.Join(outputDir, "bin", "remoteapi")

	stdout, stderr, err := runCLI(t, outputDir, nil, binary, "schema", "ping.get")
	if err != nil {
		t.Fatalf("schema failed: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"response"`) && !strings.Contains(stdout, `"responses"`) {
		t.Fatalf("expected schema output to include responses, got: %s", stdout)
	}

	stdout, stderr, err = runCLI(t, outputDir, nil, binary, "call", "ping.get")
	if err != nil {
		t.Fatalf("remote spec call failed: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"ok": true`) {
		t.Fatalf("expected successful response body, got: %s", stdout)
	}
}

func TestGenerateSupportsFileURLSpecs(t *testing.T) {
	t.Parallel()

	specDir := t.TempDir()
	specPath := filepath.Join(specDir, "file-url-spec.json")
	spec := `{
	  "openapi": "3.0.3",
	  "info": { "title": "File URL API", "version": "1.0.0" },
	  "paths": {
	    "/status": {
	      "get": {
	        "operationId": "status.get",
	        "responses": {
	          "200": {
	            "description": "ok",
	            "content": {
	              "application/json": {
	                "schema": {
	                  "type": "object",
	                  "properties": {
	                    "ok": { "type": "boolean" }
	                  }
	                }
	              }
	            }
	          }
	        }
	      }
	    }
	  }
	}`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "out")
	if err := Generate(Options{
		SpecPath:  "file://" + specPath,
		OutputDir: outputDir,
		Name:      "fileurlapi",
		Build:     true,
	}); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	binary := filepath.Join(outputDir, "bin", "fileurlapi")
	stdout, stderr, err := runCLI(t, outputDir, nil, binary, "operations", "--filter", "status.get")
	if err != nil {
		t.Fatalf("operations failed: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"id": "status.get"`) {
		t.Fatalf("expected status.get operation, got: %s", stdout)
	}
}

func TestGenerateStripsInvalidExamplesWhenTheyAreTheOnlyValidationFailure(t *testing.T) {
	t.Parallel()

	specPath := filepath.Join(t.TempDir(), "openapi.yaml")
	spec := `openapi: 3.0.3
info:
  title: Invalid Examples API
  version: 1.0.0
paths:
  /received:
    get:
      operationId: received.get
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/GetReceivedEmailResponse'
components:
  schemas:
    GetReceivedEmailResponse:
      type: object
      properties:
        created_at:
          type: string
          format: date-time
          example: '2023-10-06:23:47:56.678Z'
      required:
        - created_at
`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "out")
	if err := Generate(Options{
		SpecPath:  specPath,
		OutputDir: outputDir,
		Name:      "invalidexamples",
		Build:     true,
	}); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	binary := filepath.Join(outputDir, "bin", "invalidexamples")

	stdout, stderr, err := runCLI(t, outputDir, nil, binary, "example", "received.get", "--kind", "response")
	if err != nil {
		t.Fatalf("example failed: %v\nstderr=%s", err, stderr)
	}
	if strings.Contains(stdout, `2023-10-06:23:47:56.678Z`) {
		t.Fatalf("expected invalid schema example to be stripped, got: %s", stdout)
	}
	if !strings.Contains(stdout, `2026-01-02T15:04:05Z`) {
		t.Fatalf("expected synthesized date-time example after stripping invalid examples, got: %s", stdout)
	}
}

func TestGenerateStripsSchemaExamplesThatFailTypeValidation(t *testing.T) {
	t.Parallel()

	specPath := filepath.Join(t.TempDir(), "openapi.yaml")
	spec := `openapi: 3.1.0
info:
  title: Invalid Typed Examples API
  version: 1.0.0
paths:
  /runner:
    get:
      operationId: runner.get
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  image:
                    type: string
                    example: 20.04
                required:
                  - image
`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "out")
	if err := Generate(Options{
		SpecPath:  specPath,
		OutputDir: outputDir,
		Name:      "invalidtypedexamples",
		Build:     true,
	}); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	binary := filepath.Join(outputDir, "bin", "invalidtypedexamples")

	stdout, stderr, err := runCLI(t, outputDir, nil, binary, "example", "runner.get", "--kind", "response")
	if err != nil {
		t.Fatalf("example failed: %v\nstderr=%s", err, stderr)
	}
	if strings.Contains(stdout, `20.04`) {
		t.Fatalf("expected invalid typed schema example to be stripped, got: %s", stdout)
	}
	if !strings.Contains(stdout, `"image": "string"`) {
		t.Fatalf("expected synthesized string example after stripping invalid typed example, got: %s", stdout)
	}
}

func TestGenerateStripsInvalidDefaultsWhenNeeded(t *testing.T) {
	t.Parallel()

	specPath := filepath.Join(t.TempDir(), "openapi.yaml")
	spec := `openapi: 3.1.0
info:
  title: Invalid Defaults API
  version: 1.0.0
paths:
  /release:
    patch:
      operationId: release.patch
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                make_latest:
                  type: string
                  enum: ["true", "false", "legacy"]
                  default: true
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  ok:
                    type: boolean
                required:
                  - ok
`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "out")
	if err := Generate(Options{
		SpecPath:  specPath,
		OutputDir: outputDir,
		Name:      "invaliddefaults",
		Build:     true,
	}); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	binary := filepath.Join(outputDir, "bin", "invaliddefaults")

	stdout, stderr, err := runCLI(t, outputDir, nil, binary, "schema", "release.patch")
	if err != nil {
		t.Fatalf("schema failed: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"make_latest"`) {
		t.Fatalf("expected schema output to include make_latest, got: %s", stdout)
	}
}

func TestGenerateRelaxesPostgRESTRequestValidation(t *testing.T) {
	t.Parallel()

	specPath := filepath.Join(t.TempDir(), "openapi.json")
	spec := `{
	  "openapi": "3.0.3",
	  "info": { "title": "PostgREST API", "version": "1.0.0" },
	  "paths": {
	    "/tasks": {
	      "post": {
	        "operationId": "tasks.create",
	        "requestBody": {
	          "required": true,
	          "content": {
	            "application/json": {
	              "schema": {
	                "type": "object",
	                "required": ["id", "title", "created_at"],
	                "properties": {
	                  "id": {
	                    "type": "string",
	                    "format": "uuid",
	                    "description": "Primary key <pk/>"
	                  },
	                  "title": {
	                    "type": "string"
	                  },
	                  "created_at": {
	                    "type": "string",
	                    "format": "date-time",
	                    "default": "2026-01-02T15:04:05Z"
	                  }
	                }
	              }
	            },
	            "application/vnd.pgrst.object+json": {
	              "schema": {
	                "type": "object",
	                "required": ["id", "title", "created_at"],
	                "properties": {
	                  "id": {
	                    "type": "string",
	                    "format": "uuid",
	                    "description": "Primary key <pk/>"
	                  },
	                  "title": {
	                    "type": "string"
	                  },
	                  "created_at": {
	                    "type": "string",
	                    "format": "date-time",
	                    "default": "2026-01-02T15:04:05Z"
	                  }
	                }
	              }
	            }
	          }
	        },
	        "responses": {
	          "201": {
	            "description": "created",
	            "content": {
	              "application/json": {
	                "schema": {
	                  "type": "object",
	                  "properties": {
	                    "ok": { "type": "boolean" }
	                  },
	                  "required": ["ok"]
	                }
	              }
	            }
	          }
	        }
	      }
	    }
	  }
	}`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "out")
	if err := Generate(Options{
		SpecPath:  specPath,
		OutputDir: outputDir,
		Name:      "postgrest",
		Build:     true,
	}); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	binary := filepath.Join(outputDir, "bin", "postgrest")

	stdout, stderr, err := runCLI(t, outputDir, nil, binary, "example", "tasks.create", "--kind", "body")
	if err != nil {
		t.Fatalf("example failed: %v\nstderr=%s", err, stderr)
	}
	if strings.Contains(stdout, `"id"`) || strings.Contains(stdout, `"created_at"`) || !strings.Contains(stdout, `"title"`) {
		t.Fatalf("expected PostgREST body example to prefer user-supplied fields, got: %s", stdout)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tasks" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read request body: %v", err)
		}
		if !strings.Contains(string(body), `"title":"hello"`) {
			t.Errorf("unexpected request body: %s", string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	stdout, stderr, err = runCLI(t, outputDir, nil, binary, "call", "tasks.create", "--base-url", server.URL, "--body", `{"title":"hello"}`)
	if err != nil {
		t.Fatalf("call failed: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"ok": true`) {
		t.Fatalf("expected response body in output, got: %s", stdout)
	}
}

func TestGenerateWritesReleaseScaffoldingAndInstallSkill(t *testing.T) {
	t.Parallel()

	specPath := filepath.Join(t.TempDir(), "openapi.json")
	spec := `{
	  "openapi": "3.0.3",
	  "info": { "title": "Release API", "version": "1.0.0" },
	  "paths": {
	    "/ping": {
	      "get": {
	        "operationId": "ping.get",
	        "responses": {
	          "200": {
	            "description": "ok",
	            "content": {
	              "application/json": {
	                "schema": {
	                  "type": "object",
	                  "properties": {
	                    "ok": { "type": "boolean" }
	                  }
	                }
	              }
	            }
	          }
	        }
	      }
	    }
	  }
	}`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "out")
	if err := Generate(Options{
		SpecPath:    specPath,
		OutputDir:   outputDir,
		Name:        "releaseapi",
		ModuleName:  "github.com/acme/releaseapi",
		Publish:     "acme/releaseapi",
		HomebrewTap: "acme/homebrew-tap",
		Build:       true,
	}); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	for _, path := range []string{
		filepath.Join(outputDir, ".goreleaser.yaml"),
		filepath.Join(outputDir, "RELEASING.md"),
		filepath.Join(outputDir, "scripts", "install.sh"),
		filepath.Join(outputDir, "scripts", "install-skills.sh"),
		filepath.Join(outputDir, ".github", "workflows", "release.yml"),
		filepath.Join(outputDir, "skills", "releaseapi", "SKILL.md"),
		filepath.Join(outputDir, "skills", "releaseapi", "scripts", "ensure-cli.sh"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected generated file %s: %v", path, err)
		}
	}

	readmeBytes, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	if err != nil {
		t.Fatalf("read generated README: %v", err)
	}
	readme := string(readmeBytes)
	if !strings.Contains(readme, "curl -fsSL https://raw.githubusercontent.com/acme/releaseapi/main/scripts/install.sh | sh") {
		t.Fatalf("generated README missing install script command: %s", readme)
	}
	if !strings.Contains(readme, "npx skills add https://github.com/acme/releaseapi") {
		t.Fatalf("generated README missing skills add command: %s", readme)
	}
	if !strings.Contains(readme, "brew install acme/homebrew-tap/releaseapi") {
		t.Fatalf("generated README missing Homebrew install command: %s", readme)
	}

	goreleaserBytes, err := os.ReadFile(filepath.Join(outputDir, ".goreleaser.yaml"))
	if err != nil {
		t.Fatalf("read goreleaser config: %v", err)
	}
	goreleaser := string(goreleaserBytes)
	if !strings.Contains(goreleaser, "owner: acme") || !strings.Contains(goreleaser, "name: homebrew-tap") {
		t.Fatalf("generated goreleaser config missing brew repo metadata: %s", goreleaser)
	}
	if !strings.Contains(goreleaser, "darwin") || !strings.Contains(goreleaser, "linux") {
		t.Fatalf("generated goreleaser config missing target platforms: %s", goreleaser)
	}

	skillBytes, err := os.ReadFile(filepath.Join(outputDir, "skills", "releaseapi", "SKILL.md"))
	if err != nil {
		t.Fatalf("read skill: %v", err)
	}
	skill := string(skillBytes)
	if !strings.Contains(skill, "name: releaseapi\n") {
		t.Fatalf("skill missing name frontmatter: %s", skill)
	}
	if !strings.Contains(skill, "sh scripts/ensure-cli.sh") {
		t.Fatalf("skill missing ensure-cli bootstrap step: %s", skill)
	}
	if !strings.Contains(skill, "curl -fsSL https://raw.githubusercontent.com/acme/releaseapi/main/scripts/install.sh | sh") {
		t.Fatalf("skill missing installer command: %s", skill)
	}
	if !strings.Contains(skill, "brew install acme/homebrew-tap/releaseapi") {
		t.Fatalf("skill missing Homebrew command: %s", skill)
	}
	if !strings.Contains(skill, "## Usage") {
		t.Fatalf("skill missing usage section: %s", skill)
	}
	if !strings.Contains(skill, "schema-first flow") {
		t.Fatalf("skill missing schema-first workflow: %s", skill)
	}
	if !strings.Contains(skill, "## Auth") {
		t.Fatalf("skill missing auth section: %s", skill)
	}
	if !strings.Contains(skill, "## Operations") {
		t.Fatalf("skill missing operations section: %s", skill)
	}

	info, err := os.Stat(filepath.Join(outputDir, "scripts", "install.sh"))
	if err != nil {
		t.Fatalf("stat install script: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("expected install script to be executable, got mode %v", info.Mode())
	}

	installScriptBytes, err := os.ReadFile(filepath.Join(outputDir, "scripts", "install.sh"))
	if err != nil {
		t.Fatalf("read install script: %v", err)
	}
	installScript := string(installScriptBytes)
	if !strings.Contains(installScript, "missing required command:") {
		t.Fatalf("install script missing dependency checks: %s", installScript)
	}
	if !strings.Contains(installScript, "checksum entry not found") {
		t.Fatalf("install script missing checksum lookup error: %s", installScript)
	}

	skillScriptInfo, err := os.Stat(filepath.Join(outputDir, "scripts", "install-skills.sh"))
	if err != nil {
		t.Fatalf("stat skills install script: %v", err)
	}
	if skillScriptInfo.Mode().Perm()&0o111 == 0 {
		t.Fatalf("expected skills install script to be executable, got mode %v", skillScriptInfo.Mode())
	}

	skillScriptBytes, err := os.ReadFile(filepath.Join(outputDir, "scripts", "install-skills.sh"))
	if err != nil {
		t.Fatalf("read skills install script: %v", err)
	}
	skillScript := string(skillScriptBytes)
	if !strings.Contains(skillScript, "npx skills add") {
		t.Fatalf("skills install script missing skills add command: %s", skillScript)
	}
	if !strings.Contains(skillScript, `SKILL_NAME="releaseapi"`) {
		t.Fatalf("skills install script missing skill name: %s", skillScript)
	}

	ensureScriptBytes, err := os.ReadFile(filepath.Join(outputDir, "skills", "releaseapi", "scripts", "ensure-cli.sh"))
	if err != nil {
		t.Fatalf("read ensure-cli script: %v", err)
	}
	ensureScript := string(ensureScriptBytes)
	if !strings.Contains(ensureScript, "command -v \"$BINARY\"") {
		t.Fatalf("ensure-cli script missing PATH check: %s", ensureScript)
	}
	if !strings.Contains(ensureScript, "curl -fsSL https://raw.githubusercontent.com/acme/releaseapi/main/scripts/install.sh | sh") {
		t.Fatalf("ensure-cli script missing release installer: %s", ensureScript)
	}
}

func TestGenerateInfersReleaseRepoFromModule(t *testing.T) {
	t.Parallel()

	specPath := filepath.Join(t.TempDir(), "openapi.json")
	spec := `{
	  "openapi": "3.0.3",
	  "info": { "title": "Infer API", "version": "1.0.0" },
	  "paths": {
	    "/ping": {
	      "get": {
	        "operationId": "ping.get",
	        "responses": {
	          "200": { "description": "ok" }
	        }
	      }
	    }
	  }
	}`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "out")
	if err := Generate(Options{
		SpecPath:   specPath,
		OutputDir:  outputDir,
		Name:       "inferapi",
		ModuleName: "github.com/acme/inferapi",
	}); err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	readmeBytes, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	if err != nil {
		t.Fatalf("read generated README: %v", err)
	}
	if !strings.Contains(string(readmeBytes), "https://raw.githubusercontent.com/acme/inferapi/main/scripts/install.sh") {
		t.Fatalf("expected README to infer repo from module path, got: %s", string(readmeBytes))
	}
}

func runCLI(t *testing.T, dir string, env []string, binary string, args ...string) (string, string, error) {
	t.Helper()

	cmd := exec.Command(binary, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)

	stdout, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", string(exitErr.Stderr), err
		}
		return "", "", err
	}

	return string(stdout), "", nil
}

func unsignedJWT(claims map[string]any) string {
	headerBytes, _ := json.Marshal(map[string]any{
		"alg": "none",
		"typ": "JWT",
	})
	claimBytes, _ := json.Marshal(claims)
	return base64.RawURLEncoding.EncodeToString(headerBytes) + "." + base64.RawURLEncoding.EncodeToString(claimBytes) + "."
}

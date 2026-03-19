package generator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi2"
	"github.com/getkin/kin-openapi/openapi2conv"
	"github.com/getkin/kin-openapi/openapi3"
)

func loadSpec(path string) (*openapi3.T, error) {
	source, location, err := readSpecSource(path)
	if err != nil {
		return nil, err
	}

	normalized, err := normalizeOpenAPIBytes(source)
	if err != nil {
		return nil, err
	}

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true

	doc, err := loadOpenAPI3Doc(normalized, loader, location)
	if err != nil {
		return nil, fmt.Errorf("load spec: %w", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		if _, strippedDoc, strippedErr := reloadWithoutInvalidAnnotationsIfNeeded(location, normalized, doc); strippedErr == nil {
			if err := finalizeLoadedDoc(strippedDoc); err != nil {
				return nil, fmt.Errorf("internalize spec refs: %w", err)
			}
			return strippedDoc, nil
		}
		return nil, fmt.Errorf("validate spec: %w", err)
	}
	if err := finalizeLoadedDoc(doc); err != nil {
		return nil, fmt.Errorf("internalize spec refs: %w", err)
	}

	return doc, nil
}

func readSpecSource(specPath string) ([]byte, *url.URL, error) {
	trimmed := strings.TrimSpace(specPath)
	if trimmed == "" {
		return nil, nil, fmt.Errorf("read spec: spec path is empty")
	}

	if remoteURL, ok := parseRemoteSpecURL(trimmed); ok {
		source, err := fetchRemoteSpec(remoteURL)
		if err != nil {
			return nil, nil, err
		}
		return source, remoteURL, nil
	}

	localPath := trimmed
	if fileURL, ok := parseFileSpecURL(trimmed); ok {
		localPath = fileURL
	}
	absPath, err := filepath.Abs(localPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read spec: resolve path: %w", err)
	}
	source, err := os.ReadFile(absPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read spec: %w", err)
	}
	return source, &url.URL{Path: absPath}, nil
}

func parseRemoteSpecURL(raw string) (*url.URL, bool) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, false
	}
	switch parsed.Scheme {
	case "http", "https":
		return parsed, true
	default:
		return nil, false
	}
}

func parseFileSpecURL(raw string) (string, bool) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "file" {
		return "", false
	}
	path := parsed.Path
	if parsed.Host != "" && parsed.Host != "localhost" {
		path = "//" + parsed.Host + path
	}
	if path == "" {
		return "", false
	}
	unescaped, err := url.PathUnescape(path)
	if err != nil {
		return "", false
	}
	return unescaped, true
}

func fetchRemoteSpec(specURL *url.URL) ([]byte, error) {
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, specURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("read spec: build request: %w", err)
	}
	request.Header.Set("User-Agent", "agent-cli-generator/"+generatorVersion)

	client := &http.Client{Timeout: 30 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("read spec: fetch %s: %w", specURL.String(), err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read spec: read %s: %w", specURL.String(), err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("read spec: fetch %s returned status %d: %s", specURL.String(), response.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func finalizeLoadedDoc(doc *openapi3.T) error {
	if doc == nil {
		return fmt.Errorf("spec document is nil")
	}
	doc.InternalizeRefs(context.Background(), nil)
	sanitizeLoadedDoc(doc)
	return doc.Validate(context.Background())
}

func sanitizeLoadedDoc(doc *openapi3.T) {
	if doc == nil {
		return
	}

	seen := map[*openapi3.SchemaRef]bool{}
	if doc.Components != nil {
		for _, schema := range doc.Components.Schemas {
			sanitizeSchemaRef(doc, schema, seen)
		}
	}

	for _, pathItem := range doc.Paths.Map() {
		for _, pair := range pathOperations(pathItem) {
			operation := pair.Operation
			if operation == nil {
				continue
			}
			for _, parameter := range operation.Parameters {
				if parameter == nil || parameter.Value == nil {
					continue
				}
				sanitizeSchemaRef(doc, parameter.Value.Schema, seen)
			}
			if operation.RequestBody != nil && operation.RequestBody.Value != nil {
				for _, mediaType := range operation.RequestBody.Value.Content {
					if mediaType == nil {
						continue
					}
					sanitizeSchemaRef(doc, mediaType.Schema, seen)
				}
			}
			for _, response := range operation.Responses.Map() {
				if response == nil || response.Value == nil {
					continue
				}
				for _, mediaType := range response.Value.Content {
					if mediaType == nil {
						continue
					}
					sanitizeSchemaRef(doc, mediaType.Schema, seen)
				}
			}
		}
	}
}

func sanitizeSchemaRef(doc *openapi3.T, ref *openapi3.SchemaRef, seen map[*openapi3.SchemaRef]bool) {
	if ref == nil || seen[ref] {
		return
	}
	seen[ref] = true
	if ref.Value == nil {
		return
	}

	schema := ref.Value
	if schema.Discriminator != nil {
		filtered := openapi3.StringMap[openapi3.MappingRef]{}
		for key, mapping := range schema.Discriminator.Mapping {
			if mappingRefExists(doc, mapping.Ref) {
				filtered[key] = mapping
			}
		}
		if len(filtered) == 0 {
			schema.Discriminator = nil
		} else {
			schema.Discriminator.Mapping = filtered
		}
	}

	sanitizeSchemaRef(doc, schema.Items, seen)
	for _, property := range schema.Properties {
		sanitizeSchemaRef(doc, property, seen)
	}
	for _, branch := range schema.OneOf {
		sanitizeSchemaRef(doc, branch, seen)
	}
	for _, branch := range schema.AnyOf {
		sanitizeSchemaRef(doc, branch, seen)
	}
	for _, branch := range schema.AllOf {
		sanitizeSchemaRef(doc, branch, seen)
	}
	sanitizeSchemaRef(doc, schema.Not, seen)
	sanitizeSchemaRef(doc, schema.AdditionalProperties.Schema, seen)
}

func mappingRefExists(doc *openapi3.T, ref string) bool {
	if doc == nil {
		return false
	}
	if !strings.HasPrefix(ref, "#/components/schemas/") {
		return true
	}
	name := strings.TrimPrefix(ref, "#/components/schemas/")
	if name == "" || doc.Components == nil {
		return false
	}
	schema, ok := doc.Components.Schemas[name]
	return ok && schema != nil
}

func reloadWithoutInvalidAnnotationsIfNeeded(location *url.URL, normalized []byte, doc *openapi3.T) ([]byte, *openapi3.T, error) {
	attempts := [][]string{
		{"example", "examples"},
		{"example", "examples", "default"},
	}

	candidates := make([][]byte, 0, 2)
	if len(normalized) > 0 {
		candidates = append(candidates, normalized)
	}
	if doc != nil {
		doc.InternalizeRefs(context.Background(), nil)
		loaded, err := json.Marshal(doc)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal loaded spec for annotation stripping: %w", err)
		}
		candidates = append(candidates, loaded)
	}
	if len(candidates) == 0 {
		return nil, nil, fmt.Errorf("spec document is nil")
	}

	var lastErr error
	for _, candidate := range candidates {
		for _, fields := range attempts {
			strippedNormalized, err := stripOpenAPIFields(candidate, fields...)
			if err != nil {
				lastErr = err
				continue
			}
			strippedLoader := openapi3.NewLoader()
			strippedLoader.IsExternalRefsAllowed = true
			strippedDoc, err := loadOpenAPI3Doc(strippedNormalized, strippedLoader, location)
			if err != nil {
				lastErr = err
				continue
			}
			if err := strippedDoc.Validate(context.Background()); err != nil {
				lastErr = err
				continue
			}
			return strippedNormalized, strippedDoc, nil
		}
	}
	if lastErr != nil {
		return nil, nil, lastErr
	}
	return nil, nil, fmt.Errorf("annotation stripping did not produce a valid spec")
}

func loadOpenAPI3Doc(normalized []byte, loader *openapi3.Loader, location *url.URL) (*openapi3.T, error) {
	root := map[string]any{}
	if err := json.Unmarshal(normalized, &root); err != nil {
		return nil, fmt.Errorf("parse normalized spec: %w", err)
	}

	if isSwagger2Document(root) {
		var doc2 openapi2.T
		if err := json.Unmarshal(normalized, &doc2); err != nil {
			return nil, fmt.Errorf("parse swagger 2.0 spec: %w", err)
		}
		doc3, err := openapi2conv.ToV3WithLoader(&doc2, loader, location)
		if err != nil {
			return nil, fmt.Errorf("convert swagger 2.0 spec: %w", err)
		}
		return doc3, nil
	}

	return loader.LoadFromDataWithPath(normalized, location)
}

func isSwagger2Document(root map[string]any) bool {
	if root == nil {
		return false
	}
	if _, hasOpenAPI := root["openapi"]; hasOpenAPI {
		return false
	}
	swaggerVersion, ok := root["swagger"].(string)
	return ok && strings.TrimSpace(swaggerVersion) != ""
}

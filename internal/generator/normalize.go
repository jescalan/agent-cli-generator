package generator

import (
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

func normalizeOpenAPIBytes(source []byte) ([]byte, error) {
	var payload any
	if err := json.Unmarshal(source, &payload); err != nil {
		if yamlErr := yaml.Unmarshal(source, &payload); yamlErr != nil {
			return nil, fmt.Errorf("parse spec as JSON or YAML: json error: %v; yaml error: %w", err, yamlErr)
		}
	}

	normalized := normalizeOpenAPIValue("", payload)
	out, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("marshal normalized spec: %w", err)
	}
	return out, nil
}

func normalizeOpenAPIValue(parentKey string, value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, inner := range typed {
			out[key] = normalizeOpenAPIValue(key, inner)
		}

		normalizeDescriptionTypo(out)

		if normalized, changed := normalizeSchemaRefSiblings(out); changed {
			out = normalized
		} else if normalized, changed := normalizeNonSchemaRefSiblings(out); changed {
			out = normalized
		}

		if _, hasOpenAPI := out["openapi"]; hasOpenAPI && emptyTopLevelWebhooks(out["webhooks"]) {
			delete(out, "webhooks")
		}

		if looksLikeSchemaObject(out) {
			normalizeMisnestedObjectProperties(out)
			normalizeSchemaExamples(out)
			normalizeAmbiguousScalarOneOf(out)

			if text, ok := out["type"].(string); ok && text == "null" {
				out["nullable"] = true
				delete(out, "type")
				if _, hasEnum := out["enum"]; !hasEnum {
					out["enum"] = []any{nil}
				}
			}

			if values, ok := stringSliceValue(out["type"]); ok {
				nonNull := make([]string, 0, len(values))
				hasNull := false
				for _, value := range values {
					if value == "null" {
						hasNull = true
						continue
					}
					nonNull = append(nonNull, value)
				}
				if hasNull {
					out["nullable"] = true
					switch len(nonNull) {
					case 0:
						delete(out, "type")
						if _, hasEnum := out["enum"]; !hasEnum {
							out["enum"] = []any{nil}
						}
					case 1:
						out["type"] = nonNull[0]
					default:
						out["type"] = nonNull
					}
				}
			}

			ensureArrayItems(out)

			if constant, exists := out["const"]; exists {
				if _, hasEnum := out["enum"]; !hasEnum {
					out["enum"] = []any{constant}
				}
				delete(out, "const")
			}

			applyExclusiveBound(out, "minimum", "exclusiveMinimum", true)
			applyExclusiveBound(out, "maximum", "exclusiveMaximum", false)
		}
		if isSchemaPositionKey(parentKey) {
			if unwrapped, changed := unwrapResponseLikeSchema(out); changed {
				return unwrapped
			}
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, inner := range typed {
			out = append(out, normalizeOpenAPIValue(parentKey, inner))
		}
		return out
	default:
		return value
	}
}

func normalizeDescriptionTypo(node map[string]any) {
	if _, exists := node["description"]; exists {
		return
	}

	value, ok := node["descriptions"].(string)
	if !ok || value == "" {
		return
	}

	node["description"] = value
	delete(node, "descriptions")
}

func normalizeMisnestedObjectProperties(node map[string]any) {
	if !schemaAllowsObjectType(node["type"]) {
		return
	}

	properties, _ := node["properties"].(map[string]any)
	if properties == nil {
		properties = map[string]any{}
	}
	moved := false

	for key, value := range node {
		if isSchemaObjectKeyword(key) || strings.HasPrefix(key, "x-") {
			continue
		}
		if _, exists := properties[key]; exists {
			continue
		}
		candidate, ok := value.(map[string]any)
		if !ok || !looksLikeSchemaCandidate(candidate) {
			continue
		}
		properties[key] = candidate
		delete(node, key)
		moved = true
	}
	if moved {
		node["properties"] = properties
	}
}

func unwrapResponseLikeSchema(node map[string]any) (any, bool) {
	content, ok := node["content"].(map[string]any)
	if !ok || len(content) == 0 {
		return node, false
	}

	schema, ok := firstSchemaFromContent(content)
	if !ok {
		return node, false
	}

	if !hasStructuralSchemaShape(node) {
		return schema, true
	}

	inner, ok := schema.(map[string]any)
	if !ok || !canMergeEmbeddedSchema(node) {
		return node, false
	}

	merged := cloneMap(inner)
	for key, value := range node {
		if key == "content" {
			continue
		}
		if key == "required" {
			merged[key] = unionStringArrayValues(merged[key], value)
			continue
		}
		if _, exists := merged[key]; !exists {
			merged[key] = value
		}
	}
	return merged, true
}

func firstSchemaFromContent(content map[string]any) (any, bool) {
	preferred := []string{"application/json", "application/*+json", "text/json"}
	for _, mediaType := range preferred {
		entry, ok := content[mediaType].(map[string]any)
		if !ok {
			continue
		}
		schema, ok := entry["schema"]
		if ok {
			return schema, true
		}
	}

	for _, raw := range content {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		schema, ok := entry["schema"]
		if ok {
			return schema, true
		}
	}
	return nil, false
}

func hasStructuralSchemaShape(node map[string]any) bool {
	for _, key := range []string{
		"$ref",
		"type",
		"properties",
		"items",
		"required",
		"enum",
		"const",
		"oneOf",
		"anyOf",
		"allOf",
		"not",
		"exclusiveMinimum",
		"exclusiveMaximum",
		"minimum",
		"maximum",
		"nullable",
		"format",
		"additionalProperties",
		"prefixItems",
		"contains",
		"patternProperties",
		"propertyNames",
		"dependentSchemas",
	} {
		if _, ok := node[key]; ok {
			return true
		}
	}
	return false
}

func isSchemaPositionKey(key string) bool {
	switch key {
	case "schema", "items", "oneOf", "anyOf", "allOf", "not", "additionalProperties":
		return true
	default:
		return false
	}
}

func canMergeEmbeddedSchema(node map[string]any) bool {
	for key := range node {
		switch key {
		case "content", "title", "description", "type", "readOnly", "writeOnly", "nullable", "deprecated", "example", "examples", "default", "required":
			continue
		default:
			return false
		}
	}
	return true
}

func cloneMap(source map[string]any) map[string]any {
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func unionStringArrayValues(left, right any) any {
	values := map[string]bool{}
	ordered := make([]string, 0)
	for _, item := range append(stringArrayValue(left), stringArrayValue(right)...) {
		if values[item] {
			continue
		}
		values[item] = true
		ordered = append(ordered, item)
	}
	if len(ordered) == 0 {
		if left != nil {
			return left
		}
		return right
	}
	result := make([]any, 0, len(ordered))
	for _, item := range ordered {
		result = append(result, item)
	}
	return result
}

func stringArrayValue(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil
			}
			out = append(out, text)
		}
		return out
	default:
		return nil
	}
}

func stripOpenAPIFields(source []byte, fields ...string) ([]byte, error) {
	var payload any
	if err := json.Unmarshal(source, &payload); err != nil {
		return nil, fmt.Errorf("parse normalized spec for field stripping: %w", err)
	}

	fieldSet := map[string]bool{}
	for _, field := range fields {
		fieldSet[field] = true
	}

	stripped := stripOpenAPIFieldValues(payload, fieldSet)
	out, err := json.Marshal(stripped)
	if err != nil {
		return nil, fmt.Errorf("marshal spec after field stripping: %w", err)
	}
	return out, nil
}

func stripOpenAPIFieldValues(value any, fields map[string]bool) any {
	switch typed := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, inner := range typed {
			if fields[key] {
				continue
			}
			out[key] = stripOpenAPIFieldValues(inner, fields)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, inner := range typed {
			out = append(out, stripOpenAPIFieldValues(inner, fields))
		}
		return out
	default:
		return value
	}
}

func emptyTopLevelWebhooks(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case map[string]any:
		return len(typed) == 0
	default:
		return false
	}
}

func normalizeSchemaExamples(node map[string]any) {
	examples, hasExamples := node["examples"]
	if !hasExamples {
		return
	}
	if _, hasExample := node["example"]; hasExample {
		delete(node, "examples")
		return
	}

	first, ok := firstExampleValue(examples)
	if ok {
		node["example"] = first
	}
	delete(node, "examples")
}

func normalizeAmbiguousScalarOneOf(node map[string]any) {
	oneOf, ok := node["oneOf"].([]any)
	if !ok || len(oneOf) < 2 {
		return
	}

	sharedType := ""
	fingerprints := map[string]int{}
	hasGenericBranch := false

	for _, raw := range oneOf {
		branch, ok := raw.(map[string]any)
		if !ok {
			return
		}
		branchType, fingerprint, ok := ambiguousScalarBranchFingerprint(branch)
		if !ok {
			return
		}
		if sharedType == "" {
			sharedType = branchType
		} else if sharedType != branchType {
			return
		}
		if fingerprint == "" {
			hasGenericBranch = true
		}
		fingerprints[fingerprint]++
	}

	ambiguous := hasGenericBranch
	if !ambiguous {
		for _, count := range fingerprints {
			if count > 1 {
				ambiguous = true
				break
			}
		}
	}
	if !ambiguous {
		return
	}

	if _, hasAnyOf := node["anyOf"]; !hasAnyOf {
		node["anyOf"] = oneOf
	}
	delete(node, "oneOf")
}

func ambiguousScalarBranchFingerprint(node map[string]any) (string, string, bool) {
	if node == nil {
		return "", "", false
	}
	if _, hasRef := node["$ref"]; hasRef {
		return "", "", false
	}

	branchType, ok := node["type"].(string)
	if !ok || !isScalarSchemaType(branchType) {
		return "", "", false
	}
	for _, key := range []string{"properties", "items", "additionalProperties", "oneOf", "anyOf", "allOf", "not"} {
		if _, exists := node[key]; exists {
			return "", "", false
		}
	}

	constraintKeys := []string{
		"enum",
		"const",
		"pattern",
		"format",
		"minLength",
		"maxLength",
		"minimum",
		"maximum",
		"exclusiveMinimum",
		"exclusiveMaximum",
		"multipleOf",
		"nullable",
	}
	constraints := map[string]any{}
	for _, key := range constraintKeys {
		if value, exists := node[key]; exists {
			constraints[key] = value
		}
	}
	if len(constraints) == 0 {
		return branchType, "", true
	}
	encoded, err := json.Marshal(constraints)
	if err != nil {
		return "", "", false
	}
	return branchType, string(encoded), true
}

func isScalarSchemaType(value string) bool {
	switch value {
	case "string", "integer", "number", "boolean", "null":
		return true
	default:
		return false
	}
}

func firstExampleValue(value any) (any, bool) {
	switch typed := value.(type) {
	case []any:
		if len(typed) == 0 {
			return nil, false
		}
		return typed[0], true
	default:
		return nil, false
	}
}

func ensureArrayItems(node map[string]any) {
	if _, hasItems := node["items"]; hasItems {
		return
	}
	if !schemaAllowsArrayType(node["type"]) {
		return
	}
	node["items"] = map[string]any{}
}

func schemaAllowsArrayType(value any) bool {
	switch typed := value.(type) {
	case string:
		return typed == "array"
	case []string:
		for _, item := range typed {
			if item == "array" {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			text, ok := item.(string)
			if ok && text == "array" {
				return true
			}
		}
	}
	return false
}

func schemaAllowsObjectType(value any) bool {
	switch typed := value.(type) {
	case string:
		return typed == "object"
	case []string:
		for _, item := range typed {
			if item == "object" {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			text, ok := item.(string)
			if ok && text == "object" {
				return true
			}
		}
	}
	return false
}

func normalizeSchemaRefSiblings(node map[string]any) (map[string]any, bool) {
	ref, hasRef := node["$ref"]
	if !hasRef {
		return node, false
	}

	if !looksLikeSchemaRefWithSiblings(node) {
		return node, false
	}

	referenced := map[string]any{
		"$ref": ref,
	}

	normalized := map[string]any{
		"allOf": []any{referenced},
	}
	for key, value := range node {
		if key == "$ref" {
			continue
		}
		normalized[key] = value
	}
	return normalized, true
}

func normalizeNonSchemaRefSiblings(node map[string]any) (map[string]any, bool) {
	ref, hasRef := node["$ref"]
	if !hasRef || len(node) <= 1 {
		return node, false
	}

	if looksLikeSchemaRefWithSiblings(node) {
		return node, false
	}

	return map[string]any{
		"$ref": ref,
	}, true
}

func applyExclusiveBound(node map[string]any, inclusiveKey, exclusiveKey string, isLowerBound bool) {
	exclusiveValue, ok := numericValue(node[exclusiveKey])
	if !ok {
		return
	}

	inclusiveValue, hasInclusive := numericValue(node[inclusiveKey])
	if !hasInclusive {
		node[inclusiveKey] = exclusiveValue
		node[exclusiveKey] = true
		return
	}

	switch {
	case isLowerBound && inclusiveValue > exclusiveValue:
		node[exclusiveKey] = false
	case !isLowerBound && inclusiveValue < exclusiveValue:
		node[exclusiveKey] = false
	case inclusiveValue == exclusiveValue:
		node[inclusiveKey] = inclusiveValue
		node[exclusiveKey] = true
	default:
		node[inclusiveKey] = exclusiveValue
		node[exclusiveKey] = true
	}
}

func looksLikeSchemaObject(node map[string]any) bool {
	for key := range node {
		if isSchemaObjectKeyword(key) {
			return true
		}
	}
	return false
}

func looksLikeSchemaCandidate(node map[string]any) bool {
	if _, hasRef := node["$ref"]; hasRef {
		return true
	}
	return looksLikeSchemaObject(node)
}

func looksLikeSchemaRefWithSiblings(node map[string]any) bool {
	if _, hasRef := node["$ref"]; !hasRef || len(node) <= 1 {
		return false
	}

	for _, key := range []string{
		"type",
		"properties",
		"items",
		"required",
		"enum",
		"const",
		"oneOf",
		"anyOf",
		"allOf",
		"exclusiveMinimum",
		"exclusiveMaximum",
		"minimum",
		"maximum",
		"nullable",
		"format",
		"additionalProperties",
		"default",
		"example",
		"examples",
		"deprecated",
		"readOnly",
		"writeOnly",
	} {
		if _, ok := node[key]; ok {
			return true
		}
	}
	return false
}

func numericValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	default:
		return 0, false
	}
}

func stringSliceValue(value any) ([]string, bool) {
	switch typed := value.(type) {
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, text)
		}
		return out, true
	default:
		return nil, false
	}
}

func isSchemaObjectKeyword(key string) bool {
	switch key {
	case "$ref",
		"title",
		"multipleOf",
		"maximum",
		"exclusiveMaximum",
		"minimum",
		"exclusiveMinimum",
		"maxLength",
		"minLength",
		"pattern",
		"maxItems",
		"minItems",
		"uniqueItems",
		"maxProperties",
		"minProperties",
		"required",
		"enum",
		"type",
		"allOf",
		"oneOf",
		"anyOf",
		"not",
		"items",
		"properties",
		"additionalProperties",
		"description",
		"format",
		"default",
		"nullable",
		"discriminator",
		"readOnly",
		"writeOnly",
		"xml",
		"externalDocs",
		"example",
		"examples",
		"deprecated",
		"const",
		"contentEncoding",
		"contentMediaType",
		"contentSchema",
		"if",
		"then",
		"else",
		"prefixItems",
		"unevaluatedItems",
		"unevaluatedProperties",
		"contains",
		"minContains",
		"maxContains",
		"propertyNames",
		"patternProperties",
		"dependentSchemas",
		"dependentRequired":
		return true
	default:
		return false
	}
}

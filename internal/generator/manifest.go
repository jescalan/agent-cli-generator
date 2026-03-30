package generator

import (
	"fmt"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

type Manifest struct {
	Name              string               `json:"name"`
	Title             string               `json:"title"`
	Version           string               `json:"version"`
	Description       string               `json:"description,omitempty"`
	ServerTemplate    string               `json:"serverTemplate,omitempty"`
	DefaultServer     string               `json:"defaultServer,omitempty"`
	RelativeServer    bool                 `json:"relativeServer"`
	ServerVars        []ServerVariable     `json:"serverVariables,omitempty"`
	GeneratedAt       string               `json:"generatedAt,omitempty"`
	EnvPrefix         string               `json:"envPrefix"`
	Env               EnvConfig            `json:"env"`
	Auth              []AuthSchemeManifest `json:"auth"`
	WhoAmIOperationID string               `json:"whoamiOperationId,omitempty"`
	Operations        []OperationManifest  `json:"operations"`
}

type EnvConfig struct {
	BaseURL       string `json:"baseUrl"`
	HeadersJSON   string `json:"headersJson"`
	OverridesJSON string `json:"overridesJson"`
}

type ServerVariable struct {
	Name        string `json:"name"`
	EnvVar      string `json:"envVar"`
	Default     string `json:"default,omitempty"`
	Description string `json:"description,omitempty"`
}

type AuthSchemeManifest struct {
	Name              string                       `json:"name"`
	Type              string                       `json:"type"`
	Scheme            string                       `json:"scheme,omitempty"`
	In                string                       `json:"in,omitempty"`
	HeaderName        string                       `json:"headerName,omitempty"`
	CookieName        string                       `json:"cookieName,omitempty"`
	QueryName         string                       `json:"queryName,omitempty"`
	Description       string                       `json:"description,omitempty"`
	EnvVar            string                       `json:"envVar"`
	ClientCredentials *ClientCredentialsAuthConfig `json:"clientCredentials,omitempty"`
}

type OperationManifest struct {
	ID             string                `json:"id"`
	Aliases        []string              `json:"aliases,omitempty"`
	Method         string                `json:"method"`
	Path           string                `json:"path"`
	Summary        string                `json:"summary,omitempty"`
	Description    string                `json:"description,omitempty"`
	Tags           []string              `json:"tags,omitempty"`
	Security       []map[string][]string `json:"security,omitempty"`
	RequiredScopes []string              `json:"requiredScopes,omitempty"`
	Pagination     *PaginationManifest   `json:"pagination,omitempty"`
}

type PaginationManifest struct {
	RequestParam string `json:"requestParam,omitempty"`
	ResponsePath string `json:"responsePath,omitempty"`
	ItemsPath    string `json:"itemsPath,omitempty"`
}

type ClientCredentialsAuthConfig struct {
	TokenURL        string   `json:"tokenUrl,omitempty"`
	TokenURLEnv     string   `json:"tokenUrlEnv,omitempty"`
	ClientIDEnv     string   `json:"clientIdEnv,omitempty"`
	ClientSecretEnv string   `json:"clientSecretEnv,omitempty"`
	AudienceEnv     string   `json:"audienceEnv,omitempty"`
	ScopesEnv       string   `json:"scopesEnv,omitempty"`
	AvailableScopes []string `json:"availableScopes,omitempty"`
}

func BuildManifest(doc *openapi3.T, binaryName string) (Manifest, error) {
	if doc == nil || doc.Info == nil {
		return Manifest{}, fmt.Errorf("spec is missing info metadata")
	}

	envPrefix := sanitizeEnvPrefix(binaryName)
	serverTemplate := firstServerTemplate(doc)
	serverVars := buildServerVariables(doc, envPrefix)
	manifest := Manifest{
		Name:           binaryName,
		Title:          strings.TrimSpace(doc.Info.Title),
		Version:        strings.TrimSpace(doc.Info.Version),
		Description:    strings.TrimSpace(doc.Info.Description),
		ServerTemplate: serverTemplate,
		DefaultServer:  expandServerTemplate(serverTemplate, serverVars),
		RelativeServer: firstServerIsRelative(doc),
		ServerVars:     serverVars,
		EnvPrefix:      envPrefix,
		Env: EnvConfig{
			BaseURL:       envPrefix + "_BASE_URL",
			HeadersJSON:   envPrefix + "_HEADERS_JSON",
			OverridesJSON: envPrefix + "_OVERRIDES_JSON",
		},
		Auth: buildAuthSchemes(doc, envPrefix),
	}

	if doc.Paths == nil {
		doc.Paths = openapi3.NewPaths()
	}
	paths := doc.Paths.Map()
	var pathKeys []string
	for path := range paths {
		pathKeys = append(pathKeys, path)
	}
	sort.Strings(pathKeys)
	type operationEntry struct {
		manifest        OperationManifest
		aliasCandidates []string
	}
	type operationSource struct {
		path    string
		item    *openapi3.PathItem
		pair    methodOperationPair
		id      string
		derived bool
	}
	type derivedConflict struct {
		count   int
		methods map[string]bool
	}

	var sources []operationSource
	derivedConflicts := map[string]derivedConflict{}
	for _, path := range pathKeys {
		item := paths[path]
		for _, pair := range pathOperations(item) {
			id := strings.TrimSpace(pair.Operation.OperationID)
			derivedID := id == ""
			if derivedID {
				id = deriveOperationID(path, pair.Method, pair.Operation)
				conflict := derivedConflicts[id]
				if conflict.methods == nil {
					conflict.methods = map[string]bool{}
				}
				conflict.count++
				conflict.methods[pair.Method] = true
				derivedConflicts[id] = conflict
			}
			sources = append(sources, operationSource{
				path:    path,
				item:    item,
				pair:    pair,
				id:      id,
				derived: derivedID,
			})
		}
	}

	seenIDs := map[string]string{}
	var entries []operationEntry

	for _, source := range sources {
		id := source.id
		if source.derived {
			conflict := derivedConflicts[id]
			if conflict.count > 1 && len(conflict.methods) == conflict.count {
				id = qualifyOperationIDWithMethod(id, source.pair.Method)
			}
		}
		if previous, exists := seenIDs[id]; exists {
			return Manifest{}, fmt.Errorf("duplicate operation ID %q derived from %s and %s %s", id, previous, source.pair.Method, source.path)
		}
		seenIDs[id] = source.pair.Method + " " + source.path

		security := buildSecurityRequirements(doc.Security, source.pair.Operation.Security)
		entry := OperationManifest{
			ID:             id,
			Method:         source.pair.Method,
			Path:           source.path,
			Summary:        strings.TrimSpace(source.pair.Operation.Summary),
			Description:    strings.TrimSpace(source.pair.Operation.Description),
			Tags:           append([]string(nil), source.pair.Operation.Tags...),
			Security:       security,
			RequiredScopes: collectRequiredScopes(source.pair.Operation, security),
			Pagination:     detectPagination(source.item, source.pair.Operation),
		}
		entries = append(entries, operationEntry{
			manifest:        entry,
			aliasCandidates: buildOperationAliases(source.path, source.pair.Method, source.pair.Operation, id),
		})
	}

	aliasCounts := map[string]int{}
	blockedAliases := map[string]bool{}
	for _, entry := range entries {
		seen := map[string]bool{}
		for _, alias := range entry.aliasCandidates {
			alias = strings.TrimSpace(alias)
			if alias == "" || alias == entry.manifest.ID || seen[alias] {
				continue
			}
			seen[alias] = true
			if _, reserved := seenIDs[alias]; reserved {
				blockedAliases[alias] = true
				continue
			}
			aliasCounts[alias]++
		}
	}

	for _, entry := range entries {
		seen := map[string]bool{}
		for _, alias := range entry.aliasCandidates {
			alias = strings.TrimSpace(alias)
			if alias == "" || alias == entry.manifest.ID || seen[alias] || blockedAliases[alias] || aliasCounts[alias] != 1 {
				continue
			}
			seen[alias] = true
			entry.manifest.Aliases = append(entry.manifest.Aliases, alias)
		}
		sort.Strings(entry.manifest.Aliases)
		manifest.Operations = append(manifest.Operations, entry.manifest)
	}

	sort.Slice(manifest.Operations, func(i, j int) bool {
		return manifest.Operations[i].ID < manifest.Operations[j].ID
	})
	manifest.WhoAmIOperationID = detectWhoAmIOperation(manifest.Operations)

	return manifest, nil
}

func detectWhoAmIOperation(operations []OperationManifest) string {
	bestID := ""
	bestScore := 0
	for _, op := range operations {
		score := whoAmIScore(op)
		if score > bestScore {
			bestScore = score
			bestID = op.ID
		}
	}
	return bestID
}

func whoAmIScore(op OperationManifest) int {
	path := strings.ToLower(strings.TrimSpace(op.Path))
	id := strings.ToLower(strings.TrimSpace(op.ID))
	summary := strings.ToLower(strings.TrimSpace(op.Summary))
	description := strings.ToLower(strings.TrimSpace(op.Description))

	score := 0
	if strings.EqualFold(op.Method, "GET") {
		score += 100
	}
	if !strings.Contains(path, "{") {
		score += 25
	}

	for _, segment := range pathSegments(path) {
		switch segment {
		case "whoami":
			score += 1000
		case "me", "self":
			score += 800
		}
	}

	if strings.Contains(id, "whoami") {
		score += 900
	}
	if strings.Contains(summary, "current user") || strings.Contains(description, "current user") {
		score += 400
	}
	if strings.Contains(summary, "authenticated user") || strings.Contains(description, "authenticated user") {
		score += 400
	}
	if strings.Contains(path, "/me") || strings.Contains(path, "/self") {
		score += 150
	}

	if score < 500 {
		return 0
	}
	return score
}

func pathSegments(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	raw := strings.Split(trimmed, "/")
	out := make([]string, 0, len(raw))
	for _, segment := range raw {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		out = append(out, segment)
	}
	return out
}

func qualifyOperationIDWithMethod(id, method string) string {
	qualified := strings.TrimSpace(id)
	if qualified == "" {
		qualified = "operation"
	}
	methodSlug := sanitizeSlug(strings.ToLower(strings.TrimSpace(method)))
	if methodSlug == "" {
		return qualified
	}
	return qualified + "." + methodSlug
}

type methodOperationPair struct {
	Method    string
	Operation *openapi3.Operation
}

func pathOperations(item *openapi3.PathItem) []methodOperationPair {
	if item == nil {
		return nil
	}

	pairs := []methodOperationPair{
		{Method: "DELETE", Operation: item.Delete},
		{Method: "GET", Operation: item.Get},
		{Method: "HEAD", Operation: item.Head},
		{Method: "OPTIONS", Operation: item.Options},
		{Method: "PATCH", Operation: item.Patch},
		{Method: "POST", Operation: item.Post},
		{Method: "PUT", Operation: item.Put},
		{Method: "TRACE", Operation: item.Trace},
	}

	var out []methodOperationPair
	for _, pair := range pairs {
		if pair.Operation != nil {
			out = append(out, pair)
		}
	}
	return out
}

func firstServerTemplate(doc *openapi3.T) string {
	if doc == nil || len(doc.Servers) == 0 || doc.Servers[0] == nil {
		return ""
	}
	return strings.TrimSpace(doc.Servers[0].URL)
}

func firstServerIsRelative(doc *openapi3.T) bool {
	server := firstServerTemplate(doc)
	if server == "" {
		return false
	}
	return strings.HasPrefix(server, "/")
}

func buildServerVariables(doc *openapi3.T, envPrefix string) []ServerVariable {
	if doc == nil || len(doc.Servers) == 0 || doc.Servers[0] == nil || len(doc.Servers[0].Variables) == 0 {
		return nil
	}

	variableNames := make([]string, 0, len(doc.Servers[0].Variables))
	for name := range doc.Servers[0].Variables {
		variableNames = append(variableNames, name)
	}
	sort.Strings(variableNames)

	serverVars := make([]ServerVariable, 0, len(variableNames))
	for _, name := range variableNames {
		variable := doc.Servers[0].Variables[name]
		if variable == nil {
			continue
		}
		serverVars = append(serverVars, ServerVariable{
			Name:        strings.TrimSpace(name),
			EnvVar:      envPrefix + "_SERVER_" + sanitizeEnvPrefix(name),
			Default:     strings.TrimSpace(variable.Default),
			Description: strings.TrimSpace(variable.Description),
		})
	}
	return serverVars
}

func expandServerTemplate(template string, variables []ServerVariable) string {
	expanded := strings.TrimSpace(template)
	if expanded == "" || len(variables) == 0 {
		return expanded
	}

	for _, variable := range variables {
		if variable.Name == "" {
			continue
		}
		expanded = strings.ReplaceAll(expanded, "{"+variable.Name+"}", variable.Default)
	}
	return expanded
}

func buildAuthSchemes(doc *openapi3.T, envPrefix string) []AuthSchemeManifest {
	if doc == nil || doc.Components == nil || doc.Components.SecuritySchemes == nil {
		return nil
	}

	var names []string
	for name := range doc.Components.SecuritySchemes {
		names = append(names, name)
	}
	sort.Strings(names)

	clientCredentialCapable := 0
	for _, name := range names {
		ref := doc.Components.SecuritySchemes[name]
		if ref == nil || ref.Value == nil {
			continue
		}
		if supportsClientCredentials(ref.Value) {
			clientCredentialCapable++
		}
	}

	var schemes []AuthSchemeManifest
	for _, name := range names {
		ref := doc.Components.SecuritySchemes[name]
		if ref == nil || ref.Value == nil {
			continue
		}
		scheme := ref.Value
		entry := AuthSchemeManifest{
			Name:        name,
			Type:        scheme.Type,
			Scheme:      scheme.Scheme,
			In:          scheme.In,
			Description: strings.TrimSpace(scheme.Description),
			EnvVar:      authEnvVar(envPrefix, name, scheme),
		}
		switch scheme.In {
		case "header":
			entry.HeaderName = scheme.Name
		case "cookie":
			entry.CookieName = scheme.Name
		case "query":
			entry.QueryName = scheme.Name
		}
		if config := buildClientCredentialsConfig(envPrefix, name, scheme, clientCredentialCapable > 1); config != nil {
			entry.ClientCredentials = config
		}
		schemes = append(schemes, entry)
	}
	return schemes
}

func supportsClientCredentials(scheme *openapi3.SecurityScheme) bool {
	if scheme == nil {
		return false
	}
	if scheme.Type == "oauth2" && scheme.Flows != nil && scheme.Flows.ClientCredentials != nil {
		return true
	}
	return scheme.Type == "http" && strings.EqualFold(scheme.Scheme, "bearer")
}

func buildClientCredentialsConfig(prefix, name string, scheme *openapi3.SecurityScheme, namespaced bool) *ClientCredentialsAuthConfig {
	if !supportsClientCredentials(scheme) {
		return nil
	}

	config := &ClientCredentialsAuthConfig{
		TokenURLEnv:     authAuxEnvVar(prefix, name, "TOKEN_URL", namespaced),
		ClientIDEnv:     authAuxEnvVar(prefix, name, "CLIENT_ID", namespaced),
		ClientSecretEnv: authAuxEnvVar(prefix, name, "CLIENT_SECRET", namespaced),
		AudienceEnv:     authAuxEnvVar(prefix, name, "AUDIENCE", namespaced),
		ScopesEnv:       authAuxEnvVar(prefix, name, "SCOPES", namespaced),
	}

	if scheme.Type == "oauth2" && scheme.Flows != nil && scheme.Flows.ClientCredentials != nil {
		config.TokenURL = strings.TrimSpace(scheme.Flows.ClientCredentials.TokenURL)
		config.AvailableScopes = sortedStringKeys(scheme.Flows.ClientCredentials.Scopes)
	}

	return config
}

func authEnvVar(prefix, name string, scheme *openapi3.SecurityScheme) string {
	if scheme == nil {
		return prefix + "_" + strings.ToUpper(sanitizeSlug(name))
	}

	switch {
	case scheme.Type == "http" && strings.EqualFold(scheme.Scheme, "bearer"):
		return prefix + "_BEARER_TOKEN"
	case scheme.Type == "apiKey" && strings.EqualFold(scheme.In, "header") && looksLikeAPIKeyName(scheme.Name):
		return prefix + "_API_KEY"
	case scheme.Type == "apiKey" && strings.EqualFold(scheme.In, "query") && looksLikeAPIKeyName(scheme.Name):
		return prefix + "_API_KEY"
	case scheme.Type == "apiKey" && strings.EqualFold(scheme.In, "cookie"):
		return prefix + "_SESSION_COOKIE"
	default:
		return prefix + "_" + strings.ToUpper(strings.ReplaceAll(sanitizeSlug(name), "-", "_"))
	}
}

func authAuxEnvVar(prefix, name, suffix string, namespaced bool) string {
	if !namespaced {
		return prefix + "_" + suffix
	}
	slug := strings.ToUpper(strings.ReplaceAll(sanitizeSlug(name), "-", "_"))
	if slug == "" {
		return prefix + "_" + suffix
	}
	return prefix + "_" + slug + "_" + suffix
}

func looksLikeAPIKeyName(name string) bool {
	normalized := strings.ToLower(name)
	return strings.Contains(normalized, "api-key") ||
		strings.Contains(normalized, "api_key") ||
		strings.Contains(normalized, "apikey")
}

func buildSecurityRequirements(defaultSecurity openapi3.SecurityRequirements, operationSecurity *openapi3.SecurityRequirements) []map[string][]string {
	effective := operationSecurity
	if effective == nil {
		effective = &defaultSecurity
	}
	if effective == nil || len(*effective) == 0 {
		return nil
	}

	var out []map[string][]string
	for _, requirement := range *effective {
		entry := map[string][]string{}
		var names []string
		for name := range requirement {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			scopes := requirement[name]
			if scopes == nil {
				scopes = []string{}
			}
			entry[name] = append([]string{}, scopes...)
		}
		out = append(out, entry)
	}
	return out
}

func collectRequiredScopes(operation *openapi3.Operation, security []map[string][]string) []string {
	seen := map[string]bool{}
	var scopes []string

	add := func(scope string) {
		scope = strings.TrimSpace(scope)
		if scope == "" || seen[scope] {
			return
		}
		seen[scope] = true
		scopes = append(scopes, scope)
	}

	for _, requirement := range security {
		for _, values := range requirement {
			for _, scope := range values {
				add(scope)
			}
		}
	}

	for _, scope := range extensionScopes(operation) {
		add(scope)
	}

	sort.Strings(scopes)
	return scopes
}

func extensionScopes(operation *openapi3.Operation) []string {
	if operation == nil || len(operation.Extensions) == 0 {
		return nil
	}
	raw, ok := operation.Extensions["x-scope"]
	if !ok {
		return nil
	}
	return scopesFromAny(raw)
}

func scopesFromAny(raw any) []string {
	switch typed := raw.(type) {
	case string:
		return splitScopeString(typed)
	case []string:
		out := append([]string(nil), typed...)
		sort.Strings(out)
		return dedupeStrings(out)
	case []any:
		var out []string
		for _, item := range typed {
			out = append(out, scopesFromAny(item)...)
		}
		sort.Strings(out)
		return dedupeStrings(out)
	default:
		return nil
	}
}

func splitScopeString(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n' || r == '\t'
	})
	return dedupeStrings(parts)
}

func dedupeStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func sortedStringKeys(values map[string]string) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func deriveOperationID(path, method string, operation *openapi3.Operation) string {
	if path == "/" {
		if operation != nil {
			summary := strings.ToLower(strings.TrimSpace(operation.Summary))
			if strings.Contains(summary, "health") {
				return "system.health"
			}
			if strings.Contains(summary, "metadata") || strings.Contains(summary, "info") {
				return "system.info"
			}
		}
		return "system.root"
	}

	segments := strings.Split(strings.Trim(path, "/"), "/")
	var staticSegments []string
	lastIsParam := false
	for _, segment := range segments {
		if isPathParameter(segment) {
			lastIsParam = true
			continue
		}
		staticSegments = append(staticSegments, sanitizeSlug(segment))
		lastIsParam = false
	}

	if len(staticSegments) == 0 {
		return sanitizeSlug(strings.ToLower(method))
	}

	method = strings.ToUpper(method)
	containsParam := strings.Contains(path, "{")
	lastStatic := staticSegments[len(staticSegments)-1]

	switch {
	case lastIsParam:
		return strings.Join(append(staticSegments, itemAction(method)), ".")
	case containsParam:
		if isLikelyCollection(lastStatic) {
			return strings.Join(append(staticSegments, collectionAction(method)), ".")
		}
		if method == "GET" || method == "PATCH" || method == "PUT" || method == "DELETE" {
			return strings.Join(append(staticSegments, itemAction(method)), ".")
		}
		return strings.Join(staticSegments, ".")
	case isLikelyCollection(lastStatic) && (method == "GET" || method == "POST"):
		return strings.Join(append(staticSegments, collectionAction(method)), ".")
	case method == "PATCH" || method == "PUT" || method == "DELETE":
		return strings.Join(append(staticSegments, itemAction(method)), ".")
	case len(staticSegments) > 1 && (method == "GET" || method == "POST"):
		return strings.Join(append(staticSegments[:len(staticSegments)-1], lastStatic), ".")
	default:
		return strings.Join(staticSegments, ".")
	}
}

func buildOperationAliases(path, method string, operation *openapi3.Operation, canonicalID string) []string {
	var aliases []string
	appendAlias := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || value == canonicalID {
			return
		}
		for _, existing := range aliases {
			if existing == value {
				return
			}
		}
		aliases = append(aliases, value)
	}

	appendAlias(deriveOperationID(path, method, operation))
	appendAlias(buildSpeakeasyAlias(path, method, operation))
	return aliases
}

func buildSpeakeasyAlias(path, method string, operation *openapi3.Operation) string {
	if operation == nil {
		return ""
	}

	group := sanitizeSlug(extensionString(operation.Extensions["x-speakeasy-group"]))
	if group == "" || group == "api" {
		return ""
	}

	name := sanitizeSlug(extensionString(operation.Extensions["x-speakeasy-name-override"]))
	if name == "" || name == "api" {
		derived := deriveOperationID(path, method, operation)
		parts := strings.Split(derived, ".")
		name = parts[len(parts)-1]
	}
	if name == "" || name == "api" {
		return ""
	}

	return group + "." + name
}

func extensionString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		return ""
	}
}

func detectPagination(pathItem *openapi3.PathItem, operation *openapi3.Operation) *PaginationManifest {
	queryParams := collectParameters(pathItem, operation)
	requestParam := ""
	for _, paramRef := range queryParams {
		if paramRef == nil || paramRef.Value == nil || paramRef.Value.In != "query" {
			continue
		}
		switch strings.ToLower(paramRef.Value.Name) {
		case "pagetoken", "page_token", "cursor", "nextcursor", "next_cursor", "startingafter", "after":
			requestParam = paramRef.Value.Name
		}
	}
	if requestParam == "" {
		return nil
	}

	schema := firstSuccessJSONSchema(operation)
	if schema == nil || schema.Value == nil {
		return nil
	}

	responsePath := ""
	itemsPath := ""

	for name, prop := range schema.Value.Properties {
		lower := strings.ToLower(name)
		switch lower {
		case "nextpagetoken", "next_page_token", "nextcursor", "next_cursor", "cursor", "next":
			responsePath = "/" + name
		}

		if prop != nil && prop.Value != nil && schemaHasType(prop.Value, "array") && itemsPath == "" {
			itemsPath = "/" + name
		}
	}

	if responsePath == "" {
		return nil
	}

	return &PaginationManifest{
		RequestParam: requestParam,
		ResponsePath: responsePath,
		ItemsPath:    itemsPath,
	}
}

func collectParameters(pathItem *openapi3.PathItem, operation *openapi3.Operation) []*openapi3.ParameterRef {
	keyed := map[string]*openapi3.ParameterRef{}
	order := []string{}

	if pathItem != nil {
		for _, entry := range pathItem.Parameters {
			if entry == nil || entry.Value == nil {
				continue
			}
			key := entry.Value.In + ":" + entry.Value.Name
			if _, exists := keyed[key]; !exists {
				order = append(order, key)
			}
			keyed[key] = entry
		}
	}
	if operation != nil {
		for _, entry := range operation.Parameters {
			if entry == nil || entry.Value == nil {
				continue
			}
			key := entry.Value.In + ":" + entry.Value.Name
			if _, exists := keyed[key]; !exists {
				order = append(order, key)
			}
			keyed[key] = entry
		}
	}
	params := make([]*openapi3.ParameterRef, 0, len(order))
	for _, key := range order {
		params = append(params, keyed[key])
	}
	return params
}

func firstSuccessJSONSchema(operation *openapi3.Operation) *openapi3.SchemaRef {
	if operation == nil || operation.Responses == nil {
		return nil
	}

	statusCodes := make([]string, 0, len(operation.Responses.Map()))
	for code := range operation.Responses.Map() {
		statusCodes = append(statusCodes, code)
	}
	sort.Strings(statusCodes)
	for _, code := range statusCodes {
		if !strings.HasPrefix(code, "2") {
			continue
		}
		response := operation.Responses.Map()[code]
		if response == nil || response.Value == nil {
			continue
		}
		if schema := firstJSONContentSchema(response.Value.Content); schema != nil {
			return schema
		}
	}
	return nil
}

func firstJSONContentSchema(content openapi3.Content) *openapi3.SchemaRef {
	if content == nil {
		return nil
	}
	if media, ok := content["application/json"]; ok && media != nil {
		return media.Schema
	}
	var contentTypes []string
	for contentType := range content {
		contentTypes = append(contentTypes, contentType)
	}
	sort.Strings(contentTypes)
	for _, contentType := range contentTypes {
		if strings.Contains(contentType, "json") {
			media := content[contentType]
			if media != nil {
				return media.Schema
			}
		}
	}
	for _, contentType := range contentTypes {
		media := content[contentType]
		if media != nil {
			return media.Schema
		}
	}
	return nil
}

func sanitizeSlug(input string) string {
	input = strings.TrimSpace(strings.ToLower(input))
	replacer := strings.NewReplacer(
		" ", "-",
		"/", "-",
		"_", "-",
		".", "-",
		":", "-",
	)
	input = replacer.Replace(input)

	var builder strings.Builder
	lastDash := false
	for _, r := range input {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			builder.WriteRune(r)
			lastDash = false
		case r == '-':
			if !lastDash {
				builder.WriteRune(r)
				lastDash = true
			}
		}
	}

	out := strings.Trim(builder.String(), "-")
	if out == "" {
		return "api"
	}
	return out
}

func sanitizeEnvPrefix(name string) string {
	return strings.ToUpper(strings.ReplaceAll(sanitizeSlug(name), "-", "_"))
}

func isPathParameter(segment string) bool {
	return strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}")
}

func collectionAction(method string) string {
	switch method {
	case "GET":
		return "list"
	case "POST":
		return "create"
	case "DELETE":
		return "delete"
	case "PATCH":
		return "update"
	case "PUT":
		return "replace"
	default:
		return strings.ToLower(method)
	}
}

func itemAction(method string) string {
	switch method {
	case "GET":
		return "get"
	case "POST":
		return "create"
	case "DELETE":
		return "delete"
	case "PATCH":
		return "update"
	case "PUT":
		return "replace"
	default:
		return strings.ToLower(method)
	}
}

func isLikelyCollection(segment string) bool {
	if strings.HasSuffix(segment, "ies") || strings.HasSuffix(segment, "ses") || strings.HasSuffix(segment, "xes") {
		return true
	}
	if strings.HasSuffix(segment, "s") && !strings.HasSuffix(segment, "ss") {
		return true
	}
	return false
}

func schemaHasType(schema *openapi3.Schema, expected string) bool {
	if schema == nil || schema.Type == nil {
		return false
	}
	for _, kind := range *schema.Type {
		if kind == expected {
			return true
		}
	}
	return false
}

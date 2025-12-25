package transformer

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	// AnnotationTransform is the annotation key for transformation rules
	AnnotationTransform = "kubemirror.raczylo.com/transform"

	// AnnotationTransformStrict enables strict mode (errors block mirroring)
	AnnotationTransformStrict = "kubemirror.raczylo.com/transform-strict"
)

// Transformer applies transformation rules to Kubernetes resources.
type Transformer struct {
	options TransformOptions
}

// NewTransformer creates a new transformer with the given options.
func NewTransformer(options TransformOptions) *Transformer {
	return &Transformer{
		options: options,
	}
}

// NewDefaultTransformer creates a transformer with default options.
func NewDefaultTransformer() *Transformer {
	return NewTransformer(DefaultTransformOptions())
}

// Transform applies transformation rules to a resource.
// It returns the transformed resource and any errors encountered.
func (t *Transformer) Transform(source runtime.Object, ctx TransformContext) (runtime.Object, error) {
	// Convert to unstructured for easier manipulation
	unstructuredObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(source)
	if err != nil {
		return nil, fmt.Errorf("failed to convert to unstructured: %w", err)
	}

	u := &unstructured.Unstructured{Object: unstructuredObj}

	// Get transformation rules from annotations
	rules, err := t.parseTransformRules(u)
	if err != nil {
		if t.isStrictMode(u) {
			return nil, fmt.Errorf("failed to parse transformation rules: %w", err)
		}
		// Non-strict mode: log warning and return original
		return source, nil
	}

	if len(rules.Rules) == 0 {
		// No transformation rules
		return source, nil
	}

	// Validate rules
	if err := t.validateRules(rules); err != nil {
		if t.isStrictMode(u) {
			return nil, fmt.Errorf("invalid transformation rules: %w", err)
		}
		return source, nil
	}

	// Apply each rule
	for i, rule := range rules.Rules {
		if err := t.applyRule(u, rule, ctx); err != nil {
			if t.isStrictMode(u) {
				return nil, fmt.Errorf("failed to apply rule %d (%s): %w", i+1, rule.Path, err)
			}
			// Non-strict mode: continue with next rule
			continue
		}
	}

	return u, nil
}

// parseTransformRules extracts and parses transformation rules from resource annotations.
func (t *Transformer) parseTransformRules(u *unstructured.Unstructured) (*TransformRules, error) {
	annotations := u.GetAnnotations()
	if annotations == nil {
		return &TransformRules{}, nil
	}

	rulesYAML, exists := annotations[AnnotationTransform]
	if !exists || rulesYAML == "" {
		return &TransformRules{}, nil
	}

	// Check size limit
	if len(rulesYAML) > t.options.MaxRuleSize {
		return nil, fmt.Errorf("transformation rules exceed maximum size of %d bytes", t.options.MaxRuleSize)
	}

	var rules TransformRules
	if err := yaml.Unmarshal([]byte(rulesYAML), &rules); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	return &rules, nil
}

// validateRules validates all transformation rules.
func (t *Transformer) validateRules(rules *TransformRules) error {
	if len(rules.Rules) > t.options.MaxRules {
		return fmt.Errorf("too many rules (%d), maximum is %d", len(rules.Rules), t.options.MaxRules)
	}

	for i, rule := range rules.Rules {
		if err := rule.Validate(); err != nil {
			return fmt.Errorf("rule %d: %w", i+1, err)
		}
	}

	return nil
}

// applyRule applies a single transformation rule to the resource.
func (t *Transformer) applyRule(u *unstructured.Unstructured, rule Rule, ctx TransformContext) error {
	// Check if rule should apply to this target namespace
	if !matchesNamespacePattern(rule, ctx.TargetNamespace) {
		// Rule doesn't apply to this namespace - skip silently
		return nil
	}

	switch rule.Type() {
	case RuleTypeValue:
		return t.applyValueRule(u, rule, ctx)
	case RuleTypeTemplate:
		return t.applyTemplateRule(u, rule, ctx)
	case RuleTypeMerge:
		return t.applyMergeRule(u, rule, ctx)
	case RuleTypeDelete:
		return t.applyDeleteRule(u, rule, ctx)
	default:
		return fmt.Errorf("unknown rule type: %s", rule.Type())
	}
}

// applyValueRule sets a field to a static value.
func (t *Transformer) applyValueRule(u *unstructured.Unstructured, rule Rule, ctx TransformContext) error {
	if rule.Value == nil {
		return fmt.Errorf("value rule has nil value")
	}

	pathParts := parsePath(rule.Path)
	if len(pathParts) == 0 {
		return fmt.Errorf("empty path")
	}

	return setNestedField(u.Object, pathParts, *rule.Value)
}

// applyTemplateRule uses Go templates to generate the value.
func (t *Transformer) applyTemplateRule(u *unstructured.Unstructured, rule Rule, ctx TransformContext) error {
	if rule.Template == nil {
		return fmt.Errorf("template rule has nil template")
	}

	// Create template with timeout
	tmpl, err := template.New("transform").Funcs(templateFuncs()).Parse(*rule.Template)
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	// Execute template with timeout
	ctxWithTimeout, cancel := context.WithTimeout(context.Background(), t.options.TemplateTimeout)
	defer cancel()

	resultChan := make(chan string, 1)
	errChan := make(chan error, 1)

	go func() {
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, ctx); err != nil {
			errChan <- err
			return
		}
		resultChan <- buf.String()
	}()

	select {
	case <-ctxWithTimeout.Done():
		return fmt.Errorf("template execution timeout")
	case err := <-errChan:
		return fmt.Errorf("template execution failed: %w", err)
	case result := <-resultChan:
		pathParts := parsePath(rule.Path)
		return setNestedField(u.Object, pathParts, result)
	}
}

// applyMergeRule merges a map into the target field.
func (t *Transformer) applyMergeRule(u *unstructured.Unstructured, rule Rule, ctx TransformContext) error {
	if rule.Merge == nil {
		return fmt.Errorf("merge rule has nil merge map")
	}

	pathParts := parsePath(rule.Path)
	if len(pathParts) == 0 {
		return fmt.Errorf("empty path")
	}

	// Get existing value (if any)
	existing, found, err := unstructured.NestedMap(u.Object, pathParts...)
	if err != nil {
		return fmt.Errorf("failed to get existing value: %w", err)
	}

	// Create or merge map
	merged := make(map[string]interface{})
	if found {
		for k, v := range existing {
			merged[k] = v
		}
	}

	// Merge new values
	for k, v := range rule.Merge {
		merged[k] = v
	}

	return unstructured.SetNestedMap(u.Object, merged, pathParts...)
}

// applyDeleteRule removes a field from the resource.
func (t *Transformer) applyDeleteRule(u *unstructured.Unstructured, rule Rule, ctx TransformContext) error {
	pathParts := parsePath(rule.Path)
	if len(pathParts) == 0 {
		return fmt.Errorf("empty path")
	}

	unstructured.RemoveNestedField(u.Object, pathParts...)
	return nil
}

// isStrictMode checks if strict mode is enabled for this resource.
func (t *Transformer) isStrictMode(u *unstructured.Unstructured) bool {
	if t.options.Strict {
		return true
	}

	annotations := u.GetAnnotations()
	if annotations == nil {
		return false
	}

	strictValue, exists := annotations[AnnotationTransformStrict]
	return exists && (strictValue == "true" || strictValue == "1")
}

// parsePath splits a dot-notation path into parts.
// Handles array indexing notation like "spec.containers[0].image".
// Returns path segments where array indexes are represented as "[N]" strings.
func parsePath(path string) []string {
	if path == "" {
		return nil
	}

	var parts []string
	var current strings.Builder
	inBracket := false

	for i := 0; i < len(path); i++ {
		ch := path[i]

		switch ch {
		case '.':
			// End current segment if not in brackets
			if !inBracket && current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			} else if !inBracket {
				// Skip empty segments from consecutive dots
				continue
			} else {
				// Inside brackets, keep the dot
				current.WriteByte(ch)
			}

		case '[':
			// Start of array index - save current segment if any
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
			inBracket = true
			current.WriteByte(ch)

		case ']':
			// End of array index
			if inBracket {
				current.WriteByte(ch)
				parts = append(parts, current.String())
				current.Reset()
				inBracket = false
			} else {
				// Unmatched ], just include it
				current.WriteByte(ch)
			}

		default:
			current.WriteByte(ch)
		}
	}

	// Add final segment
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
}

// setNestedField sets a value at the given path in a nested map/array structure.
// Supports both map keys and array indexes (e.g., "containers[0]").
func setNestedField(obj map[string]interface{}, path []string, value interface{}) error {
	if len(path) == 0 {
		return fmt.Errorf("empty path")
	}

	// Navigate to the parent of the final element
	var current interface{} = obj
	for i := 0; i < len(path)-1; i++ {
		segment := path[i]

		// Check if this segment is an array index
		if isArrayIndex(segment) {
			index, err := parseArrayIndex(segment)
			if err != nil {
				return fmt.Errorf("invalid array index %s: %w", segment, err)
			}

			arr, ok := current.([]interface{})
			if !ok {
				return fmt.Errorf("path segment %s requires an array, got %T", segment, current)
			}

			if index < 0 || index >= len(arr) {
				return fmt.Errorf("array index %d out of bounds (length %d)", index, len(arr))
			}

			current = arr[index]
			continue
		}

		// Regular map key
		currentMap, ok := current.(map[string]interface{})
		if !ok {
			return fmt.Errorf("path segment %s requires a map, got %T", segment, current)
		}

		next, exists := currentMap[segment]
		if !exists {
			// Peek ahead to see if next segment is an array index
			if i+1 < len(path) && isArrayIndex(path[i+1]) {
				// Create an empty array
				newArr := make([]interface{}, 0)
				currentMap[segment] = newArr
				current = newArr
			} else {
				// Create intermediate map
				newMap := make(map[string]interface{})
				currentMap[segment] = newMap
				current = newMap
			}
			continue
		}

		current = next
	}

	// Set the final value
	finalSegment := path[len(path)-1]

	if isArrayIndex(finalSegment) {
		// Setting a value in an array
		index, err := parseArrayIndex(finalSegment)
		if err != nil {
			return fmt.Errorf("invalid array index %s: %w", finalSegment, err)
		}

		arr, ok := current.([]interface{})
		if !ok {
			return fmt.Errorf("path segment %s requires an array, got %T", finalSegment, current)
		}

		if index < 0 || index >= len(arr) {
			return fmt.Errorf("array index %d out of bounds (length %d)", index, len(arr))
		}

		arr[index] = value
		return nil
	}

	// Setting a value in a map
	currentMap, ok := current.(map[string]interface{})
	if !ok {
		return fmt.Errorf("cannot set key %s on non-map %T", finalSegment, current)
	}

	currentMap[finalSegment] = value
	return nil
}

// isArrayIndex checks if a path segment is an array index (e.g., "[0]", "[123]").
func isArrayIndex(segment string) bool {
	return len(segment) > 2 && segment[0] == '[' && segment[len(segment)-1] == ']'
}

// parseArrayIndex extracts the numeric index from an array segment like "[0]".
func parseArrayIndex(segment string) (int, error) {
	if !isArrayIndex(segment) {
		return 0, fmt.Errorf("not an array index: %s", segment)
	}

	// Extract the number between brackets
	indexStr := segment[1 : len(segment)-1]
	var index int
	_, err := fmt.Sscanf(indexStr, "%d", &index)
	if err != nil {
		return 0, fmt.Errorf("invalid array index format: %s", indexStr)
	}

	return index, nil
}

// matchesNamespacePattern checks if a target namespace matches the rule's namespace pattern.
// If no pattern is specified, the rule applies to all namespaces.
// Supports glob patterns with * (matches any characters) and ? (matches single character).
func matchesNamespacePattern(rule Rule, targetNamespace string) bool {
	// If no pattern is specified, rule applies to all namespaces
	if rule.NamespacePattern == nil || *rule.NamespacePattern == "" {
		return true
	}

	pattern := *rule.NamespacePattern
	return matchGlob(pattern, targetNamespace)
}

// matchGlob performs simple glob pattern matching with support for * and ?.
// * matches zero or more characters
// ? matches exactly one character
func matchGlob(pattern, text string) bool {
	// Fast path for exact match or wildcard-only pattern
	if pattern == text {
		return true
	}
	if pattern == "*" {
		return true
	}

	return matchGlobRecursive(pattern, text, 0, 0)
}

// matchGlobRecursive implements recursive glob matching.
func matchGlobRecursive(pattern, text string, pIdx, tIdx int) bool {
	pLen := len(pattern)
	tLen := len(text)

	// Base cases
	if pIdx == pLen {
		return tIdx == tLen
	}

	// Check for wildcard
	if pattern[pIdx] == '*' {
		// Try matching zero characters (skip *)
		if matchGlobRecursive(pattern, text, pIdx+1, tIdx) {
			return true
		}
		// Try matching one or more characters
		if tIdx < tLen && matchGlobRecursive(pattern, text, pIdx, tIdx+1) {
			return true
		}
		return false
	}

	// Check for single character wildcard or exact match
	if tIdx < tLen && (pattern[pIdx] == '?' || pattern[pIdx] == text[tIdx]) {
		return matchGlobRecursive(pattern, text, pIdx+1, tIdx+1)
	}

	return false
}

// templateFuncs returns custom template functions.
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"upper":      strings.ToUpper,
		"lower":      strings.ToLower,
		"trimPrefix": strings.TrimPrefix,
		"trimSuffix": strings.TrimSuffix,
		"replace":    strings.ReplaceAll,
		"hasPrefix":  strings.HasPrefix,
		"hasSuffix":  strings.HasSuffix,
		"default": func(defaultValue interface{}, value interface{}) interface{} {
			if value == nil || value == "" {
				return defaultValue
			}
			return value
		},
	}
}

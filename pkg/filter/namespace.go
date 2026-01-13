// Package filter provides namespace filtering and pattern matching functionality.
package filter

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/lukaszraczylo/kubemirror/pkg/constants"
)

// PatternValidationResult contains the result of validating a pattern.
type PatternValidationResult struct {
	Error   error
	Pattern string
	Valid   bool
}

// ValidatePattern checks if a glob pattern is syntactically valid.
// Returns an error if the pattern cannot be compiled by filepath.Match.
func ValidatePattern(pattern string) error {
	// Empty pattern is invalid
	if pattern == "" {
		return fmt.Errorf("empty pattern")
	}

	// Special keywords are always valid
	if pattern == constants.TargetNamespacesAll || pattern == constants.TargetNamespacesAllLabeled {
		return nil
	}

	// Use filepath.Match with a test string to validate pattern syntax
	// We use "test" as a dummy value - we only care about the error
	_, err := filepath.Match(pattern, "test")
	if err != nil {
		return fmt.Errorf("invalid glob pattern %q: %w", pattern, err)
	}

	return nil
}

// ValidatePatterns validates a list of patterns and returns results for each.
// Returns a slice of validation results and a boolean indicating if all patterns are valid.
func ValidatePatterns(patterns []string) ([]PatternValidationResult, bool) {
	if len(patterns) == 0 {
		return nil, true
	}

	results := make([]PatternValidationResult, len(patterns))
	allValid := true

	for i, pattern := range patterns {
		err := ValidatePattern(pattern)
		results[i] = PatternValidationResult{
			Pattern: pattern,
			Valid:   err == nil,
			Error:   err,
		}
		if err != nil {
			allValid = false
		}
	}

	return results, allValid
}

// InvalidPatterns returns only the invalid patterns from a validation result.
func InvalidPatterns(results []PatternValidationResult) []PatternValidationResult {
	var invalid []PatternValidationResult
	for _, r := range results {
		if !r.Valid {
			invalid = append(invalid, r)
		}
	}
	return invalid
}

// NamespaceFilter handles namespace filtering logic including patterns and exclusions.
type NamespaceFilter struct {
	excludedNamespaces map[string]bool
	includedPatterns   []string
}

// NewNamespaceFilter creates a new NamespaceFilter with the given exclusions and inclusions.
func NewNamespaceFilter(excluded, included []string) *NamespaceFilter {
	excludedMap := make(map[string]bool)
	for _, ns := range excluded {
		excludedMap[ns] = true
	}

	return &NamespaceFilter{
		excludedNamespaces: excludedMap,
		includedPatterns:   included,
	}
}

// IsAllowed checks if a namespace is allowed based on filters.
// Returns true if the namespace passes all filters.
func (nf *NamespaceFilter) IsAllowed(namespace string) bool {
	// Check if explicitly excluded
	if nf.excludedNamespaces[namespace] {
		return false
	}

	// If no include patterns specified, allow all (except excluded)
	if len(nf.includedPatterns) == 0 {
		return true
	}

	// Check if matches any include pattern
	for _, pattern := range nf.includedPatterns {
		if matchesPattern(namespace, pattern) {
			return true
		}
	}

	return false
}

// MatchesPattern checks if a namespace name matches the given pattern.
// Supports glob-style patterns: "app-*", "*-prod", "stage-*-db"
func matchesPattern(namespace, pattern string) bool {
	// Direct match
	if namespace == pattern {
		return true
	}

	// Use filepath.Match for glob-style matching
	// filepath.Match supports * (any sequence) and ? (single char)
	matched, err := filepath.Match(pattern, namespace)
	if err != nil {
		// Invalid pattern, no match
		return false
	}

	return matched
}

// ParseTargetNamespaces parses the target-namespaces annotation value.
// Returns a list of namespace patterns or special keywords.
// Input: "ns1,ns2,app-*" or "all" or "all-labeled"
func ParseTargetNamespaces(value string) []string {
	if value == "" {
		return nil
	}

	// Trim whitespace
	value = strings.TrimSpace(value)

	// Handle special keywords
	if value == constants.TargetNamespacesAll || value == constants.TargetNamespacesAllLabeled {
		return []string{value}
	}

	// Split by comma and trim each entry
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}

	return result
}

// ResolveTargetNamespaces resolves namespace patterns to concrete namespace names.
// Handles "all", "all-labeled", and glob patterns.
// Parameters:
//   - patterns: namespace patterns from annotation
//   - allNamespaces: list of all namespaces in cluster
//   - allowMirrorsNamespaces: namespaces with allow-mirrors label
//   - optOutNamespaces: namespaces with allow-mirrors="false" (explicitly opted out)
//   - sourceNamespace: exclude this namespace to prevent self-copy
//   - filter: namespace filter for exclusions
//
// Returns: list of concrete target namespace names
func ResolveTargetNamespaces(
	patterns []string,
	allNamespaces []string,
	allowMirrorsNamespaces []string,
	optOutNamespaces []string,
	sourceNamespace string,
	filter *NamespaceFilter,
) []string {
	if len(patterns) == 0 {
		return nil
	}

	// Create map of opt-out namespaces for fast lookup
	optOutMap := make(map[string]bool)
	for _, ns := range optOutNamespaces {
		optOutMap[ns] = true
	}

	// Use map to deduplicate
	targetMap := make(map[string]bool)

	for _, pattern := range patterns {
		switch pattern {
		case constants.TargetNamespacesAll:
			// Mirror to all namespaces (except source, excluded, and opt-out)
			// This implements opt-OUT model: namespaces without labels get mirrors
			// Only namespaces with allow-mirrors="false" are excluded
			for _, ns := range allNamespaces {
				if ns != sourceNamespace && filter.IsAllowed(ns) && !optOutMap[ns] {
					targetMap[ns] = true
				}
			}

		case constants.TargetNamespacesAllLabeled:
			// Mirror only to namespaces with allow-mirrors="true" label
			// This implements opt-IN model
			for _, ns := range allowMirrorsNamespaces {
				if ns != sourceNamespace && filter.IsAllowed(ns) {
					targetMap[ns] = true
				}
			}

		default:
			// Check if it's a pattern or direct namespace name
			if strings.Contains(pattern, "*") || strings.Contains(pattern, "?") {
				// It's a glob pattern - match against all namespaces
				for _, ns := range allNamespaces {
					if matchesPattern(ns, pattern) && ns != sourceNamespace && filter.IsAllowed(ns) {
						targetMap[ns] = true
					}
				}
			} else {
				// Direct namespace name
				if pattern != sourceNamespace && filter.IsAllowed(pattern) {
					targetMap[pattern] = true
				}
			}
		}
	}

	// Convert map to slice
	result := make([]string, 0, len(targetMap))
	for ns := range targetMap {
		result = append(result, ns)
	}

	return result
}

// Package transformer provides resource transformation capabilities for kubemirror.
package transformer

import (
	"fmt"
	"time"
)

// TransformRules represents a collection of transformation rules.
type TransformRules struct {
	Rules []Rule `yaml:"rules"`
}

// Rule represents a single transformation rule.
type Rule struct {
	// Path is the JSONPath to the field to transform (e.g., "data.LOG_LEVEL", "metadata.labels.env")
	Path string `yaml:"path"`

	// Value sets a static value (mutually exclusive with Template, Merge, Delete)
	Value *string `yaml:"value,omitempty"`

	// Template uses Go templates to generate the value (mutually exclusive with Value, Merge, Delete)
	Template *string `yaml:"template,omitempty"`

	// Merge merges a map into the target field (mutually exclusive with Value, Template, Delete)
	Merge map[string]interface{} `yaml:"merge,omitempty"`

	// Delete removes the field (mutually exclusive with Value, Template, Merge)
	Delete bool `yaml:"delete,omitempty"`

	// NamespacePattern is an optional glob pattern that limits this rule to specific target namespaces
	// Examples: "prod-*", "*-staging", "preprod-*"
	// If not specified, the rule applies to all namespaces
	NamespacePattern *string `yaml:"namespacePattern,omitempty"`
}

// TransformContext provides context variables for template evaluation.
type TransformContext struct {
	// TargetNamespace is the namespace where the mirror is being created
	TargetNamespace string

	// SourceNamespace is the namespace of the source resource
	SourceNamespace string

	// SourceName is the name of the source resource
	SourceName string

	// TargetName is the name of the target resource (usually same as source)
	TargetName string

	// Labels is a copy of the source resource's labels
	Labels map[string]string

	// Annotations is a copy of the source resource's annotations
	Annotations map[string]string
}

// TransformOptions configures the transformation behavior.
type TransformOptions struct {
	// Strict mode causes transformation errors to be fatal (blocks mirroring)
	Strict bool

	// MaxRules limits the number of transformation rules per resource
	MaxRules int

	// MaxRuleSize limits the size of each rule in bytes
	MaxRuleSize int

	// TemplateTimeout limits template execution time
	TemplateTimeout time.Duration
}

// DefaultTransformOptions returns default transformation options.
func DefaultTransformOptions() TransformOptions {
	return TransformOptions{
		Strict:          false,
		MaxRules:        50,
		MaxRuleSize:     10 * 1024, // 10KB
		TemplateTimeout: 100 * time.Millisecond,
	}
}

// Validate checks if the rule is valid.
func (r *Rule) Validate() error {
	if r.Path == "" {
		return fmt.Errorf("rule path cannot be empty")
	}

	// Count how many actions are set
	actionCount := 0
	if r.Value != nil {
		actionCount++
	}
	if r.Template != nil {
		actionCount++
	}
	if r.Merge != nil {
		actionCount++
	}
	if r.Delete {
		actionCount++
	}

	if actionCount == 0 {
		return fmt.Errorf("rule must specify one of: value, template, merge, or delete")
	}

	if actionCount > 1 {
		return fmt.Errorf("rule cannot specify multiple actions (value, template, merge, delete are mutually exclusive)")
	}

	return nil
}

// Type returns the type of transformation this rule performs.
func (r *Rule) Type() RuleType {
	switch {
	case r.Value != nil:
		return RuleTypeValue
	case r.Template != nil:
		return RuleTypeTemplate
	case r.Merge != nil:
		return RuleTypeMerge
	case r.Delete:
		return RuleTypeDelete
	default:
		return RuleTypeUnknown
	}
}

// RuleType represents the type of transformation.
type RuleType int

const (
	// RuleTypeUnknown represents an unknown or invalid rule type
	RuleTypeUnknown RuleType = iota

	// RuleTypeValue sets a static value
	RuleTypeValue

	// RuleTypeTemplate uses Go templates to generate a value
	RuleTypeTemplate

	// RuleTypeMerge merges a map into the target field
	RuleTypeMerge

	// RuleTypeDelete removes a field
	RuleTypeDelete
)

// String returns the string representation of the rule type.
func (rt RuleType) String() string {
	switch rt {
	case RuleTypeValue:
		return "value"
	case RuleTypeTemplate:
		return "template"
	case RuleTypeMerge:
		return "merge"
	case RuleTypeDelete:
		return "delete"
	default:
		return "unknown"
	}
}

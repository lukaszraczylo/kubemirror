package transformer

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRule_Validate(t *testing.T) {
	tests := []struct {
		name    string
		rule    Rule
		wantErr bool
		errMsg  string
	}{
		// Good cases
		{
			name: "valid value rule",
			rule: Rule{
				Path:  "data.KEY",
				Value: stringPtr("value"),
			},
			wantErr: false,
		},
		{
			name: "valid template rule",
			rule: Rule{
				Path:     "data.KEY",
				Template: stringPtr("{{.TargetNamespace}}"),
			},
			wantErr: false,
		},
		{
			name: "valid merge rule",
			rule: Rule{
				Path: "metadata.labels",
				Merge: map[string]interface{}{
					"key": "value",
				},
			},
			wantErr: false,
		},
		{
			name: "valid delete rule",
			rule: Rule{
				Path:   "data.KEY",
				Delete: true,
			},
			wantErr: false,
		},

		// Bad cases
		{
			name: "empty path",
			rule: Rule{
				Value: stringPtr("value"),
			},
			wantErr: true,
			errMsg:  "path cannot be empty",
		},
		{
			name: "no action specified",
			rule: Rule{
				Path: "data.KEY",
			},
			wantErr: true,
			errMsg:  "must specify one of",
		},
		{
			name: "multiple actions - value and template",
			rule: Rule{
				Path:     "data.KEY",
				Value:    stringPtr("value"),
				Template: stringPtr("{{.TargetNamespace}}"),
			},
			wantErr: true,
			errMsg:  "cannot specify multiple actions",
		},
		{
			name: "multiple actions - value and merge",
			rule: Rule{
				Path:  "data.KEY",
				Value: stringPtr("value"),
				Merge: map[string]interface{}{"key": "value"},
			},
			wantErr: true,
			errMsg:  "cannot specify multiple actions",
		},
		{
			name: "multiple actions - template and delete",
			rule: Rule{
				Path:     "data.KEY",
				Template: stringPtr("{{.TargetNamespace}}"),
				Delete:   true,
			},
			wantErr: true,
			errMsg:  "cannot specify multiple actions",
		},

		// Edge cases
		{
			name: "path with special characters",
			rule: Rule{
				Path:  "data.my-key.sub-key",
				Value: stringPtr("value"),
			},
			wantErr: false,
		},
		{
			name: "merge with empty map",
			rule: Rule{
				Path:  "metadata.labels",
				Merge: map[string]interface{}{},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.rule.Validate()

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRule_Type(t *testing.T) {
	tests := []struct {
		name     string
		rule     Rule
		wantType RuleType
	}{
		{
			name: "value rule",
			rule: Rule{
				Path:  "data.KEY",
				Value: stringPtr("value"),
			},
			wantType: RuleTypeValue,
		},
		{
			name: "template rule",
			rule: Rule{
				Path:     "data.KEY",
				Template: stringPtr("{{.TargetNamespace}}"),
			},
			wantType: RuleTypeTemplate,
		},
		{
			name: "merge rule",
			rule: Rule{
				Path:  "metadata.labels",
				Merge: map[string]interface{}{"key": "value"},
			},
			wantType: RuleTypeMerge,
		},
		{
			name: "delete rule",
			rule: Rule{
				Path:   "data.KEY",
				Delete: true,
			},
			wantType: RuleTypeDelete,
		},
		{
			name: "unknown rule (no action)",
			rule: Rule{
				Path: "data.KEY",
			},
			wantType: RuleTypeUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType := tt.rule.Type()
			assert.Equal(t, tt.wantType, gotType)
		})
	}
}

func TestRuleType_String(t *testing.T) {
	tests := []struct {
		name     string
		ruleType RuleType
		want     string
	}{
		{name: "value", ruleType: RuleTypeValue, want: "value"},
		{name: "template", ruleType: RuleTypeTemplate, want: "template"},
		{name: "merge", ruleType: RuleTypeMerge, want: "merge"},
		{name: "delete", ruleType: RuleTypeDelete, want: "delete"},
		{name: "unknown", ruleType: RuleTypeUnknown, want: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.ruleType.String()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDefaultTransformOptions(t *testing.T) {
	opts := DefaultTransformOptions()

	assert.False(t, opts.Strict, "default should not be strict mode")
	assert.Equal(t, 50, opts.MaxRules, "default max rules should be 50")
	assert.Equal(t, 10*1024, opts.MaxRuleSize, "default max rule size should be 10KB")
	assert.Equal(t, 100*time.Millisecond, opts.TemplateTimeout, "default timeout should be 100ms")
}

// stringPtr is a helper to create string pointers
func stringPtr(s string) *string {
	return &s
}

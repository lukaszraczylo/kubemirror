package filter

import (
	"fmt"
	"testing"

	"github.com/lukaszraczylo/kubemirror/pkg/constants"
	"github.com/stretchr/testify/assert"
)

func TestNamespaceFilter_IsAllowed(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		excluded  []string
		included  []string
		want      bool
	}{
		{
			name:      "allow when no filters",
			excluded:  []string{},
			included:  []string{},
			namespace: "app1",
			want:      true,
		},
		{
			name:      "deny when explicitly excluded",
			excluded:  []string{"kube-system", "kube-public"},
			included:  []string{},
			namespace: "kube-system",
			want:      false,
		},
		{
			name:      "allow when not excluded",
			excluded:  []string{"kube-system"},
			included:  []string{},
			namespace: "app1",
			want:      true,
		},
		{
			name:      "allow when matches include pattern",
			excluded:  []string{},
			included:  []string{"app-*"},
			namespace: "app-frontend",
			want:      true,
		},
		{
			name:      "deny when doesn't match include pattern",
			excluded:  []string{},
			included:  []string{"app-*"},
			namespace: "backend",
			want:      false,
		},
		{
			name:      "deny when excluded even if matches include",
			excluded:  []string{"app-bad"},
			included:  []string{"app-*"},
			namespace: "app-bad",
			want:      false,
		},
		{
			name:      "allow when matches one of multiple patterns",
			excluded:  []string{},
			included:  []string{"app-*", "prod-*"},
			namespace: "prod-db",
			want:      true,
		},
		{
			name:      "allow direct match in include list",
			excluded:  []string{},
			included:  []string{"specific-ns"},
			namespace: "specific-ns",
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nf := NewNamespaceFilter(tt.excluded, tt.included)
			got := nf.IsAllowed(tt.namespace)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMatchesPattern(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		pattern   string
		want      bool
	}{
		{
			name:      "exact match",
			namespace: "app-frontend",
			pattern:   "app-frontend",
			want:      true,
		},
		{
			name:      "wildcard at end",
			namespace: "app-frontend",
			pattern:   "app-*",
			want:      true,
		},
		{
			name:      "wildcard at start",
			namespace: "app-frontend",
			pattern:   "*-frontend",
			want:      true,
		},
		{
			name:      "wildcard in middle",
			namespace: "app-prod-frontend",
			pattern:   "app-*-frontend",
			want:      true,
		},
		{
			name:      "multiple wildcards",
			namespace: "my-app-prod-db",
			pattern:   "*-app-*-db",
			want:      true,
		},
		{
			name:      "single char wildcard",
			namespace: "app1",
			pattern:   "app?",
			want:      true,
		},
		{
			name:      "no match",
			namespace: "backend",
			pattern:   "app-*",
			want:      false,
		},
		{
			name:      "empty pattern matches empty string",
			namespace: "",
			pattern:   "",
			want:      true,
		},
		{
			name:      "pattern doesn't match different namespace",
			namespace: "production-app",
			pattern:   "prod-*",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesPattern(tt.namespace, tt.pattern)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseTargetNamespaces(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  []string
	}{
		{
			name:  "empty string",
			value: "",
			want:  nil,
		},
		{
			name:  "single namespace",
			value: "app1",
			want:  []string{"app1"},
		},
		{
			name:  "multiple namespaces",
			value: "app1,app2,app3",
			want:  []string{"app1", "app2", "app3"},
		},
		{
			name:  "with whitespace",
			value: "app1, app2 , app3",
			want:  []string{"app1", "app2", "app3"},
		},
		{
			name:  "special keyword 'all'",
			value: "all",
			want:  []string{"all"},
		},
		{
			name:  "special keyword 'all-labeled'",
			value: "all-labeled",
			want:  []string{"all-labeled"},
		},
		{
			name:  "mixed patterns",
			value: "app1,app-*,prod-*",
			want:  []string{"app1", "app-*", "prod-*"},
		},
		{
			name:  "trailing comma",
			value: "app1,app2,",
			want:  []string{"app1", "app2"},
		},
		{
			name:  "empty entries ignored",
			value: "app1,,app2",
			want:  []string{"app1", "app2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseTargetNamespaces(tt.value)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveTargetNamespaces(t *testing.T) {
	allNamespaces := []string{"app1", "app2", "app-frontend", "app-backend", "prod-db", "prod-api", "kube-system", "default"}
	allowMirrorsNamespaces := []string{"app1", "app-frontend", "prod-db"}
	excludeFilter := NewNamespaceFilter([]string{"kube-system"}, []string{})

	tests := []struct {
		name                   string
		patterns               []string
		allNamespaces          []string
		allowMirrorsNamespaces []string
		sourceNamespace        string
		filter                 *NamespaceFilter
		wantContains           []string
		wantNotContains        []string
	}{
		{
			name:                   "empty patterns",
			patterns:               []string{},
			allNamespaces:          allNamespaces,
			allowMirrorsNamespaces: allowMirrorsNamespaces,
			sourceNamespace:        "default",
			filter:                 excludeFilter,
			wantContains:           []string{},
			wantNotContains:        allNamespaces,
		},
		{
			name:                   "all keyword",
			patterns:               []string{constants.TargetNamespacesAll},
			allNamespaces:          allNamespaces,
			allowMirrorsNamespaces: allowMirrorsNamespaces,
			sourceNamespace:        "default",
			filter:                 excludeFilter,
			wantContains:           []string{"app1", "app2", "app-frontend", "prod-db"},
			wantNotContains:        []string{"default", "kube-system"}, // excluded: source and kube-system
		},
		{
			name:                   "all-labeled keyword",
			patterns:               []string{constants.TargetNamespacesAllLabeled},
			allNamespaces:          allNamespaces,
			allowMirrorsNamespaces: allowMirrorsNamespaces,
			sourceNamespace:        "default",
			filter:                 excludeFilter,
			wantContains:           []string{"app1", "app-frontend", "prod-db"},
			wantNotContains:        []string{"app2", "app-backend", "default"},
		},
		{
			name:                   "glob pattern app-*",
			patterns:               []string{"app-*"},
			allNamespaces:          allNamespaces,
			allowMirrorsNamespaces: allowMirrorsNamespaces,
			sourceNamespace:        "default",
			filter:                 excludeFilter,
			wantContains:           []string{"app-frontend", "app-backend"},
			wantNotContains:        []string{"app1", "app2", "prod-db"},
		},
		{
			name:                   "multiple patterns",
			patterns:               []string{"app-*", "prod-*"},
			allNamespaces:          allNamespaces,
			allowMirrorsNamespaces: allowMirrorsNamespaces,
			sourceNamespace:        "default",
			filter:                 excludeFilter,
			wantContains:           []string{"app-frontend", "app-backend", "prod-db", "prod-api"},
			wantNotContains:        []string{"app1", "app2", "default"},
		},
		{
			name:                   "direct namespace names",
			patterns:               []string{"app1", "app2"},
			allNamespaces:          allNamespaces,
			allowMirrorsNamespaces: allowMirrorsNamespaces,
			sourceNamespace:        "default",
			filter:                 excludeFilter,
			wantContains:           []string{"app1", "app2"},
			wantNotContains:        []string{"app-frontend", "prod-db", "default"},
		},
		{
			name:                   "exclude source namespace",
			patterns:               []string{"app1"},
			allNamespaces:          allNamespaces,
			allowMirrorsNamespaces: allowMirrorsNamespaces,
			sourceNamespace:        "app1",
			filter:                 excludeFilter,
			wantContains:           []string{},
			wantNotContains:        []string{"app1"}, // app1 is source, excluded
		},
		{
			name:                   "deduplication",
			patterns:               []string{"app-*", "app-frontend"}, // app-frontend matches both
			allNamespaces:          allNamespaces,
			allowMirrorsNamespaces: allowMirrorsNamespaces,
			sourceNamespace:        "default",
			filter:                 excludeFilter,
			wantContains:           []string{"app-frontend", "app-backend"},
			wantNotContains:        []string{"app1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveTargetNamespaces(
				tt.patterns,
				tt.allNamespaces,
				tt.allowMirrorsNamespaces,
				tt.sourceNamespace,
				tt.filter,
			)

			// Check that all expected namespaces are present
			for _, ns := range tt.wantContains {
				assert.Contains(t, got, ns, "should contain %s", ns)
			}

			// Check that unwanted namespaces are not present
			for _, ns := range tt.wantNotContains {
				assert.NotContains(t, got, ns, "should not contain %s", ns)
			}
		})
	}
}

// Edge case tests
func TestResolveTargetNamespaces_EdgeCases(t *testing.T) {
	t.Run("no namespaces in cluster", func(t *testing.T) {
		got := ResolveTargetNamespaces(
			[]string{"all"},
			[]string{},
			[]string{},
			"default",
			NewNamespaceFilter([]string{}, []string{}),
		)
		assert.Empty(t, got)
	})

	t.Run("invalid pattern doesn't crash", func(t *testing.T) {
		// filepath.Match should handle this gracefully
		got := ResolveTargetNamespaces(
			[]string{"[invalid"},
			[]string{"app1"},
			[]string{},
			"default",
			NewNamespaceFilter([]string{}, []string{}),
		)
		assert.NotNil(t, got)
	})

	t.Run("all excludes everything when filter denies all", func(t *testing.T) {
		strictFilter := NewNamespaceFilter([]string{}, []string{"specific-ns"})
		got := ResolveTargetNamespaces(
			[]string{"all"},
			[]string{"app1", "app2", "app3"},
			[]string{},
			"default",
			strictFilter,
		)
		// Only "specific-ns" would be allowed, but it's not in allNamespaces
		assert.Empty(t, got)
	})
}

// Benchmark tests for critical paths

func BenchmarkParseTargetNamespaces(b *testing.B) {
	tests := []struct {
		name  string
		value string
	}{
		{
			name:  "single namespace",
			value: "app1",
		},
		{
			name:  "10 namespaces",
			value: "app1,app2,app3,app4,app5,app6,app7,app8,app9,app10",
		},
		{
			name:  "50 namespaces with whitespace",
			value: "app1, app2, app3, app4, app5, app6, app7, app8, app9, app10, app11, app12, app13, app14, app15, app16, app17, app18, app19, app20, app21, app22, app23, app24, app25, app26, app27, app28, app29, app30, app31, app32, app33, app34, app35, app36, app37, app38, app39, app40, app41, app42, app43, app44, app45, app46, app47, app48, app49, app50",
		},
		{
			name:  "mixed patterns",
			value: "app1,app-*,prod-*,staging-*,dev-*",
		},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = ParseTargetNamespaces(tt.value)
			}
		})
	}
}

func BenchmarkMatchesPattern(b *testing.B) {
	tests := []struct {
		name      string
		namespace string
		pattern   string
	}{
		{
			name:      "exact match",
			namespace: "app-frontend",
			pattern:   "app-frontend",
		},
		{
			name:      "simple wildcard",
			namespace: "app-frontend",
			pattern:   "app-*",
		},
		{
			name:      "complex wildcard",
			namespace: "my-app-prod-db",
			pattern:   "*-app-*-db",
		},
		{
			name:      "no match",
			namespace: "production-api",
			pattern:   "app-*",
		},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = matchesPattern(tt.namespace, tt.pattern)
			}
		})
	}
}

func BenchmarkNamespaceFilter_IsAllowed(b *testing.B) {
	tests := []struct {
		name      string
		filter    *NamespaceFilter
		namespace string
	}{
		{
			name:      "no filters (always allow)",
			filter:    NewNamespaceFilter([]string{}, []string{}),
			namespace: "app1",
		},
		{
			name:      "simple exclusion",
			filter:    NewNamespaceFilter([]string{"kube-system", "kube-public", "kube-node-lease"}, []string{}),
			namespace: "app1",
		},
		{
			name:      "pattern inclusion",
			filter:    NewNamespaceFilter([]string{}, []string{"app-*", "prod-*"}),
			namespace: "app-frontend",
		},
		{
			name:      "complex filtering",
			filter:    NewNamespaceFilter([]string{"kube-system", "test-*"}, []string{"app-*", "prod-*"}),
			namespace: "prod-api",
		},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = tt.filter.IsAllowed(tt.namespace)
			}
		})
	}
}

func BenchmarkResolveTargetNamespaces(b *testing.B) {
	// Generate realistic namespace list
	allNamespaces := make([]string, 100)
	for i := 0; i < 100; i++ {
		if i < 30 {
			allNamespaces[i] = fmt.Sprintf("app-%d", i)
		} else if i < 60 {
			allNamespaces[i] = fmt.Sprintf("prod-%d", i)
		} else if i < 90 {
			allNamespaces[i] = fmt.Sprintf("staging-%d", i)
		} else {
			allNamespaces[i] = fmt.Sprintf("test-%d", i)
		}
	}

	allowMirrorsNamespaces := allNamespaces[:50] // Half have opt-in label
	filter := NewNamespaceFilter([]string{"kube-system", "kube-public"}, []string{})

	tests := []struct {
		name     string
		patterns []string
	}{
		{
			name:     "all keyword",
			patterns: []string{constants.TargetNamespacesAll},
		},
		{
			name:     "all-labeled keyword",
			patterns: []string{constants.TargetNamespacesAllLabeled},
		},
		{
			name:     "single pattern",
			patterns: []string{"app-*"},
		},
		{
			name:     "multiple patterns",
			patterns: []string{"app-*", "prod-*", "staging-*"},
		},
		{
			name:     "direct names",
			patterns: []string{"app-1", "app-2", "prod-1", "prod-2"},
		},
		{
			name:     "mixed direct and patterns",
			patterns: []string{"app-1", "prod-*", "staging-5"},
		},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = ResolveTargetNamespaces(
					tt.patterns,
					allNamespaces,
					allowMirrorsNamespaces,
					"default",
					filter,
				)
			}
		})
	}
}

func BenchmarkResolveTargetNamespaces_LargeScale(b *testing.B) {
	// Simulate large cluster (1000 namespaces)
	allNamespaces := make([]string, 1000)
	for i := 0; i < 1000; i++ {
		allNamespaces[i] = fmt.Sprintf("namespace-%d", i)
	}

	allowMirrorsNamespaces := allNamespaces[:500]
	filter := NewNamespaceFilter(constants.DefaultExcludedNamespaces, []string{})

	b.Run("1000 namespaces with 'all' keyword", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = ResolveTargetNamespaces(
				[]string{constants.TargetNamespacesAll},
				allNamespaces,
				allowMirrorsNamespaces,
				"default",
				filter,
			)
		}
	})

	b.Run("1000 namespaces with pattern matching", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = ResolveTargetNamespaces(
				[]string{"namespace-*"},
				allNamespaces,
				allowMirrorsNamespaces,
				"default",
				filter,
			)
		}
	})
}

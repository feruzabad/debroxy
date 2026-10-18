package archtest_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/archtest"
)

// TestRepository_follows_architecture_rules is the gate every stage must pass.
func TestRepository_follows_architecture_rules(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	violations, err := archtest.CheckRepository(root)

	require.NoError(t, err)
	for _, violation := range violations {
		t.Error(violation)
	}
}

func TestCheckFile_domain_purity(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		source    string
		wantRules []string
	}{
		{
			name:   "time_as_parameter_is_allowed",
			source: "package remoteitem\nimport \"time\"\nfunc f(now time.Time) time.Time { return now.Add(time.Second) }\n",
		},
		{
			name:      "time_now_is_rejected",
			source:    "package remoteitem\nimport \"time\"\nvar x = time.Now()\n",
			wantRules: []string{"D-10"},
		},
		{
			name:      "aliased_time_since_is_rejected",
			source:    "package remoteitem\nimport t \"time\"\nfunc f(s t.Time) t.Duration { return t.Since(s) }\n",
			wantRules: []string{"D-10"},
		},
		{
			name:      "struct_tags_are_rejected",
			source:    "package remoteitem\ntype row struct { ID string `json:\"id\"` }\n",
			wantRules: []string{"DEP-1"},
		},
		{
			name:      "generic_package_name_is_rejected",
			source:    "package utils\n",
			wantRules: []string{"N-5"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fileSet := token.NewFileSet()
			file, err := parser.ParseFile(fileSet, "file.go", tc.source, parser.SkipObjectResolution)
			require.NoError(t, err)
			pkg, err := archtest.Classify("internal/job/domain/remoteitem")
			require.NoError(t, err)

			violations := archtest.CheckFile(fileSet, pkg, file, module)

			var rules []string
			for _, violation := range violations {
				var sourceErr archtest.SourceError
				require.ErrorAs(t, violation, &sourceErr)
				rules = append(rules, sourceErr.Rule)
			}
			assert.Equal(t, tc.wantRules, rules)
		})
	}
}

package archtest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/archtest"
)

func TestClassify(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		path        string
		wantLayer   archtest.Layer
		wantContext string
		wantAdapter string
	}{
		{"cmd/debroxy", archtest.LayerCmd, "", ""},
		{"internal/common/server", archtest.LayerCommon, "", ""},
		{"internal/common/server/httperr", archtest.LayerCommon, "", ""},
		{"internal/common/httpclient", archtest.LayerCommon, "", ""},
		{"internal/job/domain/remoteitem", archtest.LayerDomain, "job", ""},
		{"internal/job/ports/in", archtest.LayerPortsIn, "job", ""},
		{"internal/job/ports/out", archtest.LayerPortsOut, "job", ""},
		{"internal/job/app/command", archtest.LayerApp, "job", ""},
		{"internal/provider/app/operation", archtest.LayerApp, "provider", ""},
		{"internal/job/adapters/in/webhook", archtest.LayerAdapterIn, "job", "in/webhook"},
		{"internal/job/adapters/out/sqlite/sqlcgen", archtest.LayerAdapterOut, "job", "out/sqlite"},
		{"internal/job/service", archtest.LayerService, "job", ""},
		{"migrations", archtest.LayerMigrations, "", ""},
		{"web", archtest.LayerWeb, "", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()

			pkg, err := archtest.Classify(tc.path)

			require.NoError(t, err)
			assert.Equal(t, tc.wantLayer, pkg.Layer)
			assert.Equal(t, tc.wantContext, pkg.Context)
			assert.Equal(t, tc.wantAdapter, pkg.Adapter)
		})
	}
}

func TestClassify_rejects_paths_outside_the_layout(t *testing.T) {
	t.Parallel()

	paths := []string{
		"pkg/thing",
		"internal/billing/domain/invoice",
		"internal/common/util",
		"internal/job",
		"internal/job/domain",
		"internal/job/domain/remoteitem/state",
		"internal/job/ports/both",
		"internal/job/app/handler",
		"internal/job/adapters/sqlite",
		"internal/job/service/wiring",
		"internal/job/helpers",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			_, err := archtest.Classify(path)

			var layoutErr archtest.LayoutError
			assert.ErrorAs(t, err, &layoutErr)
		})
	}
}

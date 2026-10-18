package archtest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/archtest"
)

const module = "github.com/feruzabad/debroxy"

func TestCheckImport(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		from     string
		imp      string
		wantRule string // empty: allowed
	}{
		// Domain (DEP-1).
		{"domain_uses_stdlib_time", "internal/job/domain/remoteitem", "time", ""},
		{"domain_uses_common_errors", "internal/job/domain/remoteitem", module + "/internal/common/errors", ""},
		{"domain_rejects_context", "internal/job/domain/remoteitem", "context", "DEP-1"},
		{"domain_rejects_net_http", "internal/job/domain/remoteitem", "net/http", "DEP-1"},
		{"domain_uses_net_url", "internal/job/domain/remoteitem", "net/url", ""},
		{"domain_rejects_net", "internal/job/domain/remoteitem", "net", "DEP-1"},
		{"domain_rejects_log_slog", "internal/job/domain/remoteitem", "log/slog", "DEP-1"},
		{"domain_rejects_database_sql", "internal/job/domain/remoteitem", "database/sql", "DEP-1"},
		{"domain_rejects_encoding_json", "internal/job/domain/remoteitem", "encoding/json", "DEP-1"},
		{"domain_rejects_third_party", "internal/job/domain/remoteitem", "github.com/google/uuid", "DEP-1/§3"},
		{"domain_rejects_common_clock", "internal/job/domain/remoteitem", module + "/internal/common/clock", "DEP-1/§3"},
		{"domain_rejects_own_ports", "internal/job/domain/remoteitem", module + "/internal/job/ports/out", "DEP-1"},
		{"domain_rejects_other_aggregate", "internal/job/domain/remoteitem", module + "/internal/job/domain/slotquota", "§3"},

		// Ports.
		{"ports_in_uses_own_domain", "internal/job/ports/in", module + "/internal/job/domain/remoteitem", ""},
		{"ports_in_uses_decorator", "internal/job/ports/in", module + "/internal/common/decorator", ""},
		{"ports_in_rejects_common_errors", "internal/job/ports/in", module + "/internal/common/errors", "§3"},
		{"ports_in_rejects_third_party", "internal/job/ports/in", "github.com/google/uuid", "DEP-1/§3"},
		{"ports_out_uses_own_domain", "internal/job/ports/out", module + "/internal/job/domain/remoteitem", ""},
		{"ports_out_uses_context", "internal/job/ports/out", "context", ""},
		{"ports_out_rejects_common", "internal/job/ports/out", module + "/internal/common/decorator", "§3"},
		{"ports_out_uses_own_ports_in_dtos", "internal/job/ports/out", module + "/internal/job/ports/in", ""},
		{"ports_out_rejects_other_context_ports_in", "internal/job/ports/out", module + "/internal/provider/ports/in", "DEP-4"},
		{"ports_in_rejects_ports_out", "internal/job/ports/in", module + "/internal/job/ports/out", "§3"},

		// App.
		{"app_uses_ports_and_domain", "internal/job/app/command", module + "/internal/job/ports/out", ""},
		{"app_uses_common", "internal/job/app/command", module + "/internal/common/clock", ""},
		{"app_rejects_adapters", "internal/job/app/command", module + "/internal/job/adapters/out/sqlite", "§3"},
		{"app_rejects_other_app_package", "internal/job/app/command", module + "/internal/job/app/query", "A-1/§3"},
		{"app_rejects_third_party", "internal/job/app/command", "github.com/google/uuid", "DEP-1/§3"},

		// Adapters (DEP-2, DEP-3).
		{"adapter_in_uses_ports_in", "internal/job/adapters/in/http", module + "/internal/job/ports/in", ""},
		{"adapter_in_uses_third_party", "internal/job/adapters/in/http", "github.com/oapi-codegen/runtime", ""},
		{"adapter_in_rejects_app", "internal/job/adapters/in/http", module + "/internal/job/app/command", "DEP-2"},
		{"adapter_in_rejects_ports_out", "internal/job/adapters/in/http", module + "/internal/job/ports/out", "§3"},
		{"adapter_out_uses_ports_out", "internal/job/adapters/out/sqlite", module + "/internal/job/ports/out", ""},
		{"adapter_out_uses_domain", "internal/job/adapters/out/sqlite", module + "/internal/job/domain/remoteitem", ""},
		{"adapter_out_uses_own_ports_in_dtos", "internal/job/adapters/out/sqlite", module + "/internal/job/ports/in", ""},
		{"adapter_out_uses_own_subpackage", "internal/job/adapters/out/sqlite", module + "/internal/job/adapters/out/sqlite/sqlcgen", ""},
		{"adapter_out_rejects_app", "internal/job/adapters/out/sqlite", module + "/internal/job/app/query", "DEP-2"},
		{"adapter_rejects_other_adapter", "internal/job/adapters/out/sqlite", module + "/internal/job/adapters/in/http", "DEP-3"},

		// Cross-context (DEP-4).
		{"supplier_adapter_uses_supplier_ports_in", "internal/job/adapters/out/provider", module + "/internal/provider/ports/in", ""},
		{"wrong_adapter_rejects_supplier_ports_in", "internal/job/adapters/out/sqlite", module + "/internal/provider/ports/in", "DEP-4"},
		{"adapter_rejects_supplier_domain", "internal/job/adapters/out/provider", module + "/internal/provider/domain/account", "DEP-4"},
		{"app_rejects_other_context", "internal/job/app/command", module + "/internal/provider/ports/in", "DEP-4"},
		{"domain_rejects_other_context", "internal/delivery/domain/signedlink", module + "/internal/job/domain/remoteitem", "DEP-4"},

		// Service and cmd.
		{"service_uses_own_adapters", "internal/job/service", module + "/internal/job/adapters/out/sqlite", ""},
		{"service_uses_own_app", "internal/job/service", module + "/internal/job/app/command", ""},
		{"service_uses_supplier_ports_in", "internal/delivery/service", module + "/internal/job/ports/in", ""},
		{"service_rejects_supplier_app", "internal/delivery/service", module + "/internal/job/app/query", "DEP-4"},
		{"service_rejects_supplier_adapter", "internal/delivery/service", module + "/internal/job/adapters/out/sqlite", "DEP-4"},
		{"cmd_uses_service", "cmd/debroxy", module + "/internal/job/service", ""},
		{"cmd_uses_common_sqlite", "cmd/debroxy", module + "/internal/common/sqlite", ""},
		{"cmd_uses_third_party", "cmd/debroxy", "go.yaml.in/yaml/v3", ""},
		{"cmd_rejects_ports", "cmd/debroxy", module + "/internal/job/ports/in", "§3"},
		{"cmd_uses_common_clock", "cmd/debroxy", module + "/internal/common/clock", ""},
		{"cmd_rejects_app", "cmd/debroxy", module + "/internal/job/app/command", "§3"},
		{"cmd_rejects_adapters", "cmd/debroxy", module + "/internal/job/adapters/out/sqlite", "§3"},
		{"cmd_rejects_domain", "cmd/debroxy", module + "/internal/job/domain/remoteitem", "§3"},

		// Shared kernel (DEP-7).
		{"common_uses_common", "internal/common/server", module + "/internal/common/errors", ""},
		{"common_rejects_context", "internal/common/server", module + "/internal/identity/ports/in", "DEP-7"},
		{"nothing_imports_cmd", "internal/job/service", module + "/cmd/debroxy", "§3"},

		// Embedded migrations (ADR 0015).
		{"cmd_uses_migrations", "cmd/debroxy", module + "/migrations", ""},
		{"cmd_uses_own_subpackage", "cmd/debroxy", module + "/cmd/debroxy/config", ""},
		{"service_rejects_migrations", "internal/job/service", module + "/migrations", "§3"},
		{"common_sqlite_rejects_migrations", "internal/common/sqlite", module + "/migrations", "§3"},
		{"migrations_uses_embed", "migrations", "embed", ""},
		{"migrations_rejects_other_stdlib", "migrations", "io/fs", "§3"},
		{"migrations_rejects_common", "migrations", module + "/internal/common/sqlite", "§3"},

		// Embedded browser client (ADR 0022).
		{"cmd_uses_web", "cmd/debroxy", module + "/web", ""},
		{"common_server_rejects_web", "internal/common/server", module + "/web", "§3"},
		{"service_rejects_web", "internal/job/service", module + "/web", "§3"},
		{"web_uses_embed", "web", "embed", ""},
		{"web_rejects_other_stdlib", "web", "io/fs", "§3"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			from, err := archtest.Classify(tc.from)
			require.NoError(t, err)

			err = archtest.CheckImport(from, tc.imp, module)

			if tc.wantRule == "" {
				assert.NoError(t, err)
				return
			}
			var dependencyErr archtest.DependencyError
			require.ErrorAs(t, err, &dependencyErr)
			assert.Equal(t, tc.wantRule, dependencyErr.Rule)
		})
	}
}

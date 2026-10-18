// Package archtest checks the repository against the binding layout and
// dependency rules of docs/ddd-rules.md (L-*, DEP-*, N-5). It holds no
// production code; its tests run with `go test ./...`.
package archtest

import (
	"fmt"
	"slices"
	"strings"
)

// Layer is the architectural role of a package (ddd-rules §1, §2).
type Layer struct{ s string }

var (
	LayerCommon     = Layer{"common"}
	LayerDomain     = Layer{"domain"}
	LayerPortsIn    = Layer{"ports/in"}
	LayerPortsOut   = Layer{"ports/out"}
	LayerApp        = Layer{"app"}
	LayerAdapterIn  = Layer{"adapters/in"}
	LayerAdapterOut = Layer{"adapters/out"}
	LayerService    = Layer{"service"}
	LayerCmd        = Layer{"cmd"}
	LayerTooling    = Layer{"tooling"}
	LayerMigrations = Layer{"migrations"}
	LayerWeb        = Layer{"web"}
)

func (l Layer) String() string { return l.s }
func (l Layer) IsZero() bool   { return l == Layer{} }

// BoundedContexts are the top-level packages under internal/ (spec §3).
var BoundedContexts = []string{"identity", "provider", "job", "delivery"}

// commonPackages are the shared-kernel packages of ddd-rules §2.
var commonPackages = []string{"errors", "decorator", "auth", "clock", "sqlite", "id", "server", "httpclient"}

// appPackages are the allowed sub-packages of app/ (ddd-rules §2, A-1, A-10).
var appPackages = []string{"command", "query", "operation"}

// forbiddenPackageNames are generic names ruled out by N-2 and N-5.
var forbiddenPackageNames = []string{"models", "types", "utils", "util", "helpers", "helper", "common", "misc"}

// Package is a classified package of this module.
type Package struct {
	// Path is the import path relative to the module root, e.g. "internal/job/domain/remoteitem".
	Path  string
	Layer Layer
	// Context is the bounded context, empty outside internal/<context>.
	Context string
	// Common is the shared-kernel package name ("errors", "server"), set only for LayerCommon.
	Common string
	// Adapter identifies one adapter tree ("in/http", "out/sqlite"); DEP-3 allows imports inside it.
	Adapter string
}

// LayoutError reports a package that has no place in the layout of ddd-rules §2.
type LayoutError struct {
	Path   string
	Reason string
}

func (e LayoutError) Error() string {
	return fmt.Sprintf("%s: violates L-2: %s", e.Path, e.Reason)
}

// Classify maps a module-relative package path to its architectural role.
func Classify(path string) (Package, error) {
	parts := strings.Split(path, "/")
	switch {
	case parts[0] == "cmd":
		return Package{Path: path, Layer: LayerCmd}, nil
	case path == "internal/archtest":
		return Package{Path: path, Layer: LayerTooling}, nil
	case path == "migrations":
		return Package{Path: path, Layer: LayerMigrations}, nil
	case path == "web":
		return Package{Path: path, Layer: LayerWeb}, nil
	case parts[0] != "internal" || len(parts) < 2:
		return Package{}, LayoutError{Path: path, Reason: "Go packages live only under cmd/, internal/, migrations/ and web/ (ADR 0015, ADR 0022)"}
	case parts[1] == "common":
		return classifyCommon(path, parts)
	case slices.Contains(BoundedContexts, parts[1]):
		return classifyContext(path, parts)
	default:
		return Package{}, LayoutError{
			Path:   path,
			Reason: fmt.Sprintf("internal/%s is neither internal/common nor a bounded context %v", parts[1], BoundedContexts),
		}
	}
}

func classifyCommon(path string, parts []string) (Package, error) {
	if len(parts) < 3 || !slices.Contains(commonPackages, parts[2]) {
		return Package{}, LayoutError{Path: path, Reason: fmt.Sprintf("internal/common holds only %v", commonPackages)}
	}
	return Package{Path: path, Layer: LayerCommon, Common: parts[2]}, nil
}

func classifyContext(path string, parts []string) (Package, error) {
	pkg := Package{Path: path, Context: parts[1]}
	rest := parts[2:]
	invalid := func(reason string) (Package, error) {
		return Package{}, LayoutError{Path: path, Reason: reason}
	}
	if len(rest) == 0 {
		return invalid("a context root holds no Go files; use its sub-packages")
	}
	switch rest[0] {
	case "domain":
		if len(rest) != 2 {
			return invalid("domain code lives in exactly one level domain/<aggregate> (L-3)")
		}
		pkg.Layer = LayerDomain
	case "ports":
		if len(rest) != 2 || (rest[1] != "in" && rest[1] != "out") {
			return invalid("ports has exactly the packages ports/in and ports/out")
		}
		pkg.Layer = LayerPortsIn
		if rest[1] == "out" {
			pkg.Layer = LayerPortsOut
		}
	case "app":
		if len(rest) != 2 || !slices.Contains(appPackages, rest[1]) {
			return invalid(fmt.Sprintf("app has exactly the packages %v", appPackages))
		}
		pkg.Layer = LayerApp
	case "adapters":
		if len(rest) < 3 || (rest[1] != "in" && rest[1] != "out") {
			return invalid("adapters live in adapters/in/<name> or adapters/out/<name>")
		}
		pkg.Layer = LayerAdapterIn
		if rest[1] == "out" {
			pkg.Layer = LayerAdapterOut
		}
		pkg.Adapter = rest[1] + "/" + rest[2]
	case "service":
		if len(rest) != 1 {
			return invalid("service has no sub-packages")
		}
		pkg.Layer = LayerService
	default:
		return invalid(fmt.Sprintf("unknown context sub-package %q; allowed: domain, ports, app, adapters, service", rest[0]))
	}
	return pkg, nil
}

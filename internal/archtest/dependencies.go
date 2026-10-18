package archtest

import (
	"fmt"
	"slices"
	"strings"
)

// DependencyError reports an import that the dependency rules of ddd-rules §3
// (as amended by ADR 0014) forbid.
type DependencyError struct {
	From   string
	Import string
	Rule   string
	Reason string
}

func (e DependencyError) Error() string {
	return fmt.Sprintf("%s imports %s: violates %s: %s", e.From, e.Import, e.Rule, e.Reason)
}

// forbiddenDomainStdlib lists standard-library packages that bring I/O,
// request context, logging or encoding into the domain (DEP-1). A path also
// matches all of its sub-packages.
var forbiddenDomainStdlib = []string{
	"context", "database", "encoding/json", "encoding/xml", "io/fs", "log",
	"net", "os", "path/filepath", "runtime", "syscall", "unsafe",
}

// pureDomainStdlib are value packages below a forbidden path that do no I/O.
var pureDomainStdlib = []string{"net/url", "net/netip"}

// CheckImport applies the dependency rules of ddd-rules §3 to one import of
// a classified package. module is the module path from go.mod.
func CheckImport(from Package, importPath, module string) error {
	if from.Layer == LayerTooling {
		return nil
	}
	deny := func(rule, reason string) error {
		return DependencyError{From: from.Path, Import: importPath, Rule: rule, Reason: reason}
	}
	if from.Layer == LayerMigrations {
		if importPath == "embed" {
			return nil
		}
		return deny("§3", "the migrations package imports only embed (ADR 0015)")
	}
	if from.Layer == LayerWeb {
		if importPath == "embed" {
			return nil
		}
		return deny("§3", "the web package imports only embed (ADR 0022)")
	}

	internalPath, isInternal := strings.CutPrefix(importPath, module+"/")
	if !isInternal {
		if isStandardLibrary(importPath) {
			return checkStandardLibraryImport(from, importPath, deny)
		}
		switch from.Layer {
		case LayerDomain, LayerPortsIn, LayerPortsOut, LayerApp:
			return deny("DEP-1/§3", from.Layer.String()+" imports only the standard library and this module; third-party code belongs in adapters")
		}
		return nil
	}

	to, err := Classify(internalPath)
	if err != nil {
		return deny("L-2", "the imported package has no place in the layout")
	}
	if to.Layer == LayerCmd && from.Layer == LayerCmd {
		return nil // packages of the cmd tree, e.g. cmd/debroxy/config
	}
	if to.Layer == LayerTooling || to.Layer == LayerCmd {
		return deny("§3", "nothing outside cmd imports cmd, and nothing imports the architecture test")
	}
	if to.Layer == LayerMigrations {
		if from.Layer == LayerCmd {
			return nil
		}
		return deny("§3", "only cmd imports the migrations package (ADR 0015)")
	}
	if to.Layer == LayerWeb {
		if from.Layer == LayerCmd {
			return nil
		}
		return deny("§3", "only cmd imports the web package (ADR 0022)")
	}
	if from.Layer == LayerCommon {
		if to.Layer != LayerCommon {
			return deny("DEP-7", "internal/common never imports a bounded context")
		}
		return nil
	}
	if from.Layer == LayerCmd {
		return checkCmdImport(to, deny)
	}
	if to.Layer == LayerCommon {
		return checkCommonImport(from, to, deny)
	}
	if to.Context != from.Context {
		return checkCrossContextImport(from, to, deny)
	}
	return checkSameContextImport(from, to, deny)
}

func checkStandardLibraryImport(from Package, importPath string, deny func(rule, reason string) error) error {
	if from.Layer != LayerDomain || slices.Contains(pureDomainStdlib, importPath) {
		return nil
	}
	for _, forbidden := range forbiddenDomainStdlib {
		if importPath == forbidden || strings.HasPrefix(importPath, forbidden+"/") {
			return deny("DEP-1", "the domain has no I/O, no context.Context, no logging and no encoding")
		}
	}
	return nil
}

// checkCmdImport: cmd → <context>/service, internal/common/* (ADR 0014 rule 4);
// migrations is handled by the caller (ADR 0015).
func checkCmdImport(to Package, deny func(rule, reason string) error) error {
	if to.Layer == LayerService || to.Layer == LayerCommon {
		return nil
	}
	return deny("§3", "cmd imports only <context>/service and internal/common")
}

// checkCommonImport: which layers may use which shared-kernel packages.
func checkCommonImport(from, to Package, deny func(rule, reason string) error) error {
	switch from.Layer {
	case LayerDomain:
		if to.Common != "errors" {
			return deny("DEP-1/§3", "the domain imports only internal/common/errors from the shared kernel")
		}
	case LayerPortsIn:
		if to.Common != "decorator" {
			return deny("§3", "ports/in imports only internal/common/decorator from the shared kernel")
		}
	case LayerPortsOut:
		return deny("§3", "ports/out imports nothing from the shared kernel")
	}
	return nil
}

// checkCrossContextImport: DEP-4, the only cross-context imports are
// <consumer>/adapters/out/<supplier> → <supplier>/ports/in, and
// <consumer>/service → <supplier>/ports/in for deps types (ADR 0014 rule 3).
func checkCrossContextImport(from, to Package, deny func(rule, reason string) error) error {
	if to.Layer == LayerPortsIn {
		if from.Layer == LayerAdapterOut && from.Adapter == "out/"+to.Context {
			return nil
		}
		if from.Layer == LayerService {
			return nil
		}
	}
	return deny("DEP-4", fmt.Sprintf(
		"only %s/adapters/out/%s and %s/service may import %s/ports/in; contexts never import each other otherwise",
		from.Context, to.Context, from.Context, to.Context))
}

// sameContextAllowed is the table of ddd-rules §3 (ADR 0014) for imports inside one context.
var sameContextAllowed = map[Layer][]Layer{
	LayerDomain:     {},
	LayerPortsIn:    {LayerDomain},
	LayerPortsOut:   {LayerDomain, LayerPortsIn},
	LayerApp:        {LayerPortsIn, LayerPortsOut, LayerDomain},
	LayerAdapterIn:  {LayerPortsIn},
	LayerAdapterOut: {LayerPortsOut, LayerDomain, LayerPortsIn},
	LayerService:    {LayerDomain, LayerPortsIn, LayerPortsOut, LayerApp, LayerAdapterIn, LayerAdapterOut},
}

func checkSameContextImport(from, to Package, deny func(rule, reason string) error) error {
	isAdapter := func(l Layer) bool { return l == LayerAdapterIn || l == LayerAdapterOut }
	if isAdapter(from.Layer) && isAdapter(to.Layer) {
		if from.Adapter == to.Adapter {
			return nil
		}
		return deny("DEP-3", "adapters never import other adapters")
	}
	if from.Layer == to.Layer && from.Layer == LayerDomain && from.Path != to.Path {
		return deny("§3", "an aggregate package never imports another aggregate package")
	}
	if from.Layer == to.Layer && from.Layer == LayerApp && from.Path != to.Path {
		return deny("A-1/§3", "app packages never import each other")
	}
	if slices.Contains(sameContextAllowed[from.Layer], to.Layer) {
		return nil
	}
	rule := "§3"
	switch {
	case to.Layer == LayerApp && isAdapter(from.Layer):
		rule = "DEP-2"
	case from.Layer == LayerDomain:
		rule = "DEP-1"
	}
	return deny(rule, fmt.Sprintf("%s may import only %v of its own context", from.Layer, sameContextAllowed[from.Layer]))
}

// isStandardLibrary reports whether an import path belongs to the Go
// standard library: its first element has no dot.
func isStandardLibrary(importPath string) bool {
	first, _, _ := strings.Cut(importPath, "/")
	return !strings.Contains(first, ".")
}

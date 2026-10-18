package archtest

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// SourceError reports a rule violation found inside a Go file.
type SourceError struct {
	Position string
	Rule     string
	Reason   string
}

func (e SourceError) Error() string {
	return fmt.Sprintf("%s: violates %s: %s", e.Position, e.Rule, e.Reason)
}

// skippedDirectories are never scanned for Go packages.
var skippedDirectories = []string{"testdata", "vendor", "node_modules", "bin", "dist", "docs", "api"}

// clockFunctions read the wall clock; the domain receives time as a parameter (D-10).
var clockFunctions = []string{"Now", "Since", "Until"}

// CheckRepository scans every non-test Go file under root and returns all
// violations of the layout, dependency and domain-purity rules.
func CheckRepository(root string) ([]error, error) {
	module, err := modulePath(root)
	if err != nil {
		return nil, err
	}
	packages, err := goPackageDirectories(root)
	if err != nil {
		return nil, err
	}

	var violations []error
	fileSet := token.NewFileSet()
	for _, dir := range packages {
		rel, err := filepath.Rel(root, dir)
		if err != nil {
			return nil, fmt.Errorf("relative path of %s: %w", dir, err)
		}
		pkg, err := Classify(filepath.ToSlash(rel))
		if err != nil {
			violations = append(violations, err)
			continue
		}
		files, err := parsePackage(fileSet, dir)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			violations = append(violations, CheckFile(fileSet, pkg, file, module)...)
		}
	}
	return violations, nil
}

// CheckFile applies the import rules and, for domain packages, the purity
// rules to one parsed file.
func CheckFile(fileSet *token.FileSet, pkg Package, file *ast.File, module string) []error {
	var violations []error
	if slices.Contains(forbiddenPackageNames, file.Name.Name) {
		violations = append(violations, SourceError{
			Position: fileSet.Position(file.Name.Pos()).String(),
			Rule:     "N-5",
			Reason:   fmt.Sprintf("package name %q is generic", file.Name.Name),
		})
	}
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		if err := CheckImport(pkg, importPath, module); err != nil {
			violations = append(violations, err)
		}
	}
	if pkg.Layer == LayerDomain {
		violations = append(violations, checkDomainPurity(fileSet, file)...)
	}
	return violations
}

// checkDomainPurity enforces DEP-1 and D-10 inside one domain file: no
// struct tags and no reads of the wall clock.
func checkDomainPurity(fileSet *token.FileSet, file *ast.File) []error {
	timeName := ""
	for _, spec := range file.Imports {
		if spec.Path.Value == `"time"` {
			timeName = "time"
			if spec.Name != nil {
				timeName = spec.Name.Name
			}
		}
	}

	var violations []error
	ast.Inspect(file, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.Field:
			if n.Tag != nil {
				violations = append(violations, SourceError{
					Position: fileSet.Position(n.Tag.Pos()).String(),
					Rule:     "DEP-1",
					Reason:   "domain types carry no struct tags; storage and transport rows live in adapters",
				})
			}
		case *ast.SelectorExpr:
			ident, ok := n.X.(*ast.Ident)
			if ok && timeName != "" && ident.Name == timeName && slices.Contains(clockFunctions, n.Sel.Name) {
				violations = append(violations, SourceError{
					Position: fileSet.Position(n.Pos()).String(),
					Rule:     "D-10",
					Reason:   fmt.Sprintf("the domain never calls time.%s; pass now as a parameter", n.Sel.Name),
				})
			}
		}
		return true
	})
	return violations
}

func goPackageDirectories(root string) ([]string, error) {
	var dirs []string
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			name := entry.Name()
			if path != root && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || slices.Contains(skippedDirectories, name)) {
				return filepath.SkipDir
			}
			return nil
		}
		if isProductionGoFile(entry.Name()) {
			dir := filepath.Dir(path)
			if !seen[dir] {
				seen[dir] = true
				dirs = append(dirs, dir)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", root, err)
	}
	return dirs, nil
}

func parsePackage(fileSet *token.FileSet, dir string) ([]*ast.File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	var files []*ast.File
	for _, entry := range entries {
		if entry.IsDir() || !isProductionGoFile(entry.Name()) {
			continue
		}
		file, err := parser.ParseFile(fileSet, filepath.Join(dir, entry.Name()), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("parse: %w", err)
		}
		files = append(files, file)
	}
	return files, nil
}

// isProductionGoFile excludes tests: test packages may use testify, fixed
// clocks and fixtures that the rules forbid in production code.
func isProductionGoFile(name string) bool {
	return strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
}

func modulePath(root string) (string, error) {
	content, err := os.ReadFile(filepath.Join(root, "go.mod")) //nolint:gosec // root is the repository under test, not user input
	if err != nil {
		return "", fmt.Errorf("read go.mod: %w", err)
	}
	for line := range strings.Lines(string(content)) {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.Trim(strings.TrimSpace(rest), `"`), nil
		}
	}
	return "", errors.New("go.mod has no module directive")
}

// Package harvest provides functionality for the harvest subsystem.
package harvest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"go/types"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unique"

	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/packages"
)

// HarvestedSymbol represents deep-linked structural metadata for a Go symbol.
type HarvestedSymbol struct {
	PkgPath      string   `json:"pkg_path"`
	Name         string   `json:"name"`
	SymbolType   string   `json:"symbol_type"`
	Summary      string   `json:"summary,omitempty,omitzero"`
	Receiver     string   `json:"receiver,omitempty,omitzero"`
	Signature    string   `json:"signature"`
	Doc          string   `json:"doc"`
	Interfaces   []string `json:"interfaces,omitempty,omitzero"`
	Dependencies []string `json:"dependencies,omitempty,omitzero"`
	Examples     []string `json:"examples,omitempty,omitzero"`
}

// HarvestResult wraps the list of symbols and the CheckDrift checksum.
type HarvestResult struct {
	Symbols     []HarvestedSymbol
	Checksum    string
	PackageDocs map[string]string
	Errors      []string
}

// Engine encapsulates the extraction logic for AST+Types.
type Engine struct {
	ifaceCache  map[string]*types.Interface
	excludeDirs []string
}

// NewEngine creates a new harvester engine.
func NewEngine(excludeDirs ...[]string) *Engine {
	var excludes []string
	if len(excludeDirs) > 0 {
		excludes = excludeDirs[0]
	}
	return &Engine{
		ifaceCache:  make(map[string]*types.Interface),
		excludeDirs: excludes,
	}
}

// setupRemoteHarvestDir creates an ephemeral module workspace for remote package harvesting.
func (e *Engine) setupRemoteHarvestDir(ctx context.Context, pkgPath string) (string, func(), bool) {
	if !strings.Contains(pkgPath, ".") || strings.HasPrefix(pkgPath, ".") || strings.HasPrefix(pkgPath, "/") {
		return "", nil, false
	}

	emptyDir, err := os.MkdirTemp("", "recall-*")
	if err != nil {
		return "", nil, false
	}

	cleanup := func() {
		if rmErr := os.RemoveAll(emptyDir); rmErr != nil {
			slog.Debug("failed to remove ephemeral harvest workspace", "error", rmErr)
		}
	}

	cmdMod := exec.CommandContext(ctx, resolveGoBin(), "mod", "init", "temp") //nolint:gosec // go binary path is resolved internally
	cmdMod.Dir = emptyDir
	if modErr := cmdMod.Run(); modErr != nil {
		slog.Debug("ephemeral go mod init failed", "error", modErr)
	}

	basePkg := strings.TrimSuffix(pkgPath, "...")
	basePkg = strings.TrimSuffix(basePkg, "/")
	if basePkg != "" {
		slog.Info("Downloading remote package to ephemeral workspace", "pkg", basePkg)
		cmdGet := exec.CommandContext(ctx, resolveGoBin(), "get", basePkg+"@latest") //nolint:gosec // go binary path is resolved internally
		cmdGet.Dir = emptyDir
		if getErr := cmdGet.Run(); getErr != nil {
			slog.Debug("ephemeral go get failed", "pkg", basePkg, "error", getErr)
		}
	}

	return emptyDir, cleanup, true
}

func (e *Engine) harvestLoadedPackage(ctx context.Context, pkgs []*packages.Package, res *HarvestResult, checksumBuilder *bytes.Buffer) {
	for _, p := range pkgs {
		if len(p.Errors) > 0 {
			for _, err := range p.Errors {
				res.Errors = append(res.Errors, err.Error())
			}
		}

		if p.Types == nil {
			continue
		}

		t1 := time.Now()
		allExamples := e.extractExamples(pkgs)
		slog.Info("[Perf] extractExamples", "dur_ms", time.Since(t1).Milliseconds())

		var absDir string
		if len(p.GoFiles) > 0 {
			absDir = filepath.Dir(p.GoFiles[0])
		} else {
			absDir = p.PkgPath
		}

		t2 := time.Now()
		pDoc, extractErr := e.extractPackageDoc(ctx, absDir)
		slog.Info("[Perf] extractPackageDoc", "dur_ms", time.Since(t2).Milliseconds())
		if extractErr != nil {
			slog.Debug("Failed to extract package documentation", "pkg", p.PkgPath, "error", extractErr)
		}
		if pDoc != "" {
			checksumBuilder.WriteString(pDoc)
			res.PackageDocs[p.PkgPath] = pDoc
		}

		t3 := time.Now()
		syms := e.resolveSymbols(ctx, p, allExamples)
		slog.Info("[Perf] resolveSymbols", "dur_ms", time.Since(t3).Milliseconds())
		res.Symbols = append(res.Symbols, syms...)
	}
}

// Run executes the full extraction, including the 'go doc' structural hash.
func (e *Engine) Run(ctx context.Context, pkgPath string) (*HarvestResult, error) {
	var origDir string
	if filepath.IsAbs(pkgPath) {
		origDir = pkgPath
	} else if dir, cleanup, ok := e.setupRemoteHarvestDir(ctx, pkgPath); ok {
		defer cleanup()
		origDir = dir
	}

	pkgPaths, err := e.discoverPackages(ctx, pkgPath, origDir)
	if err != nil {
		return nil, err
	}

	res := &HarvestResult{
		PackageDocs: make(map[string]string),
	}
	var checksumBuilder bytes.Buffer

	for i, path := range pkgPaths {
		if e.isNoisePackage(path) {
			continue
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		slog.Debug("Deep-scanning package", "pkg", path, "progress", fmt.Sprintf("%d/%d", i+1, len(pkgPaths)))

		pkgs, loadErr := e.loadPackageFull(ctx, path, origDir)
		if loadErr != nil {
			res.Errors = append(res.Errors, loadErr.Error())
			continue
		}

		e.harvestLoadedPackage(ctx, pkgs, res, &checksumBuilder)

		// Throttle parsing to prevent AST cache explosion and CPU starvation
		time.Sleep(50 * time.Millisecond)
	}

	if checksumBuilder.Len() > 0 {
		hash := sha256.Sum256(checksumBuilder.Bytes())
		res.Checksum = hex.EncodeToString(hash[:])
	} else {
		res.Checksum = "no-doc-checksum"
	}

	return res, nil
}

// isNoisePackage checks if the module path traverses third-party or auto-generated tests
func (e *Engine) isNoisePackage(pkgPath string) bool {
	excludes := e.excludeDirs
	if len(excludes) == 0 {
		excludes = []string{"/vendor/", "/testdata/", "/mocks", "/internal/logs"}
	}
	for _, tag := range excludes {
		if strings.Contains(pkgPath, tag) {
			return true
		}
	}
	return false
}

// extractPackageDoc runs 'go doc -all' and returns the formatted markdown documentation content.
func (e *Engine) extractPackageDoc(ctx context.Context, pkgPath string) (string, error) {
	docPkg := pkgPath
	var dir string
	if filepath.IsAbs(pkgPath) {
		dir = pkgPath
		docPkg = "."
	}
	goBinary := resolveGoBin()
	if goBinary == "" {
		return "", fmt.Errorf("go toolchain not found: set RECALL_GO_BIN or install Go at a standard path")
	}
	cmd := exec.CommandContext(ctx, goBinary, "doc", "-all", docPkg) //nolint:gosec // go binary path is resolved internally
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "PAGER=") // Prevent 'less' from blocking the pipeline
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}

	if out.Len() == 0 {
		return "", nil
	}
	return out.String(), nil
}

// discoverPackages performs a lightweight scan to enumerate package paths.
func (e *Engine) discoverPackages(ctx context.Context, pkgPath, origDir string) ([]string, error) {
	cfg := &packages.Config{
		Mode:    packages.NeedName | packages.NeedFiles,
		Context: ctx,
		Tests:   false,
		Env:     goEnv(),
	}

	loadPath := pkgPath
	if origDir != "" {
		cfg.Dir = origDir
		if filepath.IsAbs(pkgPath) {
			loadPath = "."
		}
	}

	if !strings.HasSuffix(loadPath, "...") {
		if loadPath == "." {
			loadPath = "./..."
		} else {
			if !strings.HasSuffix(loadPath, "/") {
				loadPath += "/"
			}
			loadPath += "..."
		}
	}

	slog.Info("Discovering packages (throttled mode)", "pkg", pkgPath)

	pkgs, err := packages.Load(cfg, loadPath)
	if err != nil {
		return nil, fmt.Errorf("failed to discover packages: %w", err)
	}

	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no packages found matching %s", pkgPath)
	}

	var paths []string
	for _, p := range pkgs {
		paths = append(paths, p.PkgPath)
	}

	return paths, nil
}

// loadPackageFull executes a deep AST/Types parse for exactly one target package.
func (e *Engine) loadPackageFull(ctx context.Context, targetPkg, origDir string) ([]*packages.Package, error) {
	start := time.Now()
	slog.Info("[Perf] Start loadPackageFull", "pkg", targetPkg)
	defer func() {
		slog.Info("[Perf] End loadPackageFull", "pkg", targetPkg, "dur_ms", time.Since(start).Milliseconds())
	}()

	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedSyntax |
			packages.NeedTypesInfo | packages.NeedModule | packages.NeedFiles,
		Context: ctx,
		Tests:   false,
		Env:     goEnv(),
	}
	if origDir != "" {
		cfg.Dir = origDir
	}

	pkgs, err := packages.Load(cfg, targetPkg)
	if err != nil {
		return nil, fmt.Errorf("failed to load package %s via AST: %w", targetPkg, err)
	}

	return pkgs, nil
}

// extractExamples scans package syntax trees for Example* functions.
func (e *Engine) extractExamples(pkgs []*packages.Package) map[string]string {
	allExamples := make(map[string]string)
	fset := token.NewFileSet()
	for _, p := range pkgs {
		insp := inspector.New(p.Syntax)
		insp.Preorder([]ast.Node{(*ast.FuncDecl)(nil)}, func(n ast.Node) {
			fn, ok := n.(*ast.FuncDecl)
			if !ok {
				return
			}
			if fn.Body != nil && strings.HasPrefix(fn.Name.Name, "Example") {
				var buf bytes.Buffer
				if err := printer.Fprint(&buf, fset, fn.Body); err == nil {
					allExamples[fn.Name.Name] = buf.String()
				}
			}
		})
	}
	return allExamples
}

// resolveSymbols iterates the package scope to build HarvestedSymbol entries.
func (e *Engine) resolveSymbols(ctx context.Context, p *packages.Package, allExamples map[string]string) []HarvestedSymbol {
	var symbols []HarvestedSymbol
	scope := p.Types.Scope()
	docMap := buildDocMap(p.Syntax)

	// Keep interning handles alive in scope to prevent GC during resolution
	var handles []unique.Handle[string]

	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		if !obj.Exported() {
			continue
		}

		pkgPathHandle := unique.Make(p.PkgPath)
		nameHandle := unique.Make(name)
		handles = append(handles, pkgPathHandle, nameHandle)
		_ = handles // keep interned handles alive for the resolution pass

		sym := HarvestedSymbol{
			PkgPath: pkgPathHandle.Value(),
			Name:    nameHandle.Value(),
		}

		switch typed := obj.(type) {
		case *types.Func:
			sym.Signature = unique.Make(typed.Type().String()).Value()
			sym.SymbolType = "FUNC"
			if sig, ok := typed.Type().(*types.Signature); ok && sig.Recv() != nil {
				sym.Receiver = unique.Make(sig.Recv().Type().String()).Value()
				sym.SymbolType = "METHOD"
			}
			if sig, ok := typed.Type().(*types.Signature); ok {
				sym.Dependencies = e.extractSignatureDeps(sig)
			}
		case *types.TypeName:
			sym.Signature = unique.Make(typed.Type().String()).Value()
			if named, ok := typed.Type().(*types.Named); ok {
				sym.Interfaces = e.detectInterfaces(ctx, named)
				switch named.Underlying().(type) {
				case *types.Interface:
					sym.SymbolType = "INTERFACE"
				case *types.Struct:
					sym.SymbolType = "STRUCT"
					if st, ok := named.Underlying().(*types.Struct); ok {
						sym.Dependencies = e.extractStructDeps(st)
					}
				default:
					sym.SymbolType = "TYPE"
				}
			} else {
				sym.SymbolType = "TYPE"
			}
		case *types.Const:
			sym.Signature = unique.Make(typed.Type().String()).Value()
			sym.SymbolType = "CONST"
		case *types.Var:
			sym.Signature = unique.Make(typed.Type().String()).Value()
			sym.SymbolType = "VAR"
		default:
			sym.Signature = unique.Make(obj.Type().String()).Value()
			sym.SymbolType = "UNKNOWN"
		}

		// Docstring linking via pre-built lookup map
		sym.Doc = docMap[name]

		if sym.Doc != "" {
			parts := strings.SplitN(sym.Doc, ".", 2)
			if len(parts) > 0 && strings.TrimSpace(parts[0]) != "" {
				sym.Summary = strings.TrimSpace(strings.ReplaceAll(parts[0], "\n", " ")) + "."
			}
		}

		for exName, exBody := range allExamples {
			if strings.HasPrefix(exName, "Example"+name) {
				sym.Examples = append(sym.Examples, exBody)
			}
		}

		symbols = append(symbols, sym)
	}

	return symbols
}

// buildDocMap pre-indexes docstrings from AST in O(D) via inspector.
func buildDocMap(syntax []*ast.File) map[string]string {
	docMap := make(map[string]string)
	insp := inspector.New(syntax)
	insp.Preorder([]ast.Node{(*ast.FuncDecl)(nil), (*ast.GenDecl)(nil)}, func(n ast.Node) {
		switch d := n.(type) {
		case *ast.FuncDecl:
			if d.Doc != nil {
				docMap[unique.Make(d.Name.Name).Value()] = d.Doc.Text()
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok {
					if d.Doc != nil {
						docMap[unique.Make(ts.Name.Name).Value()] = d.Doc.Text()
					} else if ts.Doc != nil {
						docMap[unique.Make(ts.Name.Name).Value()] = ts.Doc.Text()
					}
				}
			}
		}
	})
	return docMap
}

// detectInterfaces checks for io.Closer, io.Reader, encoding/json.Marshaler, etc.
func (e *Engine) detectInterfaces(ctx context.Context, named *types.Named) []string {
	var impls []string

	targets := map[string]string{
		"io.Closer": "io", "io.Reader": "io", "io.Writer": "io",
		"encoding/json.Marshaler": "encoding/json", "encoding/json.Unmarshaler": "encoding/json",
	}

	ptrMatched := types.NewPointer(named)

	for ifaceName, pkgName := range targets {
		if ifaceType := e.findIface(ctx, pkgName, strings.Split(ifaceName, ".")[1]); ifaceType != nil {
			if types.Implements(named, ifaceType) || types.Implements(ptrMatched, ifaceType) {
				impls = append(impls, unique.Make(ifaceName).Value())
			}
		}
	}
	return impls
}

// findIface performs a direct, targeted load of a specific standard library
// package to resolve an interface type. This avoids the recursive import
// traversal that caused OOM conditions with NeedDeps.
func (e *Engine) findIface(ctx context.Context, pkgPath, typeName string) *types.Interface {
	key := pkgPath + "." + typeName
	if cached, ok := e.ifaceCache[key]; ok {
		return cached
	}

	cfg := &packages.Config{
		Mode:    packages.NeedName | packages.NeedTypes,
		Context: ctx,
		Env:     goEnv(),
	}

	pkgs, err := packages.Load(cfg, pkgPath)
	if err != nil || len(pkgs) == 0 {
		return nil
	}

	p := pkgs[0]
	if p.Types == nil {
		return nil
	}

	obj := p.Types.Scope().Lookup(typeName)
	if obj == nil {
		return nil
	}

	tName, ok := obj.(*types.TypeName)
	if !ok {
		return nil
	}

	named, ok := tName.Type().(*types.Named)
	if !ok {
		return nil
	}

	iface, ok := named.Underlying().(*types.Interface)
	if !ok {
		return nil
	}

	e.ifaceCache[key] = iface
	return iface
}

// extractDeps extracts unique dependencies from a type.
func (e *Engine) extractDeps(t types.Type, deps map[string]bool) {
	switch t := t.(type) {
	case *types.Named:
		if t.Obj() != nil && t.Obj().Pkg() != nil {
			dep := fmt.Sprintf("%s.%s", t.Obj().Pkg().Name(), t.Obj().Name())
			deps[unique.Make(dep).Value()] = true
		}
	case *types.Pointer:
		e.extractDeps(t.Elem(), deps)
	case *types.Slice:
		e.extractDeps(t.Elem(), deps)
	case *types.Map:
		e.extractDeps(t.Key(), deps)
		e.extractDeps(t.Elem(), deps)
	case *types.Array:
		e.extractDeps(t.Elem(), deps)
	case *types.Chan:
		e.extractDeps(t.Elem(), deps)
	case *types.Signature:
		if params := t.Params(); params != nil {
			for v := range params.Variables() {
				e.extractDeps(v.Type(), deps)
			}
		}
		if results := t.Results(); results != nil {
			for v := range results.Variables() {
				e.extractDeps(v.Type(), deps)
			}
		}
	}
}

func (e *Engine) extractStructDeps(sType *types.Struct) []string {
	deps := make(map[string]bool)
	for field := range sType.Fields() {
		e.extractDeps(field.Type(), deps)
	}
	var res []string
	for k := range deps {
		res = append(res, k)
	}
	return res
}

func (e *Engine) extractSignatureDeps(sig *types.Signature) []string {
	deps := make(map[string]bool)
	if params := sig.Params(); params != nil {
		for v := range params.Variables() {
			e.extractDeps(v.Type(), deps)
		}
	}
	if results := sig.Results(); results != nil {
		for v := range results.Variables() {
			e.extractDeps(v.Type(), deps)
		}
	}
	var res []string
	for k := range deps {
		res = append(res, k)
	}
	return res
}

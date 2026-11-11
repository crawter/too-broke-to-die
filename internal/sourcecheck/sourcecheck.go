// Copyright (c) TOO BROKE TO DIE contributors

// Package sourcecheck checks selected source-level architecture conventions.
package sourcecheck

import (
	"context"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	monkit "github.com/spacemonkeygo/monkit/v3"
)

var mon = monkit.Package()

const (
	modulePath = "github.com/crawter/too-broke-to-die"
	copyright  = "// Copyright (c) TOO BROKE TO DIE contributors"
)

// Diagnostic identifies a source rule violation.
type Diagnostic struct {
	File    string
	Line    int
	Rule    string
	Message string
}

// String formats a diagnostic for a command-line user.
func (diagnostic Diagnostic) String() string {
	return fmt.Sprintf("%s:%d: %s: %s", diagnostic.File, diagnostic.Line, diagnostic.Rule, diagnostic.Message)
}

type sourceFile struct {
	path       string
	set        *token.FileSet
	file       *ast.File
	constraint constraint.Expr
}

// Check inspects all project Go files, including files excluded by the host's
// build tags. It excludes vendored code and test fixtures in testdata.
// Its checks do not replace architecture, instrumentation, or product review.
func Check(ctx context.Context, root string) (_ []Diagnostic, err error) {
	defer mon.Task()(&ctx)(&err)

	var diagnostics []Diagnostic
	var files []*sourceFile
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && skipDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 || !strings.HasSuffix(entry.Name(), ".go") {
			return nil
		}

		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		set := token.NewFileSet()
		file, parseErr := parser.ParseFile(set, path, nil, parser.ParseComments|parser.AllErrors)
		if parseErr != nil {
			diagnostics = append(diagnostics, Diagnostic{
				File:    relative,
				Line:    1,
				Rule:    "syntax",
				Message: parseErr.Error(),
			})
			return nil
		}
		files = append(files, &sourceFile{
			path:       relative,
			set:        set,
			file:       file,
			constraint: fileConstraint(ctx, relative, file),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk source tree: %w", err)
	}

	for _, source := range files {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		found, checkErr := checkFile(ctx, source, files)
		if checkErr != nil {
			return nil, checkErr
		}
		diagnostics = append(diagnostics, found...)
	}

	sort.Slice(diagnostics, func(i, j int) bool {
		left, right := diagnostics[i], diagnostics[j]
		if left.File != right.File {
			return left.File < right.File
		}
		if left.Line != right.Line {
			return left.Line < right.Line
		}
		if left.Rule != right.Rule {
			return left.Rule < right.Rule
		}
		return left.Message < right.Message
	})
	return diagnostics, nil
}

func skipDirectory(name string) bool {
	return name == ".git" || name == "vendor" || name == "testdata" ||
		strings.HasPrefix(name, ".intake-")
}

func checkFile(ctx context.Context, source *sourceFile, files []*sourceFile) (_ []Diagnostic, err error) {
	defer mon.Task()(&ctx)(&err)

	file := source.file
	var diagnostics []Diagnostic
	add := func(position token.Pos, rule, message string) {
		diagnostics = append(diagnostics, Diagnostic{
			File:    source.path,
			Line:    source.set.Position(position).Line,
			Rule:    rule,
			Message: message,
		})
	}

	checkConstraints(ctx, file, add)
	generated := ast.IsGenerated(file)
	if !generated {
		found := false
		for _, group := range file.Comments {
			if group.Pos() >= file.Package {
				break
			}
			for _, comment := range group.List {
				if comment.Text == copyright {
					found = true
				}
			}
		}
		if !found {
			add(file.Package, "header", "missing required copyright comment before package declaration")
		}
	}

	switch file.Name.Name {
	case "utils", "helpers", "common", "misc":
		add(file.Name.Pos(), "package", "catch-all package names are not permitted")
	}

	checkImports(ctx, file, source.path, add)
	if !generated && !strings.HasSuffix(source.path, "_test.go") {
		if err = checkInstrumentation(ctx, source, files, add); err != nil {
			return nil, err
		}
	}
	return diagnostics, nil
}

func checkConstraints(ctx context.Context, file *ast.File, add func(token.Pos, string, string)) {
	defer mon.Task()(&ctx)(nil)

	modern := 0
	for _, group := range file.Comments {
		for _, comment := range group.List {
			if !constraint.IsGoBuild(comment.Text) && !constraint.IsPlusBuild(comment.Text) {
				continue
			}
			if comment.Pos() >= file.Package {
				add(comment.Pos(), "build-constraint", "build constraint must precede the package declaration")
			}
			if constraint.IsGoBuild(comment.Text) {
				modern++
				if modern > 1 {
					add(comment.Pos(), "build-constraint", "multiple //go:build lines")
				}
			}
			if _, err := constraint.Parse(comment.Text); err != nil {
				add(comment.Pos(), "build-constraint", fmt.Sprintf("invalid build constraint: %v", err))
			}
		}
	}
}

func checkImports(ctx context.Context, file *ast.File, relative string, add func(token.Pos, string, string)) {
	defer mon.Task()(&ctx)(nil)

	directory := filepath.ToSlash(filepath.Dir(relative))
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			add(spec.Pos(), "import", "invalid import path")
			continue
		}
		if reason := forbiddenImport(ctx, directory, importPath); reason != "" {
			add(spec.Pos(), "dependency-direction", fmt.Sprintf("import %q: %s", importPath, reason))
		}
	}
}

func within(path, parent string) bool {
	return path == parent || strings.HasPrefix(path, parent+"/")
}

func forbiddenImport(ctx context.Context, directory, importPath string) string {
	defer mon.Task()(&ctx)(nil)

	project := modulePath + "/"
	local := strings.TrimPrefix(importPath, project)
	isLocal := strings.HasPrefix(importPath, project)

	if isLocal && within(local, "cmd") && !within(directory, "cmd") {
		return "packages must not import executable entrypoints"
	}
	if isLocal && within(directory, "pkg") &&
		(within(local, "internal/tbtd") || within(local, "cmd")) {
		return "general-purpose libraries must not import application or entrypoint packages"
	}

	domain := within(directory, "internal/tbtd/content") ||
		within(directory, "internal/tbtd/campaign") ||
		within(directory, "internal/tbtd/simulation") ||
		within(directory, "internal/tbtd/session")
	lower := strings.ToLower(importPath)
	native := importPath == "C" || importPath == "syscall" ||
		strings.Contains(lower, "raylib") ||
		within(lower, "golang.org/x/sys/windows") ||
		within(lower, "internal/syscall/windows")
	if domain && native {
		return "domain packages must not depend on raylib or platform APIs"
	}
	if domain && isLocal &&
		(within(local, "internal/tbtd/windows") ||
			within(local, "internal/tbtd/persistence") ||
			within(local, "internal/tbtd/desktop") ||
			within(local, "cmd")) {
		return "domain packages must not depend on platform, persistence, desktop, or entrypoint implementations"
	}
	if isLocal && !within(directory, "internal/tbtd/desktop") &&
		within(local, "internal/tbtd/desktop") && !within(directory, "cmd") {
		return "shared packages must not import the desktop program subtree"
	}
	if isLocal && within(directory, "internal/tbtd/desktop") &&
		directory != "internal/tbtd/desktop" &&
		(within(local, "internal/tbtd/campaign") ||
			within(local, "internal/tbtd/simulation") ||
			within(local, "internal/tbtd/persistence") ||
			within(local, "internal/tbtd/windows")) {
		return "desktop adapters use session contracts, not mutable domain or infrastructure implementations"
	}
	if isLocal && within(directory, "internal/tbtd/persistence") &&
		within(local, "internal/tbtd/simulation") {
		return "persistence must not depend on the live simulation"
	}
	if isLocal && within(directory, "cmd/tbtd") &&
		(within(local, "internal/tbtd/content") ||
			within(local, "internal/tbtd/campaign") ||
			within(local, "internal/tbtd/simulation") ||
			within(local, "internal/tbtd/session") ||
			within(local, "internal/tbtd/persistence") ||
			strings.HasPrefix(local, "internal/tbtd/desktop/")) {
		return "the game entrypoint depends on the composition root, not individual implementations"
	}
	if isLocal && within(directory, "internal/tbtd/content") &&
		(within(local, "internal/tbtd/campaign") ||
			within(local, "internal/tbtd/simulation") ||
			within(local, "internal/tbtd/session")) {
		return "content definitions must not depend on their consumers"
	}
	if isLocal &&
		(within(directory, "internal/tbtd/campaign") ||
			within(directory, "internal/tbtd/simulation")) &&
		within(local, "internal/tbtd/session") {
		return "campaign and simulation must not depend on their session coordinator"
	}
	if isLocal && within(directory, "internal/tbtd/campaign") &&
		within(local, "internal/tbtd/simulation") {
		return "campaign must not depend on the live simulation"
	}
	if isLocal &&
		(within(directory, "internal/tbtd/windows") ||
			within(directory, "internal/tbtd/diagnostics")) &&
		(within(local, "internal/tbtd/content") ||
			within(local, "internal/tbtd/campaign") ||
			within(local, "internal/tbtd/simulation") ||
			within(local, "internal/tbtd/session") ||
			within(local, "internal/tbtd/persistence")) {
		return "infrastructure must not depend on application domain or persistence implementations"
	}
	return ""
}

func checkInstrumentation(ctx context.Context, source *sourceFile, files []*sourceFile, add func(token.Pos, string, string)) (err error) {
	defer mon.Task()(&ctx)(&err)

	file := source.file
	contextNames := importNames(ctx, file, "context", "context")
	var handles []constraint.Expr
	for _, candidate := range files {
		if filepath.Dir(candidate.path) != filepath.Dir(source.path) ||
			candidate.file.Name.Name != file.Name.Name ||
			strings.HasSuffix(candidate.path, "_test.go") {
			continue
		}
		if declaresMon(ctx, candidate.file) {
			handles = append(handles, candidate.constraint)
		}
	}

	hot := hotLoopHelpers(ctx, source, files)
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil || !needsTask(ctx, function.Body) {
			continue
		}
		contextName := contextParameter(ctx, function.Type, contextNames)
		if contextName == "" {
			fixed, fixedErr := fixedSignature(ctx, source, files, function)
			if fixedErr != nil {
				return fixedErr
			}
			if fixed || hot[function.Name.Name] {
				continue
			}
			add(function.Pos(), "context", "nontrivial operation must accept context.Context as its first parameter")
			continue
		}
		covered, coverageErr := handlesCover(ctx, source.constraint, handles)
		if coverageErr != nil {
			return coverageErr
		}
		if !covered {
			add(function.Pos(), "instrumentation", "monkit.Package handle is not available for every build of this file")
		}
		errorResult := returnsError(ctx, function.Type)
		if errorResult && !namedError(ctx, function.Type) {
			add(function.Pos(), "instrumentation", "error result must be named err for deferred task instrumentation")
		}
		if len(function.Body.List) == 0 || !taskDefer(ctx, function.Body.List[0], contextName, errorResult) {
			add(function.Pos(), "instrumentation", "start with defer mon.Task()(&ctx)(&err), or (nil) without an error result")
		}
	}
	return nil
}

func importNames(ctx context.Context, file *ast.File, path, defaultName string) map[string]bool {
	defer mon.Task()(&ctx)(nil)

	names := map[string]bool{}
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil || importPath != path {
			continue
		}
		name := defaultName
		if spec.Name != nil {
			name = spec.Name.Name
		}
		names[name] = true
	}
	return names
}

func declaresMon(ctx context.Context, file *ast.File) bool {
	defer mon.Task()(&ctx)(nil)

	names := importNames(ctx, file, "github.com/spacemonkeygo/monkit/v3", "monkit")
	for _, declaration := range file.Decls {
		group, ok := declaration.(*ast.GenDecl)
		if !ok || group.Tok != token.VAR {
			continue
		}
		for _, spec := range group.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || len(value.Names) != 1 || value.Names[0].Name != "mon" || len(value.Values) != 1 {
				continue
			}
			call, ok := value.Values[0].(*ast.CallExpr)
			if !ok || len(call.Args) != 0 {
				continue
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Package" {
				continue
			}
			name, ok := selector.X.(*ast.Ident)
			if ok && names[name.Name] {
				return true
			}
		}
	}
	return false
}

var operatingSystems = []string{
	"aix", "android", "darwin", "dragonfly", "freebsd", "illumos", "ios",
	"js", "linux", "netbsd", "openbsd", "plan9", "solaris", "wasip1", "windows",
}

var architectures = []string{
	"386", "amd64", "arm", "arm64", "loong64", "mips", "mipsle", "mips64",
	"mips64le", "ppc64", "ppc64le", "riscv64", "s390x", "wasm",
}

func containsName(ctx context.Context, names []string, value string) bool {
	defer mon.Task()(&ctx)(nil)

	for _, name := range names {
		if name == value {
			return true
		}
	}
	return false
}

func andConstraint(left, right constraint.Expr) constraint.Expr {
	if left == nil {
		return right
	}
	if right == nil {
		return left
	}
	return &constraint.AndExpr{X: left, Y: right}
}

func fileConstraint(ctx context.Context, path string, file *ast.File) constraint.Expr {
	defer mon.Task()(&ctx)(nil)

	var modern, legacy constraint.Expr
	for _, group := range file.Comments {
		if group.Pos() >= file.Package {
			break
		}
		for _, comment := range group.List {
			if !constraint.IsGoBuild(comment.Text) && !constraint.IsPlusBuild(comment.Text) {
				continue
			}
			expression, err := constraint.Parse(comment.Text)
			if err != nil {
				continue
			}
			if constraint.IsGoBuild(comment.Text) {
				modern = expression
			} else {
				legacy = andConstraint(legacy, expression)
			}
		}
	}
	expression := modern
	if expression == nil {
		expression = legacy
	}

	base := strings.TrimSuffix(filepath.Base(path), ".go")
	base = strings.TrimSuffix(base, "_test")
	parts := strings.Split(base, "_")
	if len(parts) > 1 {
		last := parts[len(parts)-1]
		if containsName(ctx, operatingSystems, last) || containsName(ctx, architectures, last) {
			expression = andConstraint(expression, &constraint.TagExpr{Tag: last})
			if len(parts) > 2 && containsName(ctx, operatingSystems, parts[len(parts)-2]) &&
				containsName(ctx, architectures, last) {
				expression = andConstraint(expression, &constraint.TagExpr{Tag: parts[len(parts)-2]})
			}
		}
	}
	return expression
}

func collectTags(ctx context.Context, expression constraint.Expr, tags map[string]bool) {
	defer mon.Task()(&ctx)(nil)

	switch value := expression.(type) {
	case *constraint.TagExpr:
		tags[value.Tag] = true
	case *constraint.NotExpr:
		collectTags(ctx, value.X, tags)
	case *constraint.AndExpr:
		collectTags(ctx, value.X, tags)
		collectTags(ctx, value.Y, tags)
	case *constraint.OrExpr:
		collectTags(ctx, value.X, tags)
		collectTags(ctx, value.Y, tags)
	}
}

func matchesConstraint(ctx context.Context, expression constraint.Expr, match func(string) bool) bool {
	defer mon.Task()(&ctx)(nil)

	return expression == nil || expression.Eval(match)
}

// handlesCover checks that the union of handle declarations covers every
// configuration in which the source file can be selected. Unknown build tags
// are treated as independent booleans; excessive complexity fails closed.
func handlesCover(ctx context.Context, target constraint.Expr, handles []constraint.Expr) (_ bool, err error) {
	defer mon.Task()(&ctx)(&err)

	if len(handles) == 0 {
		return false, nil
	}
	for _, handle := range handles {
		if handle == nil {
			return true, nil
		}
	}

	tags := map[string]bool{}
	collectTags(ctx, target, tags)
	for _, handle := range handles {
		collectTags(ctx, handle, tags)
	}
	var custom []string
	for tag := range tags {
		if tag != "unix" && !containsName(ctx, operatingSystems, tag) && !containsName(ctx, architectures, tag) {
			custom = append(custom, tag)
		}
	}
	sort.Strings(custom)
	if len(custom) > 10 {
		return false, nil
	}

	for _, operatingSystem := range operatingSystems {
		if err = ctx.Err(); err != nil {
			return false, err
		}
		for _, architecture := range architectures {
			for bits := 0; bits < 1<<len(custom); bits++ {
				match := func(tag string) bool {
					if tag == operatingSystem || tag == architecture {
						return true
					}
					if tag == "linux" && operatingSystem == "android" ||
						tag == "darwin" && operatingSystem == "ios" ||
						tag == "solaris" && operatingSystem == "illumos" {
						return true
					}
					if tag == "unix" {
						switch operatingSystem {
						case "aix", "android", "darwin", "dragonfly", "freebsd", "illumos", "ios", "linux", "netbsd", "openbsd", "solaris":
							return true
						}
					}
					for index, name := range custom {
						if name == tag {
							return bits&(1<<index) != 0
						}
					}
					return false
				}
				if !matchesConstraint(ctx, target, match) {
					continue
				}
				covered := false
				for _, handle := range handles {
					if matchesConstraint(ctx, handle, match) {
						covered = true
						break
					}
				}
				if !covered {
					return false, nil
				}
			}
		}
	}
	return true, nil
}

func contextParameter(ctx context.Context, function *ast.FuncType, names map[string]bool) string {
	defer mon.Task()(&ctx)(nil)

	if function.Params == nil || len(function.Params.List) == 0 {
		return ""
	}
	first := function.Params.List[0]
	selector, ok := first.Type.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Context" || len(first.Names) != 1 {
		return ""
	}
	pkg, ok := selector.X.(*ast.Ident)
	if !ok || !names[pkg.Name] {
		return ""
	}
	return first.Names[0].Name
}

func returnsError(ctx context.Context, function *ast.FuncType) bool {
	defer mon.Task()(&ctx)(nil)

	if function.Results == nil {
		return false
	}
	for _, field := range function.Results.List {
		name, ok := field.Type.(*ast.Ident)
		if ok && name.Name == "error" {
			return true
		}
	}
	return false
}

func namedError(ctx context.Context, function *ast.FuncType) bool {
	defer mon.Task()(&ctx)(nil)

	if function.Results == nil {
		return false
	}
	for _, field := range function.Results.List {
		name, ok := field.Type.(*ast.Ident)
		if ok && name.Name == "error" && len(field.Names) == 1 && field.Names[0].Name == "err" {
			return true
		}
	}
	return false
}

// needsTask detects loops and calls other than common trivial operations.
// Validation and fixed-signature exceptions still require human review.
func needsTask(ctx context.Context, body *ast.BlockStmt) bool {
	defer mon.Task()(&ctx)(nil)

	needed := false
	ast.Inspect(body, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ForStmt, *ast.RangeStmt:
			needed = true
			return false
		case *ast.CallExpr:
			if !trivialCall(value) {
				needed = true
			}
		}
		return !needed
	})
	return needed
}

func trivialCall(call *ast.CallExpr) bool {
	switch function := call.Fun.(type) {
	case *ast.Ident:
		switch function.Name {
		case "len", "cap", "append", "copy", "delete", "make", "new", "close",
			"complex", "real", "imag", "min", "max", "clear", "panic", "recover",
			"string", "bool", "byte", "rune", "int", "int8", "int16", "int32",
			"int64", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
			"float32", "float64":
			return true
		}
	case *ast.SelectorExpr:
		pkg, ok := function.X.(*ast.Ident)
		if !ok {
			return false
		}
		if pkg.Name == "strings" {
			return true
		}
		if pkg.Name == "fmt" && (function.Sel.Name == "Sprintf" || function.Sel.Name == "Errorf") {
			return true
		}
		if pkg.Name == "errors" && function.Sel.Name == "New" {
			return true
		}
	}
	return false
}

func fixedSignature(ctx context.Context, source *sourceFile, files []*sourceFile, function *ast.FuncDecl) (_ bool, err error) {
	defer mon.Task()(&ctx)(&err)

	if source.file.Name.Name == "main" && function.Recv == nil &&
		(function.Name.Name == "main" || function.Name.Name == "init") &&
		len(function.Type.Params.List) == 0 && function.Type.Results == nil {
		return true, nil
	}
	if function.Recv == nil {
		return callbackContract(ctx, source, files, function.Name.Name)
	}

	parameters := fieldTypes(ctx, function.Type.Params)
	results := fieldTypes(ctx, function.Type.Results)
	switch function.Name.Name {
	case "String", "Error":
		if equalTypes(ctx, parameters, nil) && equalTypes(ctx, results, []string{"string"}) {
			return true, nil
		}
	case "Close":
		if equalTypes(ctx, parameters, nil) && equalTypes(ctx, results, []string{"error"}) {
			return true, nil
		}
	case "Read", "Write":
		if equalTypes(ctx, parameters, []string{"[]byte"}) && equalTypes(ctx, results, []string{"int", "error"}) {
			return true, nil
		}
	case "ReadAt", "WriteAt":
		if equalTypes(ctx, parameters, []string{"[]byte", "int64"}) && equalTypes(ctx, results, []string{"int", "error"}) {
			return true, nil
		}
	case "Seek":
		if equalTypes(ctx, parameters, []string{"int64", "int"}) && equalTypes(ctx, results, []string{"int64", "error"}) {
			return true, nil
		}
	}
	return implementsLocalInterface(ctx, source, files, function)
}

func fieldTypes(ctx context.Context, fields *ast.FieldList) []string {
	defer mon.Task()(&ctx)(nil)

	var result []string
	if fields == nil {
		return result
	}
	for _, field := range fields.List {
		count := len(field.Names)
		if count == 0 {
			count = 1
		}
		for index := 0; index < count; index++ {
			result = append(result, types.ExprString(field.Type))
		}
	}
	return result
}

func equalTypes(ctx context.Context, left, right []string) bool {
	defer mon.Task()(&ctx)(nil)

	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func sameSignature(ctx context.Context, left, right *ast.FuncType) bool {
	defer mon.Task()(&ctx)(nil)

	return equalTypes(ctx, fieldTypes(ctx, left.Params), fieldTypes(ctx, right.Params)) &&
		equalTypes(ctx, fieldTypes(ctx, left.Results), fieldTypes(ctx, right.Results))
}

func samePackage(ctx context.Context, source, candidate *sourceFile) bool {
	defer mon.Task()(&ctx)(nil)

	return filepath.Dir(source.path) == filepath.Dir(candidate.path) &&
		source.file.Name.Name == candidate.file.Name.Name &&
		!strings.HasSuffix(candidate.path, "_test.go")
}

func receiverType(function *ast.FuncDecl) (string, bool) {
	if function.Recv == nil || len(function.Recv.List) != 1 {
		return "", false
	}
	expression := function.Recv.List[0].Type
	pointer := false
	if star, ok := expression.(*ast.StarExpr); ok {
		expression = star.X
		pointer = true
	}
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name, pointer
	case *ast.IndexExpr:
		if name, ok := value.X.(*ast.Ident); ok {
			return name.Name, pointer
		}
	case *ast.IndexListExpr:
		if name, ok := value.X.(*ast.Ident); ok {
			return name.Name, pointer
		}
	}
	return "", false
}

func implementsLocalInterface(ctx context.Context, source *sourceFile, files []*sourceFile, function *ast.FuncDecl) (_ bool, err error) {
	defer mon.Task()(&ctx)(&err)

	receiver, pointer := receiverType(function)
	if receiver == "" {
		return false, nil
	}
	for _, candidate := range files {
		if !samePackage(ctx, source, candidate) {
			continue
		}
		for _, declaration := range candidate.file.Decls {
			group, ok := declaration.(*ast.GenDecl)
			if !ok || group.Tok != token.TYPE {
				continue
			}
			for _, spec := range group.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				iface, ok := typeSpec.Type.(*ast.InterfaceType)
				if !ok || iface.Methods == nil || len(iface.Methods.List) == 0 {
					continue
				}
				matched := false
				complete := true
				for _, method := range iface.Methods.List {
					signature, ok := method.Type.(*ast.FuncType)
					if !ok || len(method.Names) != 1 {
						complete = false
						break
					}
					if method.Names[0].Name == function.Name.Name && sameSignature(ctx, signature, function.Type) {
						matched = true
					}
					var available []constraint.Expr
					for _, methodFile := range files {
						if !samePackage(ctx, source, methodFile) {
							continue
						}
						for _, methodDecl := range methodFile.file.Decls {
							implementation, ok := methodDecl.(*ast.FuncDecl)
							if !ok || implementation.Name.Name != method.Names[0].Name {
								continue
							}
							name, methodPointer := receiverType(implementation)
							if name == receiver && (pointer || !methodPointer) && sameSignature(ctx, signature, implementation.Type) {
								available = append(available, methodFile.constraint)
							}
						}
					}
					covered, coverErr := handlesCover(ctx, source.constraint, available)
					if coverErr != nil {
						return false, coverErr
					}
					if !covered {
						complete = false
						break
					}
				}
				if matched && complete {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

func callbackContract(ctx context.Context, source *sourceFile, files []*sourceFile, name string) (_ bool, err error) {
	defer mon.Task()(&ctx)(&err)

	var uses []constraint.Expr
	for _, candidate := range files {
		if !samePackage(ctx, source, candidate) {
			continue
		}
		found := false
		ast.Inspect(candidate.file, func(node ast.Node) bool {
			switch value := node.(type) {
			case *ast.CallExpr:
				for _, argument := range value.Args {
					if identifier, ok := argument.(*ast.Ident); ok && identifier.Name == name {
						found = true
					}
				}
			case *ast.ValueSpec:
				if _, ok := value.Type.(*ast.FuncType); ok {
					for _, expression := range value.Values {
						if identifier, ok := expression.(*ast.Ident); ok && identifier.Name == name {
							found = true
						}
					}
				}
			}
			return !found
		})
		if found {
			uses = append(uses, candidate.constraint)
		}
	}
	return handlesCover(ctx, source.constraint, uses)
}

// hotLoopHelpers recognizes unexported free functions called only from loops
// in the same package, provided their bodies contain no potentially I/O calls.
func hotLoopHelpers(ctx context.Context, source *sourceFile, files []*sourceFile) map[string]bool {
	defer mon.Task()(&ctx)(nil)

	type calls struct {
		inside  bool
		outside bool
	}
	observed := map[string]calls{}
	for _, candidate := range files {
		if filepath.Dir(candidate.path) != filepath.Dir(source.path) ||
			candidate.file.Name.Name != source.file.Name.Name ||
			strings.HasSuffix(candidate.path, "_test.go") {
			continue
		}
		var stack []bool
		depth := 0
		ast.Inspect(candidate.file, func(node ast.Node) bool {
			if node == nil {
				if stack[len(stack)-1] {
					depth--
				}
				stack = stack[:len(stack)-1]
				return true
			}
			loop := false
			switch node.(type) {
			case *ast.ForStmt, *ast.RangeStmt:
				loop = true
				depth++
			}
			stack = append(stack, loop)
			if call, ok := node.(*ast.CallExpr); ok {
				if name, ok := call.Fun.(*ast.Ident); ok {
					entry := observed[name.Name]
					if depth > 0 {
						entry.inside = true
					} else {
						entry.outside = true
					}
					observed[name.Name] = entry
				}
			}
			return true
		})
	}
	result := map[string]bool{}
	for _, declaration := range source.file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv != nil || function.Name.IsExported() || function.Body == nil {
			continue
		}
		entry := observed[function.Name.Name]
		result[function.Name.Name] = entry.inside && !entry.outside && purePerItem(ctx, function.Body)
	}
	return result
}

func purePerItem(ctx context.Context, body *ast.BlockStmt) bool {
	defer mon.Task()(&ctx)(nil)

	pure := true
	ast.Inspect(body, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.FuncLit, *ast.SendStmt, *ast.GoStmt, *ast.DeferStmt:
			pure = false
		case *ast.UnaryExpr:
			if value.Op == token.ARROW {
				pure = false
			}
		case *ast.CallExpr:
			name, ok := value.Fun.(*ast.Ident)
			if !ok || name.Obj != nil {
				pure = false
				break
			}
			switch name.Name {
			case "len", "cap", "append", "copy", "delete", "make", "new",
				"complex", "real", "imag", "min", "max", "clear",
				"string", "bool", "byte", "rune", "int", "int8", "int16", "int32",
				"int64", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
				"float32", "float64":
			default:
				pure = false
			}
		}
		return pure
	})
	return pure
}

func taskDefer(ctx context.Context, statement ast.Stmt, contextName string, errorResult bool) bool {
	defer mon.Task()(&ctx)(nil)

	deferred, ok := statement.(*ast.DeferStmt)
	if !ok || len(deferred.Call.Args) != 1 {
		return false
	}
	if errorResult {
		if !addressOf(deferred.Call.Args[0], "err") {
			return false
		}
	} else if name, ok := deferred.Call.Args[0].(*ast.Ident); !ok || name.Name != "nil" {
		return false
	}
	contextCall, ok := deferred.Call.Fun.(*ast.CallExpr)
	if !ok || len(contextCall.Args) != 1 || !addressOf(contextCall.Args[0], contextName) {
		return false
	}
	taskCall, ok := contextCall.Fun.(*ast.CallExpr)
	if !ok || len(taskCall.Args) != 0 {
		return false
	}
	selector, ok := taskCall.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Task" {
		return false
	}
	name, ok := selector.X.(*ast.Ident)
	return ok && name.Name == "mon"
}

func addressOf(expression ast.Expr, name string) bool {
	address, ok := expression.(*ast.UnaryExpr)
	if !ok || address.Op != token.AND {
		return false
	}
	identifier, ok := address.X.(*ast.Ident)
	return ok && identifier.Name == name
}

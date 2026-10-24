// Command activity_boundaries inventories mutation endpoints and their direct
// activity transaction calls. It is read-only and never labels an unreviewed
// branch safe: routing dispatch and inferred middleware require path review.
package main

import (
	"encoding/json"
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Boundary struct {
	Function            string   `json:"function"`
	File                string   `json:"file"`
	Line                int      `json:"line"`
	Commits             []int    `json:"commit_lines"`
	Annotations         []int    `json:"annotation_lines"`
	AtomicEnqueues      []int    `json:"atomic_enqueue_lines"`
	Skips               []int    `json:"skip_lines"`
	DirectMutationCalls []int    `json:"mutation_call_lines"`
	Callees             []string `json:"callees"`
}
type Route struct {
	Pattern  string    `json:"pattern"`
	Handler  string    `json:"handler"`
	Review   string    `json:"review"`
	Boundary *Boundary `json:"direct_boundary,omitempty"`
}

func main() {
	root := flag.String("root", ".", "backend repository root")
	flag.Parse()
	fset := token.NewFileSet()
	boundaries := map[string]*Boundary{}
	var server *ast.File
	entries, err := os.ReadDir(filepath.Join(*root, "internal/httpapi"))
	if err != nil {
		panic(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		relative := filepath.ToSlash(filepath.Join("internal/httpapi", entry.Name()))
		file, err := parser.ParseFile(fset, filepath.Join(*root, relative), nil, 0)
		if err != nil {
			panic(err)
		}
		if entry.Name() == "server.go" {
			server = file
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			b := &Boundary{Function: fn.Name.Name, File: relative, Line: fset.Position(fn.Pos()).Line, Commits: []int{}, Annotations: []int{}, AtomicEnqueues: []int{}, Skips: []int{}, DirectMutationCalls: []int{}, Callees: []string{}}
			seen := map[string]bool{}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				name := ""
				switch fun := call.Fun.(type) {
				case *ast.Ident:
					name = fun.Name
				case *ast.SelectorExpr:
					name = fun.Sel.Name
				}
				line := fset.Position(call.Pos()).Line
				if !seen[name] && name != "" {
					b.Callees = append(b.Callees, name)
					seen[name] = true
				}
				switch name {
				case "Commit":
					b.Commits = append(b.Commits, line)
				case "annotateActivity", "annotateActivityID", "annotateActivityDelta", "annotateActivityIDDelta":
					b.Annotations = append(b.Annotations, line)
				case "EnqueueDurableTx", "enqueueRequestActivityTx":
					b.AtomicEnqueues = append(b.AtomicEnqueues, line)
				case "skipRequestActivity":
					b.Skips = append(b.Skips, line)
				case "Exec", "QueryRow":
					b.DirectMutationCalls = append(b.DirectMutationCalls, line)
				}
				return true
			})
			sort.Strings(b.Callees)
			// Several workers intentionally share names with HTTP handlers. Keep
			// their receiver-qualified symbols instead of replacing the Server
			// entry point (for example BlueprintWorker.convertBlueprint).
			key := fn.Name.Name
			if fn.Recv != nil && len(fn.Recv.List) > 0 {
				receiver := fn.Recv.List[0].Type
				if pointer, ok := receiver.(*ast.StarExpr); ok {
					receiver = pointer.X
				}
				if named, ok := receiver.(*ast.Ident); ok && named.Name != "Server" {
					key = named.Name + "." + key
				}
			}
			boundaries[key] = b
		}
	}
	routes := []Route{}
	ast.Inspect(server, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) != 2 {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "HandleFunc" {
			return true
		}
		literal, ok := call.Args[0].(*ast.BasicLit)
		if !ok {
			return true
		}
		pattern, err := strconv.Unquote(literal.Value)
		if err != nil {
			return true
		}
		method := strings.Fields(pattern)[0]
		if method == "GET" || method == "HEAD" {
			if !strings.Contains(pattern, "download") {
				return true
			}
		}
		handler := ""
		ast.Inspect(call.Args[1], func(node ast.Node) bool {
			if selected, ok := node.(*ast.SelectorExpr); ok {
				if _, exists := boundaries[selected.Sel.Name]; exists {
					handler = selected.Sel.Name
				}
			}
			return true
		})
		routes = append(routes, Route{Pattern: pattern, Handler: handler, Review: "STATIC_CANDIDATE_REQUIRES_BRANCH_REVIEW_NOT_A_PASS", Boundary: boundaries[handler]})
		return true
	})
	sort.Slice(routes, func(i, j int) bool { return routes[i].Pattern < routes[j].Pattern })
	report := struct {
		Version    int                  `json:"version"`
		Rule       string               `json:"rule"`
		RouteCount int                  `json:"route_count"`
		Routes     []Route              `json:"routes"`
		Functions  map[string]*Boundary `json:"functions"`
	}{2, "Receiver-disambiguated Server route inventory. Direct call inventory only. Inferred activity middleware may apply even without annotateActivity. Atomic calls/skips in one branch do not establish all branches or transitive helpers are safe. This tool performs no data repair or reward reconstruction.", len(routes), routes, boundaries}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(report); err != nil {
		panic(err)
	}
}

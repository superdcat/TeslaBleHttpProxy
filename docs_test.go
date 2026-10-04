package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/wimaha/TeslaBleHttpProxy/internal/tesla/commands"
)

// proxyRouteNames are the proxy-specific routes of /api/proxy/1/. The list is kept by hand;
// internal/api/routes TestCapabilitiesRouteListsProxyRoutes reads the routes actually registered,
// so a new route has to be added there and here.
var proxyRouteNames = []string{"capabilities", "connection_status", "version"}

// documentsName reports whether the markdown src lists name as `name` or as a "- name" bullet.
// A name that is only a substring of another one does not count.
func documentsName(src, name string) bool {
	q := regexp.QuoteMeta(name)
	re := regexp.MustCompile("(?m)`" + q + "`|^\\s*[-*]\\s+" + q + "(?:\\s|$)")
	return re.MatchString(src)
}

func TestREADMEDocumentsCommandsEndpointsAndRoutes(t *testing.T) {
	readme := readRepoFile(t, "README.md")
	groups := []struct {
		kind  string
		names []string
	}{
		{"command", commands.FleetCommandNames()},
		{"vehicle_data endpoint", commands.VehicleDataEndpointNames()},
		{"proxy route", proxyRouteNames},
	}
	for _, g := range groups {
		if len(g.names) == 0 {
			t.Errorf("no %s name found: the test source is empty", g.kind)
		}
		for _, name := range g.names {
			if !documentsName(readme, name) {
				t.Errorf("README.md does not document the %s %q (as `%s` or a \"- %s\" bullet)", g.kind, name, name, name)
			}
		}
	}
}

func TestDocumentsNameRejectsSubstrings(t *testing.T) {
	tests := []struct {
		src  string
		want bool
	}{
		{"use `charge_start` now", true},
		{"- charge_start\n", true},
		{"  * charge_start - starts", true},
		{"`charge_start_now`", false},
		{"- charge_start_now", false},
		{"charge_start in prose", false},
	}
	for _, tt := range tests {
		if got := documentsName(tt.src, "charge_start"); got != tt.want {
			t.Errorf("documentsName(%q) = %v, want %v", tt.src, got, tt.want)
		}
	}
}

// envVarsRead returns the names passed as string literals to os.Getenv / os.LookupEnv by the
// non-test Go files of the given directories. Only config/ and main.go are scanned (all the
// variables are read in config/config.go): a variable read elsewhere, or through an alias of os
// or a helper, is not detected.
func envVarsRead(t *testing.T, patterns ...string) []string {
	t.Helper()
	seen := map[string]bool{}
	fset := token.NewFileSet()
	for _, pattern := range patterns {
		files, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("glob %s: %v", pattern, err)
		}
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, file, nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", file, err)
			}
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) == 0 {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || (sel.Sel.Name != "Getenv" && sel.Sel.Name != "LookupEnv") {
					return true
				}
				if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "os" {
					return true
				}
				lit, ok := call.Args[0].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					t.Errorf("%s: %s with a non-literal name: the docs test cannot check it", fset.Position(call.Pos()), sel.Sel.Name)
					return true
				}
				name, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("%s: %v", fset.Position(lit.Pos()), err)
				}
				seen[name] = true
				return true
			})
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func TestEnvironmentVariablesDocumented(t *testing.T) {
	names := envVarsRead(t, "config/*.go", "main.go")
	if len(names) == 0 {
		t.Fatalf("no environment variable found in config/ and main.go")
	}
	doc := readRepoFile(t, "docs/environment_variables.md")
	titles := map[string]bool{}
	for _, line := range strings.Split(doc, "\n") {
		if title, ok := strings.CutPrefix(strings.TrimRight(line, "\r"), "## "); ok {
			titles[strings.TrimSpace(title)] = true
		}
	}
	for _, name := range names {
		if !titles[name] {
			t.Errorf("docs/environment_variables.md has no \"## %s\" section (variable read by the proxy)", name)
		}
	}
}

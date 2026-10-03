package commands

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"testing"
)

// sendSwitchCases reads commands.go and returns the string literals of the
// case clauses of the top-level command switch in (*Command).Send.
// The vehicle SDK and BLE are never touched.
func sendSwitchCases(t *testing.T) map[string]bool {
	t.Helper()

	src, err := os.ReadFile("commands.go")
	if err != nil {
		t.Fatalf("failed to read commands.go: %v", err)
	}
	return parseSendCases(t, src)
}

// parseSendCases parses src and returns the string literals of the case
// clauses of the top-level `switch x.Command` in the Send method.
// Nested switches (roles, endpoints, type switches) are ignored on purpose.
func parseSendCases(t *testing.T, src []byte) map[string]bool {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "src.go", src, 0)
	if err != nil {
		t.Fatalf("failed to parse source: %v", err)
	}

	var send *ast.FuncDecl
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Recv != nil && fn.Name.Name == "Send" && fn.Body != nil {
			send = fn
			break
		}
	}
	if send == nil {
		t.Fatal("method Send not found in source")
	}

	var commandSwitch *ast.SwitchStmt
	for _, stmt := range send.Body.List {
		sw, ok := stmt.(*ast.SwitchStmt)
		if !ok {
			continue
		}
		if sel, ok := sw.Tag.(*ast.SelectorExpr); ok && sel.Sel.Name == "Command" {
			commandSwitch = sw
			break
		}
	}
	if commandSwitch == nil {
		t.Fatal("switch on command.Command not found in (*Command).Send")
	}

	handled := make(map[string]bool)
	for _, stmt := range commandSwitch.Body.List {
		clause, ok := stmt.(*ast.CaseClause)
		if !ok {
			continue
		}
		for _, expr := range clause.List {
			lit, ok := expr.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			value, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatalf("failed to unquote case %s: %v", lit.Value, err)
			}
			handled[value] = true
		}
	}
	if len(handled) == 0 {
		t.Fatal("no string case found in the command switch of (*Command).Send")
	}

	return handled
}

// missingCases returns the expected commands absent from handled, in the
// order of expected.
func missingCases(expected []string, handled map[string]bool) []string {
	missing := []string{}
	for _, name := range expected {
		if !handled[name] {
			missing = append(missing, name)
		}
	}
	return missing
}

func TestLegacyRouteCommandsHaveSendCase(t *testing.T) {
	for _, name := range missingCases(legacyRouteCommands, sendSwitchCases(t)) {
		t.Errorf("command %q is in legacyRouteCommands but has no case in (*Command).Send", name)
	}
}

func TestRegistryCommandsAreNotInSendSwitch(t *testing.T) {
	cases := sendSwitchCases(t)
	for name := range fleetVehicleCommands {
		if cases[name] {
			t.Errorf("command %q is in the registry and also has a case in (*Command).Send", name)
		}
	}
}

func TestParseSendCasesIgnoresNestedSwitches(t *testing.T) {
	src := []byte(`package commands

type Command struct{ Command string }

func (c *Command) Send() {
	switch c.Command {
	case "a":
		switch c.Command {
		case "c":
		}
	case "b":
	}
}
`)

	got := parseSendCases(t, src)
	want := map[string]bool{"a": true, "b": true}
	if len(got) != len(want) {
		t.Fatalf("parseSendCases() = %v, want %v", got, want)
	}
	for name := range want {
		if !got[name] {
			t.Errorf("parseSendCases() is missing %q, got %v", name, got)
		}
	}
	if got["c"] {
		t.Errorf("parseSendCases() reported nested case %q, got %v", "c", got)
	}
}

func TestMissingCasesReportsUnhandledCommand(t *testing.T) {
	tests := []struct {
		name     string
		expected []string
		handled  map[string]bool
		want     []string
	}{
		{
			name:     "all handled",
			expected: []string{"a", "b"},
			handled:  map[string]bool{"a": true, "b": true},
			want:     []string{},
		},
		{
			name:     "one missing",
			expected: []string{"a", "b", "c"},
			handled:  map[string]bool{"a": true, "b": true},
			want:     []string{"c"},
		},
		{
			name:     "empty",
			expected: []string{},
			handled:  map[string]bool{},
			want:     []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := missingCases(tt.expected, tt.handled)
			if len(got) != len(tt.want) {
				t.Fatalf("missingCases() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("missingCases()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

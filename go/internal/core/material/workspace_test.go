package material

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sampleCatalog() Catalog {
	return Catalog{"material": {Type: "material", Inputs: []Port{{Name: "albedo", Type: "rgb"}}, Outputs: []Port{}, Parameters: []Param{{Name: "roughness", Type: "float"}}}, "noise": {Type: "noise", Inputs: []Port{}, Outputs: []Port{{Type: "rgb"}}, Parameters: []Param{{Name: "scale", Type: "float"}}}}
}
func sampleGraph() Graph {
	return Graph{"nodes": []any{map[string]any{"name": "Material", "type": "material", "parameters": map[string]any{"roughness": 0.5}}, map[string]any{"name": "n", "type": "noise", "parameters": map[string]any{"scale": 2.0}}}, "connections": []any{map[string]any{"from": "n", "from_port": 0, "to": "Material", "to_port": 0}}}
}
func TestWorkspaceCreateEditAtomicAndConflict(t *testing.T) {
	w, err := OpenWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m, problems, err := w.Create("stone", sampleGraph(), sampleCatalog())
	if err != nil {
		t.Fatalf("create: %v %+v", err, problems)
	}
	if m.Revision == "" {
		t.Fatal("no revision")
	}
	if _, _, err := w.Create("stone", sampleGraph(), sampleCatalog()); err == nil {
		t.Fatal("create overwrote existing material")
	}
	original := m.Revision
	_, _, err = w.Edit("stone", original, []Operation{{Op: "set_param", Node: "n", Param: "scale", Value: 3.0}, {Op: "set_param", Node: "does-not-exist", Param: "scale", Value: 4.0}}, sampleCatalog())
	if err == nil {
		t.Fatal("partial invalid batch was accepted")
	}
	unchanged, err := w.Inspect("stone")
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Revision != original {
		t.Fatal("failed batch changed material")
	}
	edited, _, err := w.Edit("stone", original, []Operation{{Op: "set_param", Node: "n", Param: "scale", Value: 3.0}, {Op: "move_node", Node: "n", X: 8, Y: 9}}, sampleCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if edited.Revision == original {
		t.Fatal("edit did not change revision")
	}
	if _, _, err := w.Edit("stone", original, []Operation{{Op: "set_param", Node: "n", Param: "scale", Value: 4.0}}, sampleCatalog()); err == nil {
		t.Fatal("stale edit accepted")
	}
	if _, err := os.Stat(filepath.Join(w.Root, "assets", "materials", "source", "stone.ptex")); err != nil {
		t.Fatal(err)
	}
}
func TestRejectTraversalAndSymlink(t *testing.T) {
	w, err := OpenWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"../secret", "bad/name", "", ".env", "C:\\tmp\\secret"} {
		if _, err := w.SourcePath(n); err == nil {
			t.Errorf("accepted name %q", n)
		}
	}
	out := t.TempDir()
	if err := os.MkdirAll(filepath.Join(w.Root, "assets", "materials"), 0755); err != nil {
		t.Fatal(err)
	}
	err = os.Symlink(out, filepath.Join(w.Root, "assets", "materials", "source"))
	if err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	if _, err := w.SourcePath("safe"); err == nil {
		t.Fatal("accepted symlinked source directory")
	}
}
func TestTemplateDoesNotEscapeCookbook(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "stone"), 0755)
	if err := os.WriteFile(filepath.Join(root, "stone", "cliff.ptex"), []byte(`{"nodes":[],"connections":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTemplate(root, "stone", "cliff"); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTemplate(root, "stone", "../.env"); err == nil {
		t.Fatal("template traversal accepted")
	}
}
func TestScopedEditAndValidation(t *testing.T) {
	g := sampleGraph()
	g["nodes"] = append(g["nodes"].([]any), map[string]any{"name": "inner", "type": "graph", "nodes": []any{map[string]any{"name": "n2", "type": "noise", "parameters": map[string]any{"scale": 5.0}}}, "connections": []any{}})
	result, err := ApplyOps(g, []Operation{{Op: "set_param", Scope: "inner", Node: "n2", Param: "scale", Value: 8.0}}, sampleCatalog())
	if err != nil {
		t.Fatal(err)
	}
	inner, _, err := findNode(result, "inner")
	if err != nil {
		t.Fatal(err)
	}
	child, _, err := findNode(Graph(inner), "n2")
	if err != nil {
		t.Fatal(err)
	}
	if child["parameters"].(map[string]any)["scale"] != 8.0 {
		t.Fatal("nested parameter not updated")
	}
	if len(Validate(result, sampleCatalog())) > 0 {
		t.Fatalf("graph validation failed: %+v", Validate(result, sampleCatalog()))
	}
	_, err = ApplyOps(result, []Operation{{Op: "set_param", Scope: "inner/../", Node: "n2", Param: "scale", Value: 9.0}}, sampleCatalog())
	if err == nil {
		t.Fatal("accepted traversal scope")
	}
}
func TestNodeDefinitionsCatalog(t *testing.T) {
	dir := t.TempDir()
	leaf := `{"shader_model":{"inputs":[{"name":"in","type":"f"}],"outputs":[{"type":"f"}],"parameters":[{"name":"scale","type":"float","min":0,"max":10,"default":2}]}}`
	generic := `{"nodes":[{"type":"ios","name":"gen_inputs","ports":[{"name":"input","type":"f"}]},{"type":"ios","name":"gen_outputs","ports":[{"name":"output","type":"f"}]},{"type":"remote","parameters":{"param0":4},"widgets":[{"name":"param0","linked_widgets":[{"node":"leaf","widget":"scale"}]}]},{"type":"noise","name":"leaf"}]}`
	for name, body := range map[string]string{"noise.mmg": leaf, "compound.mmg": generic} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	c, err := LoadCatalog(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c["noise"].Parameters[0].Name != "scale" {
		t.Fatal("leaf catalog not parsed")
	}
	if p := c["compound"].Parameters[0]; p.Default != float64(4) || p.Min == nil || *p.Min != 0 {
		t.Fatalf("compound parameter not resolved: %+v", p)
	}
	if names := c.List("noise", 10); len(names) != 1 || !strings.EqualFold(names[0], "noise") {
		t.Fatalf("search: %v", names)
	}
}

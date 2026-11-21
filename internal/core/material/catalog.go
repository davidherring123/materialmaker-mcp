// Package material provides workspace-scoped, deterministic .ptex graph operations.
package material

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Param struct {
	Name    string   `json:"name"`
	Type    string   `json:"type,omitempty"`
	Default any      `json:"default,omitempty"`
	Min     *float64 `json:"min,omitempty"`
	Max     *float64 `json:"max,omitempty"`
	Values  []string `json:"values,omitempty"`
}
type Port struct {
	Name string `json:"name,omitempty"`
	Type string `json:"type,omitempty"`
}
type NodeType struct {
	Type       string  `json:"type"`
	Inputs     []Port  `json:"inputs"`
	Outputs    []Port  `json:"outputs"`
	Parameters []Param `json:"parameters"`
}
type Catalog map[string]NodeType

type rawNode struct {
	ShaderModel *struct {
		Inputs     []Port           `json:"inputs"`
		Outputs    []Port           `json:"outputs"`
		Parameters []map[string]any `json:"parameters"`
	} `json:"shader_model"`
	Nodes       []map[string]any `json:"nodes"`
	GenericSize int              `json:"generic_size"`
}

// LoadCatalog reads the installed Material Maker node definitions. No download or execution occurs.
func LoadCatalog(dir string) (Catalog, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.mmg"))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no Material Maker .mmg node definitions in %q", dir)
	}
	sort.Strings(paths)
	catalog := Catalog{}
	compounds := map[string]rawNode{}
	for _, p := range paths {
		name, raw, err := readNodeDefinition(p)
		if err != nil {
			return nil, err
		}
		if raw.ShaderModel != nil {
			catalog[name] = parseShaderNode(name, raw)
		}
		if raw.ShaderModel == nil && len(raw.Nodes) > 0 {
			compounds[name] = raw
			catalog[name] = NodeType{Type: name, Inputs: []Port{}, Outputs: []Port{}, Parameters: []Param{}}
		}
	}
	resolveCompounds(catalog, compounds)
	return catalog, nil
}
func readNodeDefinition(path string) (string, rawNode, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", rawNode{}, err
	}
	var raw rawNode
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", rawNode{}, fmt.Errorf("%s: %w", path, err)
	}
	return strings.TrimSuffix(filepath.Base(path), ".mmg"), raw, nil
}
func parseShaderNode(name string, raw rawNode) NodeType {
	result := NodeType{Type: name, Inputs: []Port{}, Outputs: raw.ShaderModel.Outputs, Parameters: []Param{}}
	repeat := raw.GenericSize
	if repeat <= 0 {
		repeat = 1
	}
	for _, input := range raw.ShaderModel.Inputs {
		count := 1
		if strings.Contains(input.Name, "#") {
			count = repeat
		}
		for i := 0; i < count; i++ {
			result.Inputs = append(result.Inputs, input)
		}
	}
	for _, p := range raw.ShaderModel.Parameters {
		result.Parameters = append(result.Parameters, parseParam(p))
	}
	return result
}
func resolveCompounds(catalog Catalog, compounds map[string]rawNode) {
	for round := 0; round < 12; round++ {
		changed := false
		for _, name := range sortedKeys(compounds) {
			next := parseCompound(name, compounds[name], catalog)
			before, _ := json.Marshal(catalog[name])
			after, _ := json.Marshal(next)
			if string(before) != string(after) {
				catalog[name] = next
				changed = true
			}
		}
		if !changed {
			break
		}
	}
}
func sortedKeys(m map[string]rawNode) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
func parseParam(p map[string]any) Param {
	out := Param{Name: str(p["name"]), Type: str(p["type"]), Default: p["default"]}
	if out.Type == "enum" {
		if values, ok := p["values"].([]any); ok {
			for _, v := range values {
				if m, ok := v.(map[string]any); ok {
					out.Values = append(out.Values, str(m["name"]))
				}
			}
		}
		z := float64(0)
		upper := float64(len(out.Values) - 1)
		if upper < 0 {
			upper = 0
		}
		out.Min = &z
		out.Max = &upper
	} else {
		if v, ok := num(p["min"]); ok {
			out.Min = &v
		}
		if v, ok := num(p["max"]); ok {
			out.Max = &v
		}
	}
	return out
}
func parseCompound(name string, raw rawNode, catalog Catalog) NodeType {
	out := NodeType{Type: name, Inputs: []Port{}, Outputs: []Port{}, Parameters: []Param{}}
	for _, child := range raw.Nodes {
		if str(child["type"]) != "ios" {
			continue
		}
		switch str(child["name"]) {
		case "gen_inputs":
			out.Inputs = readPorts(child["ports"])
		case "gen_outputs":
			out.Outputs = readPorts(child["ports"])
		}
	}
	for _, child := range raw.Nodes {
		if str(child["type"]) != "remote" {
			continue
		}
		defaults, _ := child["parameters"].(map[string]any)
		widgets, _ := child["widgets"].([]any)
		for _, entry := range widgets {
			widget, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			pname := str(widget["name"])
			if pname == "" {
				continue
			}
			out.Parameters = append(out.Parameters, parseCompoundParameter(pname, widget, defaults, raw.Nodes, catalog))
		}
	}
	return out
}
func parseCompoundParameter(name string, widget, defaults map[string]any, children []map[string]any, catalog Catalog) Param {
	param := Param{Name: name}
	if def, ok := defaults[name]; ok {
		param.Default = def
	}
	if minimum, ok := num(widget["min"]); ok {
		param.Min = &minimum
		param.Type = "float"
	}
	if maximum, ok := num(widget["max"]); ok {
		param.Max = &maximum
	}
	links, ok := widget["linked_widgets"].([]any)
	if !ok || len(links) == 0 {
		return param
	}
	link, ok := links[0].(map[string]any)
	if !ok {
		return param
	}
	for _, child := range children {
		if str(child["name"]) != str(link["node"]) {
			continue
		}
		for _, candidate := range catalog[str(child["type"])].Parameters {
			if candidate.Name != str(link["widget"]) {
				continue
			}
			param.Type = candidate.Type
			param.Min = candidate.Min
			param.Max = candidate.Max
			param.Values = candidate.Values
			if param.Default == nil {
				param.Default = candidate.Default
			}
			break
		}
		break
	}
	return param
}
func readPorts(value any) []Port {
	out := []Port{}
	entries, _ := value.([]any)
	for _, v := range entries {
		m, ok := v.(map[string]any)
		if ok {
			out = append(out, Port{Name: str(m["name"]), Type: str(m["type"])})
		}
	}
	return out
}
func str(v any) string { s, _ := v.(string); return s }
func num(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case json.Number:
		f, e := n.Float64()
		return f, e == nil
	}
	return 0, false
}
func (c Catalog) List(query string, limit int) []string {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	out := []string{}
	for name := range c {
		if strings.Contains(strings.ToLower(name), strings.ToLower(query)) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	if len(out) > limit {
		return out[:limit]
	}
	return out
}

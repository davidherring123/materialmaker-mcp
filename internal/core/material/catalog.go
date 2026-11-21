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
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var raw rawNode
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		name := strings.TrimSuffix(filepath.Base(p), ".mmg")
		if raw.ShaderModel != nil {
			definition := NodeType{Type: name, Inputs: []Port{}, Outputs: raw.ShaderModel.Outputs, Parameters: []Param{}}
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
					definition.Inputs = append(definition.Inputs, input)
				}
			}
			for _, rawParam := range raw.ShaderModel.Parameters {
				definition.Parameters = append(definition.Parameters, parseParam(rawParam))
			}
			catalog[name] = definition
		} else if len(raw.Nodes) > 0 {
			compounds[name] = raw
			catalog[name] = NodeType{Type: name, Inputs: []Port{}, Outputs: []Port{}, Parameters: []Param{}}
		}
	}
	// Compound parameters can reference another compound. Resolve to a fixed point.
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
	return catalog, nil
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
		max := float64(len(out.Values) - 1)
		if max < 0 {
			max = 0
		}
		out.Min = &z
		out.Max = &max
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
		if str(child["type"]) == "ios" {
			ports := readPorts(child["ports"])
			switch str(child["name"]) {
			case "gen_inputs":
				out.Inputs = ports
			case "gen_outputs":
				out.Outputs = ports
			}
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
			param := Param{Name: pname}
			if def, ok := defaults[pname]; ok {
				param.Default = def
			}
			if min, ok := num(widget["min"]); ok {
				param.Min = &min
				param.Type = "float"
			}
			if max, ok := num(widget["max"]); ok {
				param.Max = &max
			}
			if links, ok := widget["linked_widgets"].([]any); ok && len(links) > 0 {
				if link, ok := links[0].(map[string]any); ok {
					for _, inner := range raw.Nodes {
						if str(inner["name"]) != str(link["node"]) {
							continue
						}
						t := str(inner["type"])
						if spec, ok := catalog[t]; ok {
							for _, p := range spec.Parameters {
								if p.Name == str(link["widget"]) {
									param.Type = p.Type
									param.Min = p.Min
									param.Max = p.Max
									param.Values = p.Values
									if param.Default == nil {
										param.Default = p.Default
									}
									break
								}
							}
						}
						break
					}
				}
			}
			out.Parameters = append(out.Parameters, param)
		}
	}
	return out
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

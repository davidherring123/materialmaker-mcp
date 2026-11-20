package material

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
)

type Graph map[string]any
type Problem struct {
	Severity string `json:"severity"`
	Where    string `json:"where"`
	Message  string `json:"message"`
}
type Operation struct {
	Op         string         `json:"op"`
	Scope      string         `json:"scope,omitempty"`
	Node       string         `json:"node,omitempty"`
	Type       string         `json:"type,omitempty"`
	Param      string         `json:"param,omitempty"`
	Value      any            `json:"value,omitempty"`
	Parameters map[string]any `json:"parameters,omitempty"`
	From       string         `json:"from,omitempty"`
	To         string         `json:"to,omitempty"`
	FromPort   int            `json:"from_port,omitempty"`
	ToPort     int            `json:"to_port,omitempty"`
	X          float64        `json:"x,omitempty"`
	Y          float64        `json:"y,omitempty"`
}

var special = map[string]bool{"graph": true, "comment": true, "remote": true, "shader": true, "buffer": true, "image": true, "switch": true, "debug": true, "ios": true}

func ParseGraph(data []byte) (Graph, error) {
	var g Graph
	if err := json.Unmarshal(data, &g); err != nil {
		return nil, err
	}
	if g == nil {
		return nil, errors.New("graph must be an object")
	}
	return g, nil
}
func (g Graph) Clone() (Graph, error) {
	data, err := json.Marshal(g)
	if err != nil {
		return nil, err
	}
	return ParseGraph(data)
}
func objects(v any) ([]any, bool)           { raw, ok := v.([]any); return raw, ok }
func asObject(v any) (map[string]any, bool) { raw, ok := v.(map[string]any); return raw, ok }
func nodes(g Graph) ([]any, error) {
	n, ok := objects(g["nodes"])
	if !ok {
		return nil, errors.New("graph.nodes must be an array")
	}
	return n, nil
}
func findNode(g Graph, name string) (map[string]any, int, error) {
	ns, err := nodes(g)
	if err != nil {
		return nil, -1, err
	}
	for i, v := range ns {
		m, ok := asObject(v)
		if ok && str(m["name"]) == name {
			return m, i, nil
		}
	}
	return nil, -1, fmt.Errorf("node %q not found", name)
}
func scopeGraph(g Graph, scope string) (Graph, error) {
	if scope == "" {
		return g, nil
	}
	current := g
	for _, s := range strings.Split(scope, "/") {
		if s == "" || s == "." || s == ".." {
			return nil, errors.New("invalid graph scope")
		}
		node, _, err := findNode(current, s)
		if err != nil {
			return nil, err
		}
		if str(node["type"]) != "graph" {
			return nil, fmt.Errorf("%q is not a subgraph", s)
		}
		current = Graph(node)
	}
	return current, nil
}
func Validate(g Graph, catalog Catalog) []Problem {
	var result []Problem
	validateAt(g, catalog, "", &result, 0)
	return result
}
func validateAt(g Graph, catalog Catalog, prefix string, out *[]Problem, depth int) {
	issue := func(severity, where, msg string) { *out = append(*out, Problem{severity, prefix + where, msg}) }
	if depth > 32 {
		issue("error", "graph", "subgraph nesting too deep")
		return
	}
	ns, err := nodes(g)
	if err != nil {
		issue("error", "graph", err.Error())
		return
	}
	byName := map[string]map[string]any{}
	for _, item := range ns {
		n, ok := asObject(item)
		if !ok {
			issue("error", "graph", "node must be an object")
			continue
		}
		name := str(n["name"])
		t := str(n["type"])
		if name == "" {
			issue("error", "graph", "node name is empty")
			continue
		}
		if _, exists := byName[name]; exists {
			issue("error", name, "duplicate node name")
		}
		byName[name] = n
		if t == "" {
			issue("error", name, "node type is empty")
			continue
		}
		spec, known := catalog[t]
		if !known && !special[t] {
			issue("error", name, "unknown node type "+t)
			continue
		}
		params, _ := asObject(n["parameters"])
		if known {
			allowed := map[string]Param{}
			for _, p := range spec.Parameters {
				allowed[p.Name] = p
			}
			for key, val := range params {
				param, ok := allowed[key]
				if !ok {
					issue("warning", name, "unknown parameter "+key)
					continue
				}
				if number, ok := num(val); ok {
					if math.IsNaN(number) || math.IsInf(number, 0) {
						issue("error", name, "non-finite parameter "+key)
						continue
					}
					if param.Min != nil && number < *param.Min || param.Max != nil && number > *param.Max {
						severity := "warning"
						if param.Type == "enum" {
							severity = "error"
						}
						issue(severity, name, fmt.Sprintf("parameter %s=%v outside declared bounds", key, number))
					}
				}
			}
		}
		if t == "graph" {
			if _, ok := objects(n["nodes"]); ok {
				validateAt(Graph(n), catalog, name+"/", out, depth+1)
			}
		}
	}
	links, ok := objects(g["connections"])
	if !ok {
		issue("error", "graph", "graph.connections must be an array")
		return
	}
	for _, item := range links {
		link, ok := asObject(item)
		if !ok {
			issue("error", "connection", "connection must be an object")
			continue
		}
		from, to := str(link["from"]), str(link["to"])
		if from != "graph" && byName[from] == nil {
			issue("error", "connection", "missing source node "+from)
		}
		if to != "graph" && byName[to] == nil {
			issue("error", "connection", "missing destination node "+to)
		}
		for _, side := range []struct {
			name, key string
			ports     func(NodeType) int
		}{{"from", from, func(n NodeType) int { return len(n.Outputs) }}, {"to", to, func(n NodeType) int { return len(n.Inputs) }}} {
			if n := byName[side.key]; n != nil {
				spec, ok := catalog[str(n["type"])]
				if !ok {
					continue
				}
				port := 0
				if v, ok := num(link[side.name+"_port"]); ok {
					port = int(v)
					if float64(port) != v {
						issue("error", "connection", side.name+" port must be an integer")
					}
				}
				if port < 0 || port >= side.ports(spec) {
					issue("error", "connection", fmt.Sprintf("%s_port %d out of range for %s", side.name, port, side.key))
				}
			}
		}
	}
}

// ApplyOps changes an in-memory copy. Callers validate and atomically save the result.
func ApplyOps(g Graph, ops []Operation, catalog Catalog) (Graph, error) {
	next, err := g.Clone()
	if err != nil {
		return nil, err
	}
	if len(ops) == 0 || len(ops) > 100 {
		return nil, errors.New("edit requires 1-100 operations")
	}
	for i, op := range ops {
		if err := applyOne(next, op, catalog); err != nil {
			return nil, fmt.Errorf("operation %d (%s): %w", i, op.Op, err)
		}
	}
	return next, nil
}
func applyOne(root Graph, op Operation, catalog Catalog) error {
	g, err := scopeGraph(root, op.Scope)
	if err != nil {
		return err
	}
	ns, err := nodes(g)
	if err != nil {
		return err
	}
	switch op.Op {
	case "set_param":
		if op.Param == "" {
			return errors.New("parameter name required")
		}
		node, _, err := findNode(g, op.Node)
		if err != nil {
			return err
		}
		t := str(node["type"])
		if !special[t] {
			spec, ok := catalog[t]
			if !ok {
				return fmt.Errorf("unknown node type %q", t)
			}
			known := false
			for _, p := range spec.Parameters {
				if p.Name == op.Param {
					known = true
					break
				}
			}
			if !known {
				return fmt.Errorf("unknown parameter %q for %q", op.Param, t)
			}
		}
		params, _ := asObject(node["parameters"])
		if params == nil {
			params = map[string]any{}
			node["parameters"] = params
		}
		params[op.Param] = op.Value
	case "add_node":
		if op.Node == "" || strings.Contains(op.Node, "/") {
			return errors.New("node must be a nonempty name without /")
		}
		if _, _, err := findNode(g, op.Node); err == nil {
			return fmt.Errorf("node %q already exists", op.Node)
		}
		if !special[op.Type] {
			if _, ok := catalog[op.Type]; !ok {
				return fmt.Errorf("unknown node type %q", op.Type)
			}
		}
		if op.Parameters == nil {
			op.Parameters = map[string]any{}
		}
		g["nodes"] = append(ns, map[string]any{"name": op.Node, "type": op.Type, "parameters": op.Parameters, "node_position": map[string]any{"x": op.X, "y": op.Y}})
	case "remove_node":
		_, index, err := findNode(g, op.Node)
		if err != nil {
			return err
		}
		if op.Node == "Material" {
			return errors.New("cannot delete Material output node")
		}
		g["nodes"] = append(ns[:index:index], ns[index+1:]...)
		links, _ := objects(g["connections"])
		clean := []any{}
		for _, v := range links {
			m, ok := asObject(v)
			if ok && (str(m["from"]) == op.Node || str(m["to"]) == op.Node) {
				continue
			}
			clean = append(clean, v)
		}
		g["connections"] = clean
	case "move_node":
		node, _, err := findNode(g, op.Node)
		if err != nil {
			return err
		}
		if math.IsNaN(op.X) || math.IsInf(op.X, 0) || math.IsNaN(op.Y) || math.IsInf(op.Y, 0) {
			return errors.New("position must be finite")
		}
		node["node_position"] = map[string]any{"x": op.X, "y": op.Y}
	case "connect", "disconnect":
		if _, _, err := findNode(g, op.From); err != nil && op.From != "graph" {
			return err
		}
		if _, _, err := findNode(g, op.To); err != nil && op.To != "graph" {
			return err
		}
		if op.FromPort < 0 || op.ToPort < 0 {
			return errors.New("negative ports not supported")
		}
		links, _ := objects(g["connections"])
		matches := func(v any) bool {
			m, ok := asObject(v)
			if !ok {
				return false
			}
			fp, _ := num(m["from_port"])
			tp, _ := num(m["to_port"])
			return str(m["from"]) == op.From && str(m["to"]) == op.To && int(fp) == op.FromPort && int(tp) == op.ToPort
		}
		if op.Op == "disconnect" {
			updated := []any{}
			found := false
			for _, v := range links {
				if matches(v) {
					found = true
					continue
				}
				updated = append(updated, v)
			}
			if !found {
				return errors.New("connection not found")
			}
			g["connections"] = updated
			break
		}
		for _, v := range links {
			if matches(v) {
				return errors.New("connection already exists")
			}
			m, ok := asObject(v)
			if !ok {
				continue
			}
			tp, _ := num(m["to_port"])
			if str(m["to"]) == op.To && int(tp) == op.ToPort {
				return errors.New("destination port already connected; disconnect it first")
			}
		}
		g["connections"] = append(links, map[string]any{"from": op.From, "from_port": op.FromPort, "to": op.To, "to_port": op.ToPort})
	default:
		return fmt.Errorf("unknown edit operation %q", op.Op)
	}
	return nil
}

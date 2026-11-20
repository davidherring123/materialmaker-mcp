// Package app coordinates core material operations and the configured renderer.
// Neither the core nor this application layer imports or depends on MCP.
package app

import (
	"context"
	"errors"

	"github.com/davidherring123/materialmaker-mcp/go/internal/adapter/materialmaker"
	"github.com/davidherring123/materialmaker-mcp/go/internal/core/material"
)

type Service struct {
	Workspace *material.Workspace
	Catalog   material.Catalog
	Templates string
	Renderer  *materialmaker.Renderer
}

func (s *Service) List() ([]string, error)                        { return s.Workspace.List() }
func (s *Service) Inspect(name string) (material.Material, error) { return s.Workspace.Inspect(name) }
func (s *Service) CatalogSearch(query string, limit int) []string {
	return s.Catalog.List(query, limit)
}
func (s *Service) Describe(nodeType string) (material.NodeType, error) {
	node, ok := s.Catalog[nodeType]
	if !ok {
		return material.NodeType{}, errors.New("node type not found")
	}
	return node, nil
}
func (s *Service) Create(name, category, template string) (material.Material, []material.Problem, error) {
	var graph material.Graph
	if category != "" || template != "" {
		g, err := material.LoadTemplate(s.Templates, category, template)
		if err != nil {
			return material.Material{}, nil, err
		}
		graph = g
	} else {
		graph = material.Graph{"name": name, "label": name, "nodes": []any{map[string]any{"name": "Material", "type": "material", "parameters": map[string]any{}}}, "connections": []any{}}
	}
	return s.Workspace.Create(name, graph, s.Catalog)
}
func (s *Service) Validate(name string) ([]material.Problem, error) {
	mat, err := s.Inspect(name)
	if err != nil {
		return nil, err
	}
	return material.Validate(mat.Graph, s.Catalog), nil
}
func (s *Service) Edit(name, revision string, ops []material.Operation) (material.Material, []material.Problem, error) {
	return s.Workspace.Edit(name, revision, ops, s.Catalog)
}
func (s *Service) Render(ctx context.Context, name string, size int) (materialmaker.RenderResult, error) {
	if s.Renderer == nil {
		return materialmaker.RenderResult{}, errors.New("renderer not configured")
	}
	mat, err := s.Inspect(name)
	if err != nil {
		return materialmaker.RenderResult{}, err
	}
	if issues := material.Validate(mat.Graph, s.Catalog); hasErrors(issues) {
		return materialmaker.RenderResult{}, errors.New("graph validation failed; cannot render")
	}
	return s.Renderer.Render(ctx, s.Workspace, name, size)
}
func hasErrors(problems []material.Problem) bool {
	for _, p := range problems {
		if p.Severity == "error" {
			return true
		}
	}
	return false
}

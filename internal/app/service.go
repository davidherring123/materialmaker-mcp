// Package app coordinates core material operations and the configured renderer.
// Neither the core nor this application layer imports or depends on MCP.
package app

import (
	"context"
	"errors"

	"github.com/davidherring123/materialmaker-mcp/internal/adapter/materialmaker"
	"github.com/davidherring123/materialmaker-mcp/internal/core/material"
)

type Service struct {
	Workspace *material.Workspace
	Catalog   material.Catalog
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

// Create makes a fresh material or clones an existing user-owned material.
// No external files or bundled cookbook templates are read.
func (s *Service) Create(name, sourceMaterial string) (material.Material, []material.Problem, error) {
	var graph material.Graph
	if sourceMaterial != "" {
		source, err := s.Workspace.Inspect(sourceMaterial)
		if err != nil {
			return material.Material{}, nil, err
		}
		cloned, err := source.Graph.Clone()
		if err != nil {
			return material.Material{}, nil, err
		}
		graph = cloned
		graph["name"] = name
		graph["label"] = name
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

// Preview validates the current graph, then renders maps and a 3D screenshot
// through the same renderer adapter. Domain services contain no MCP types.
func (s *Service) Preview(ctx context.Context, name string, size int) (materialmaker.PreviewResult, error) {
	if s.Renderer == nil {
		return materialmaker.PreviewResult{}, errors.New("renderer not configured")
	}
	mat, err := s.Inspect(name)
	if err != nil {
		return materialmaker.PreviewResult{}, err
	}
	if issues := material.Validate(mat.Graph, s.Catalog); hasErrors(issues) {
		return materialmaker.PreviewResult{}, errors.New("graph validation failed; cannot preview")
	}
	return s.Renderer.Preview(ctx, s.Workspace, name, size)
}
func hasErrors(problems []material.Problem) bool {
	for _, p := range problems {
		if p.Severity == "error" {
			return true
		}
	}
	return false
}

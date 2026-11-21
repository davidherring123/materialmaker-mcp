// Package core verifies the material domain through its exported API, independent of MCP.
package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/davidherring123/materialmaker-mcp/internal/core/material"
)

func TestReferenceGraphContractAndOptimisticEdits(t *testing.T) {
	nodeDir := filepath.Join("..", "fixtures", "nodes")
	catalog, err := material.LoadCatalog(nodeDir)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join("..", "fixtures", "graphs", "stone.ptex"))
	if err != nil {
		t.Fatal(err)
	}
	graph, err := material.ParseGraph(body)
	if err != nil {
		t.Fatal(err)
	}
	if issues := material.Validate(graph, catalog); len(issues) != 0 {
		t.Fatalf("valid fixture rejected: %+v", issues)
	}
	workspace, err := material.OpenWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	created, problems, err := workspace.Create("stone", graph, catalog)
	if err != nil {
		t.Fatalf("create: %v (problems: %+v)", err, problems)
	}
	originalRevision := created.Revision
	if originalRevision == "" {
		t.Fatal("missing initial revision")
	}
	_, _, err = workspace.Edit("stone", originalRevision, []material.Operation{
		{Op: "set_param", Node: "StoneNoise", Param: "scale", Value: 8},
		{Op: "set_param", Node: "MissingNode", Param: "scale", Value: 10},
	}, catalog)
	if err == nil {
		t.Fatal("invalid transaction unexpectedly succeeded")
	}
	unchanged, err := workspace.Inspect("stone")
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Revision != originalRevision {
		t.Fatal("failed transaction changed the material")
	}
	updated, problems, err := workspace.Edit("stone", originalRevision, []material.Operation{
		{Op: "set_param", Node: "StoneNoise", Param: "scale", Value: 8},
	}, catalog)
	if err != nil {
		t.Fatalf("edit: %v (problems: %+v)", err, problems)
	}
	if updated.Revision == originalRevision {
		t.Fatal("successful edit did not change revision")
	}
	if _, _, err := workspace.Edit("stone", originalRevision, []material.Operation{{Op: "set_param", Node: "StoneNoise", Param: "scale", Value: 9}}, catalog); err == nil {
		t.Fatal("stale revision was accepted")
	}
	if _, err := workspace.SourcePath("../private"); err == nil {
		t.Fatal("path traversal was accepted")
	}
}

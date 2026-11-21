package app

import (
	"github.com/davidherring123/materialmaker-mcp/internal/core/material"
	"testing"
)

func TestCreateFromWorkspaceReference(t *testing.T) {
	w, err := material.OpenWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{Workspace: w, Catalog: material.Catalog{"material": {Type: "material", Inputs: []material.Port{}, Outputs: []material.Port{}, Parameters: []material.Param{}}}}
	source, _, err := svc.Create("stone", "")
	if err != nil {
		t.Fatal(err)
	}
	cloned, _, err := svc.Create("similar_stone", "stone")
	if err != nil {
		t.Fatal(err)
	}
	if cloned.Name != "similar_stone" || cloned.Graph["name"] != "similar_stone" {
		t.Fatalf("bad clone name %+v", cloned)
	}
	if _, _, err := svc.Create("bad", "../secret"); err == nil {
		t.Fatal("allowed non-workspace reference")
	}
	current, err := svc.Inspect("stone")
	if err != nil {
		t.Fatal(err)
	}
	if current.Revision != source.Revision {
		t.Fatal("source mutated")
	}
}

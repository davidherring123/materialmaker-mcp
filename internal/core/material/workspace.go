package material

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

var materialName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,79}$`)

// Workspace is a filesystem-backed material store. It has no MCP or rendering dependency.
// The caller chooses the workspace; material paths are generated from validated names.
type Workspace struct {
	Root string
	mu   sync.Mutex
}

type Material struct {
	Name     string `json:"name"`
	Graph    Graph  `json:"graph"`
	Revision string `json:"revision"`
}

func OpenWorkspace(root string) (*Workspace, error) {
	if root == "" {
		return nil, errors.New("workspace is required")
	}
	p, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(p)
	if err != nil {
		return nil, fmt.Errorf("workspace: %w", err)
	}
	if !info.IsDir() {
		return nil, errors.New("workspace must be a directory")
	}
	p, err = filepath.EvalSymlinks(p)
	if err != nil {
		return nil, err
	}
	return &Workspace{Root: p}, nil
}
func ValidName(name string) error {
	if !materialName.MatchString(name) {
		return fmt.Errorf("invalid material name %q (use letters, numbers, underscores, hyphens)", name)
	}
	return nil
}

// rejectSymlinks prevents a pre-existing path component from escaping a workspace.
// Paths are derived exclusively from a validated material name, not agent-controlled filenames.
func rejectSymlinks(root, path string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return errors.New("path escapes workspace")
	}
	current := root
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink in managed path: %s", current)
		}
	}
	return nil
}
func (w *Workspace) SourcePath(name string) (string, error) {
	if err := ValidName(name); err != nil {
		return "", err
	}
	p := filepath.Join(w.Root, "assets", "materials", "source", name+".ptex")
	if err := rejectSymlinks(w.Root, p); err != nil {
		return "", err
	}
	return p, nil
}
func (w *Workspace) GeneratedRoot() (string, error) {
	p := filepath.Join(w.Root, "assets", "materials", "generated")
	if err := rejectSymlinks(w.Root, p); err != nil {
		return "", err
	}
	return p, nil
}

func checksum(content []byte) string { h := sha256.Sum256(content); return hex.EncodeToString(h[:]) }
func (w *Workspace) read(name string) (Material, error) {
	p, err := w.SourcePath(name)
	if err != nil {
		return Material{}, err
	}
	body, err := os.ReadFile(p)
	if err != nil {
		return Material{}, err
	}
	graph, err := ParseGraph(body)
	if err != nil {
		return Material{}, fmt.Errorf("%s: %w", p, err)
	}
	return Material{Name: name, Graph: graph, Revision: checksum(body)}, nil
}
func (w *Workspace) Inspect(name string) (Material, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.read(name)
}
func (w *Workspace) List() ([]string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	root := filepath.Join(w.Root, "assets", "materials", "source")
	if err := rejectSymlinks(w.Root, root); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".ptex") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".ptex")
		if ValidName(name) == nil && e.Type()&os.ModeSymlink == 0 {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}
func writeAtomically(path string, graph Graph) error {
	body, err := json.MarshalIndent(graph, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".material-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(body); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func containsErrors(problems []Problem) bool {
	for _, p := range problems {
		if p.Severity == "error" {
			return true
		}
	}
	return false
}

// Create stores a new graph. Existing materials are never overwritten.
func (w *Workspace) Create(name string, graph Graph, catalog Catalog) (Material, []Problem, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	p, err := w.SourcePath(name)
	if err != nil {
		return Material{}, nil, err
	}
	if _, err := os.Lstat(p); err == nil {
		return Material{}, nil, fmt.Errorf("material %q already exists", name)
	} else if !os.IsNotExist(err) {
		return Material{}, nil, err
	}
	problems := Validate(graph, catalog)
	if containsErrors(problems) {
		return Material{}, problems, errors.New("graph validation failed")
	}
	if err := writeAtomically(p, graph); err != nil {
		return Material{}, problems, err
	}
	mat, err := w.read(name)
	return mat, problems, err
}

// Edit applies operations to a copy, validates the whole result, and commits it once.
// expectedRevision guards against stale AI edits. In-process edits are serialized.
func (w *Workspace) Edit(name, expectedRevision string, operations []Operation, catalog Catalog) (Material, []Problem, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	current, err := w.read(name)
	if err != nil {
		return Material{}, nil, err
	}
	if expectedRevision != "" && current.Revision != expectedRevision {
		return Material{}, nil, fmt.Errorf("revision conflict: expected %s, current %s", expectedRevision, current.Revision)
	}
	graph, err := ApplyOps(current.Graph, operations, catalog)
	if err != nil {
		return Material{}, nil, err
	}
	problems := Validate(graph, catalog)
	if containsErrors(problems) {
		return Material{}, problems, errors.New("graph validation failed; no changes saved")
	}
	p, err := w.SourcePath(name)
	if err != nil {
		return Material{}, nil, err
	}
	if err := writeAtomically(p, graph); err != nil {
		return Material{}, problems, err
	}
	m, err := w.read(name)
	return m, problems, err
}

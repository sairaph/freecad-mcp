//go:build e2e

package e2e

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/sairaph/freecad-mcp/internal/guide"
)

var pythonBlock = regexp.MustCompile("(?s)```python\n(.*?)```")

// TestMeshGuideRecipes runs every recipe of mesh.md in execute_code_headless on a clipped ball
// (the case the guide describes: a sphere whose lower cap a flat at z 0 cut off) and checks what
// each prints. The file names in capitals in the recipes are replaced by the test's files.
func TestMeshGuideRecipes(t *testing.T) {
	text, err := fs.ReadFile(guide.FS(), "mesh.md")
	if err != nil {
		t.Fatal(err)
	}
	var recipes []string
	for _, m := range pythonBlock.FindAllStringSubmatch(string(text), -1) {
		recipes = append(recipes, m[1])
	}
	if len(recipes) != 3 {
		t.Fatalf("mesh.md has %d python recipes, want 3 (measure, patch, compare)", len(recipes))
	}

	cs := session(t)
	dir := filepath.ToSlash(t.TempDir())
	ball, moved, patched := dir+"/ball.stl", dir+"/moved.stl", dir+"/patched.stl"
	// radius 3.112731 and centre height 2.593943 are the values of the field report's ball.
	must(t, call(t, cs, "execute_code_headless", map[string]any{"timeout": 120, "code": fmt.Sprintf(`import FreeCAD, Part, Mesh, MeshPart
V = FreeCAD.Vector
sphere = Part.makeSphere(3.112731, V(0, 0, 2.593943))
clipped = sphere.common(Part.makeBox(20, 20, 20, V(-10, -10, 0)))
mesh = MeshPart.meshFromShape(Shape=clipped, LinearDeflection=0.01, AngularDeflection=0.1)
mesh.write(%q)
mesh.translate(50, -20, 3)
mesh.write(%q)
doc = FreeCAD.newDocument('Fixture')
f = doc.addObject('Mesh::Feature', 'M')
f.Mesh = mesh
f.Placement = FreeCAD.Placement(V(100, 0, 0), FreeCAD.Rotation())
print('feature points include the placement', abs(max(p.x for p in f.Mesh.Points) - f.Mesh.BoundBox.XMax) < 1e-9, f.Mesh.BoundBox.XMax > 150)
`, ball, moved)}), "feature points include the placement True True")

	run := func(i int, replace ...string) reply {
		code := strings.NewReplacer(replace...).Replace(recipes[i])
		return call(t, cs, "execute_code_headless", map[string]any{"code": code, "timeout": 100})
	}
	must(t, run(0, "DESIGN.stl", ball), "radius 3.112731", "missing cap height 0.518788", "circle centre 0.0 0.0 radius 1.720626")
	must(t, run(1, "DESIGN.stl", ball, "PATCHED.stl", patched),
		"closed True", "non-uniform facets 0", "non-manifold False", "facets outside the patch identical True")
	// The guide sends the written file to analyze_mesh.
	newDoc(t, cs, "E2EMeshGuide")
	must(t, call(t, cs, "import_file", map[string]any{"path": patched, "doc_name": "E2EMeshGuide", "include_screenshot": false}))
	must(t, call(t, cs, "analyze_mesh", map[string]any{"doc_name": "E2EMeshGuide", "obj_name": "patched"}),
		"is_solid: true", "has no defects")
	must(t, run(2, "A.stl", ball, "B.stl", moved), "offset [-50.", "max 0.0000")
}

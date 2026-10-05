//go:build e2e

package e2e

import (
	"archive/zip"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// threeMFObjects returns the name attribute of every object in a 3MF file, in file order.
func threeMFObjects(t *testing.T, path string) []string {
	t.Helper()
	z, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	f, err := z.Open("3D/3dmodel.model")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	var model struct {
		Objects []struct {
			Name string `xml:"name,attr"`
		} `xml:"resources>object"`
	}
	if err := xml.Unmarshal(data, &model); err != nil {
		t.Fatalf("%s: the model part is not valid XML: %v", path, err)
	}
	var names []string
	for _, o := range model.Objects {
		names = append(names, o.Name)
	}
	return names
}

// TestThreeMFKeepsObjectNames: every object of a 3MF export carries its label, escaped, for a
// single file and for one file per object.
func TestThreeMFKeepsObjectNames(t *testing.T) {
	cs := session(t)
	newDoc(t, cs, "E2EThreeMF")
	must(t, call(t, cs, "execute_code", map[string]any{"include_screenshot": false,
		"code": "doc = FreeCAD.getDocument('E2EThreeMF')\n" +
			"a = doc.addObject('Part::Box', 'BoxA')\na.Label = 'Left half'\n" +
			"b = doc.addObject('Part::Box', 'BoxB')\nb.Label = 'Rib & <\"x\">'\n" +
			"b.Placement.Base = FreeCAD.Vector(20, 0, 0)\ndoc.recompute()"}))

	one := tempPath(t, "both.3mf")
	must(t, call(t, cs, "export_document", map[string]any{"doc_name": "E2EThreeMF", "path": one}), "format: 3mf")
	if got, want := threeMFObjects(t, one), []string{"Left half", `Rib & <"x">`}; !reflect.DeepEqual(got, want) {
		t.Fatalf("object names = %q, want %q", got, want)
	}

	folder := filepath.Dir(tempPath(t, "each.3mf"))
	must(t, call(t, cs, "export_document", map[string]any{"doc_name": "E2EThreeMF", "path": folder, "per_object": true,
		"format": "3mf", "object_names": []string{"BoxA", "BoxB"}}), "one file each")
	entries, err := os.ReadDir(folder)
	if err != nil || len(entries) != 2 {
		t.Fatalf("folder %s holds %v, %v; want two files", folder, entries, err)
	}
	for _, e := range entries {
		want := strings.TrimSuffix(e.Name(), ".3mf")
		if strings.HasPrefix(want, "Rib") {
			want = `Rib & <"x">`
		}
		if got := threeMFObjects(t, filepath.Join(folder, e.Name())); !reflect.DeepEqual(got, []string{want}) {
			t.Fatalf("%s: object names = %q, want %q", e.Name(), got, []string{want})
		}
	}
}

// TestPrintabilityOfMeshesAndSolids: a mesh sits on the plate by its global bounds (its Placement
// counted once), a solid beside it is checked exactly, and a part nested in the mesh's box is
// reported as overlapping it.
func TestPrintabilityOfMeshesAndSolids(t *testing.T) {
	cs := session(t)
	newDoc(t, cs, "E2EPrintMesh")
	must(t, call(t, cs, "execute_code", map[string]any{"include_screenshot": false,
		"code": "import MeshPart, Part\ndoc = FreeCAD.getDocument('E2EPrintMesh')\n" +
			"m = MeshPart.meshFromShape(Shape=Part.makeBox(10, 20, 30), LinearDeflection=0.1, AngularDeflection=0.5)\n" +
			"f = doc.addObject('Mesh::Feature', 'Ball')\nf.Mesh = m\n" +
			"f.Placement = FreeCAD.Placement(FreeCAD.Vector(30, 10, 0), FreeCAD.Rotation())\n" +
			"b = doc.addObject('Part::Box', 'Block')\nb.Length, b.Width, b.Height = 10, 10, 10\n" +
			"b.Placement.Base = FreeCAD.Vector(60, 10, 0)\ndoc.recompute()"}))
	args := map[string]any{"doc_name": "E2EPrintMesh", "bed_x": 100, "bed_y": 100, "object_names": []string{"Ball", "Block"}}
	must(t, call(t, cs, "check_printability", args), "printable: true", "size 10 x 20 x 30 mm",
		"free margin to the plate edges 10 mm", "overlap is checked by its bounding box")

	must(t, call(t, cs, "execute_code", map[string]any{"include_screenshot": false,
		"code": "FreeCAD.getDocument('E2EPrintMesh').getObject('Block').Placement.Base = FreeCAD.Vector(32, 12, 5)"}))
	must(t, call(t, cs, "check_printability", args), "printable: false", "overlap_count: 1",
		"their bounding boxes share 800.00 mm^3")

	// The default selection takes the visible meshes and solids.
	must(t, call(t, cs, "check_printability", map[string]any{"doc_name": "E2EPrintMesh", "bed_x": 100, "bed_y": 100}),
		"object_count: 2")
}

// TestMeshLinkBoxMatchesFreeCADsOwnPlacement: an App::Link to a Mesh::Feature, inside a moved and
// rotated App::Part, with a Placement and a LinkPlacement of its own, gets the same global box
// as the identical link to a Part::Box, which FreeCAD places itself (Part.getShape), for both
// LinkTransform values.
func TestMeshLinkBoxMatchesFreeCADsOwnPlacement(t *testing.T) {
	cs := session(t)
	newDoc(t, cs, "E2ELink")
	r := call(t, cs, "execute_code", map[string]any{"include_screenshot": false, "code": `import MeshPart, Part
from rpc_server import tessellation
doc = FreeCAD.getDocument('E2ELink')
V, P, R = FreeCAD.Vector, FreeCAD.Placement, FreeCAD.Rotation
solid = Part.makeBox(10, 20, 30)
mesh = doc.addObject('Mesh::Feature', 'M')
mesh.Mesh = MeshPart.meshFromShape(Shape=solid, LinearDeflection=0.1, AngularDeflection=0.5)
mesh.Placement = P(V(100, 0, 0), R(V(0, 0, 1), 30))
box = doc.addObject('Part::Box', 'B')
box.Length, box.Width, box.Height = 10, 20, 30
box.Placement = P(V(100, 0, 0), R(V(0, 0, 1), 30))
part = doc.addObject('App::Part', 'Asm')
part.Placement = P(V(0, 500, 20), R(V(0, 1, 0), 90))
links = {}
for name, target in (('LM', mesh), ('LB', box)):
    link = doc.addObject('App::Link', name)
    link.LinkedObject = target
    link.Placement = P(V(0, 0, 50), R(V(1, 0, 0), 45))
    link.LinkPlacement = P(V(7, 0, 0), R(V(0, 0, 1), 20))
    part.addObject(link)
    links[name] = link
for transform in (False, True):
    for link in links.values():
        link.LinkTransform = transform
    doc.recompute()
    wanted = Part.getShape(links['LB'])
    wanted.transformShape(part.Placement.toMatrix())
    w = wanted.optimalBoundingBox(False, False)
    boxes = tessellation.mesh_boxes(links['LM'])
    got = boxes[0]
    diff = max(abs(a - b) for a, b in zip((got.XMin, got.YMin, got.ZMin, got.XMax, got.YMax, got.ZMax),
                                          (w.XMin, w.YMin, w.ZMin, w.XMax, w.YMax, w.ZMax)))
    print('LinkTransform', transform, 'boxes', len(boxes), 'match', diff < 0.001, 'difference', round(diff, 5), 'expected', [round(v, 2) for v in (w.XMin, w.YMin, w.ZMin, w.XMax, w.YMax, w.ZMax)])
`})
	// The mesh's corners lie on the solid's, so the two boxes agree to float32 rounding.
	must(t, r, "LinkTransform False boxes 1 match True", "LinkTransform True boxes 1 match True")
}

// TestMeshLinkBoxByHand: the same link with plain translations, so the expected box is worked
// out by hand. A 10 x 20 x 30 mesh with Placement x 100; a link with Placement z 50 in an
// App::Part moved to y 500. LinkTransform false shows the mesh at the link's Placement alone
// (x 0 to 10); true adds the mesh's own x 100.
func TestMeshLinkBoxByHand(t *testing.T) {
	cs := session(t)
	newDoc(t, cs, "E2ELinkHand")
	r := call(t, cs, "execute_code", map[string]any{"include_screenshot": false, "code": `import MeshPart, Part
from rpc_server import tessellation
doc = FreeCAD.getDocument('E2ELinkHand')
V, P = FreeCAD.Vector, FreeCAD.Placement
mesh = doc.addObject('Mesh::Feature', 'M')
mesh.Mesh = MeshPart.meshFromShape(Shape=Part.makeBox(10, 20, 30), LinearDeflection=0.1, AngularDeflection=0.5)
mesh.Placement = P(V(100, 0, 0), FreeCAD.Rotation())
part = doc.addObject('App::Part', 'Asm')
part.Placement = P(V(0, 500, 0), FreeCAD.Rotation())
link = doc.addObject('App::Link', 'L')
link.LinkedObject = mesh
link.Placement = P(V(0, 0, 50), FreeCAD.Rotation())
part.addObject(link)
for transform in (False, True):
    link.LinkTransform = transform
    doc.recompute()
    b = tessellation.mesh_boxes(link)[0]
    print('LinkTransform', transform, [round(v, 3) for v in (b.XMin, b.YMin, b.ZMin, b.XMax, b.YMax, b.ZMax)])
`})
	must(t, r, "LinkTransform False [0.0, 500.0, 50.0, 10.0, 520.0, 80.0]",
		"LinkTransform True [100.0, 500.0, 50.0, 110.0, 520.0, 80.0]")
}

// TestTightSizeOfASweptThread: a thread swept along a helix is measured by its exact box, not
// the loose Shape.BoundBox.
func TestTightSizeOfASweptThread(t *testing.T) {
	cs := session(t)
	newDoc(t, cs, "E2EThread")
	r := call(t, cs, "execute_code", map[string]any{"include_screenshot": false,
		"code": "import Part\ndoc = FreeCAD.getDocument('E2EThread')\nV = FreeCAD.Vector\n" +
			"helix = Part.makeHelix(2.0, 12.0, 20.0)\np0 = V(20, 0, 0)\n" +
			"prof = Part.makePolygon([p0 + V(-1.2, 0, -0.8), p0 + V(0, 0, -0.1), p0 + V(0, 0, 0.1), p0 + V(-1.2, 0, 0.8), p0 + V(-1.2, 0, -0.8)])\n" +
			"s = Part.Wire(helix).makePipeShell([prof], True, True)\n" +
			// Read before the shape is stored or drawn: once it has a triangulation, BoundBox uses it.
			"print('loose', round(s.BoundBox.XLength, 2), 'tight', round(s.optimalBoundingBox(False, False).XLength, 2))\n" +
			"doc.addObject('Part::Feature', 'Thread').Shape = s"})
	must(t, r, "tight 40.0")
	if strings.Contains(r.text, "loose 40.0") {
		t.Fatalf("the swept thread's plain box is not loose, so the test proves nothing:\n%s", r.text)
	}
	must(t, call(t, cs, "check_printability", map[string]any{"doc_name": "E2EThread", "bed_x": 100, "bed_y": 100}),
		"size 40 x 40 x 13.6 mm")
}

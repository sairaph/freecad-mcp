//go:build e2e

package e2e

import (
	"math"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The reference case: an aluminium cantilever 60 x 30 x 4 mm, fixed at Face1 (x = 0), 200 N spread on the top face
// (Face6) pushing down. Hand result: 75 MPa at the root, 0.48 mm at the tip (4 mm thick); 133 MPa for 3 mm.
func cantilever(t *testing.T, cs *mcp.ClientSession, doc string) {
	t.Helper()
	newDoc(t, cs, doc)
	create := func(name, typ, analysis string, props map[string]any) reply {
		args := map[string]any{"doc_name": doc, "obj_name": name, "obj_type": typ, "obj_properties": props, "include_screenshot": false}
		if analysis != "" {
			args["analysis_name"] = analysis
		}
		return call(t, cs, "create_object", args)
	}
	must(t, create("Beam", "Part::Box", "", map[string]any{"Length": 60, "Width": 30, "Height": 4}))
	must(t, create("Analysis", "Fem::AnalysisPython", "", nil))
	must(t, create("Alu", "Fem::MaterialCommon", "Analysis", map[string]any{"Material": map[string]any{"Name": "Aluminium", "YoungsModulus": "70 GPa", "PoissonRatio": 0.33, "Density": "2700 kg/m^3"}}))
	must(t, create("Mesh", "Fem::FemMeshGmsh", "Analysis", map[string]any{"Shape": "Beam", "CharacteristicLengthMax": 3, "CharacteristicLengthMin": 1}),
		"Mesh Mesh: 2nd order tetrahedra,")
	must(t, create("Fixed", "Fem::ConstraintFixed", "Analysis", map[string]any{"References": []any{map[string]any{"object_name": "Beam", "faces": []any{"Face1"}}}}))
	must(t, create("Load", "Fem::ConstraintForce", "Analysis", map[string]any{"References": []any{map[string]any{"object_name": "Beam", "faces": []any{"Face6"}}}, "Force": "200 N", "Reversed": true}))
}

func runAnalysis(t *testing.T, cs *mcp.ClientSession, doc string) reply {
	t.Helper()
	return call(t, cs, "run_fem_analysis", map[string]any{"doc_name": doc, "analysis_name": "Analysis", "include_screenshot": false})
}

func within(t *testing.T, what string, got, want, fraction float64) {
	t.Helper()
	if math.Abs(got-want) > fraction*want {
		t.Fatalf("%s = %g, want %g within %.0f %%", what, got, want, fraction*100)
	}
}

// TestFemMeshIsSecondOrderAndFollowsItsSolid: the default mesh matches the hand result (linear tetrahedra came out three
// times too stiff), a thinner beam is meshed again by the run, a changed parameter meshes again, a first order mesh warns.
func TestFemMeshIsSecondOrderAndFollowsItsSolid(t *testing.T) {
	cs := session(t)
	const doc = "E2EFemCantilever"
	cantilever(t, cs, doc)

	r := runAnalysis(t, cs, doc)
	must(t, r, "2nd order)")
	if strings.Contains(r.text, "Warning:") || strings.Contains(r.text, "Remeshed") {
		t.Fatalf("a fresh second order mesh was flagged:\n%s", r.text)
	}
	within(t, "max von Mises", frontFloat(t, r, "max_von_mises_mpa"), 75, 0.10)
	within(t, "max displacement", frontFloat(t, r, "max_displacement_mm"), 0.48, 0.10)

	// Height 4 -> 3: recompute names the mesh, the run meshes again and finds about the 3 mm hand result.
	must(t, call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Beam", "obj_properties": map[string]any{"Height": 3}, "include_screenshot": false}))
	must(t, call(t, cs, "recompute_document", map[string]any{"doc_name": doc, "include_screenshot": false}),
		"Mesh: Beam changed since it was meshed; run_fem_analysis remeshes it.", "stale_meshes: 1")
	r = runAnalysis(t, cs, doc)
	must(t, r, "Remeshed Mesh: Beam changed since it was meshed (")
	within(t, "max von Mises at 3 mm", frontFloat(t, r, "max_von_mises_mpa"), 133, 0.15)
	must(t, call(t, cs, "recompute_document", map[string]any{"doc_name": doc, "include_screenshot": false}), "stale_meshes: 0")

	// A meshing parameter meshes again and the reply says the new node count.
	u := call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Mesh", "obj_properties": map[string]any{"CharacteristicLengthMax": 6}, "include_screenshot": false})
	must(t, u, "Mesh Mesh: 2nd order tetrahedra,", "meshed again (it had ")

	// First order meshes the old way and the run warns.
	u = call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Mesh", "obj_properties": map[string]any{"ElementOrder": "1st"}, "include_screenshot": false})
	must(t, u, "1st order tetrahedra", "bending results come out too stiff")
	r = runAnalysis(t, cs, doc)
	must(t, r, "Warning: Mesh uses first order tetrahedra: bending results come out too stiff. Set ElementOrder to 2nd with update_object.")
}

// TestFemRefusesAMissingFaceAndFlagsAnAllZeroResult.
func TestFemRefusesAMissingFaceAndFlagsAnAllZeroResult(t *testing.T) {
	cs := session(t)
	const doc = "E2EFemZero"
	cantilever(t, cs, doc)

	// A load on a face the solid does not have.
	v0416Code(t, cs, doc, "d.getObject('Load').References = [(d.getObject('Beam'), ['Face9'])]\nd.recompute()\nprint('set')", "set")
	r := runAnalysis(t, cs, doc)
	fails(t, r, "Constraint 'Load' names Face9 of 'Beam', which no longer exists", "list_subelements")
	v0416Code(t, cs, doc, "d.getObject('Load').References = [(d.getObject('Beam'), ['Face6'])]\nd.recompute()\nprint('set')", "set")

	// A load that moves nothing: the run succeeded in CalculiX, the reply must not call it a success.
	must(t, call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Load", "obj_properties": map[string]any{"Force": "0 N"}, "include_screenshot": false}))
	r = runAnalysis(t, cs, doc)
	fails(t, r, "The result is all zero: no load reached the mesh. The mesh may not match the solid, or a load names a face that no longer exists.",
		"result_object", "max_displacement_mm")
}

// TestForceDirectionByEdge: Direction takes the forms References takes for one sub-element, and the reply states the direction.
func TestForceDirectionByEdge(t *testing.T) {
	cs := session(t)
	const doc = "E2EFemDirection"
	cantilever(t, cs, doc)
	forms := map[string]any{
		"object_name and edge": map[string]any{"object_name": "Beam", "edge": "Edge5"},
		"list with a name":     []any{"Beam", "Edge5"},
		"list with a list":     []any{"Beam", []any{"Edge5"}},
	}
	for name, form := range forms {
		r := call(t, cs, "create_object", map[string]any{"doc_name": doc, "obj_name": "Axial", "obj_type": "Fem::ConstraintForce", "analysis_name": "Analysis",
			"obj_properties": map[string]any{"References": []any{map[string]any{"object_name": "Beam", "faces": []any{"Face2"}}}, "Force": "50 N", "Direction": form}, "include_screenshot": false})
		must(t, r, "the direction of Beam.Edge5")
		t.Log("form accepted: " + name)
		must(t, call(t, cs, "delete_object", map[string]any{"doc_name": doc, "obj_name": "Axial", "include_screenshot": false}))
	}
}

// TestTourStopsAreChecked: a stop on a hidden object is refused like a static focus, naming the stop; a stop the same call shows passes.
func TestTourStopsAreChecked(t *testing.T) {
	cs := session(t)
	const doc = "E2EFemTour"
	newDoc(t, cs, doc)
	must(t, call(t, cs, "create_object", map[string]any{"doc_name": doc, "obj_name": "Beam", "obj_type": "Part::Box", "include_screenshot": false}))
	must(t, call(t, cs, "create_object", map[string]any{"doc_name": doc, "obj_name": "Other", "obj_type": "Part::Box", "include_screenshot": false,
		"obj_properties": map[string]any{"Placement": map[string]any{"Base": map[string]any{"x": 30, "y": 0, "z": 0}}}}))
	must(t, call(t, cs, "set_view", map[string]any{"doc_name": doc, "hide": []string{"Beam"}, "include_screenshot": false}))

	r := call(t, cs, "set_view", map[string]any{"doc_name": doc, "mode": "tour", "include_screenshot": false,
		"stops": []any{map[string]any{"focus": []string{"Other"}}, map[string]any{"focus": []string{"Beam"}}}})
	fails(t, r, "Stop 2: none of its focus objects has a shape on screen")
	fails(t, call(t, cs, "set_view", map[string]any{"doc_name": doc, "mode": "tour", "include_screenshot": false, "stops": []any{map[string]any{"focus": []string{"Nowhere"}}}}), "Nowhere")

	must(t, call(t, cs, "set_view", map[string]any{"doc_name": doc, "mode": "tour", "show": []string{"Beam"}, "include_screenshot": false,
		"stops": []any{map[string]any{"focus": []string{"Beam"}, "dwell_seconds": 0.5}}}), "tour")
	must(t, call(t, cs, "set_view", map[string]any{"doc_name": doc, "mode": "static", "include_screenshot": false}))
}

// TestFemMeshSeesAMovedFeature: a hole moved inside the same block keeps the box, volume, area and face count; the
// mesh is still found stale and meshed again, a recompute with nothing changed leaves it alone, and so does the run.
func TestFemMeshSeesAMovedFeature(t *testing.T) {
	cs := session(t)
	const doc = "E2EFemMovedHole"
	newDoc(t, cs, doc)
	create := func(name, typ, analysis string, props map[string]any) reply {
		args := map[string]any{"doc_name": doc, "obj_name": name, "obj_type": typ, "obj_properties": props, "include_screenshot": false}
		if analysis != "" {
			args["analysis_name"] = analysis
		}
		return call(t, cs, "create_object", args)
	}
	at := func(x float64) map[string]any {
		return map[string]any{"Base": map[string]any{"x": x, "y": 15, "z": 0}}
	}
	must(t, create("Block", "Part::Box", "", map[string]any{"Length": 60, "Width": 30, "Height": 10}))
	must(t, create("Hole", "Part::Cylinder", "", map[string]any{"Radius": 4, "Height": 10, "Placement": at(10)}))
	must(t, create("Plate", "Part::Cut", "", map[string]any{"Base": "Block", "Tool": "Hole"}))
	must(t, create("Analysis", "Fem::AnalysisPython", "", nil))
	must(t, create("Mesh", "Fem::FemMeshGmsh", "Analysis", map[string]any{"Shape": "Plate", "CharacteristicLengthMax": 5, "CharacteristicLengthMin": 2}))

	stale := func() reply {
		return call(t, cs, "recompute_document", map[string]any{"doc_name": doc, "include_screenshot": false})
	}
	must(t, stale(), "stale_meshes: 0")
	must(t, stale(), "stale_meshes: 0") // a recompute with nothing changed does not find a change

	must(t, call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Hole", "obj_properties": map[string]any{"Placement": at(40)}, "include_screenshot": false}))
	must(t, stale(), "Mesh: Plate changed since it was meshed; run_fem_analysis remeshes it.", "stale_meshes: 1")
	// Updating the mesh itself meshes it again and leaves it up to date.
	must(t, call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Mesh", "obj_properties": map[string]any{"CharacteristicLengthMax": 5}, "include_screenshot": false}), "meshed again")
	v0416Code(t, cs, doc, "m = d.getObject('Mesh')\nprint('state', m.State)", "state ['Up-to-date']")
	must(t, stale(), "stale_meshes: 0")

	// A pocket turned in place (a rectangular tool, 20 x 6 turned to 6 x 20) changes no box, volume or area.
	must(t, create("Pocket", "Part::Box", "", map[string]any{"Length": 20, "Width": 6, "Height": 10, "Placement": map[string]any{"Base": map[string]any{"x": 20, "y": 5, "z": 0}}}))
	must(t, create("Plate2", "Part::Cut", "", map[string]any{"Base": "Plate", "Tool": "Pocket"}))
	must(t, call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Mesh", "obj_properties": map[string]any{"Shape": "Plate2"}, "include_screenshot": false}), "meshed again")
	must(t, stale(), "stale_meshes: 0")
	must(t, call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Pocket", "obj_properties": map[string]any{"Length": 6, "Width": 20,
		"Placement": map[string]any{"Base": map[string]any{"x": 27, "y": 5, "z": 0}}}, "include_screenshot": false}))
	must(t, stale(), "Mesh: Plate2 changed since it was meshed", "stale_meshes: 1")
}

// TestUndoOfAnUpdateThatMeshedAgainIsFoundAndMeshedAgain: undo restores the mesh object's parameter and its fingerprint
// record but not its mesh, so the two disagree; recompute names it and the run meshes it again.
func TestUndoOfAnUpdateThatMeshedAgainIsFoundAndMeshedAgain(t *testing.T) {
	cs := session(t)
	const doc = "E2EFemUndo"
	cantilever(t, cs, doc)
	snapshot := "m = d.getObject('Mesh')\nimport json\nf = json.loads(m.McpMeshedShape)\n" +
		"print('MESH', m.FemMesh.NodeCount, m.CharacteristicLengthMax, round(f['volume'], 3), f['nodes'], m.State)\n"
	read := func() string {
		r := call(t, cs, "execute_code", map[string]any{"include_screenshot": false, "code": "d = FreeCAD.getDocument('" + doc + "')\n" + snapshot})
		must(t, r, "MESH")
		for _, l := range strings.Split(r.text, "\n") {
			if i := strings.Index(l, "MESH "); i >= 0 {
				return l[i:]
			}
		}
		return ""
	}
	before := read()
	must(t, call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Mesh", "obj_properties": map[string]any{"CharacteristicLengthMax": 8}, "include_screenshot": false}), "meshed again")
	if changed := read(); changed == before || !strings.HasSuffix(changed, "['Up-to-date']") {
		t.Fatalf("the update did not change the mesh, or left it touched: %s", changed)
	}
	must(t, call(t, cs, "undo", map[string]any{"doc_name": doc, "include_screenshot": false}), "Undone 1 transaction(s)")
	// The record (3746 nodes) and the parameter are back; the mesh object's mesh is not.
	afterUndo := read()
	if !strings.HasPrefix(afterUndo, "MESH 1003 3.0 mm") || !strings.Contains(afterUndo, " 3746 ") {
		t.Fatalf("after the undo the record should be restored and the mesh not:\nbefore %s\nafter  %s", before, afterUndo)
	}
	must(t, call(t, cs, "recompute_document", map[string]any{"doc_name": doc, "include_screenshot": false}),
		"Mesh: its mesh is not the one last made from Beam", "stale_meshes: 1")
	// The run meshes it again from the restored parameters, and record and mesh agree again.
	must(t, runAnalysis(t, cs, doc), "Remeshed Mesh: it was not the mesh last made from Beam (1003 -> 3746 nodes).")
	if after := read(); after != before {
		t.Fatalf("the run did not bring the mesh back:\nbefore %s\nafter  %s", before, after)
	}
	must(t, call(t, cs, "recompute_document", map[string]any{"doc_name": doc, "include_screenshot": false}), "stale_meshes: 0")
}

// TestTourDefaultStopsFollowThisCallsChanges: stops made from the drawn objects skip what the call hides or isolates away;
// a stop the caller passes on a hidden object is still refused.
func TestTourDefaultStopsFollowThisCallsChanges(t *testing.T) {
	cs := session(t)
	const doc = "E2ETourDefault"
	newDoc(t, cs, doc)
	create := func(name string, x float64) {
		must(t, call(t, cs, "create_object", map[string]any{"doc_name": doc, "obj_name": name, "obj_type": "Part::Box", "include_screenshot": false,
			"obj_properties": map[string]any{"Placement": map[string]any{"Base": map[string]any{"x": x, "y": 0, "z": 0}}}}))
	}
	create("A", 0)
	create("B", 30)
	stop := func() {
		must(t, call(t, cs, "set_view", map[string]any{"doc_name": doc, "mode": "static", "include_screenshot": false}))
	}

	must(t, call(t, cs, "set_view", map[string]any{"doc_name": doc, "mode": "tour", "hide": []string{"A"}, "include_screenshot": false}), "tour")
	stop()
	must(t, call(t, cs, "set_view", map[string]any{"doc_name": doc, "show": []string{"A"}, "include_screenshot": false}))
	must(t, call(t, cs, "set_view", map[string]any{"doc_name": doc, "mode": "tour", "isolate": []string{"B"}, "include_screenshot": false}), "tour")
	stop()
	must(t, call(t, cs, "set_view", map[string]any{"doc_name": doc, "show": []string{"A"}, "hide": []string{"B"}, "include_screenshot": false}))
	fails(t, call(t, cs, "set_view", map[string]any{"doc_name": doc, "mode": "tour", "include_screenshot": false, "stops": []any{map[string]any{"focus": []string{"B"}}}}),
		"Stop 1: none of its focus objects has a shape on screen")
	// Every object hidden away: no stop is left.
	fails(t, call(t, cs, "set_view", map[string]any{"doc_name": doc, "mode": "tour", "hide": []string{"A"}, "include_screenshot": false}), "The tour has no stops")
}

// TestFemMeshRecordSurvivesSaveAndReopen: after save and reopen the mesh holds the node count its record names and the
// shape matches its record, so nothing is meshed again; the size of the record is logged for a 700-face compound.
func TestFemMeshRecordSurvivesSaveAndReopen(t *testing.T) {
	cs := session(t)
	path := tempPath(t, "E2EFemReopen.FCStd")
	cantilever(t, cs, "E2EFemReopen")
	read := "m = d.getObject('Mesh')\nimport json\nf = json.loads(m.McpMeshedShape)\nprint('RECORD', m.FemMesh.NodeCount, f['nodes'], len(m.McpMeshedShape), m.State)\n"
	before := call(t, cs, "execute_code", map[string]any{"include_screenshot": false, "code": "d = FreeCAD.getDocument('E2EFemReopen')\n" + read})
	must(t, before, "RECORD 3746 3746")
	must(t, call(t, cs, "save_document_as", map[string]any{"doc_name": "E2EFemReopen", "path": path}))
	must(t, call(t, cs, "close_document", map[string]any{"doc_name": "E2EFemReopen"}))
	must(t, call(t, cs, "open_document", map[string]any{"path": path}))
	closeOnCleanup(t, cs, "E2EFemReopen")
	after := call(t, cs, "execute_code", map[string]any{"include_screenshot": false, "code": "d = FreeCAD.getDocument('E2EFemReopen')\n" + read})
	must(t, after, "RECORD 3746 3746")
	must(t, call(t, cs, "recompute_document", map[string]any{"doc_name": "E2EFemReopen", "include_screenshot": false}), "stale_meshes: 0")
	// The record is the tool's own bookkeeping: get_object and the full object list leave it out.
	for _, r := range []reply{
		call(t, cs, "get_object", map[string]any{"doc_name": "E2EFemReopen", "obj_name": "Mesh"}),
		call(t, cs, "list_objects", map[string]any{"doc_name": "E2EFemReopen"}),
	} {
		must(t, r, "CharacteristicLengthMax")
		if strings.Contains(r.text, "McpMeshedShape") {
			t.Fatalf("the mesh record is shown: %.600s", r.text)
		}
	}
	r := runAnalysis(t, cs, "E2EFemReopen")
	if strings.Contains(r.text, "Remeshed") {
		t.Fatalf("a reopened document was meshed again:\n%s", r.text)
	}
	within(t, "max von Mises after reopen", frontFloat(t, r, "max_von_mises_mpa"), 75, 0.10)
}

// TestFemRecordSizeOnALargeCompound logs what the stored record costs on 700 faces and how long its comparison takes.
func TestFemRecordSizeOnALargeCompound(t *testing.T) {
	cs := session(t)
	const doc = "E2EFemRecordSize"
	newDoc(t, cs, doc)
	r := call(t, cs, "execute_code", map[string]any{"include_screenshot": false, "code": "import Part, time, json\nfrom rpc_server import fem_mesh\nd = FreeCAD.getDocument('" + doc + "')\nV = FreeCAD.Vector\n" +
		"parts = []\nfor i in range(100):\n    b = Part.makeBox(8, 8, 8).cut(Part.makeCylinder(2, 8, V(4, 4, 0)))\n    b.translate(V((i % 10) * 12, (i // 10) * 12, 0))\n    parts.append(b)\n" +
		"many = d.addObject('Part::Feature', 'Many')\nmany.Shape = Part.makeCompound(parts)\nd.recompute()\n" +
		"t0 = time.time()\nrecord = fem_mesh.fingerprint(many)\nt1 = time.time()\ntext = json.dumps(record, separators=(',', ':'))\n" +
		"shape = many.Shape\nt2 = time.time()\ndiffers = fem_mesh._record_differs(json.loads(text), shape, fem_mesh._box_of(shape))\nt3 = time.time()\n" +
		"print('RECORDSIZE faces', len(shape.Faces), 'bytes', len(text), 'build', round(t1 - t0, 3), 'compare', round(t3 - t2, 3), 'differs', differs)"})
	must(t, r, "RECORDSIZE faces 700", "differs False")
}

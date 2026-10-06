//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// TestSettingVisibilityIsNotAHiddenInput: update_object Visibility false on a box says nothing about hidden inputs.
func TestSettingVisibilityIsNotAHiddenInput(t *testing.T) {
	cs := session(t)
	const doc = "E2EVisibility"
	newDoc(t, cs, doc)
	must(t, pdCreate(t, cs, doc, "SmallA", "Part::Box", nil, nil))
	r := call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "SmallA", "include_screenshot": false,
		"obj_properties": map[string]any{"Visibility": false}})
	must(t, r)
	if strings.Contains(r.text, "Hidden:") {
		t.Fatalf("the Visibility the caller set is reported as a hidden input:\n%s", r.text)
	}
	// An object hidden as a consequence of the call is still named.
	must(t, pdCreate(t, cs, doc, "Cutter", "Part::Cylinder", nil, nil))
	must(t, pdCreate(t, cs, doc, "Cut", "Part::Cut", map[string]any{"Base": "SmallA", "Tool": "Cutter"}, nil), "Hidden: Cutter (inputs of Cut).")
}

// TestAFemMaterialIsEchoedAsStored: create_object and update_object of a material say what FreeCAD stored.
func TestAFemMaterialIsEchoedAsStored(t *testing.T) {
	cs := session(t)
	const doc = "E2EMaterialEcho"
	newDoc(t, cs, doc)
	must(t, pdCreate(t, cs, doc, "Analysis", "Fem::AnalysisPython", nil, nil))
	material := map[string]any{"Name": "Alu", "YoungsModulus": "70 GPa", "PoissonRatio": 0.33, "Density": "2700 kg/m^3"}
	r := pdCreate(t, cs, doc, "Alu", "Fem::MaterialCommon", map[string]any{"Material": material}, map[string]any{"analysis_name": "Analysis"})
	must(t, r, "Material Alu: YoungsModulus 70 GPa, PoissonRatio 0.33, Density 2700 kg/m^3")
	material["YoungsModulus"] = "71 GPa"
	must(t, call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Alu", "include_screenshot": false,
		"obj_properties": map[string]any{"Material": material}}), "Material Alu: YoungsModulus 71 GPa")
}

// TestOneSheetChangeListsOnlyShapesThatReallyChanged: a cell that rebuilds three objects lists the one that grew and the one whose hole was
// enlarged inside the same box, and leaves out the one rebuilt to identical geometry.
func TestOneSheetChangeListsOnlyShapesThatReallyChanged(t *testing.T) {
	cs := session(t)
	const doc = "E2EShapesNoise"
	newDoc(t, cs, doc)
	must(t, pdCreate(t, cs, doc, "Params", "Spreadsheet::Sheet", nil, nil))
	must(t, call(t, cs, "update_spreadsheet_cells", map[string]any{"doc_name": doc, "sheet_name": "Params", "cells": []any{
		map[string]any{"cell": "A1", "content": "20", "alias": "width"}, map[string]any{"cell": "A2", "content": "5", "alias": "unused"},
		map[string]any{"cell": "A3", "content": "2", "alias": "hole"}}}))
	must(t, pdCreate(t, cs, doc, "Wide", "Part::Box", map[string]any{"Length": "=Params.width", "Width": 10, "Height": 3}, nil))
	// Rebuilt by the cell to the same geometry: a Placement expression that cancels out.
	must(t, pdCreate(t, cs, doc, "Still", "Part::Box", map[string]any{"Length": 10, "Width": 10, "Height": 3,
		"Placement.Base.x": "=Params.unused - Params.unused"}, nil))
	// A block with a hole bound to a cell: the hole grows inside the same box.
	must(t, pdCreate(t, cs, doc, "Block", "Part::Box", map[string]any{"Length": 30, "Width": 30, "Height": 10, "Placement": vec(100, 0, 0)}, nil))
	must(t, pdCreate(t, cs, doc, "Pin", "Part::Cylinder", map[string]any{"Radius": "=Params.hole", "Height": 12, "Placement": vec(115, 15, -1)}, nil))
	must(t, pdCreate(t, cs, doc, "Drilled", "Part::Cut", map[string]any{"Base": "Block", "Tool": "Pin"}, nil))
	r := call(t, cs, "update_spreadsheet_cells", map[string]any{"doc_name": doc, "sheet_name": "Params", "cells": []any{
		map[string]any{"cell": "A1", "content": "30"}, map[string]any{"cell": "A2", "content": "6"}, map[string]any{"cell": "A3", "content": "4"}}})
	must(t, r, "Wide: 1 solid, 30 x 10 x 3 mm", "Drilled: 1 solid, 30 x 30 x 10 mm")
	if strings.Contains(r.text, "Still:") {
		t.Fatalf("an object rebuilt to identical geometry is listed:\n%s", r.text)
	}
}

// TestABodyRebuiltToTheSameShapeIsNotListedAfterItsPadWasCreated: the reply that created the Pad (the Body's Tip) showed the same shape for the
// Body, so a later rebuild to identical geometry (the sketch touched with the geometry it has) says nothing, although the reply never listed the Body itself.
func TestABodyRebuiltToTheSameShapeIsNotListedAfterItsPadWasCreated(t *testing.T) {
	cs := session(t)
	const doc = "E2ETipTwin"
	newDoc(t, cs, doc)
	must(t, pdCreate(t, cs, doc, "Params", "Spreadsheet::Sheet", nil, nil))
	cell := func(content string) reply {
		return call(t, cs, "update_spreadsheet_cells", map[string]any{"doc_name": doc, "sheet_name": "Params", "cells": []any{
			map[string]any{"cell": "A1", "content": content, "alias": "thick"}}})
	}
	must(t, cell("4"))
	must(t, pdCreate(t, cs, doc, "Body", "PartDesign::Body", nil, nil))
	must(t, pdCreate(t, cs, doc, "Sk", "Sketcher::SketchObject", map[string]any{"AttachmentSupport": "XY_Plane",
		"Geometry": []any{map[string]any{"rectangle": map[string]any{"corner": []any{0, 0}, "size": []any{10, 10}}}}}, nil))
	must(t, pdCreate(t, cs, doc, "Pad", "PartDesign::Pad", map[string]any{"Profile": "Sk", "Length": "=Params.thick"}, nil), "Shape: 1 solid, 10 x 10 x 4 mm")
	// Rebuild the Body for real: touch its sketch with the geometry it already has (the Pad and the Body are recomputed).
	sketch := map[string]any{"Geometry": []any{map[string]any{"rectangle": map[string]any{"corner": []any{0, 0}, "size": []any{10, 10}}}}}
	r := call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Sk", "include_screenshot": false, "obj_properties": sketch})
	must(t, r)
	if strings.Contains(r.text, "Shapes now") {
		t.Fatalf("a Body rebuilt to the same shape is listed:\n%s", r.text)
	}
	must(t, cell("6"), "Body: 1 solid, 10 x 10 x 6 mm")
}

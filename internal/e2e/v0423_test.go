//go:build e2e

package e2e

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestAFuseThatLosesAnInputWarns: the core and the thread of a bolt whose profile only touches the core (M8 x 1.25, 13 mm) fuse to the
// thread alone, 119.6 mm^3 against the 472.9 of the core, and FreeCAD calls the result valid. The reply of create_object, update_object
// and recompute_document says so; ten ordinary booleans say nothing.
func TestAFuseThatLosesAnInputWarns(t *testing.T) {
	cs := session(t)
	const doc = "E2ELostInput"
	newDoc(t, cs, doc)
	build := "import math\npitch, major, depth, length, inset = 1.25, 8.0, 0.75, 13.0, 0.0\nr = major / 2\nroot = r - depth\n" +
		"helix = Part.makeHelix(pitch, length, root)\n" +
		"profile = Part.makePolygon([FreeCAD.Vector(root - inset, 0, -0.45 * pitch), FreeCAD.Vector(r, 0, -0.1 * pitch), FreeCAD.Vector(r, 0, 0.1 * pitch), FreeCAD.Vector(root - inset, 0, 0.45 * pitch), FreeCAD.Vector(root - inset, 0, -0.45 * pitch)])\n" +
		"d.addObject('Part::Feature', 'Thread').Shape = Part.Wire(helix).makePipeShell([profile], True, True)\n" +
		"d.addObject('Part::Feature', 'Core').Shape = Part.makeCylinder(root, length + pitch, FreeCAD.Vector(0, 0, -pitch / 2))\nd.recompute()\n" +
		"print('BUILT', round(d.Core.Shape.Volume, 1), round(d.Thread.Shape.Volume, 1))\n"
	must(t, call(t, cs, "execute_code", map[string]any{"include_screenshot": false, "timeout": 120,
		"code": "import FreeCAD, Part\nd = FreeCAD.getDocument('" + doc + "')\n" + build}), "BUILT 472.9 119.5")

	const lost = "Warning: The result is smaller than its largest input: part of an input was dropped. Check the inputs touch properly, or fuse them one at a time."
	must(t, pdCreate(t, cs, doc, "Bolt", "Part::Fuse", map[string]any{"Base": "Core", "Tool": "Thread"}, nil), "volume 119.6", lost)
	must(t, pdCreate(t, cs, doc, "BoltMulti", "Part::MultiFuse", map[string]any{"Shapes": []any{"Core", "Thread"}}, nil), lost)
	r := call(t, cs, "recompute_document", map[string]any{"doc_name": doc, "include_screenshot": false})
	must(t, r, "empty_count: 2", "Bolt (smaller than its largest input: part of an input was dropped)")
	must(t, call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Bolt", "include_screenshot": false,
		"obj_properties": map[string]any{"Refine": true}}), lost)

	// Ordinary booleans: boxes, cylinders and spheres, each pair fused, intersected and cut.
	const plain = "E2EOrdinaryBooleans"
	newDoc(t, cs, plain)
	must(t, pdCreate(t, cs, plain, "Box", "Part::Box", map[string]any{"Length": 30, "Width": 20, "Height": 10}, nil))
	must(t, pdCreate(t, cs, plain, "Cyl", "Part::Cylinder", map[string]any{"Radius": 6, "Height": 40, "Placement": vec(15, 10, -5)}, nil))
	must(t, pdCreate(t, cs, plain, "Sph", "Part::Sphere", map[string]any{"Radius": 9, "Placement": vec(10, 10, 10)}, nil))
	must(t, pdCreate(t, cs, plain, "Cone", "Part::Cone", map[string]any{"Radius1": 8, "Radius2": 2, "Height": 14, "Placement": vec(25, 10, 0)}, nil))
	n := 0
	for _, pair := range [][2]string{{"Box", "Cyl"}, {"Box", "Sph"}, {"Cyl", "Sph"}, {"Box", "Cone"}} {
		for _, typ := range []string{"Part::Fuse", "Part::Common", "Part::Cut"} {
			n++
			if n > 10 {
				break
			}
			r := pdCreate(t, cs, plain, "R"+string(rune('A'+n)), typ, map[string]any{"Base": pair[0], "Tool": pair[1]}, nil)
			must(t, r, "Shape: 1 solid")
			if strings.Contains(r.text, "Warning") {
				t.Fatalf("an ordinary %s of %v warns:\n%s", typ, pair, r.text)
			}
		}
	}
	must(t, call(t, cs, "recompute_document", map[string]any{"doc_name": plain, "include_screenshot": false}), "empty_count: 0")
}

// TestGuideClaimsOfThisRound: an expression takes a negative and arithmetic, and a hidden member of an App::Part is left out of its
// shape (the sentences added to values.md and assembly.md).
func TestGuideClaimsOfThisRound(t *testing.T) {
	cs := session(t)
	const doc = "E2EGuideClaims"
	newDoc(t, cs, doc)
	must(t, pdCreate(t, cs, doc, "Params", "Spreadsheet::Sheet", nil, nil))
	must(t, call(t, cs, "update_spreadsheet_cells", map[string]any{"doc_name": doc, "sheet_name": "Params", "cells": []any{
		map[string]any{"cell": "A1", "content": "20", "alias": "width"}}}))
	must(t, pdCreate(t, cs, doc, "Plate", "Part::Box", map[string]any{"Length": "=(Params.width + 2) * 2", "Width": 5, "Height": 3,
		"Placement.Base.x": "=-Params.width / 2"}, nil))
	v0416Code(t, cs, doc, "print('EXPR', d.Plate.Length.Value, d.Plate.Placement.Base.x)", "EXPR 44.0 -10.0")

	must(t, pdCreate(t, cs, doc, "PartA", "App::Part", nil, nil))
	must(t, pdCreate(t, cs, doc, "Big", "Part::Box", map[string]any{"Length": 40, "Width": 30, "Height": 3}, nil))
	must(t, pdCreate(t, cs, doc, "Small", "Part::Box", map[string]any{"Length": 10, "Width": 10, "Height": 10, "Placement": vec(50, 0, 0)}, nil))
	pushInto(t, cs, doc, "PartA", "Big", "Small")
	size := func() reply {
		return call(t, cs, "check_printability", map[string]any{"doc_name": doc, "bed_x": 220, "bed_y": 220, "object_names": []string{"PartA"}})
	}
	must(t, size(), "size 60 x 30 x 10 mm")
	v0416Code(t, cs, doc, "d.Small.ViewObject.Visibility = False\nprint('HID')", "HID")
	must(t, size(), "size 40 x 30 x 3 mm")
}

// TestACompoundInputIsNotSizeChecked: two overlapping 10 mm cubes held in one compound report the sum of their volumes (2000), more than their
// union (1500). A box of 1795.2 mm^3 that contains both fuses to itself, which is smaller than the compound's 2000: without the skip of compound
// inputs the fuse would be reported as having dropped part of its largest input.
func TestACompoundInputIsNotSizeChecked(t *testing.T) {
	cs := session(t)
	const doc = "E2ECompoundInput"
	newDoc(t, cs, doc)
	must(t, call(t, cs, "execute_code", map[string]any{"include_screenshot": false, "code": "import FreeCAD, Part\nd = FreeCAD.getDocument('" + doc + "')\n" +
		"d.addObject('Part::Feature', 'Pair').Shape = Part.makeCompound([Part.makeBox(10, 10, 10), Part.makeBox(10, 10, 10, FreeCAD.Vector(5, 0, 0))])\n" +
		"d.recompute()\nprint('PAIR', round(d.Pair.Shape.Volume), len(d.Pair.Shape.Solids))"}), "PAIR 2000 2")
	must(t, pdCreate(t, cs, doc, "Holder", "Part::Box", map[string]any{"Length": 16, "Width": 10.2, "Height": 11, "Placement": vec(-0.5, -0.1, -0.5)}, nil))
	r := pdCreate(t, cs, doc, "Union", "Part::Fuse", map[string]any{"Base": "Pair", "Tool": "Holder"}, nil)
	must(t, r, "volume 1795.2")
	if strings.Contains(r.text, "Warning") {
		t.Fatalf("a compound input made the fuse warn:\n%s", r.text)
	}
	must(t, call(t, cs, "recompute_document", map[string]any{"doc_name": doc, "include_screenshot": false}), "empty_count: 0")
}

var solidShapeLine = regexp.MustCompile(`Shape: [1-9][0-9]* solids?`)

// TestNoFalseSizeWarningOnNonPrimitiveShapes: booleans of a threaded bolt, swept springs, lofts and filleted boxes, which the volume rule
// was measured on (the worst excess over the bound was below zero), say nothing about a lost input.
func TestNoFalseSizeWarningOnNonPrimitiveShapes(t *testing.T) {
	cs := session(t)
	const doc = "E2ENonPrimitive"
	newDoc(t, cs, doc)
	build := `
V = FreeCAD.Vector
pitch, major, depth, length, inset = 3.0, 8.0, 1.0, 13.0, 0.3
r = major / 2; root = r - depth
profile = Part.makePolygon([V(root - inset, 0, -0.45 * pitch), V(r, 0, -0.1 * pitch), V(r, 0, 0.1 * pitch), V(root - inset, 0, 0.45 * pitch), V(root - inset, 0, -0.45 * pitch)])
thread = Part.Wire(Part.makeHelix(pitch, length, root)).makePipeShell([profile], True, True)
bolt = Part.makeCylinder(root, length + pitch, V(0, 0, -pitch / 2)).fuse(thread).common(Part.makeCylinder(r + 1, length)).removeSplitter()
def spring(at):
    s = Part.Wire(Part.makeHelix(3, 12, 6)).makePipeShell([Part.Wire(Part.makeCircle(1, V(6, 0, 0), V(0, 1, 0)))], True, True)
    s.translate(V(*at)); return s
def loft(r1, r2, h, at):
    return Part.makeLoft([Part.Wire(Part.makeCircle(r1, V(*at))), Part.Wire(Part.makeCircle(r2, V(at[0] + 1, at[1], at[2] + h)))], True)
box = Part.makeBox(30, 20, 10); box = box.makeFillet(2.5, box.Edges)
for name, shape in (('Bolt', bolt), ('SpringA', spring((0, 0, 0))), ('SpringB', spring((2, 0, 1.5))), ('LoftA', loft(6, 3, 20, (0, 0, 0))),
                    ('LoftB', loft(5, 2, 20, (2, 0, 4))), ('Fillet', box), ('Notch', Part.makeBox(2, 2, 2, V(3, -1, 5))),
                    ('Ball', Part.makeSphere(1.2, V(3.6, 0, 6))), ('Bore', Part.makeCylinder(1.0, 14, V(0, 0, -0.5))),
                    ('Pipe', Part.makeCylinder(5, 12, V(15, 10, -1))), ('Sphere', Part.makeSphere(8, V(28, 10, 8)))):
    d.addObject('Part::Feature', name).Shape = shape
d.recompute()
print('SHAPES', len(d.Objects))
`
	must(t, call(t, cs, "execute_code", map[string]any{"include_screenshot": false, "timeout": 120,
		"code": "import FreeCAD, Part\nd = FreeCAD.getDocument('" + doc + "')\n" + build}), "SHAPES 11")
	n := 0
	for _, c := range []struct{ typ, base, tool string }{
		{"Part::Cut", "Bolt", "Notch"}, {"Part::Cut", "Bolt", "Ball"}, {"Part::Cut", "Bolt", "Bore"},
		{"Part::Fuse", "SpringA", "SpringB"}, {"Part::Common", "SpringA", "SpringB"}, {"Part::Cut", "SpringA", "SpringB"},
		{"Part::Fuse", "LoftA", "LoftB"}, {"Part::Common", "LoftA", "LoftB"}, {"Part::Cut", "LoftA", "LoftB"},
		{"Part::Cut", "Fillet", "Pipe"}, {"Part::Fuse", "Fillet", "Sphere"}, {"Part::Common", "Fillet", "Sphere"},
	} {
		n++
		r := pdCreate(t, cs, doc, "R"+strconv.Itoa(n), c.typ, map[string]any{"Base": c.base, "Tool": c.tool}, nil)
		must(t, r, "Shape: ")
		if !solidShapeLine.MatchString(r.text) {
			t.Fatalf("%s of %s and %s has no solid:\n%s", c.typ, c.base, c.tool, r.text)
		}
		if strings.Contains(r.text, "The result is") || strings.Contains(r.text, "Warning: The result holds no solid") {
			t.Fatalf("%s of %s and %s warns:\n%s", c.typ, c.base, c.tool, r.text)
		}
	}
	must(t, call(t, cs, "recompute_document", map[string]any{"doc_name": doc, "include_screenshot": false}), "empty_count: 0")
}

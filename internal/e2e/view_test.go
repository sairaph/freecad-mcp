//go:build e2e

package e2e

import "testing"

// TestSetViewShowsAndFocusesHiddenObjectsInOneCall: objects that are hidden are shown and framed
// by one set_view call (focus counts what the same call shows), and a call that names hidden
// objects without showing them still says there is nothing to frame.
func TestSetViewShowsAndFocusesHiddenObjectsInOneCall(t *testing.T) {
	cs := session(t)
	newDoc(t, cs, "E2EShowFocus")
	must(t, call(t, cs, "execute_code", map[string]any{"include_screenshot": false, "code": `doc = FreeCAD.getDocument('E2EShowFocus')
for name, x in (('FSA', 0), ('RRD', 60)):
    box = doc.addObject('Part::Box', name)
    box.Placement.Base = FreeCAD.Vector(x, 0, 0)
doc.recompute()
for name in ('FSA', 'RRD'):
    doc.getObject(name).ViewObject.Visibility = False
print('hidden', [doc.getObject(n).ViewObject.Visibility for n in ('FSA', 'RRD')])
`}), "hidden [False, False]")

	fails(t, call(t, cs, "set_view", map[string]any{"doc_name": "E2EShowFocus", "focus": []string{"FSA", "RRD"}, "include_screenshot": false}),
		"None of the focus objects has a shape on screen")

	r := call(t, cs, "set_view", map[string]any{"doc_name": "E2EShowFocus", "show": []string{"FSA", "RRD"},
		"focus": []string{"FSA", "RRD"}, "include_screenshot": false})
	must(t, r, "document: E2EShowFocus")

	// Both are shown and the camera frames both: the view's box holds the two boxes.
	must(t, call(t, cs, "execute_code", map[string]any{"include_screenshot": false, "code": `doc = FreeCAD.getDocument('E2EShowFocus')
print('visible', [doc.getObject(n).ViewObject.Visibility for n in ('FSA', 'RRD')])
`}), "visible [True, True]")
}

// TestSetViewChangesNothingWhenAShownObjectHasNothingToFrame: an object with no shape (an empty
// sketch, a spreadsheet) cannot be framed, so a call that shows and focuses only it is refused before
// anything changes: the object stays hidden.
func TestSetViewChangesNothingWhenAShownObjectHasNothingToFrame(t *testing.T) {
	cs := session(t)
	newDoc(t, cs, "E2ENoShape")
	must(t, call(t, cs, "execute_code", map[string]any{"include_screenshot": false, "code": `doc = FreeCAD.getDocument('E2ENoShape')
doc.addObject('Sketcher::SketchObject', 'Sketch')
doc.addObject('Spreadsheet::Sheet', 'Sheet')
doc.addObject('Part::Box', 'Box')
doc.recompute()
for name in ('Sketch', 'Sheet'):
    doc.getObject(name).ViewObject.Visibility = False
print('state', [doc.getObject(n).ViewObject.Visibility for n in ('Sketch', 'Sheet')], [doc.getObject(n).Shape.isNull() for n in ('Sketch',)])
`}), "state [False, False]")
	for _, name := range []string{"Sketch", "Sheet"} {
		fails(t, call(t, cs, "set_view", map[string]any{"doc_name": "E2ENoShape", "show": []string{name},
			"focus": []string{name}, "include_screenshot": false}), "None of the focus objects has a shape on screen")
		must(t, call(t, cs, "execute_code", map[string]any{"include_screenshot": false,
			"code": "print('after', FreeCAD.getDocument('E2ENoShape').getObject('" + name + "').ViewObject.Visibility)"}), "after False")
	}
	// An object with a shape in the same call is still shown and framed.
	must(t, call(t, cs, "set_view", map[string]any{"doc_name": "E2ENoShape", "show": []string{"Sketch", "Box"},
		"focus": []string{"Sketch", "Box"}, "include_screenshot": false}), "document: E2ENoShape")
}

// TestSetViewShowsAndFocusesObjectsHiddenSinceTheFileLoaded: the same call on a file saved by a
// headless script with hidden objects. Such an object has no scene box until it is drawn, so the
// check before the change must not demand one of an object the call shows.
func TestSetViewShowsAndFocusesObjectsHiddenSinceTheFileLoaded(t *testing.T) {
	cs := session(t)
	file := tempPath(t, "hidden.FCStd")
	must(t, call(t, cs, "execute_code_headless", map[string]any{"timeout": 120, "code": `import FreeCAD
doc = FreeCAD.newDocument('E2EHeadlessHidden')
for name, x in (('FSA', 0), ('RRD', 60)):
    box = doc.addObject('Part::Box', name)
    box.Placement.Base = FreeCAD.Vector(x, 0, 0)
    box.Visibility = False
part = doc.addObject('App::Part', 'Asm')
inner = doc.addObject('Part::Box', 'Inner')
part.addObject(inner)
part.Visibility = False
doc.recompute()
doc.saveAs(r"` + file + `")
print('saved')
`}), "saved")
	opened := call(t, cs, "open_document", map[string]any{"path": file})
	must(t, opened)
	name := frontValue(opened.text, "document")
	closeOnCleanup(t, cs, name)
	must(t, call(t, cs, "execute_code", map[string]any{"include_screenshot": false, "code": `doc = FreeCAD.getDocument('` + name + `')
for name in ('FSA', 'RRD'):
    doc.getObject(name).ViewObject.Visibility = False
doc.getObject('Asm').ViewObject.Visibility = False
print('state', [doc.getObject(n).ViewObject.Visibility for n in ('FSA', 'RRD', 'Asm', 'Inner')])
`}), "state [False, False, False")

	must(t, call(t, cs, "set_view", map[string]any{"doc_name": name, "show": []string{"FSA", "RRD"},
		"focus": []string{"FSA", "RRD"}, "include_screenshot": false}), "document: "+name)
	// An object inside a hidden part is not drawn by showing it alone, and the reply says so.
	fails(t, call(t, cs, "set_view", map[string]any{"doc_name": name, "show": []string{"Inner"},
		"focus": []string{"Inner"}, "include_screenshot": false}), "None of the focus objects has a shape on screen")
}

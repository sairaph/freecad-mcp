//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// TestTheShapeLineSaysWhereTheShapeLies: a Part::Chamfer moved by its own Placement on a cylinder that is moved too lands where the two
// Placements add up, and the Shape line gives that box.
func TestTheShapeLineSaysWhereTheShapeLies(t *testing.T) {
	cs := session(t)
	const doc = "E2EShapeSpan"
	newDoc(t, cs, doc)
	must(t, pdCreate(t, cs, doc, "Post", "Part::Cylinder", map[string]any{"Radius": 3.5, "Height": 5, "Placement": vec(12, 0, 0)}, nil),
		"at x 8.5 to 15.5, y -3.5 to 3.5, z 0 to 5")
	r := pdCreate(t, cs, doc, "Round", "Part::Chamfer", map[string]any{"Base": "Post", "Edges": []any{"Edge3"}, "Size": 0.5, "Placement": vec(22, 0, 0)}, nil)
	must(t, r, "Placement now: Base (22, 0, 0).", "7 x 7 x 5 mm at x 30.5 to 37.5, y -3.5 to 3.5, z 0 to 5")
}

// TestAValueFreeCADChangedIsNamed: Part::Circle takes Angle1 from 0 to 360, so -90 is stored as 0 and the reply says so; a value kept is not named.
func TestAValueFreeCADChangedIsNamed(t *testing.T) {
	cs := session(t)
	const doc = "E2EAdjusted"
	newDoc(t, cs, doc)
	r := pdCreate(t, cs, doc, "Arc", "Part::Circle", map[string]any{"Radius": 5, "Angle1": -90, "Angle2": 90}, nil)
	must(t, r, "Angle1: given -90, FreeCAD stored 0 (outside the range it allows).")
	if strings.Contains(r.text, "Radius: given") || strings.Contains(r.text, "Angle2: given") {
		t.Fatalf("a value that was kept is named as changed:\n%s", r.text)
	}
}

// TestProfilesOfASolidMakerGoHidden: a loft hides its sections and a sweep its profile and path, and the reply names them.
func TestProfilesOfASolidMakerGoHidden(t *testing.T) {
	cs := session(t)
	const doc = "E2EProfilesHidden"
	newDoc(t, cs, doc)
	must(t, pdCreate(t, cs, doc, "Round", "Part::Circle", map[string]any{"Radius": 10}, nil))
	must(t, pdCreate(t, cs, doc, "Oval", "Part::Ellipse", map[string]any{"MajorRadius": 12, "MinorRadius": 6, "Placement": vec(0, 0, 20)}, nil))
	r := pdCreate(t, cs, doc, "Vase", "Part::Loft", map[string]any{"Sections": []any{"Round", "Oval"}, "Solid": true, "Ruled": false}, nil)
	must(t, r, "Shape: 1 solid", "Hidden: Round, Oval (inputs of Vase).")
	if got := docOutput(t, cs, doc, "print(d.Round.ViewObject.Visibility, d.Oval.ViewObject.Visibility, d.Vase.ViewObject.Visibility)"); got != "False False True" {
		t.Fatalf("visibility of the sections and the loft: %q", got)
	}

	must(t, pdCreate(t, cs, doc, "Profile", "Part::Circle", map[string]any{"Radius": 2, "Placement": vec(40, 0, 0)}, nil))
	must(t, pdCreate(t, cs, doc, "Path", "Part::Line", map[string]any{"X1": 40, "Y1": 0, "Z1": 0, "X2": 40, "Y2": 0, "Z2": 30}, nil))
	r = pdCreate(t, cs, doc, "Rod", "Part::Sweep", map[string]any{"Sections": []any{"Profile"}, "Spine": []any{"Path", []any{"Edge1"}}, "Solid": true}, nil)
	must(t, r, "Shape: 1 solid", "Hidden: Profile, Path (inputs of Rod).")
	if got := docOutput(t, cs, doc, "print(d.Profile.ViewObject.Visibility, d.Path.ViewObject.Visibility, d.Rod.ViewObject.Visibility)"); got != "False False True" {
		t.Fatalf("visibility of the profile, the path and the sweep: %q", got)
	}
}

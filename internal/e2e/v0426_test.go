//go:build e2e

package e2e

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Indexes of a bounding box read by pdRead.
const (
	bxMin = iota
	byMin
	bzMin
	bxMax
	byMax
	bzMax
)

// pdState is the Body as the document holds it: its Tip, its shape's volume, validity, solid count and box.
type pdState struct {
	tip    string
	volume float64
	valid  bool
	solids int
	box    [6]float64
}

func pdRead(t *testing.T, cs *mcp.ClientSession, doc string) pdState {
	t.Helper()
	out := docOutput(t, cs, doc, "b = d.Body\ns = b.Shape\nbb = s.BoundBox\n"+
		"print(b.Tip.Name, round(s.Volume, 3), s.isValid(), len(s.Solids), round(bb.XMin, 3), round(bb.YMin, 3), round(bb.ZMin, 3), "+
		"round(bb.XMax, 3), round(bb.YMax, 3), round(bb.ZMax, 3))")
	f := strings.Fields(out)
	if len(f) != 10 {
		t.Fatalf("Body state %q is not 10 fields", out)
	}
	s := pdState{tip: f[0], valid: f[2] == "True"}
	s.volume, _ = strconv.ParseFloat(f[1], 64)
	n, _ := strconv.Atoi(f[3])
	s.solids = n
	for i := range s.box {
		s.box[i], _ = strconv.ParseFloat(f[4+i], 64)
	}
	return s
}

// pdBase returns the name of the BaseFeature of the object, or "None".
func pdBase(t *testing.T, cs *mcp.ClientSession, doc, name string) string {
	t.Helper()
	return docOutput(t, cs, doc, "bf = d.getObject('"+name+"').BaseFeature\nprint(bf.Name if bf else None)")
}

// pdIn creates an object in Body "Body" with create_object.
func pdIn(t *testing.T, cs *mcp.ClientSession, doc, name, typ string, props map[string]any) reply {
	t.Helper()
	return pdCreate(t, cs, doc, name, typ, props, map[string]any{"body_name": "Body"})
}

func pdRect(x, y, w, h float64) map[string]any {
	return map[string]any{"rectangle": map[string]any{"corner": []any{x, y}, "size": []any{w, h}}}
}

func pdCircle(x, y, r float64) map[string]any {
	return map[string]any{"circle": map[string]any{"center": []any{x, y}, "radius": r}}
}

func pdLine(x1, y1, x2, y2 float64) map[string]any {
	return map[string]any{"line": []any{[]any{x1, y1}, []any{x2, y2}}}
}

// pdSketch makes a sketch in Body "Body" on an origin plane, moved by offsets (AttachmentOffset parts, in the sketch's own axes).
func pdSketch(t *testing.T, cs *mcp.ClientSession, doc, name, plane string, offsets map[string]any, geometry ...any) {
	t.Helper()
	props := map[string]any{"AttachmentSupport": plane, "Geometry": geometry}
	for k, v := range offsets {
		props[k] = v
	}
	must(t, pdIn(t, cs, doc, name, "Sketcher::SketchObject", props))
}

// pdLift is the offset that moves a sketch along its normal, which for XY_Plane is up.
func pdLift(z float64) map[string]any { return map[string]any{"AttachmentOffset.Base.z": z} }

// pdPick returns the name of the first sub-element of an object whose shape matches a Python condition on e (an edge or a face).
func pdPick(t *testing.T, cs *mcp.ClientSession, doc, obj, kind, cond string) string {
	t.Helper()
	list, prefix := "Faces", "Face"
	if kind == "edge" {
		list, prefix = "Edges", "Edge"
	}
	got := docOutput(t, cs, doc, "s = d."+obj+".Shape\nprint(['"+prefix+"%d' % (i + 1) for i, e in enumerate(s."+list+") if "+cond+"][0])")
	if !strings.HasPrefix(got, prefix) {
		t.Fatalf("no %s matches %s on %s: %q", kind, cond, obj, got)
	}
	return got
}

// pdCase is one PartDesign feature type. prep builds what the feature needs (sketches, originals, a second Body); make creates the feature
// (default: create_object with typ and props). The checks compare the Body before make with the Body after it.
type pdCase struct {
	name    string
	typ     string
	props   map[string]any
	prep    func(t *testing.T, cs *mcp.ClientSession, doc string)
	make    func(t *testing.T, cs *mcp.ClientSession, doc string) reply
	sign    int                                 // +1 the volume grows, -1 it shrinks, 0 it only has to change
	delta   float64                             // the exact volume change, 0 to check the direction only
	size    float64                             // the size of the volume change, sign free, within 5 percent
	grows   func(before, after [6]float64) bool // what the box must do; nil means it stays
	boxFree bool                                // the box is not checked
	next    []float64                           // where the AdditiveBox that follows sits; default 34, 24, 10 (on top of the base, clear of the feature)
}

func above(i int, by float64) func(b, a [6]float64) bool {
	return func(b, a [6]float64) bool { return a[i] > b[i]+by }
}

func below(i int, by float64) func(b, a [6]float64) bool {
	return func(b, a [6]float64) bool { return a[i] < b[i]-by }
}

// pdHole is a SubtractiveCylinder hole 4 mm wide through the base, the original of the pattern cases.
func pdHole(x, y float64) func(t *testing.T, cs *mcp.ClientSession, doc string) {
	return func(t *testing.T, cs *mcp.ClientSession, doc string) {
		must(t, pdIn(t, cs, doc, "Hole", "PartDesign::SubtractiveCylinder", map[string]any{"Radius": 2, "Height": 10, "Placement": vec(x, y, 0)}))
	}
}

const holeCut = math.Pi * 2 * 2 * 10

func pdCases() []pdCase {
	prim := func(name, typ string, props map[string]any, sign int, delta float64, grows func(b, a [6]float64) bool) pdCase {
		return pdCase{name: name, typ: typ, props: props, sign: sign, delta: delta, grows: grows}
	}
	at := func(x, y, z float64, props map[string]any) map[string]any {
		props["Placement"] = vec(x, y, z)
		return props
	}
	return []pdCase{
		// Additive primitives, on or into the top of the base.
		prim("AdditiveBox", "PartDesign::AdditiveBox", at(5, 5, 10, map[string]any{"Length": 10, "Width": 10, "Height": 10}), 1, 1000, above(bzMax, 9)),
		prim("AdditiveCylinder", "PartDesign::AdditiveCylinder", at(20, 15, 10, map[string]any{"Radius": 5, "Height": 10}), 1, math.Pi*25*10, above(bzMax, 9)),
		prim("AdditiveSphere", "PartDesign::AdditiveSphere", at(20, 15, 10, map[string]any{"Radius": 5}), 1, 2.0/3*math.Pi*125, above(bzMax, 4)),
		prim("AdditiveCone", "PartDesign::AdditiveCone", at(20, 15, 10, map[string]any{"Radius1": 5, "Radius2": 2, "Height": 10}), 1, math.Pi*10/3*(25+10+4), above(bzMax, 9)),
		prim("AdditiveTorus", "PartDesign::AdditiveTorus", at(20, 15, 11, map[string]any{"Radius1": 10, "Radius2": 2}), 1, 0, above(bzMax, 2)),
		prim("AdditivePrism", "PartDesign::AdditivePrism", at(20, 15, 10, map[string]any{"Polygon": 6, "Circumradius": 5, "Height": 10}), 1, 1.5*math.Sqrt(3)*25*10, above(bzMax, 9)),
		prim("AdditiveWedge", "PartDesign::AdditiveWedge", at(20, 15, 5, map[string]any{}), 1, 0, above(bzMax, 4)),
		prim("AdditiveEllipsoid", "PartDesign::AdditiveEllipsoid", at(20, 15, 10, map[string]any{"Radius1": 4, "Radius2": 6, "Radius3": 5}), 1, 0, above(bzMax, 3)),
		// Subtractive primitives, cut into the base.
		prim("SubtractiveBox", "PartDesign::SubtractiveBox", at(5, 5, 0, map[string]any{"Length": 10, "Width": 10, "Height": 10}), -1, -1000, nil),
		prim("SubtractiveCylinder", "PartDesign::SubtractiveCylinder", at(20, 15, 0, map[string]any{"Radius": 5, "Height": 10}), -1, -math.Pi*25*10, nil),
		prim("SubtractiveSphere", "PartDesign::SubtractiveSphere", at(20, 15, 10, map[string]any{"Radius": 5}), -1, -2.0/3*math.Pi*125, nil),
		prim("SubtractiveCone", "PartDesign::SubtractiveCone", at(20, 15, 0, map[string]any{"Radius1": 5, "Radius2": 2, "Height": 10}), -1, -math.Pi*10/3*(25+10+4), nil),
		prim("SubtractiveTorus", "PartDesign::SubtractiveTorus", at(20, 15, 5, map[string]any{"Radius1": 10, "Radius2": 2}), -1, 0, nil),
		prim("SubtractivePrism", "PartDesign::SubtractivePrism", at(20, 15, 0, map[string]any{"Polygon": 6, "Circumradius": 5, "Height": 10}), -1, -1.5*math.Sqrt(3)*25*10, nil),
		prim("SubtractiveWedge", "PartDesign::SubtractiveWedge", at(20, 15, 0, map[string]any{}), -1, 0, nil),
		prim("SubtractiveEllipsoid", "PartDesign::SubtractiveEllipsoid", at(20, 15, 5, map[string]any{"Radius1": 3, "Radius2": 6, "Radius3": 5}), -1, 0, nil),

		// Sketch-based features. Sketches on XY_Plane are lifted to the top of the base (z 10).
		{
			name: "Pad", typ: "PartDesign::Pad", props: map[string]any{"Profile": "S", "Length": 5}, sign: 1, delta: 500, grows: above(bzMax, 4),
			prep: func(t *testing.T, cs *mcp.ClientSession, doc string) {
				pdSketch(t, cs, doc, "S", "XY_Plane", pdLift(10), pdRect(5, 5, 10, 10))
			},
		},
		{
			name: "Pocket", typ: "PartDesign::Pocket", props: map[string]any{"Profile": "S", "Length": 5}, sign: -1, delta: -math.Pi * 16 * 5,
			prep: func(t *testing.T, cs *mcp.ClientSession, doc string) {
				pdSketch(t, cs, doc, "S", "XY_Plane", pdLift(10), pdCircle(20, 15, 4))
			},
		},
		{
			// A flat-bottomed 6 mm hole 5 deep (Hole's own defaults give an angled drill point and a depth of 25).
			name: "Hole", typ: "PartDesign::Hole",
			props: map[string]any{"Profile": "S", "Diameter": 6, "DepthType": "Dimension", "Depth": 5, "DrillPoint": "Flat"}, sign: -1, delta: -math.Pi * 9 * 5,
			prep: func(t *testing.T, cs *mcp.ClientSession, doc string) {
				pdSketch(t, cs, doc, "S", "XY_Plane", pdLift(10), pdCircle(20, 15, 3))
			},
		},
		{
			// The sketch sits on XZ_Plane moved to x 20 and spans local x 0 to 5, y 10 to 15: a half of a cylinder of radius 5 around the line x 20, y 0.
			name: "Revolution", typ: "PartDesign::Revolution", props: map[string]any{"Profile": "S", "ReferenceAxis": []any{"S", "V_Axis"}, "Angle": 360},
			sign: 1, delta: math.Pi * 25 * 5, grows: below(byMin, 4),
			prep: func(t *testing.T, cs *mcp.ClientSession, doc string) {
				pdSketch(t, cs, doc, "S", "XZ_Plane", map[string]any{"AttachmentOffset.Base.x": 20}, pdRect(0, 10, 5, 5))
			},
		},
		{
			// The same axis, cutting a cylinder of radius 5 from z 2 to 8, half of which is in the base.
			name: "Groove", typ: "PartDesign::Groove", props: map[string]any{"Profile": "S", "ReferenceAxis": []any{"S", "V_Axis"}, "Angle": 360},
			sign: -1, delta: -math.Pi * 25 * 6 / 2,
			prep: func(t *testing.T, cs *mcp.ClientSession, doc string) {
				pdSketch(t, cs, doc, "S", "XZ_Plane", map[string]any{"AttachmentOffset.Base.x": 20}, pdRect(0, 2, 5, 6))
			},
		},
		{
			// A frustum from a 20 x 20 square at z 10 to a 10 x 10 square at z 30.
			name: "AdditiveLoft", typ: "PartDesign::AdditiveLoft", props: map[string]any{"Profile": "A", "Sections": []any{"B"}, "Ruled": true},
			sign: 1, delta: 20.0 / 3 * (400 + 100 + 200), grows: above(bzMax, 19),
			prep: func(t *testing.T, cs *mcp.ClientSession, doc string) {
				pdSketch(t, cs, doc, "A", "XY_Plane", pdLift(10), pdRect(5, 5, 20, 20))
				pdSketch(t, cs, doc, "B", "XY_Plane", pdLift(30), pdRect(10, 10, 10, 10))
			},
		},
		{
			// A frustum cut from a 10 x 10 square at the top face to a 4 x 4 square at the bottom face.
			name: "SubtractiveLoft", typ: "PartDesign::SubtractiveLoft", props: map[string]any{"Profile": "A", "Sections": []any{"B"}, "Ruled": true},
			sign: -1, delta: -10.0 / 3 * (100 + 16 + 40),
			prep: func(t *testing.T, cs *mcp.ClientSession, doc string) {
				pdSketch(t, cs, doc, "A", "XY_Plane", pdLift(10), pdRect(15, 10, 10, 10))
				pdSketch(t, cs, doc, "B", "XY_Plane", nil, pdRect(18, 13, 4, 4))
			},
		},
		{
			// A 4 x 4 square at z 10 swept up a vertical line at (7, 7) from z 10 to 25. The path sketch is on YZ_Plane moved to x 7.
			name: "AdditivePipe", typ: "PartDesign::AdditivePipe", props: map[string]any{"Profile": "P", "Spine": []any{"Path", []any{"Edge1"}}},
			sign: 1, delta: 16 * 15, grows: above(bzMax, 14),
			prep: func(t *testing.T, cs *mcp.ClientSession, doc string) {
				pdSketch(t, cs, doc, "P", "XY_Plane", pdLift(10), pdRect(5, 5, 4, 4))
				pdSketch(t, cs, doc, "Path", "YZ_Plane", pdLift(7), pdLine(7, 10, 7, 25))
			},
		},
		{
			name: "SubtractivePipe", typ: "PartDesign::SubtractivePipe", props: map[string]any{"Profile": "P", "Spine": []any{"Path", []any{"Edge1"}}},
			sign: -1, delta: -16 * 10,
			prep: func(t *testing.T, cs *mcp.ClientSession, doc string) {
				pdSketch(t, cs, doc, "P", "XY_Plane", pdLift(10), pdRect(5, 5, 4, 4))
				pdSketch(t, cs, doc, "Path", "YZ_Plane", pdLift(7), pdLine(7, 10, 7, 0))
			},
		},
		{
			// A circle of radius 1.5 at radius 8 around the line x 20, y 15 (the sketch is on XZ_Plane moved to x 20, y 15), swept in two turns of pitch 6.
			name: "AdditiveHelix", typ: "PartDesign::AdditiveHelix",
			props: map[string]any{"Profile": "S", "ReferenceAxis": []any{"S", "V_Axis"}, "Pitch": 6, "Height": 12}, sign: 1, grows: above(bzMax, 10),
			prep: func(t *testing.T, cs *mcp.ClientSession, doc string) {
				pdSketch(t, cs, doc, "S", "XZ_Plane", map[string]any{"AttachmentOffset.Base.x": 20, "AttachmentOffset.Base.z": -15}, pdCircle(8, 10, 1.5))
			},
		},
		{
			// One turn of the same shape inside the base, from z 2.5 to 9.5.
			name: "SubtractiveHelix", typ: "PartDesign::SubtractiveHelix",
			props: map[string]any{"Profile": "S", "ReferenceAxis": []any{"S", "V_Axis"}, "Pitch": 4, "Height": 4}, sign: -1,
			prep: func(t *testing.T, cs *mcp.ClientSession, doc string) {
				pdSketch(t, cs, doc, "S", "XZ_Plane", map[string]any{"AttachmentOffset.Base.x": 20, "AttachmentOffset.Base.z": -15}, pdCircle(8, 4, 1.5))
			},
		},

		// Dress-ups of the base box. Edge and face names are read from its shape.
		{
			name: "Fillet", sign: -1, delta: -(1 - math.Pi/4) * 4 * 30,
			make: func(t *testing.T, cs *mcp.ClientSession, doc string) reply {
				edge := pdPick(t, cs, doc, "BaseBox", "edge", "all(abs(v.X) < 1e-6 and abs(v.Z - 10) < 1e-6 for v in e.Vertexes)")
				return pdIn(t, cs, doc, "Feat", "PartDesign::Fillet", map[string]any{"Base": "BaseBox", "Edges": []any{edge}, "Radius": 2})
			},
		},
		{
			name: "Chamfer", sign: -1, delta: -0.5 * 4 * 30,
			make: func(t *testing.T, cs *mcp.ClientSession, doc string) reply {
				edge := pdPick(t, cs, doc, "BaseBox", "edge", "all(abs(v.X) < 1e-6 and abs(v.Z - 10) < 1e-6 for v in e.Vertexes)")
				return pdIn(t, cs, doc, "Feat", "PartDesign::Chamfer", map[string]any{"Base": "BaseBox", "Edges": []any{edge}, "Size": 2})
			},
		},
		{
			// Tilts the side face at x 0 by 5 degrees from the bottom face as the neutral plane. Which way FreeCAD tilts it is not asserted.
			name: "Draft", sign: 0, boxFree: true, size: 0.5 * math.Tan(5*math.Pi/180) * 10 * 10 * 30,
			make: func(t *testing.T, cs *mcp.ClientSession, doc string) reply {
				side := pdPick(t, cs, doc, "BaseBox", "face", "abs(e.CenterOfMass.x) < 1e-6")
				bottom := pdPick(t, cs, doc, "BaseBox", "face", "abs(e.CenterOfMass.z) < 1e-6")
				return pdIn(t, cs, doc, "Feat", "PartDesign::Draft", map[string]any{"Base": []any{"BaseBox", []any{side}}, "NeutralPlane": []any{"BaseBox", []any{bottom}}, "Angle": 5})
			},
		},
		{
			// A hollow box open at the top with 2 mm walls: the 36 x 26 x 8 cavity is removed. The next box sits on a wall.
			name: "Thickness", sign: -1, delta: -36 * 26 * 8, next: []float64{0, 0, 10},
			make: func(t *testing.T, cs *mcp.ClientSession, doc string) reply {
				top := pdPick(t, cs, doc, "BaseBox", "face", "abs(e.CenterOfMass.z - 10) < 1e-6")
				return pdIn(t, cs, doc, "Feat", "PartDesign::Thickness", map[string]any{"Base": []any{"BaseBox", []any{top}}, "Value": 2})
			},
		},

		// Patterns.
		{
			// Holes at x 10, 20 and 30: two more than the original.
			name: "LinearPattern", typ: "PartDesign::LinearPattern", prep: pdHole(10, 10), sign: -1, delta: -2 * holeCut,
			props: map[string]any{"Originals": []any{"Hole"}, "Direction": "X_Axis", "Length": 20, "Occurrences": 3},
		},
		{
			// The hole at 15 degrees and radius 20 from the origin turns by 60 degrees into a second hole inside the base.
			name: "PolarPattern", typ: "PartDesign::PolarPattern", prep: pdHole(20*math.Cos(15*math.Pi/180), 20*math.Sin(15*math.Pi/180)), sign: -1, delta: -holeCut,
			props: map[string]any{"Originals": []any{"Hole"}, "Axis": "Z_Axis", "Angle": 60, "Occurrences": 2},
		},
		{
			// A box on the top face crossing the YZ plane (x -3 to 7) mirrored to x -7 to 3: the union is 14 wide, so 200 more.
			name: "Mirrored", typ: "PartDesign::Mirrored", sign: 1, delta: 4 * 10 * 5, grows: below(bxMin, 3),
			props: map[string]any{"Originals": []any{"Orig"}, "MirrorPlane": "YZ_Plane"},
			prep: func(t *testing.T, cs *mcp.ClientSession, doc string) {
				must(t, pdIn(t, cs, doc, "Orig", "PartDesign::AdditiveBox", map[string]any{"Length": 10, "Width": 10, "Height": 5, "Placement": vec(-3, 5, 10)}))
			},
		},
		{
			// A LinearPattern made with no Originals is the child; the MultiTransform repeats the hole by it. The Tip is the MultiTransform.
			name: "MultiTransform", sign: -1, delta: -2 * holeCut,
			prep: func(t *testing.T, cs *mcp.ClientSession, doc string) {
				pdHole(10, 10)(t, cs, doc)
				r := pdIn(t, cs, doc, "Child", "PartDesign::LinearPattern", map[string]any{"Direction": "X_Axis", "Length": 20, "Occurrences": 3})
				must(t, r)
				if strings.Contains(r.text, "Tip stays") {
					t.Fatalf("the child reports Tip stays:\n%s", r.text)
				}
				if tip := pdRead(t, cs, doc).tip; tip == "Child" {
					t.Fatalf("the child pattern is the Body Tip")
				}
			},
			make: func(t *testing.T, cs *mcp.ClientSession, doc string) reply {
				return pdIn(t, cs, doc, "Feat", "PartDesign::MultiTransform", map[string]any{"Originals": []any{"Hole"}, "Transformations": []any{"Child"}})
			},
		},
		{
			// A 10 mm box on the top face scaled by 2 about its centre: it grows to 20 mm, up to z 25.
			name: "Scaled", typ: "PartDesign::Scaled", sign: 1, grows: above(bzMax, 3),
			props: map[string]any{"Originals": []any{"Orig"}, "Factor": 2, "Occurrences": 2},
			prep: func(t *testing.T, cs *mcp.ClientSession, doc string) {
				must(t, pdIn(t, cs, doc, "Orig", "PartDesign::AdditiveBox", map[string]any{"Length": 10, "Width": 10, "Height": 10, "Placement": vec(15, 10, 10)}))
			},
		},
		{
			// A second Body with a 10 mm cube through the base's corner region, cut from the base.
			name: "Boolean", typ: "PartDesign::Boolean", sign: -1, delta: -1000, props: map[string]any{"Group": []any{"Tool"}, "Type": "Cut"},
			prep: func(t *testing.T, cs *mcp.ClientSession, doc string) {
				must(t, pdCreate(t, cs, doc, "Tool", "PartDesign::Body", nil, nil))
				must(t, pdCreate(t, cs, doc, "ToolBox", "PartDesign::AdditiveBox", map[string]any{"Length": 10, "Width": 10, "Height": 10, "Placement": vec(5, 5, 0)}, map[string]any{"body_name": "Tool"}))
			},
		},
	}
}

// TestEveryPartDesignFeatureBecomesTheTip: for each PartDesign type create_object can make, built with tool calls only in its own Body on a
// 40 x 30 x 10 box: the reply names the Body and the new feature as the Tip, the document agrees (Tip, BaseFeature, one valid solid, volume
// and box changed as the type should), and the AdditiveBox made next builds on the new feature and adds its volume.
func TestEveryPartDesignFeatureBecomesTheTip(t *testing.T) {
	cs := session(t)
	for _, c := range pdCases() {
		t.Run(c.name, func(t *testing.T) {
			doc := "E2EPD" + c.name
			newDoc(t, cs, doc)
			must(t, pdCreate(t, cs, doc, "Body", "PartDesign::Body", nil, nil))
			must(t, pdIn(t, cs, doc, "BaseBox", "PartDesign::AdditiveBox", map[string]any{"Length": 40, "Width": 30, "Height": 10}), "In Body 'Body' (Tip: BaseBox).")
			if c.prep != nil {
				c.prep(t, cs, doc)
			}
			before := pdRead(t, cs, doc)

			var r reply
			if c.make != nil {
				r = c.make(t, cs, doc)
			} else {
				r = pdIn(t, cs, doc, "Feat", c.typ, c.props)
			}
			feat := frontValue(r.text, "object_name")
			must(t, r, "In Body 'Body' (Tip: "+feat+").")
			if strings.Contains(r.text, "Tip stays") {
				t.Fatalf("the new feature is reported as not the Tip:\n%s", r.text)
			}
			after := pdRead(t, cs, doc)
			if after.tip != feat {
				t.Fatalf("Body.Tip = %q, want %q", after.tip, feat)
			}
			if got := pdBase(t, cs, doc, feat); got != before.tip {
				t.Fatalf("%s.BaseFeature = %q, want the previous Tip %q", feat, got, before.tip)
			}
			if !after.valid || after.solids != 1 {
				t.Fatalf("the Body shape is valid=%v with %d solids, want one valid solid", after.valid, after.solids)
			}
			change := after.volume - before.volume
			switch {
			case c.sign > 0 && change <= 0.001, c.sign < 0 && change >= -0.001, c.sign == 0 && math.Abs(change) <= 0.001:
				t.Fatalf("the Body volume went from %g to %g (%+g), want sign %+d", before.volume, after.volume, change, c.sign)
			}
			if c.size != 0 && math.Abs(math.Abs(change)-c.size) > 0.05*c.size {
				t.Fatalf("the Body volume changed by %g, want a change of size %g within 5 percent", change, c.size)
			}
			if c.delta != 0 && math.Abs(change-c.delta) > 1 {
				t.Fatalf("the Body volume changed by %g, want %g", change, c.delta)
			}
			if c.boxFree {
				// no box expectation
			} else if c.grows != nil {
				if !c.grows(before.box, after.box) {
					t.Fatalf("the box went from %v to %v: not the change this type makes", before.box, after.box)
				}
			} else {
				for i := range before.box {
					if math.Abs(before.box[i]-after.box[i]) > 0.01 {
						t.Fatalf("the box moved from %v to %v, want it unchanged", before.box, after.box)
					}
				}
			}

			// A feature made next builds on this one.
			x, y, z := 34.0, 24.0, 10.0
			if c.next != nil {
				x, y, z = c.next[0], c.next[1], c.next[2]
			}
			must(t, pdIn(t, cs, doc, "Next", "PartDesign::AdditiveBox", map[string]any{"Length": 4, "Width": 4, "Height": 4, "Placement": vec(x, y, z)}),
				"In Body 'Body' (Tip: Next).")
			next := pdRead(t, cs, doc)
			if next.tip != "Next" {
				t.Fatalf("Body.Tip = %q after the next box, want Next", next.tip)
			}
			if got := pdBase(t, cs, doc, "Next"); got != feat {
				t.Fatalf("Next.BaseFeature = %q, want %q", got, feat)
			}
			if !next.valid || next.solids != 1 || math.Abs(next.volume-after.volume-64) > 0.5 {
				t.Fatalf("after the next box: valid=%v, %d solids, volume %g, want one valid solid of %g", next.valid, next.solids, next.volume, after.volume+64)
			}
		})
	}
}

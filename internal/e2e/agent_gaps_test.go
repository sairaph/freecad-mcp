//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/png"
	"math"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestExecuteCodeErrorReturnsTheTraceback: a script that raises on the GUI thread returns the
// exception with the frames and lines that lead to it, not a pointer to the Report View.
func TestExecuteCodeErrorReturnsTheTraceback(t *testing.T) {
	cs := session(t)
	r := call(t, cs, "execute_code", map[string]any{"include_screenshot": false,
		"code": "a = 1\ndef f():\n    raise ValueError('boom from f')\nf()\n"})
	fails(t, r, "ValueError: boom from f", "Traceback:", `File "<string>", line 4, in <module>`, `line 3, in f`, "raise ValueError('boom from f')")
	if regexp.MustCompile("Report View").MatchString(r.text) {
		t.Fatalf("the reply still sends the agent to the Report View:\n%s", r.text)
	}
}

// TestCreateObjectKeepsTheLabelAndEchoesPlacement: a name FreeCAD sanitises keeps the label that was
// asked for, the reply shows both, a Placement that was set is echoed, and the export reply names the
// label the 3MF holds.
func TestCreateObjectKeepsTheLabelAndEchoesPlacement(t *testing.T) {
	cs := session(t)
	newDoc(t, cs, "E2ELabels")
	must(t, call(t, cs, "create_object", map[string]any{"doc_name": "E2ELabels", "obj_name": "Base Plate", "obj_type": "Part::Box",
		"obj_properties":     map[string]any{"Placement": map[string]any{"Base": map[string]any{"x": 5, "y": 6, "z": 0}}},
		"include_screenshot": false}), "object_name: Base_Plate", `Label: "Base Plate"`, "Placement now: Base (5, 6, 0).")
	must(t, call(t, cs, "update_object", map[string]any{"doc_name": "E2ELabels", "obj_name": "Base_Plate",
		"obj_properties": map[string]any{"Placement.Base.z": 3}, "include_screenshot": false}), "Placement now: Base (5, 6, 3).")
	// Large and negative values keep their digits.
	must(t, call(t, cs, "update_object", map[string]any{"doc_name": "E2ELabels", "obj_name": "Base_Plate",
		"obj_properties": map[string]any{"Placement.Base.x": 12345.6789, "Placement.Base.y": -1234567.8, "Placement.Base.z": -0.00001}, "include_screenshot": false}),
		"Placement now: Base (12345.6789, -1234567.8, 0).")
	// A rename for another reason is not a label to restore: the second Box is Box001 with its own label.
	must(t, call(t, cs, "create_object", map[string]any{"doc_name": "E2ELabels", "obj_name": "Box", "obj_type": "Part::Box", "include_screenshot": false}), "object_name: Box")
	r := call(t, cs, "create_object", map[string]any{"doc_name": "E2ELabels", "obj_name": "Box", "obj_type": "Part::Box", "include_screenshot": false})
	must(t, r, "object_name: Box001")

	path := tempPath(t, "labels.3mf")
	must(t, call(t, cs, "export_document", map[string]any{"doc_name": "E2ELabels", "path": path}),
		`"Base Plate" (object Base_Plate)`)
	names := threeMFObjects(t, path)
	if len(names) != 3 || names[0] != "Base Plate" {
		t.Fatalf("the 3MF holds %q", names)
	}
}

// TestSetViewFocusOnAMissingObjectSaysItDoesNotExist.
func TestSetViewFocusOnAMissingObjectSaysItDoesNotExist(t *testing.T) {
	cs := session(t)
	newDoc(t, cs, "E2EMissing")
	fails(t, call(t, cs, "set_view", map[string]any{"doc_name": "E2EMissing", "focus": []string{"Nope"}, "include_screenshot": false}),
		"code: not_found", "Object 'Nope' does not exist in document 'E2EMissing'", "list_objects")
}

func imageSize(t *testing.T, cs *mcp.ClientSession, args map[string]any) (int, int) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_view", Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range res.Content {
		if im, ok := c.(*mcp.ImageContent); ok {
			cfg, _, err := image.DecodeConfig(bytes.NewReader(im.Data))
			if err != nil {
				t.Fatal(err)
			}
			return cfg.Width, cfg.Height
		}
	}
	t.Fatalf("get_view %v returned no image", args)
	return 0, 0
}

// TestGetViewWithOneSideFollowsTheViewportAspect: width alone or height alone scales the picture, it
// does not crop it to a window-sized other side.
func TestGetViewWithOneSideFollowsTheViewportAspect(t *testing.T) {
	cs := session(t)
	newDoc(t, cs, "E2ESize")
	must(t, call(t, cs, "create_object", map[string]any{"doc_name": "E2ESize", "obj_name": "Box", "obj_type": "Part::Box", "include_screenshot": false}))
	size := call(t, cs, "execute_code", map[string]any{"include_screenshot": false,
		"code": "print('size', *FreeCADGui.getDocument('E2ESize').ActiveView.getSize())"})
	m := regexp.MustCompile(`size (\d+) (\d+)`).FindStringSubmatch(size.text)
	if m == nil {
		t.Fatalf("no viewport size: %s", size.text)
	}
	vw, _ := strconv.Atoi(m[1])
	vh, _ := strconv.Atoi(m[2])
	w, h := imageSize(t, cs, map[string]any{"doc_name": "E2ESize", "width": 400})
	if w != 400 || math.Abs(float64(h)-400*float64(vh)/float64(vw)) > 1 {
		t.Errorf("width 400 gave %dx%d on a %dx%d viewport", w, h, vw, vh)
	}
	w, h = imageSize(t, cs, map[string]any{"doc_name": "E2ESize", "height": 300})
	if h != 300 || math.Abs(float64(w)-300*float64(vw)/float64(vh)) > 1 {
		t.Errorf("height 300 gave %dx%d on a %dx%d viewport", w, h, vw, vh)
	}
	if w, h = imageSize(t, cs, map[string]any{"doc_name": "E2ESize", "width": 400, "height": 300}); w != 400 || h != 300 {
		t.Errorf("width and height 400x300 gave %dx%d", w, h)
	}
}

var cameraLine = regexp.MustCompile(`dir \(([-\d.]+), ([-\d.]+), ([-\d.]+)\) up \(([-\d.]+), ([-\d.]+), ([-\d.]+)\)`)

// camera reads the live camera's view direction and up vector.
func camera(t *testing.T, cs *mcp.ClientSession, doc string) [6]float64 {
	t.Helper()
	r := call(t, cs, "execute_code", map[string]any{"include_screenshot": false, "code": "v = FreeCADGui.getDocument('" + doc + "').ActiveView\n" +
		"r = v.getCameraOrientation()\nd = r.multVec(FreeCAD.Vector(0, 0, -1))\nu = r.multVec(FreeCAD.Vector(0, 1, 0))\n" +
		"print('dir (%.4f, %.4f, %.4f) up (%.4f, %.4f, %.4f)' % (d.x, d.y, d.z, u.x, u.y, u.z))"})
	m := cameraLine.FindStringSubmatch(r.text)
	if m == nil {
		t.Fatalf("no camera in %s", r.text)
	}
	var out [6]float64
	for i := range out {
		out[i], _ = strconv.ParseFloat(m[i+1], 64)
	}
	return out
}

// TestOrbitTurnsAboutTheWorldVerticalAxis: during an orbit the camera's elevation stays and its
// heading changes: the view direction's z and the up vector's z hold while x and y move. A turn
// about the camera's own axis (the old behaviour) keeps the direction and spins the picture.
func TestOrbitTurnsAboutTheWorldVerticalAxis(t *testing.T) {
	cs := session(t)
	newDoc(t, cs, "E2EOrbit")
	must(t, call(t, cs, "execute_code", map[string]any{"include_screenshot": false, "code": `doc = FreeCAD.getDocument('E2EOrbit')
b = doc.addObject('Part::Box', 'Box'); b.Length, b.Width, b.Height = 20, 20, 40
doc.recompute()`}))
	must(t, call(t, cs, "set_view", map[string]any{"doc_name": "E2EOrbit", "view_name": "Isometric", "include_screenshot": false}))
	start := camera(t, cs, "E2EOrbit")
	must(t, call(t, cs, "set_view", map[string]any{"doc_name": "E2EOrbit", "mode": "orbit", "degrees_per_second": 30, "include_screenshot": false}), "mode: orbit")
	var samples [][6]float64
	for i := 0; i < 4; i++ {
		time.Sleep(1500 * time.Millisecond)
		samples = append(samples, camera(t, cs, "E2EOrbit"))
	}
	moved := false
	for _, s := range samples {
		if math.Abs(s[2]-start[2]) > 0.01 || math.Abs(s[5]-start[5]) > 0.01 {
			t.Fatalf("the elevation changed: start %v, sample %v", start, s)
		}
		if math.Hypot(s[0]-start[0], s[1]-start[1]) > 0.2 {
			moved = true
		}
	}
	if !moved {
		t.Fatalf("the view direction never turned about the vertical axis: start %v, samples %v", start, samples)
	}

	// The reply of the call that stops it states the direction the camera really has.
	r := call(t, cs, "set_view", map[string]any{"doc_name": "E2EOrbit", "mode": "static", "include_screenshot": false})
	must(t, r, "stopped: replaced")
	live := camera(t, cs, "E2EOrbit")
	want := fmt.Sprintf("looking along (%.2f, %.2f, %.2f)", live[0], live[1], live[2])
	if m := regexp.MustCompile(`looking along \(([-\d.]+), ([-\d.]+), ([-\d.]+)\)`).FindStringSubmatch(r.text); m == nil {
		t.Fatalf("no direction in the reply: %s", r.text)
	} else {
		for i := 0; i < 3; i++ {
			got, _ := strconv.ParseFloat(m[i+1], 64)
			if math.Abs(got-live[i]) > 0.01 {
				t.Fatalf("the reply says %s, the camera looks along %v", m[0], live[:3])
			}
		}
	}
	_ = want
}

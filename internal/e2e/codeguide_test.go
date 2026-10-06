//go:build e2e

package e2e

import (
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sairaph/freecad-mcp/internal/guide"
)

const recipeSizes = "pitch, depth, major, length = 2.0, 1.0, 12.0, 10.0"

// recipeOf returns the thread recipe of code.md (the python block with makePipeShell).
func recipeOf(t *testing.T) string {
	t.Helper()
	text, err := fs.ReadFile(guide.FS(), "code.md")
	if err != nil {
		t.Fatal(err)
	}
	var recipe string
	for _, m := range pythonBlock.FindAllStringSubmatch(string(text), -1) {
		if strings.Contains(m[1], "makePipeShell") {
			recipe = m[1]
		}
	}
	if recipe == "" {
		t.Fatal("code.md has no thread recipe (a python block with makePipeShell)")
	}
	if !strings.Contains(recipe, recipeSizes) {
		t.Fatalf("the recipe no longer starts with %q", recipeSizes)
	}
	return recipe
}

// TestCodeGuideThreadRecipe runs the thread recipe of code.md as written, at its own size and at M8 x 3 (13 mm long, the
// size of the agent's claim) and at M6 x 1, in execute_code_headless. Each bolt is one valid solid, as wide as the thread
// and as long as asked, its volume is within 5 % of the expected one (the recipe prints the ratio) and the thread itself
// (what lies outside the core) reaches both ends, so a bolt that lost its thread or its core cannot pass.
func TestCodeGuideThreadRecipe(t *testing.T) {
	recipe := recipeOf(t)
	cs := session(t)
	dir := t.TempDir()
	for _, size := range []struct {
		pitch, depth, major, length float64
	}{{2, 1, 12, 10}, {3, 1, 8, 13}, {1, 0.6, 6, 12}} {
		code := strings.Replace(recipe, recipeSizes, fmt.Sprintf("pitch, depth, major, length = %g, %g, %g, %g", size.pitch, size.depth, size.major, size.length), 1)
		probe := code + "\nb = bolt.optimalBoundingBox(False, False)\n" +
			"outside = bolt.cut(Part.makeCylinder(root + 0.02, length + 10, FreeCAD.Vector(0, 0, -5))).optimalBoundingBox(False, False)\n" +
			"print('PROBE valid', bolt.isValid(), 'solids', len(bolt.Solids), 'box', round(b.XLength, 2), round(b.YLength, 2), round(b.ZLength, 2), " +
			"'thread', round(outside.ZMin, 2), round(outside.ZMax, 2))\n" +
			"bolt.exportBrep(" + pyQuote(strings.ReplaceAll(dir, "\\", "/")+"/bolt.brep") + ")\n"
		r := call(t, cs, "execute_code_headless", map[string]any{"code": probe, "timeout": 110})
		must(t, r, "PROBE valid True solids 1")
		want := fmt.Sprintf("box %.1f %.1f %.1f", size.major, size.major, size.length)
		if !strings.Contains(r.text, want) {
			t.Fatalf("size %+v: the bolt is not %s:\n%s", size, want, r.text)
		}
		// The line before PROBE: valid, solids, volume, ratio.
		var ratio float64
		for _, line := range strings.Split(r.text, "\n") {
			if f := strings.Fields(line); len(f) == 4 && f[0] == "True" && f[1] == "1" {
				ratio, _ = strconv.ParseFloat(f[3], 64)
			}
		}
		if ratio < 0.95 || ratio > 1.05 {
			t.Fatalf("size %+v: volume ratio %v, want 0.95 to 1.05:\n%s", size, ratio, r.text)
		}
		m := strings.Index(r.text, "thread ")
		var lo, hi float64
		if _, err := fmt.Sscanf(r.text[m:], "thread %f %f", &lo, &hi); err != nil || lo > 0.5*size.pitch || hi < size.length-0.5*size.pitch {
			t.Fatalf("size %+v: the thread spans z %v to %v (%v), want it to reach both ends of 0 to %v:\n%s", size, lo, hi, err, size.length, r.text)
		}
	}

	// The edge list of a threaded bolt stays quick (it is timed in the notes): the last bolt written above.
	newDoc(t, cs, "E2EBolt")
	must(t, call(t, cs, "execute_code", map[string]any{"include_screenshot": false, "code": "import Part\nd = FreeCAD.getDocument('E2EBolt')\n" +
		"s = Part.Shape()\ns.importBrep(" + pyQuote(strings.ReplaceAll(dir, "\\", "/")+"/bolt.brep") + ")\nd.addObject('Part::Feature', 'Bolt').Shape = s\nd.recompute()\nprint('bolt faces', len(s.Faces), 'edges', len(s.Edges))"}), "bolt faces")
	start := time.Now()
	e := call(t, cs, "list_subelements", map[string]any{"doc_name": "E2EBolt", "obj_name": "Bolt", "kind": "edges"})
	must(t, e)
	t.Logf("TIMING list_subelements edges on the bolt: %v", time.Since(start))
}

func pyQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `\x27`) + "'" }

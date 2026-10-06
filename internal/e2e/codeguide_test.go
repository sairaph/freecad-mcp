//go:build e2e

package e2e

import (
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/sairaph/freecad-mcp/internal/guide"
)

// TestCodeGuideThreadRecipe runs the thread recipe of code.md exactly as written in execute_code_headless and checks
// the bolt is one valid solid as wide as the thread (the major diameter), so the recipe cannot rot.
func TestCodeGuideThreadRecipe(t *testing.T) {
	text, err := fs.ReadFile(guide.FS(), "code.md")
	if err != nil {
		t.Fatal(err)
	}
	blocks := pythonBlock.FindAllStringSubmatch(string(text), -1)
	var recipe string
	for _, m := range blocks {
		if strings.Contains(m[1], "makePipeShell") {
			recipe = m[1]
		}
	}
	if recipe == "" {
		t.Fatal("code.md has no thread recipe (a python block with makePipeShell)")
	}
	cs := session(t)
	dir := t.TempDir()
	probe := recipe + "\nb = bolt.optimalBoundingBox(False, False)\n" +
		"print('PROBE valid', bolt.isValid(), 'solids', len(bolt.Solids), 'faces', len(bolt.Faces), 'volume', round(bolt.Volume, 1), " +
		"'box', round(b.XLength, 2), round(b.YLength, 2), round(b.ZLength, 2))\n" +
		"bolt.exportBrep(" + pyQuote(strings.ReplaceAll(dir, "\\", "/")+"/bolt.brep") + ")\n"
	r := call(t, cs, "execute_code_headless", map[string]any{"code": probe, "timeout": 110})
	must(t, r, "PROBE valid True solids 1")
	// The core alone is 10 mm of a 10 mm radius-5 cylinder, 12 mm wide only with the thread on it.
	if !strings.Contains(r.text, "box 12.0 12.0 10.0") {
		t.Fatalf("the bolt is not 12 x 12 x 10 mm:\n%s", r.text)
	}

	// The edge list of a threaded bolt stays quick (it is timed in the notes).
	newDoc(t, cs, "E2EBolt")
	must(t, call(t, cs, "execute_code", map[string]any{"include_screenshot": false, "code": "import Part\nd = FreeCAD.getDocument('E2EBolt')\n" +
		"s = Part.Shape()\ns.importBrep(" + pyQuote(strings.ReplaceAll(dir, "\\", "/")+"/bolt.brep") + ")\nd.addObject('Part::Feature', 'Bolt').Shape = s\nd.recompute()\nprint('bolt faces', len(s.Faces), 'edges', len(s.Edges))"}), "bolt faces")
	start := time.Now()
	e := call(t, cs, "list_subelements", map[string]any{"doc_name": "E2EBolt", "obj_name": "Bolt", "kind": "edges"})
	must(t, e)
	t.Logf("TIMING list_subelements edges on the bolt: %v", time.Since(start))
}

func pyQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `\x27`) + "'" }

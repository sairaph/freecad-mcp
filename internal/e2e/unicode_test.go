//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// text holds characters outside every Windows code page FreeCAD's console could use: the sign
// and arrow of the real failures, an accented letter and a CJK character.
const text = "café ≥ → 日本"

// nonASCIIScript writes a script into a folder with a non-ASCII name. The script prints text, its
// own __file__ and the content of a sibling file that also has a non-ASCII name.
func nonASCIIScript(t *testing.T) (script, sibling string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "dossier café ≥ 日本")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sibling = filepath.Join(dir, "données ≥.txt")
	if err := os.WriteFile(sibling, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	script = filepath.Join(dir, "script café.py")
	source := "print('text', " + `"` + text + `"` + ")\nprint('file', __file__)\n" +
		"print('read', open(" + pyPath(sibling) + ", encoding='utf-8').read())\n"
	if err := os.WriteFile(script, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	return script, sibling
}

// pyPath renders a path as a Python raw string literal.
func pyPath(p string) string { return `r"` + p + `"` }

// TestHeadlessPrintsAndReadsNonASCII: headless scripts print non-ASCII text, run from a file with a
// non-ASCII path, see that path in __file__ and read a file with a non-ASCII name.
func TestHeadlessPrintsAndReadsNonASCII(t *testing.T) {
	cs := session(t)
	must(t, call(t, cs, "execute_code_headless", map[string]any{"code": "print(" + `"` + text + `"` + ")", "timeout": 120}),
		"exit 0", text)

	script, _ := nonASCIIScript(t)
	r := call(t, cs, "execute_code_headless", map[string]any{"path": script, "timeout": 120})
	must(t, r, "exit 0", "text "+text, "file "+script, "read "+text)

	bg := call(t, cs, "execute_code_headless", map[string]any{"path": script, "background": true})
	must(t, bg, "job_id:")
	deadline := time.Now().Add(60 * time.Second)
	for {
		st := call(t, cs, "get_async_status", map[string]any{"job_id": frontValue(bg.text, "job_id")})
		if strings.Contains(st.text, "finished") || strings.Contains(st.text, "failed") {
			must(t, st, "text "+text, "file "+script, "read "+text)
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("background job did not finish: %s", st.text)
		}
		time.Sleep(time.Second)
	}
}

// TestGUIScriptsPrintNonASCII: execute_code and execute_code_async capture non-ASCII output and run
// a file with a non-ASCII path.
func TestGUIScriptsPrintNonASCII(t *testing.T) {
	cs := session(t)
	must(t, call(t, cs, "execute_code", map[string]any{"code": "print(" + `"` + text + `"` + ")", "include_screenshot": false}), text)

	script, _ := nonASCIIScript(t)
	must(t, call(t, cs, "execute_code", map[string]any{"path": script, "include_screenshot": false}),
		"text "+text, "file "+script, "read "+text)

	// An async job returns no output, so it writes what it saw to a file with a non-ASCII name.
	out := filepath.Join(filepath.Dir(script), "résultat ≥.txt")
	code := "import sys\nopen(" + pyPath(out) + ", 'w', encoding='utf-8').write(" + `"` + text + `"` + " + ' ' + __file__)\n"
	if err := os.WriteFile(filepath.Join(filepath.Dir(script), "async café.py"), []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	async := call(t, cs, "execute_code_async", map[string]any{"path": filepath.Join(filepath.Dir(script), "async café.py")})
	must(t, async, "job_id:")
	deadline := time.Now().Add(30 * time.Second)
	for {
		if data, err := os.ReadFile(out); err == nil {
			if want := text + " " + filepath.Join(filepath.Dir(script), "async café.py"); string(data) != want {
				t.Fatalf("the async job wrote %q, want %q", data, want)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the async job wrote no file: %s", call(t, cs, "get_async_status", map[string]any{"job_id": frontValue(async.text, "job_id")}).text)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

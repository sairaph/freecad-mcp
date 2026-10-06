package mcpserver

import (
	"strings"
	"testing"

	"github.com/sairaph/freecad-mcp/internal/domain"
)

func TestUnknownArgumentNamesTheClosestValidOne(t *testing.T) {
	cs := session(t, domain.Settings{Host: "127.0.0.1", Port: 9})
	cases := []struct {
		tool string
		args map[string]any
		want string
	}{
		// The real case: the name the References format uses.
		{"get_object", map[string]any{"doc_name": "D", "object_name": "Box"}, "get_object has no argument object_name; did you mean obj_name?"},
		{"get_object", map[string]any{"doc_name": "D", "obj_name": "Box", "object": "Box"}, "get_object has no argument object; did you mean obj_name?"},
		{"delete_object", map[string]any{"doc_name": "D", "obj_name": "Box", "object_name": "x"}, "delete_object has no argument object_name; did you mean obj_name?"},
		// A typo, by edit distance.
		{"export_document", map[string]any{"doc_name": "D", "path": "/x.stl", "overwite": true}, "export_document has no argument overwite; did you mean overwrite?"},
		{"check_printability", map[string]any{"doc_name": "D", "bed_x": 100, "bed_y": 100, "bed_zz": 5}, "check_printability has no argument bed_zz; did you mean bed_z?"},
	}
	for _, c := range cases {
		res := call(t, cs, c.tool, c.args)
		text := replyText(res)
		if !res.IsError || !strings.Contains(text, "code: invalid_input") || !strings.Contains(text, c.want) {
			t.Errorf("%s %v: reply lacks %q:\n%s", c.tool, c.args, c.want, text)
		}
	}
}

// A name that is wrong inside an argument is not one of the tool's own argument names, so it gets
// no suggestion from them.
func TestUnknownNestedNameGetsNoTopLevelSuggestion(t *testing.T) {
	cs := session(t, domain.Settings{Host: "127.0.0.1", Port: 9})
	res := call(t, cs, "measure", map[string]any{"doc_name": "D", "kind": "distance",
		"refs": []map[string]any{{"object": "Box", "sub": "Face1", "obj_name": "Box"}}})
	text := replyText(res)
	if !res.IsError || strings.Contains(text, "did you mean") {
		t.Errorf("reply = %s", text)
	}
}

func TestUnknownArgumentFarFromEveryNameGetsNoGuess(t *testing.T) {
	cs := session(t, domain.Settings{Host: "127.0.0.1", Port: 9})
	res := call(t, cs, "get_object", map[string]any{"doc_name": "D", "obj_name": "Box", "zzzzzz": 1})
	text := replyText(res)
	if !res.IsError || strings.Contains(text, "did you mean") || !strings.Contains(text, "zzzzzz") {
		t.Errorf("reply = %s", text)
	}
}

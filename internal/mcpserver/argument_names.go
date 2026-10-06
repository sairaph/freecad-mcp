package mcpserver

// "Did you mean": an agent that sends an argument name a tool does not have is
// told the closest name the tool does have.

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"
)

// toolArguments maps a tool name to its argument names, filled as tools are
// registered. Every server registers the same names, so concurrent servers
// store equal values.
var toolArguments sync.Map

func rememberArguments(tool string, schema *jsonschema.Schema) {
	names := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		names = append(names, name)
	}
	sort.Strings(names)
	toolArguments.Store(tool, names)
}

// argumentSynonyms are names agents use for an argument under another name,
// each with the names of ours they stand for, best first. The References
// format of measure and update_object calls an object "object"; other tools
// of other servers say "document", "file" or "script".
var argumentSynonyms = map[string][]string{
	"object_name":   {"obj_name"},
	"object":        {"obj_name"},
	"obj":           {"obj_name"},
	"name":          {"obj_name", "doc_name"},
	"objname":       {"obj_name"},
	"document":      {"doc_name"},
	"document_name": {"doc_name"},
	"doc":           {"doc_name"},
	"docname":       {"doc_name"},
	"file":          {"path"},
	"file_path":     {"path"},
	"filepath":      {"path"},
	"filename":      {"path"},
	"script":        {"code", "path"},
	"source":        {"code"},
	"properties":    {"obj_properties"},
	"props":         {"obj_properties"},
	"type":          {"obj_type"},
	"object_type":   {"obj_type"},
}

// unexpectedProperties matches the SDK's error for arguments a tool does not
// have; the names follow as a quoted list.
var unexpectedProperties = regexp.MustCompile(`^unexpected additional properties \[(.*)\]$`)

var quotedName = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)

// unknownArgumentAdvice rewrites the SDK's "unexpected additional properties"
// problem of tool into one sentence per name that says which valid name is
// meant when one is close. Any other problem is returned as it is.
func unknownArgumentAdvice(tool, problem string) string {
	m := unexpectedProperties.FindStringSubmatch(problem)
	if m == nil {
		return problem
	}
	valid, _ := toolArguments.Load(tool)
	names, _ := valid.([]string)
	var sentences []string
	for _, quoted := range quotedName.FindAllString(m[1], -1) {
		name, err := strconv.Unquote(quoted)
		if err != nil {
			return problem
		}
		sentence := fmt.Sprintf("%s has no argument %s", tool, name)
		if guess := closestArgument(name, names); guess != "" {
			sentence += "; did you mean " + guess + "?"
		} else {
			sentence += "."
		}
		sentences = append(sentences, sentence)
	}
	if len(sentences) == 0 {
		return problem
	}
	return strings.Join(sentences, " ")
}

// closestArgument is the name in valid that name most likely stands for: a
// known synonym the tool has, else the nearest by edit distance when that is
// small next to the name's length. "" means no good guess.
func closestArgument(name string, valid []string) string {
	has := make(map[string]bool, len(valid))
	for _, v := range valid {
		has[v] = true
	}
	for _, candidate := range argumentSynonyms[strings.ToLower(name)] {
		if has[candidate] {
			return candidate
		}
	}
	best, bestDistance := "", -1
	for _, v := range valid {
		d := editDistance(strings.ToLower(name), v)
		if bestDistance < 0 || d < bestDistance {
			best, bestDistance = v, d
		}
	}
	// A third of the longer name may differ, at least one letter and at most three.
	longest := max(len(name), len(best))
	if bestDistance < 0 || bestDistance > min(3, max(1, longest/3)) {
		return ""
	}
	return best
}

// editDistance is the Levenshtein distance between a and b.
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

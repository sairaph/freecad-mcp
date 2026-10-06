package mcpserver

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sairaph/freecad-mcp/internal/domain"
	"github.com/sairaph/freecad-mcp/internal/headless"
	"github.com/sairaph/freecad-mcp/internal/xmlrpc/xmlrpctest"
)

func TestFailedToolCallIsLogged(t *testing.T) {
	fc := addon(t, map[string]xmlrpctest.Handler{
		"edit_object": func([]any) (any, error) {
			return map[string]any{"success": false, "error": "Object 'Nope' not found"}, nil
		},
	})
	cs := session(t, settingsFor(fc))
	long := strings.Repeat("x", 500)
	call(t, cs, "update_object", map[string]any{"doc_name": "D", "obj_name": long, "obj_properties": map[string]any{"Length": 5}})

	lines, err := RecentErrors(10)
	if err != nil || len(lines) == 0 {
		t.Fatalf("error log = %v, %v", lines, err)
	}
	line := lines[len(lines)-1]
	for _, want := range []string{
		"tool=update_object", "code=freecad_error", `message="Failed to update object: Object 'Nope' not found"`, "hint=", `client="test on `, "session=",
		`"doc_name":"D"`, `"obj_properties":"{\"Length\":5}"`,
	} {
		if !strings.Contains(line, want) {
			t.Errorf("log line lacks %s:\n%s", want, line)
		}
	}
	// The hint the addon built repeats the name in full; the argument summary cuts it.
	args := line[strings.LastIndex(line, " args="):]
	if strings.Contains(args, long) || !strings.Contains(args, `"`+strings.Repeat("x", errorLogArgChars)+`"`) {
		t.Errorf("an argument value was not cut to %d characters:\n%s", errorLogArgChars, args)
	}
	if strings.Count(line, "\n") != 0 {
		t.Errorf("log entry spans lines: %q", line)
	}
	if _, err := time.Parse("2006-01-02T15:04:05Z", strings.Fields(line)[0]); err != nil {
		t.Errorf("log line does not start with a UTC time: %v", err)
	}
}

func TestSuccessfulToolCallIsNotLogged(t *testing.T) {
	before, _ := RecentErrors(1000)
	fc := addon(t, nil)
	cs := session(t, settingsFor(fc))
	call(t, cs, "get_rpc_status", nil)
	after, _ := RecentErrors(1000)
	if len(after) != len(before) {
		t.Fatalf("a call that succeeded was logged: %v", after[len(before):])
	}
}

func TestRefusedArgumentsAreLogged(t *testing.T) {
	fc := addon(t, nil)
	cs := session(t, settingsFor(fc))
	call(t, cs, "get_view", map[string]any{"view_name": "Sideways"})
	lines, _ := RecentErrors(1)
	if len(lines) != 1 || !strings.Contains(lines[0], "tool=get_view") || !strings.Contains(lines[0], "code=invalid_input") ||
		!strings.Contains(lines[0], `"view_name":"Sideways"`) {
		t.Fatalf("log = %v", lines)
	}
}

// lastLogLine is the newest line of the error log.
func lastLogLine(t *testing.T) string {
	t.Helper()
	lines, err := RecentErrors(1)
	if err != nil || len(lines) != 1 {
		t.Fatalf("error log = %v, %v", lines, err)
	}
	return lines[0]
}

func TestRefusedArgumentsAreLoggedAsTheAgentReceivedThem(t *testing.T) {
	cs := session(t, domain.Settings{Host: "127.0.0.1", Port: 9})
	res := call(t, cs, "get_object", map[string]any{"doc_name": "D", "object_name": "Box"})
	line := lastLogLine(t)
	for _, want := range []string{
		"tool=get_object", "code=invalid_input",
		`message="Invalid arguments: get_object has no argument object_name; did you mean obj_name?"`,
		`hint="Call get_object again with arguments that match its input schema`,
	} {
		if !strings.Contains(line, want) {
			t.Errorf("log line lacks %s:\n%s", want, line)
		}
	}
	if !strings.Contains(allText(res), "did you mean obj_name?") {
		t.Errorf("the agent was not told: %s", allText(res))
	}
}

func TestFailedHeadlessScriptLogsItsExceptionLine(t *testing.T) {
	py := pythonCommand(t)
	scriptDir := t.TempDir()
	old := headless.ScriptDir
	headless.ScriptDir = func() (string, error) { return scriptDir, nil }
	t.Cleanup(func() { headless.ScriptDir = old })
	cs := session(t, domain.Settings{Host: "127.0.0.1", Port: 9, FreecadCmd: py})

	call(t, cs, "execute_code_headless", map[string]any{"code": "a = 1\nraise ValueError('Null shape ' + 'x' * 400)"})
	line := lastLogLine(t)
	want := `cause="ValueError: Null shape ` + strings.Repeat("x", 300-len("ValueError: Null shape ")) + `"`
	if !strings.Contains(line, "tool=execute_code_headless") || !strings.Contains(line, want) {
		t.Errorf("the exception line is missing or not cut to %d characters:\n%s", errorLogCauseChars, line)
	}

	call(t, cs, "execute_code_headless", map[string]any{"code": "import sys\nsys.exit(3)"})
	if line := lastLogLine(t); strings.Contains(line, "cause=") {
		t.Errorf("a script that raised nothing has a cause:\n%s", line)
	}
}

func TestFailedAsyncJobIsLoggedOnceWithItsException(t *testing.T) {
	fc := addon(t, map[string]xmlrpctest.Handler{
		"get_async_status": func([]any) (any, error) {
			return map[string]any{"success": true, "job": map[string]any{
				"id": "job-log-7", "state": "failed", "error": "ValueError: boom",
				"traceback": "Traceback (most recent call last):\n  File \"<string>\", line 1, in <module>\nValueError: boom"}}, nil
		},
	})
	cs := session(t, settingsFor(fc))
	before, _ := RecentErrors(1000)
	for range 3 {
		call(t, cs, "get_async_status", map[string]any{"job_id": "job-log-7"})
	}
	after, _ := RecentErrors(1000)
	if len(after) != len(before)+1 {
		t.Fatalf("polling a failed job logged %d lines, want 1: %v", len(after)-len(before), after[len(before):])
	}
	line := after[len(after)-1]
	for _, want := range []string{"tool=get_async_status", "code=job_failed", `message="Async job job-log-7: failed"`, `cause="ValueError: boom"`} {
		if !strings.Contains(line, want) {
			t.Errorf("log line lacks %s:\n%s", want, line)
		}
	}
}

// A call that fails after it moved to the background is logged when it ends; reading its kept reply
// with get_async_status, as often as the agent likes, adds nothing.
func TestFailedBackgroundCallIsLoggedOnceHoweverOftenItIsPolled(t *testing.T) {
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	cs, _ := backgroundServer(t, 100*time.Millisecond, release)
	before, _ := RecentErrors(1000)

	id := callJobID.FindString(allText(call(t, cs, "execute_code", map[string]any{"code": "boom", "include_screenshot": false})))
	if id == "" {
		t.Fatal("the call did not move to the background")
	}
	close(release)
	finished := waitForReply(t, cs, id)
	if !finished.IsError {
		t.Fatalf("the finished reply is not the call's error: %s", allText(finished))
	}
	for range 2 {
		call(t, cs, "get_async_status", map[string]any{"job_id": id})
	}

	after, _ := RecentErrors(1000)
	logged := after[len(before):]
	if len(logged) != 1 || !strings.Contains(logged[0], "tool=execute_code") || !strings.Contains(logged[0], "NameError") {
		t.Fatalf("the failed call and its polls logged %d lines, want the call's one:\n%s", len(logged), strings.Join(logged, "\n"))
	}
}

func TestSecretArgumentsAreNeverLogged(t *testing.T) {
	got := argumentSummary([]byte(`{"auth_token": "hunter2", "Password": "hunter3", "code": "print(1)"}`))
	if strings.Contains(got, "hunter") || !strings.Contains(got, `"code":"print(1)"`) {
		t.Fatalf("summary = %s", got)
	}
}

func TestErrorLogRotatesPastFiveMegabytes(t *testing.T) {
	path, err := ErrorLogPath()
	if err != nil {
		t.Fatal(err)
	}
	os.Remove(path)
	os.Remove(path + ".1")
	appendErrorLog("first")
	if err := os.WriteFile(path, []byte(strings.Repeat("y", errorLogMaxBytes+1)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	appendErrorLog("second")
	appendErrorLog("third")

	old, err := os.Stat(path + ".1")
	if err != nil || old.Size() != errorLogMaxBytes+2 {
		t.Fatalf("rotated copy = %v, %v", old, err)
	}
	lines, err := RecentErrors(2)
	if err != nil || strings.Join(lines, "|") != "second|third" {
		t.Fatalf("recent = %v, %v", lines, err)
	}
	// The rotated copy is read first, oldest entries first, when the current file is short.
	lines, _ = RecentErrors(3)
	if len(lines) != 3 || lines[1] != "second" {
		t.Fatalf("recent across the rotation = %d lines", len(lines))
	}
}

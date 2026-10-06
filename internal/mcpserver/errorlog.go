package mcpserver

// The error log: one line per failed tool call, kept across sessions so the
// person running the agents can review what went wrong (`freecad-mcp errors`).

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sairaph/freecad-mcp/internal/domain"
	"github.com/sairaph/freecad-mcp/internal/headless"
	"github.com/sairaph/freecad-mcp/internal/xmlrpc"
	"gopkg.in/yaml.v3"
)

const (
	errorLogName = "errors.log"
	// errorLogMaxBytes is where the log is rotated to errors.log.1, replacing
	// the earlier copy: an anti-break guard so the file cannot grow without end.
	errorLogMaxBytes = 5 << 20
	// errorLogArgChars cuts each argument value in a log line.
	errorLogArgChars = 200
	// errorLogCauseChars cuts the exception line of a failed script.
	errorLogCauseChars = 300
)

// secretArgument matches the names of arguments whose values are never logged.
var secretArgument = regexp.MustCompile(`(?i)token|password|secret`)

// ErrorLogPath is the error log file: errors.log in freecad-mcp's cache
// directory.
func ErrorLogPath() (string, error) {
	dir, err := headless.CacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, errorLogName), nil
}

var errorLogMu sync.Mutex

// appendErrorLog appends line to the error log, rotating it first when it is
// over errorLogMaxBytes. A failure to write is ignored: the log never fails
// the tool call it describes.
func appendErrorLog(line string) {
	path, err := ErrorLogPath()
	if err != nil {
		return
	}
	errorLogMu.Lock()
	defer errorLogMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	if info, err := os.Stat(path); err == nil && info.Size() > errorLogMaxBytes {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line + "\n")
}

// RecentErrors returns the last n lines of the error log, oldest first: the
// rotated copy is read before the current file.
func RecentErrors(n int) ([]string, error) {
	path, err := ErrorLogPath()
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, p := range []string{path + ".1", path} {
		f, err := os.Open(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		r := bufio.NewReader(f)
		for {
			line, err := r.ReadString('\n')
			if line = strings.TrimRight(line, "\r\n"); line != "" {
				lines = append(lines, line)
			}
			if err != nil {
				break
			}
		}
		f.Close()
	}
	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}

// logToolErrors is the receiving middleware that logs every failed tools/call:
// an error reply, an argument error the SDK refused, or a Go error. It sits
// innermost, so a call that moved to the background is logged when it ends and
// the session identity is already in its context.
func logToolErrors(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		result, err := next(ctx, method, req)
		call, ok := req.(*mcp.CallToolRequest)
		if method != "tools/call" || !ok || call.Params == nil {
			return result, err
		}
		tool := call.Params.Name
		code, message, hint, cause := "", "", "", ""
		switch res, _ := result.(*mcp.CallToolResult); {
		case err != nil:
			code, message = "protocol_error", err.Error()
		case res != nil && res.IsError:
			if tool == "get_async_status" && isFinishedCallReply(res) {
				// The call's own failure was logged when it ended.
				return result, err
			}
			code, message, hint = errorOfResult(tool, res)
			cause = exceptionLine(replyText(res))
		case res != nil && tool == "get_async_status":
			var failed bool
			if message, failed = newlyFailedJob(res); !failed {
				return result, err
			}
			code, cause = "job_failed", exceptionLine(replyText(res))
		default:
			return result, err
		}
		headers := xmlrpc.HeadersFrom(ctx)
		appendErrorLog(errorLogLine(time.Now(), headers[domain.HeaderSession], headers[domain.HeaderClient],
			tool, code, message, hint, cause, call.Params.Arguments))
		return result, err
	}
}

// errorOfResult reads the code, message and hint of an error reply: from the
// front matter render.ErrorResult wrote, or, for arguments the SDK refused,
// from the error the agent receives in its place (invalidArguments swaps it in
// after this middleware has run).
func errorOfResult(tool string, res *mcp.CallToolResult) (code, message, hint string) {
	if e := res.GetError(); e != nil {
		replaced := invalidArgumentError(tool, e)
		return replaced.Code, replaced.Message, replaced.Hint
	}
	for _, c := range res.Content {
		tc, ok := c.(*mcp.TextContent)
		if !ok {
			continue
		}
		front, _, ok := splitFrontMatter(tc.Text)
		if !ok {
			return "", shortErrorText(tc.Text), ""
		}
		var doc struct {
			Error struct {
				Code    string `yaml:"code"`
				Message string `yaml:"message"`
				Hint    string `yaml:"hint"`
			} `yaml:"error"`
		}
		if yaml.Unmarshal([]byte(front), &doc) == nil && doc.Error.Code != "" {
			return doc.Error.Code, doc.Error.Message, doc.Error.Hint
		}
		return "", shortErrorText(tc.Text), ""
	}
	return "", "", ""
}

// isFinishedCallReply reports whether res is the kept reply of a call that
// moved to the background and has ended (withJobFront puts the job id and
// state finished in its front matter): get_async_status hands it out again on
// every poll, though the failure it holds was logged once, with the call.
func isFinishedCallReply(res *mcp.CallToolResult) bool {
	front, _, ok := splitFrontMatter(replyText(res))
	if !ok {
		return false
	}
	var doc struct {
		JobID string `yaml:"job_id"`
		State string `yaml:"state"`
	}
	return yaml.Unmarshal([]byte(front), &doc) == nil && isCallJobID(doc.JobID) && doc.State == "finished"
}

// splitFrontMatter cuts the YAML front matter render writes off the body of a
// reply; ok is false for text that has none.
func splitFrontMatter(text string) (front, body string, ok bool) {
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return "", text, false
	}
	front, body, _ = strings.Cut(rest, "\n---")
	return front, strings.TrimPrefix(body, "\n"), true
}

// replyText joins the text blocks of a reply.
func replyText(res *mcp.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// loggedJobs holds the ids of failed jobs already in the error log, so polling
// a failed job again does not log it again.
var loggedJobs sync.Map

// newlyFailedJob says whether a get_async_status reply reports a job that
// failed and was not logged before, and gives the line that states it. A
// failed script is otherwise invisible to the log: the status call itself
// succeeds.
func newlyFailedJob(res *mcp.CallToolResult) (message string, failed bool) {
	for _, c := range res.Content {
		tc, ok := c.(*mcp.TextContent)
		if !ok {
			continue
		}
		front, body, ok := splitFrontMatter(tc.Text)
		if !ok {
			continue
		}
		var doc struct {
			JobID    string `yaml:"job_id"`
			State    string `yaml:"state"`
			ExitCode *int   `yaml:"exit_code"`
			Crashed  bool   `yaml:"crashed"`
			TimedOut bool   `yaml:"timed_out"`
		}
		if yaml.Unmarshal([]byte(front), &doc) != nil || doc.JobID == "" {
			continue
		}
		failed = doc.State == "failed" || doc.Crashed || doc.TimedOut ||
			(doc.State == "finished" && doc.ExitCode != nil && *doc.ExitCode != 0)
		if !failed {
			return "", false
		}
		if _, seen := loggedJobs.LoadOrStore(doc.JobID, true); seen {
			return "", false
		}
		line, _, _ := strings.Cut(strings.TrimSpace(body), "\n")
		return shortErrorText(line), true
	}
	return "", false
}

// exceptionLine is the exception that ends the last Python traceback in text,
// cut to errorLogCauseChars, or "" when there is none: the line after the
// traceback's indented frames, such as "ValueError: Null shape".
func exceptionLine(text string) string {
	i := strings.LastIndex(text, tracebackMarker)
	if i < 0 {
		return ""
	}
	for _, line := range strings.Split(text[i+len(tracebackMarker):], "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || line[0] == ' ' || line[0] == '\t' {
			continue
		}
		return capRunes(line, errorLogCauseChars)
	}
	return ""
}

const tracebackMarker = "Traceback (most recent call last):"

func shortErrorText(s string) string { return capRunes(s, 500) }

// errorLogLine renders one log line: UTC time, session id, client label,
// tool, error code, message, hint, the exception that ended a failed script
// (cause, only when there is one) and an argument summary. Text fields are
// quoted, so a line never spans lines.
func errorLogLine(at time.Time, session, client, tool, code, message, hint, cause string, arguments json.RawMessage) string {
	if code == "" {
		code = "unknown"
	}
	if cause != "" {
		cause = " cause=" + strconv.Quote(cause)
	}
	return fmt.Sprintf("%s session=%s client=%s tool=%s code=%s message=%s hint=%s%s args=%s",
		at.UTC().Format("2006-01-02T15:04:05Z"), orDash(session), strconv.Quote(client), orDash(tool), code,
		strconv.Quote(message), strconv.Quote(hint), cause, argumentSummary(arguments))
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// argumentSummary lists a call's argument names with their values cut to
// errorLogArgChars characters, as one JSON object; the value of an argument
// named like a secret is left out.
func argumentSummary(arguments json.RawMessage) string {
	var args map[string]any
	if len(arguments) == 0 || json.Unmarshal(arguments, &args) != nil {
		return "{}"
	}
	summary := make(map[string]string, len(args))
	for name, value := range args {
		if secretArgument.MatchString(name) {
			summary[name] = "(hidden)"
			continue
		}
		text, ok := value.(string)
		if !ok {
			b, _ := json.Marshal(value)
			text = string(b)
		}
		summary[name] = capRunes(text, errorLogArgChars)
	}
	b, _ := json.Marshal(summary)
	return string(b)
}

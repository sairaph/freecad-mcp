package main

// The connection check of the app and `check-connection`, and the doctor's
// RPC server check: both ask the FreeCAD the MCP server would use, on this
// machine or through a listener on another. Through a listener, FreeCAD not
// running there is reported the same way as locally not started: a warning
// with the advice to start it, never a failure.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/sairaph/mcp-wizard/doctor"

	"github.com/sairaph/freecad-mcp/internal/domain"
	"github.com/sairaph/freecad-mcp/internal/freecad"
	"github.com/sairaph/freecad-mcp/internal/xmlrpc"
)

// connectionReport checks the RPC server the MCP server would use and prints
// the results the way the doctor does. It returns 1 when a check failed.
func connectionReport(ctx context.Context, w io.Writer) int {
	results, code := connectionResults(ctx)
	for _, r := range results {
		tag := map[doctor.Status]string{doctor.OK: "[ok]", doctor.Warn: "[warn]", doctor.Fail: "[fail]"}[r.Status]
		fmt.Fprintf(w, "  %-6s %s\n         %s\n", tag, r.Name, r.Detail)
	}
	return code
}

// connectionResults checks the RPC server the MCP server would use: one
// result for the server, and one for whether the addon matches this server
// when it answered. The code is 1 when a result failed.
func connectionResults(ctx context.Context) ([]doctor.Result, int) {
	const name = "FreeCAD RPC server"
	settings, err := serverSettings(ctx)
	if err != nil {
		return []doctor.Result{{Name: name, Status: doctor.Fail, Detail: err.Error()}}, 1
	}
	// 10 s, not less: right after FreeCAD starts its GUI thread is still busy
	// with startup Python, and a shorter bound was seen to time out here
	// while the same ping succeeded a moment later.
	conn := freecad.NewConnection(settings.Host, settings.Port, settings.Token, 10*time.Second)
	defer conn.Close()
	ok, err := conn.Ping(ctx)
	if err != nil || !ok {
		if down := listenerFreeCADDown(err); down {
			return []doctor.Result{{Name: name, Status: doctor.Warn,
				Detail: fmt.Sprintf("FreeCAD is not running on %s; agents start it with start_freecad", settings.Host)}}, 0
		}
		if fault, unreadable := settingsUnreadableFault(err); unreadable {
			// FreeCAD is running (it answered with a fault, not silence or a
			// refused connection); the fault's own text already carries the
			// "save them again" advice (live check L4).
			return []doctor.Result{{Name: name, Status: doctor.Fail, Detail: fault}}, 1
		}
		return []doctor.Result{{Name: name, Status: doctor.Fail,
			Detail: fmt.Sprintf("%s: %s", conn.URL(), rpcProblem(err))}}, 1
	}
	results := []doctor.Result{{Name: name, Status: doctor.OK, Detail: conn.URL() + " answers"}}
	if warning, _ := conn.CheckAddonVersion(ctx, version); warning != "" {
		return append(results, doctor.Result{Name: "FreeCAD addon", Status: doctor.Warn, Detail: warning}), 0
	}
	return append(results, doctor.Result{Name: "FreeCAD addon", Status: doctor.OK, Detail: "matches this server"}), 0
}

func rpcProblem(err error) string {
	if err == nil {
		return "it did not answer ping"
	}
	msg := err.Error()
	if strings.Contains(msg, "401") {
		return "FreeCAD asks for a password; it is set in `" + domain.BinaryName + "` > Share this PC on the computer running FreeCAD"
	}
	return msg + " (is FreeCAD running with the RPC server started?)"
}

// listenerFreeCADDown reports whether err is a listener's 503 saying FreeCAD
// is not running there (domain.ListenerFreeCADDown on the /RPC2 reply):
// unreachable in the ordinary sense, but expected whenever nothing has
// started FreeCAD there yet, so it is never a failure.
func listenerFreeCADDown(err error) bool {
	var perr *xmlrpc.ProtocolError
	return errors.As(err, &perr) && perr.Listener == domain.ListenerFreeCADDown
}

// settingsUnreadableFault reports whether err is the addon's settings file
// being unreadable, and its own message when it is: FreeCAD answered (a
// fault, not silence or a refused connection), so this is never "not
// running" (live check L4).
func settingsUnreadableFault(err error) (string, bool) {
	var fault *xmlrpc.Fault
	if errors.As(err, &fault) && fault.SettingsUnreadable() {
		return fault.String, true
	}
	return "", false
}

type rpcCheck struct{}

func (rpcCheck) Name() string { return "FreeCAD RPC server" }

func (c rpcCheck) Run(ctx context.Context) doctor.Result {
	settings, err := serverSettings(ctx)
	if err != nil {
		return doctor.Result{Name: c.Name(), Status: doctor.Fail, Detail: err.Error()}
	}
	// 10 s: see connectionReport's comment on the same bound.
	conn := freecad.NewConnection(settings.Host, settings.Port, settings.Token, 10*time.Second)
	defer conn.Close()
	conn.VersionCheckTimeout = 10 * time.Second
	ok, err := conn.Ping(ctx)
	if err != nil || !ok {
		if listenerFreeCADDown(err) {
			return doctor.Result{Name: c.Name(), Status: doctor.Warn,
				Detail: fmt.Sprintf("FreeCAD is not running on %s; agents start it with start_freecad", settings.Host)}
		}
		if fault, unreadable := settingsUnreadableFault(err); unreadable {
			return doctor.Result{Name: c.Name(), Status: doctor.Fail, Detail: fault}
		}
		// FreeCAD not running is normal when nothing is being modelled.
		return doctor.Result{Name: c.Name(), Status: doctor.Warn, Detail: fmt.Sprintf("%s: %s", conn.URL(), rpcProblem(err))}
	}
	if warning, _ := conn.CheckAddonVersion(ctx, version); warning != "" {
		return doctor.Result{Name: c.Name(), Status: doctor.Warn, Detail: warning}
	}
	return doctor.Result{Name: c.Name(), Status: doctor.OK, Detail: conn.URL() + " answers; versions match"}
}

package main

// The install wizard's "FreeCAD was not found" choice: install FreeCAD
// first (the wizard ends with exitCancelled), or use FreeCAD on another
// computer (host, test, password, test again). The connection is saved by
// applyConnectChoice after registration.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/sairaph/mcp-wizard/flow"
	"github.com/sairaph/mcp-wizard/tui"

	"github.com/sairaph/freecad-mcp/internal/domain"
	"github.com/sairaph/freecad-mcp/internal/listenerapi"
	"github.com/sairaph/freecad-mcp/internal/remote"
)

// connectPhase is the sub-screen of connectHostStep.
type connectPhase int

const (
	connectHostPhase connectPhase = iota
	connectPasswordPhase
)

// connectState is the "FreeCAD on another computer" choice. The password is
// held in memory only, until applyConnectChoice.
type connectState struct {
	// InstallFirst is set when the user chose "Install FreeCAD on this
	// computer first": runWizard prints installFirstText and exits with
	// exitCancelled.
	InstallFirst bool
	// Chosen is set when the user chose "Use FreeCAD on another computer"
	// and the connection was tested; the addon is then not installed.
	Chosen   bool
	Host     string
	Port     int
	Password string

	// The rest is connectHostStep's own view state; it does not survive
	// past the wizard.
	phase connectPhase
	// hostInput, portInput and passwordInput are the text fields; Host, Port
	// and Password follow them. onPort is true while tab has moved the focus
	// to the Port field on the host screen.
	hostInput     textinput.Model
	portInput     textinput.Model
	passwordInput textinput.Model
	onPort        bool
	// hostErr is the inline error under the Host or Port field.
	hostErr   string
	errOnPort bool
	testing   bool
	// message is the current [ok]/[warn]/[fail] line(s) (joined by "\n"),
	// or "" before the first test.
	message string
	// ready is set once a test succeeded; enter then moves on.
	ready bool
}

// connectSteps are the steps after the addon step when FreeCAD was not found
// on this computer; each skips itself otherwise.
func connectSteps(ctx context.Context) []flow.Step[AppState] {
	return []flow.Step[AppState]{&connectHostStep{ctx: ctx}}
}

// connectSkipMsg is emitted by Init when this step does not apply, so
// Update can return flow.Skip (the flow.Step interface has no Init
// directive of its own).
type connectSkipMsg struct{}

// connectResultMsg carries the outcome of a testConnect call.
type connectResultMsg struct {
	result connectResult
}

type connectHostStep struct {
	ctx context.Context
}

func (s *connectHostStep) ID() string { return "freecad-connect" }

func (s *connectHostStep) Title(*AppState) string { return "FreeCAD on another computer" }

// Hints has no "q cancel": both phases are text-entry screens, where q
// types like any other rune (only ctrl+c quits; see Update).
func (s *connectHostStep) Hints(state *AppState) []struct{ Key, Label string } {
	c := &state.Connect
	if c.testing {
		return nil
	}
	if c.phase == connectPasswordPhase {
		label := "test again"
		if c.ready {
			label = "continue"
		}
		return []struct{ Key, Label string }{{"enter", label}, {"esc", "back"}}
	}
	label := "test connection"
	switch {
	case c.ready:
		label = "continue"
	case c.message != "":
		label = "retry"
	}
	return []struct{ Key, Label string }{{"tab", "switch field"}, {"enter", label}, {"esc", "back"}}
}

func (s *connectHostStep) Init(state *AppState) tea.Cmd {
	if state.Addon.Phase != addonNotFound {
		// FreeCAD was found this time (or is being located again): any
		// earlier "Use FreeCAD on another computer" choice no longer
		// applies, so finishWizard must not save it.
		state.Connect.Chosen = false
		return func() tea.Msg { return connectSkipMsg{} }
	}
	c := &state.Connect
	if c.Port == 0 {
		c.Port = domain.DefaultListenerPort
	}
	c.phase = connectHostPhase
	c.testing = false
	c.onPort = false
	c.hostErr = ""
	c.hostInput = shareInput("192.168.1.20 or a host name", false, c.Host)
	c.portInput = shareInput(strconv.Itoa(domain.DefaultListenerPort), false, strconv.Itoa(c.Port))
	c.portInput.Blur()
	c.passwordInput = shareInput("only if that computer set one", true, c.Password)
	return nil
}

func (s *connectHostStep) Update(msg tea.Msg, state *AppState) (flow.Directive, tea.Cmd) {
	c := &state.Connect
	switch m := msg.(type) {
	case connectSkipMsg:
		return flow.Skip, nil
	case connectResultMsg:
		c.testing = false
		applyConnectResult(c, m.result)
		return flow.Continue, nil
	case tea.KeyMsg:
		key := m.String()
		// Both phases are text-entry screens (host, password): only ctrl+c
		// quits here, so q (and every other rune) types into the field.
		if key == "ctrl+c" {
			return flow.Quit, nil
		}
		if c.testing {
			return flow.Continue, nil
		}
		if c.phase == connectPasswordPhase {
			return s.updatePassword(key, m, state)
		}
		return s.updateHost(key, m, state)
	}
	if tui.IsSpinMsg(msg) && c.testing {
		state.Spinner.Frame++
		return flow.Continue, tui.Spinner()
	}
	return flow.Continue, nil
}

func (s *connectHostStep) updateHost(key string, m tea.KeyMsg, state *AppState) (flow.Directive, tea.Cmd) {
	c := &state.Connect
	switch key {
	case "esc":
		state.Retreating = true
		c.Password = ""
		c.ready = false
		c.message = ""
		c.hostErr = ""
		c.Chosen = false
		return flow.Back, nil
	case "tab", "shift+tab":
		c.onPort = !c.onPort
		if c.onPort {
			c.hostInput.Blur()
			c.portInput.Focus()
		} else {
			c.portInput.Blur()
			c.hostInput.Focus()
		}
		return flow.Continue, nil
	case "enter":
		if c.ready {
			return flow.Next, nil
		}
		c.Host = strings.TrimSpace(c.hostInput.Value())
		if c.Host == "" {
			c.hostErr, c.errOnPort = "Enter a host first.", false
			return flow.Continue, nil
		}
		if err := domain.ValidateHost(c.Host); err != nil {
			c.hostErr, c.errOnPort = err.Error(), false
			return flow.Continue, nil
		}
		port, err := validatePort(c.portInput.Value())
		if err != nil {
			c.hostErr, c.errOnPort = err.Error(), true
			return flow.Continue, nil
		}
		c.Port = port
		c.hostErr = ""
		c.testing = true
		c.message = ""
		return flow.Continue, tea.Batch(tui.Spinner(), testConnectCmd(s.ctx, c.Host, c.Port, ""))
	}
	var cmd tea.Cmd
	field := &c.hostInput
	if c.onPort {
		field = &c.portInput
	}
	if c.onPort {
		var ok bool
		if m, ok = digitsOnly(m); !ok {
			return flow.Continue, nil
		}
	}
	before := field.Value()
	*field, cmd = field.Update(m)
	if field.Value() != before {
		c.ready = false
		c.message = ""
		c.hostErr = ""
	}
	return flow.Continue, cmd
}

func (s *connectHostStep) updatePassword(key string, m tea.KeyMsg, state *AppState) (flow.Directive, tea.Cmd) {
	c := &state.Connect
	switch key {
	case "esc":
		// One screen back: the host screen, secrets cleared (pitfall).
		c.phase = connectHostPhase
		c.Password = ""
		c.passwordInput.SetValue("")
		c.message = ""
		c.ready = false
		return flow.Continue, nil
	case "enter":
		if c.ready {
			return flow.Next, nil
		}
		c.Password = c.passwordInput.Value()
		if err := validatePassword(c.Password); err != nil {
			c.message = "[fail] " + err.Error()
			return flow.Continue, nil
		}
		c.testing = true
		c.message = ""
		return flow.Continue, tea.Batch(tui.Spinner(), testConnectCmd(s.ctx, c.Host, c.Port, c.Password))
	}
	var cmd tea.Cmd
	before := c.passwordInput.Value()
	c.passwordInput, cmd = c.passwordInput.Update(m)
	if c.passwordInput.Value() != before {
		c.Password = c.passwordInput.Value()
		c.ready = false
		c.message = ""
	}
	return flow.Continue, cmd
}

// applyConnectResult classifies a testConnect result by the listener
// marker, matching whatever the status code was, into the connectState's
// view fields.
func applyConnectResult(c *connectState, r connectResult) {
	switch {
	case r.PasswordRequired:
		c.ready = false
		if c.phase == connectHostPhase {
			c.phase = connectPasswordPhase
			c.message = ""
			return
		}
		c.message = passwordNotAcceptedLine
	case r.NotListener:
		c.phase = connectHostPhase
		c.ready = false
		c.message = notListenerLine(c.Host, c.Port)
	case r.Err != nil:
		c.ready = false
		c.message = connectErrLine(r.Err, c.Host, c.Port)
	default:
		lines := []string{connectSuccessLine(r.Status, c.Host, c.Port)}
		if w := protocolWarningLine(r.Status); w != "" {
			lines = append(lines, w)
		}
		c.message = strings.Join(lines, "\n")
		c.ready = true
		c.Chosen = true
	}
}

func connectSuccessLine(st listenerapi.Status, host string, port int) string {
	if st.RPC == listenerapi.RPCReachable {
		return fmt.Sprintf("[ok] freecad-mcp %s on %s answers. FreeCAD is running.", st.Version, hostPort(host, port))
	}
	return fmt.Sprintf(
		"[ok] freecad-mcp %s on %s answers. FreeCAD is not running; agents start it with start_freecad.",
		st.Version, hostPort(host, port))
}

// connectErrLine renders a testConnect failure that is neither
// PasswordRequired nor NotListener: a listener that answered but refused
// (marker present, *remote.Error) gets its own reason, not "Could not
// reach", which is reserved for a genuine network failure (nothing
// answered, or something without the marker fell through here).
func connectErrLine(err error, host string, port int) string {
	var refusal *remote.Error
	if errors.As(err, &refusal) {
		return connectRefusalLine(refusal, host)
	}
	return fmt.Sprintf(
		"[fail] Could not reach %s (%v). Check that \"Share this PC\" is on there, that its firewall "+
			"allows freecad-mcp, and that this computer's IP address is in its allowed list.",
		hostPort(host, port), err)
}

// connectRefusalLine renders a listener's own refusal. It matches on
// listenerapi.Error.Reason first (the exact refusal the listener wrote);
// the status code is only a fallback for a reply that somehow carries the
// marker without a Reason.
func connectRefusalLine(e *remote.Error, host string) string {
	reason := e.Body.Reason
	if reason == "" {
		switch e.StatusCode {
		case http.StatusForbidden:
			reason = listenerapi.ReasonRemoteOff
		case http.StatusTooManyRequests:
			reason = listenerapi.ReasonRateLimited
		}
	}
	switch reason {
	case listenerapi.ReasonRemoteOff:
		// Remote access is turned off there; the only 403 a plain status
		// test can reach (a 403 from the Origin check needs a browser).
		return fmt.Sprintf(
			"[fail] Remote access is off on %s. Turn on \"Share this PC\" in freecad-mcp on that computer.", host)
	case listenerapi.ReasonRateLimited:
		wait := 60 // the listener's own default when it sends no Retry-After.
		if e.RetryAfter > 0 {
			wait = e.RetryAfter
		}
		return fmt.Sprintf(
			"[fail] The listener on %s refuses this computer for %d s after too many wrong passwords. "+
				"Check the password, then test again after that.", host, wait)
	default:
		msg := strings.TrimSpace(e.Body.Error)
		if msg == "" {
			msg = e.Error()
		}
		if e.Body.Hint != "" {
			return "[fail] " + msg + " " + e.Body.Hint
		}
		return "[fail] " + msg
	}
}

// testConnectCmd runs testConnect off the UI goroutine.
func testConnectCmd(ctx context.Context, host string, port int, password string) tea.Cmd {
	return func() tea.Msg {
		return connectResultMsg{result: testConnect(ctx, host, port, password)}
	}
}

func (s *connectHostStep) View(state *AppState) string {
	theme := tui.DefaultTheme
	c := &state.Connect
	var b strings.Builder
	switch {
	case c.testing:
		fmt.Fprintf(&b, "  %s Testing the connection to %s...\n", tui.SpinFrame(state.Spinner.Frame), hostPort(c.Host, c.Port))
	case c.phase == connectPasswordPhase:
		b.WriteString(wizardParagraph(state, c.Host+" asks for a password: the one set in Share this PC on that computer.") + "\n")
		b.WriteString("   Password  " + c.passwordInput.View() + "\n")
		if c.message != "" {
			b.WriteString("\n" + wizardParagraph(state, c.message))
		}
		passwordEnterLabel := "test again"
		if c.ready {
			passwordEnterLabel = "continue"
		}
		b.WriteString(tui.Footer(theme, tui.Hints(theme,
			tui.Hint{Key: "enter", Label: passwordEnterLabel}, tui.Hint{Key: "esc", Label: "back"})))
	default:
		b.WriteString(wizardParagraph(state, "Enter the IP address or host name of the computer that runs FreeCAD. That computer needs freecad-mcp installed with Share this PC turned on.") + "\n")
		hostErr := func(onPort bool) {
			if c.hostErr != "" && c.errOnPort == onPort {
				b.WriteString(strings.Repeat(" ", 13) + newPalette().fail.Render(c.hostErr) + "\n")
			}
		}
		b.WriteString("   Host      " + c.hostInput.View() + "\n")
		hostErr(false)
		b.WriteString("   Port      " + c.portInput.View() + "\n")
		hostErr(true)
		if c.message != "" {
			b.WriteString("\n")
			for _, line := range strings.Split(c.message, "\n") {
				b.WriteString(wizardParagraph(state, line))
			}
		}
		enterLabel := "test connection"
		switch {
		case c.ready:
			enterLabel = "continue"
		case c.message != "":
			enterLabel = "retry"
		}
		b.WriteString(tui.Footer(theme, tui.Hints(theme,
			tui.Hint{Key: "tab", Label: "switch field"}, tui.Hint{Key: "enter", Label: enterLabel}, tui.Hint{Key: "esc", Label: "back"})))
	}
	return tui.Section(theme, s.Title(state), b.String())
}

// applyConnectChoice saves the host, port and password of FreeCAD on
// another computer into the credential store (domain.HostKey, PortKey,
// TokenKey), printing one [ok] or [fail] line, when that was chosen. It
// returns an exit code.
func applyConnectChoice(ctx context.Context, w io.Writer, state *AppState, dryRun bool) int {
	c := &state.Connect
	if !c.Chosen {
		return 0
	}
	if dryRun {
		fmt.Fprintf(w, "  would save: agents on this computer use FreeCAD on %s\n", hostPort(c.Host, c.Port))
		return 0
	}
	if err := saveConnection(ctx, c.Host, c.Port, c.Password); err != nil {
		fmt.Fprintf(w, "  [fail] save the connection to %s: %v\n", hostPort(c.Host, c.Port), err)
		return 1
	}
	fmt.Fprintf(w, "  [ok] Saved: agents on this computer use FreeCAD on %s.\n", hostPort(c.Host, c.Port))
	return 0
}

// installFirstText is the block runWizard prints after the wizard closed
// on "Install FreeCAD on this computer first".
func installFirstText() string {
	command := strings.TrimSpace(os.Getenv(domain.EnvInstallCommand))
	if command == "" {
		command = domain.BinaryName + " install"
	}
	return "  Install FreeCAD 1.0 or newer from https://www.freecad.org/downloads.php and start it once.\n" +
		"  Then run the same install command again:\n\n" +
		"    " + command + "\n\n" +
		"  Nothing was changed on this computer."
}

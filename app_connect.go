package main

// The app's "Use another computer" page: host, port and password, an async
// Test connection action, Save (enter) and "Use this computer instead" (which
// removes the saved connection), each reporting through the shared remote.go
// logic (testConnect, saveConnection, clearConnection). connectSuccessLine and
// connectErrLine are reused from wizard_connect.go, same package.
//
// The fields are always editable while focused (see form.go), so there are no
// letter shortcuts: the two actions are rows of the form.

import (
	"context"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sairaph/mcp-wizard/tui"

	"github.com/sairaph/freecad-mcp/internal/domain"
)

// Control ids of the Connect form.
const (
	connectHost     = "host"
	connectPort     = "port"
	connectPassword = "password"
	connectTest     = "test"
	connectClear    = "clear"
)

// connectPage is the Connect page's state. Host, port and password start
// from the stored connection (loadCredential), if any, and are written only
// on Save (through remote.go's saveConnection); nothing is written while the
// page is only being edited or tested.
type connectPage struct {
	ctx  context.Context
	p    palette
	size screenSize

	form *form
	// passwordLoaded is true while the password field still holds the one
	// stored with the loaded host; editing the host clears it.
	passwordLoaded bool

	testing bool
	saving  bool
	// clearing is true while "use this computer instead" is removing the
	// stored connection, after the y/n confirmation.
	clearing        bool
	confirmingClear bool
	spinnerFrame    int

	resultLines []string
}

func newConnectPage(ctx context.Context) appPage {
	p := &connectPage{ctx: ctx, p: newPalette()}

	host := newTextField(p.p, "192.168.1.20 or a host name", false)
	port := newTextField(p.p, strconv.Itoa(domain.DefaultListenerPort), false)
	port.SetValue(strconv.Itoa(domain.DefaultListenerPort))
	password := newTextField(p.p, "only if that computer set one", true)

	if token, h, prt, err := loadCredentials(ctx); err == nil && h != "" {
		host.SetValue(h)
		if prt != "" {
			port.SetValue(prt)
		}
		// The stored password belongs to this stored connection; without a
		// host it would be this computer's own "Share this PC" password
		// (or none), which does not belong on another computer's field.
		if token != "" {
			password.SetValue(token)
			p.passwordLoaded = true
		}
	}

	p.form = newForm(p.p,
		&control{id: connectHost, kind: kindText, label: "Host", input: host},
		&control{id: connectPort, kind: kindText, label: "Port", input: port, digits: true},
		&control{id: connectPassword, kind: kindText, label: "Password", input: password},
		&control{id: connectTest, kind: kindAction, label: "Test connection"},
		&control{id: connectClear, kind: kindAction, label: "Use this computer instead"},
	)
	return p
}

func (p *connectPage) Init() tea.Cmd { return nil }

// connectTestDoneMsg carries a Test connection result.
type connectTestDoneMsg struct{ lines []string }

// connectSaveDoneMsg carries a Save result.
type connectSaveDoneMsg struct{ err error }

// connectClearDoneMsg carries a "use this computer instead" result.
type connectClearDoneMsg struct{ err error }

func (p *connectPage) busy() bool { return p.testing || p.saving || p.clearing }

func (p *connectPage) value(id string) string { return p.form.control(id).input.Value() }

func (p *connectPage) Update(msg tea.Msg) (bool, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		p.size = screenSize{m.Width, m.Height}
		p.form.setWidth(p.size.width())
		return false, nil
	case connectTestDoneMsg:
		p.testing = false
		p.resultLines = m.lines
		return false, nil
	case connectSaveDoneMsg:
		p.saving = false
		if m.err != nil {
			p.resultLines = []string{"[fail] " + m.err.Error()}
		} else {
			p.resultLines = []string{"[ok] Saved. AI clients use it the next time they start freecad-mcp (restart them)."}
		}
		return false, nil
	case connectClearDoneMsg:
		p.clearing = false
		if m.err != nil {
			p.resultLines = []string{"[fail] " + m.err.Error()}
		} else {
			p.form.control(connectHost).input.SetValue("")
			p.form.control(connectPort).input.SetValue(strconv.Itoa(domain.DefaultListenerPort))
			p.form.control(connectPassword).input.SetValue("")
			p.passwordLoaded = false
			p.resultLines = []string{"[ok] Removed. AI clients use FreeCAD on this computer the next time they start freecad-mcp."}
		}
		return false, nil
	}

	if tui.IsSpinMsg(msg) {
		if p.busy() {
			p.spinnerFrame++
			return false, tui.Spinner()
		}
		return false, nil
	}

	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return false, nil
	}

	if p.confirmingClear {
		switch key.String() {
		case "y":
			p.confirmingClear = false
			p.clearing = true
			p.resultLines = nil
			return false, p.startClear()
		case "n", "esc":
			p.confirmingClear = false
		}
		return false, nil
	}

	if p.busy() {
		return false, nil
	}

	switch key.String() {
	case "esc":
		return true, nil
	case "enter":
		switch p.form.current().id {
		case connectTest:
			return false, p.startTest()
		case connectClear:
			p.confirmingClear = true
			p.resultLines = nil
			return false, nil
		}
		return false, p.startSave()
	}
	changed, cmd := p.form.handleKey(key)
	if changed && p.form.current().id == connectHost && p.passwordLoaded {
		// The loaded password belongs to the host that was loaded, not
		// whatever the user is now editing it into.
		p.form.control(connectPassword).input.SetValue("")
		p.passwordLoaded = false
	}
	return false, cmd
}

// connectPageFields reads and validates the fields, putting each error under
// its field and the focus on the first one, and returning ok false when any is
// invalid.
func (p *connectPage) connectPageFields() (host string, port int, password string, ok bool) {
	var first string
	fail := func(id string, text string) {
		p.form.control(id).err = text
		if first == "" {
			first = id
		}
	}
	for _, id := range []string{connectHost, connectPort, connectPassword} {
		p.form.control(id).err = ""
	}
	host = strings.TrimSpace(p.value(connectHost))
	switch {
	case host == "":
		fail(connectHost, "Enter a host first.")
	default:
		if err := domain.ValidateHost(host); err != nil {
			fail(connectHost, err.Error())
		}
	}
	port, err := validatePort(p.value(connectPort))
	if err != nil {
		fail(connectPort, err.Error())
	}
	password = p.value(connectPassword)
	if err := validatePassword(password); err != nil {
		fail(connectPassword, err.Error())
	}
	if first != "" {
		p.form.focusID(first)
		return "", 0, "", false
	}
	return host, port, password, true
}

func (p *connectPage) startTest() tea.Cmd {
	p.resultLines = nil
	host, port, password, ok := p.connectPageFields()
	if !ok {
		return nil
	}
	p.testing = true
	p.spinnerFrame = 0
	ctx := p.ctx
	return tea.Batch(tui.Spinner(), func() tea.Msg {
		res := testConnect(ctx, host, port, password)
		return connectTestDoneMsg{lines: formatConnectTest(res, host, port)}
	})
}

func (p *connectPage) startSave() tea.Cmd {
	p.resultLines = nil
	host, port, password, ok := p.connectPageFields()
	if !ok {
		return nil
	}
	p.saving = true
	p.spinnerFrame = 0
	ctx := p.ctx
	return tea.Batch(tui.Spinner(), func() tea.Msg {
		return connectSaveDoneMsg{err: saveConnection(ctx, host, port, password)}
	})
}

func (p *connectPage) startClear() tea.Cmd {
	p.spinnerFrame = 0
	ctx := p.ctx
	return tea.Batch(tui.Spinner(), func() tea.Msg {
		return connectClearDoneMsg{err: clearConnection(ctx)}
	})
}

// formatConnectTest turns a testConnect result into the page's result
// lines: a 200 (plus a protocol warning when the two versions differ), a
// simplified line asking for a password, "not a listener", or a failure
// (connectErrLine, shared with wizard_connect.go, tells a network failure
// from a listener's own refusal).
func formatConnectTest(res connectResult, host string, port int) []string {
	switch {
	case res.PasswordRequired:
		return []string{"[warn] That computer asks for a password. Enter it and test again."}
	case res.NotListener:
		return []string{notListenerLine(host, port)}
	case res.Err != nil:
		return []string{connectErrLine(res.Err, host, port)}
	default:
		lines := []string{connectSuccessLine(res.Status, host, port)}
		if w := protocolWarningLine(res.Status); w != "" {
			lines = append(lines, w)
		}
		return lines
	}
}

// connectHelp is the help of the highlighted control.
func (p *connectPage) connectHelp() string {
	switch p.form.current().id {
	case connectHost:
		return "The IP address or host name of the computer that runs FreeCAD. That computer needs freecad-mcp with Share this PC turned on."
	case connectPort:
		return "The port that computer shares on; 9876 unless it was changed in Share this PC."
	case connectPassword:
		return "The password set in Share this PC on that computer. Leave it empty if none was set."
	case connectTest:
		return "Checks that the computer answers with these settings. Nothing is saved."
	case connectClear:
		return "Removes the saved host, port and password. Agents then use FreeCAD on this computer."
	}
	return ""
}

func (p *connectPage) footer() string {
	width := p.size.width()
	if p.confirmingClear {
		return footerText(width, hint{"y", "remove"}, hint{"n", "keep"})
	}
	hints := []hint{{"tab", "move"}}
	switch p.form.current().id {
	case connectTest:
		hints = append(hints, hint{"enter", "test"})
	case connectClear:
		hints = append(hints, hint{"enter", "remove"})
	default:
		hints = append(hints, hint{"enter", "save"})
	}
	hints = append(hints, hint{"esc", "back"})
	return footerText(width, hints...)
}

func (p *connectPage) View() string {
	width := p.size.width()
	body := paragraph("Use FreeCAD that runs on another computer.", width, nil)
	body = append(body, "")
	body = append(body, p.form.view(width)...)

	switch {
	case p.confirmingClear:
		body = append(body, "")
		body = append(body, paragraph("Use FreeCAD on this computer instead? The saved host, port and password are removed.", width, nil)...)
	case p.testing:
		where := p.value(connectHost) + ":" + p.value(connectPort)
		if port, err := strconv.Atoi(p.value(connectPort)); err == nil {
			where = hostPort(p.value(connectHost), port)
		}
		body = append(body, "", "  "+tui.SpinFrame(p.spinnerFrame)+" Testing the connection to "+where+"...")
	case p.saving:
		body = append(body, "", "  "+tui.SpinFrame(p.spinnerFrame)+" Saving...")
	case p.clearing:
		body = append(body, "", "  "+tui.SpinFrame(p.spinnerFrame)+" Removing...")
	case len(p.resultLines) > 0:
		body = append(body, "")
		for _, l := range p.resultLines {
			body = append(body, p.p.resultLines(l, width)...)
		}
	}
	if !p.confirmingClear {
		body = append(body, "")
		body = append(body, paragraph(p.connectHelp(), width, p.p.dim.Render)...)
	}
	return renderScreen(p.p, p.size, headerText("Use another computer"), body, p.footer())
}

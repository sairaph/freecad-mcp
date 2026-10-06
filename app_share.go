package main

// The app's "Share this PC" page: sharing on or off, password and session
// minutes (allowed devices and port sit behind the Advanced toggle), a Test
// connection action and Save, each reporting through the shared remote.go
// logic (applyShare, applyUnshare, testShare). Test connection only proves the
// port answers from this computer; it says so, and points to the Use another
// computer page for a real test from another device.

import (
	"bytes"
	"context"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sairaph/mcp-wizard/async"
	"github.com/sairaph/mcp-wizard/tui"

	"github.com/sairaph/freecad-mcp/internal/addoninstall"
	"github.com/sairaph/freecad-mcp/internal/domain"
)

// sharePhase is the page's loading state: sharePhaseLoading while the
// installed addon and its current settings are located, sharePhaseForm once
// the fields hold a starting value.
type sharePhase int

const (
	sharePhaseLoading sharePhase = iota
	sharePhaseForm
)

// Control ids of the Share form.
const (
	shareEnabled  = "enabled"
	sharePassword = "password"
	shareMinutes  = "minutes"
	shareAdvanced = "advanced"
	shareAllowed  = "allowed"
	sharePort     = "port"
	shareTest     = "test"
)

// shareReport is the text applyShare or applyUnshare wrote, kept as its own
// type so its async.Result does not collide with the app's own report results.
type shareReport string

// shareLoad is the page's starting state: the installed addon copies and the
// remote access settings of the first one, or the defaults when none is
// installed. loadErr is set when the settings file exists but could not be
// read or parsed: the page then shows it instead of silently offering to
// save over whatever the file actually holds.
type shareLoad struct {
	targets  []addoninstall.Target
	settings addoninstall.RemoteSettings
	loadErr  error
}

type sharePage struct {
	ctx  context.Context
	p    palette
	size screenSize

	phase        sharePhase
	spinnerFrame int
	targets      []addoninstall.Target
	// loadError is set when the settings file could not be read or parsed;
	// Test and Save both refuse while it is set, rather than acting on the
	// defaults the form was left showing.
	loadError error

	form *form

	testing   bool
	testLines []shareTestLine

	saving     bool
	saveOutput string
}

func newSharePage(ctx context.Context) appPage {
	return &sharePage{ctx: ctx, p: newPalette()}
}

func (p *sharePage) Init() tea.Cmd {
	p.phase = sharePhaseLoading
	ctx := p.ctx
	return tea.Batch(tui.Spinner(), async.Load(func() (shareLoad, error) {
		targets := addoninstall.LocateInstalled(ctx, freecadCommand())
		settings := addoninstall.DefaultRemoteSettings()
		var loadErr error
		if len(targets) > 0 {
			s, err := addoninstall.ReadRemoteSettings(targets[0])
			if err != nil {
				loadErr = err
			} else {
				settings = s
			}
		}
		return shareLoad{targets: targets, settings: settings, loadErr: loadErr}, nil
	}))
}

// buildForm creates the controls with the loaded settings as their values.
func (p *sharePage) buildForm(s addoninstall.RemoteSettings) {
	password := newTextField(p.p, "none", true)
	password.SetValue(s.AuthToken)
	minutes := newTextField(p.p, "30", false)
	minutes.SetValue(strconv.Itoa(s.SessionTimeoutMinutes))
	allowed := newTextField(p.p, "192.168.1.0/24", false)
	allowed.SetValue(initialAllowedIPs(s))
	port := newTextField(p.p, strconv.Itoa(domain.DefaultListenerPort), false)
	port.SetValue(strconv.Itoa(s.ListenerPort))

	p.form = newForm(p.p,
		&control{id: shareEnabled, kind: kindToggle, label: "Sharing", on: s.RemoteEnabled},
		&control{id: sharePassword, kind: kindText, label: "Password", input: password},
		&control{id: shareMinutes, kind: kindText, label: "Session minutes", input: minutes, digits: true},
		&control{id: shareAdvanced, kind: kindToggle, label: "Advanced"},
		&control{id: shareAllowed, kind: kindText, label: "Allowed devices", input: allowed, hidden: true, indent: true},
		&control{id: sharePort, kind: kindText, label: "Port", input: port, digits: true, hidden: true, indent: true},
		&control{id: shareTest, kind: kindAction, label: "Test connection"},
	)
	p.form.setWidth(p.size.width())
}

func (p *sharePage) value(id string) string { return p.form.control(id).input.Value() }

func (p *sharePage) Update(msg tea.Msg) (bool, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		p.size = screenSize{m.Width, m.Height}
		if p.form != nil {
			p.form.setWidth(p.size.width())
		}
		return false, nil

	case async.Result[shareLoad]:
		p.targets = m.Value.targets
		p.loadError = m.Value.loadErr
		p.buildForm(m.Value.settings)
		p.phase = sharePhaseForm
		return false, nil

	case async.Result[[]shareTestLine]:
		p.testing = false
		p.testLines = m.Value
		return false, nil

	case async.Result[shareReport]:
		p.saving = false
		p.saveOutput = string(m.Value)
		return false, nil

	case tea.KeyMsg:
		if p.phase == sharePhaseLoading {
			return m.String() == "esc", nil
		}
		if p.testing || p.saving {
			return false, nil
		}
		switch m.String() {
		case "esc":
			return true, nil
		case "enter":
			if p.form.current().id == shareTest {
				return false, p.startTest()
			}
			return false, p.startSave()
		}
		changed, cmd := p.form.handleKey(m)
		if changed {
			p.syncAdvanced()
		}
		return false, cmd
	}

	if tui.IsSpinMsg(msg) {
		if p.phase == sharePhaseLoading || p.testing || p.saving {
			p.spinnerFrame++
			return false, tui.Spinner()
		}
	}
	return false, nil
}

// syncAdvanced shows the allowed devices and port fields while Advanced is on.
func (p *sharePage) syncAdvanced() {
	on := p.form.control(shareAdvanced).on
	p.form.control(shareAllowed).hidden = !on
	p.form.control(sharePort).hidden = !on
}

// options builds shareOptions from the fields on screen. A field that does not
// parse gets its error under it and the focus, and ok is false: a port or
// timeout out of range is refused, not silently replaced with the default.
func (p *sharePage) options() (opts shareOptions, ok bool) {
	var first string
	fail := func(id string, err error) {
		p.form.control(id).err = err.Error()
		if first == "" {
			first = id
		}
	}
	for _, id := range []string{sharePassword, shareMinutes, shareAllowed, sharePort} {
		p.form.control(id).err = ""
	}
	var o shareOptions
	o.Password = p.value(sharePassword)
	o.AllowedIPs = p.value(shareAllowed)
	if err := validatePassword(o.Password); err != nil {
		fail(sharePassword, err)
	}
	if minutes, err := validateMinutes(p.value(shareMinutes)); err != nil {
		fail(shareMinutes, err)
	} else {
		o.TimeoutMinutes = minutes
	}
	if _, err := domain.ParseAllowedIPs(o.AllowedIPs); err != nil {
		fail(shareAllowed, err)
	}
	if port, err := validatePort(p.value(sharePort)); err != nil {
		fail(sharePort, err)
	} else {
		o.Port = port
	}
	if first == "" {
		return o, true
	}
	if first == shareAllowed || first == sharePort {
		p.form.control(shareAdvanced).on = true
		p.syncAdvanced()
	}
	p.form.focusID(first)
	return o, false
}

// startTest runs testShare with the values on screen; nothing is saved.
func (p *sharePage) startTest() tea.Cmd {
	p.testLines = nil
	p.saveOutput = ""
	if p.loadError != nil {
		p.testLines = []shareTestLine{{Text: "[fail] " + p.loadError.Error()}}
		return nil
	}
	opts, ok := p.options()
	if !ok {
		return nil
	}
	ctx := p.ctx
	p.testing = true
	return tea.Batch(tui.Spinner(), async.Load(func() ([]shareTestLine, error) {
		return testShare(ctx, opts), nil
	}))
}

// startSave applies the fields on screen: share on through applyShare, or
// share off through applyUnshare when Sharing is off.
func (p *sharePage) startSave() tea.Cmd {
	p.testLines = nil
	p.saveOutput = ""
	if p.loadError != nil {
		p.saveOutput = "[fail] " + p.loadError.Error()
		return nil
	}
	enabled := p.form.control(shareEnabled).on
	var opts shareOptions
	if enabled {
		// Turning sharing off does not need the other fields to be valid.
		var ok bool
		if opts, ok = p.options(); !ok {
			return nil
		}
	}
	ctx := p.ctx
	targets := p.targets
	p.saving = true
	return tea.Batch(tui.Spinner(), async.Load(func() (shareReport, error) {
		var buf bytes.Buffer
		switch {
		case len(targets) == 0:
			buf.WriteString("[fail] No installed FreeCAD addon was found; install it first (menu > Install addon).\n")
		case enabled:
			applyShare(ctx, &buf, targets, opts)
		default:
			applyUnshare(ctx, &buf, targets)
		}
		return shareReport(buf.String()), nil
	}))
}

// shareHelp is the help of the highlighted control: one or two sentences.
func (p *sharePage) shareHelp() string {
	switch p.form.current().id {
	case shareEnabled:
		return "Turn on to let agents on other devices use FreeCAD on this computer. Each of those devices needs freecad-mcp installed too."
	case sharePassword:
		return "Other devices type this password to connect, and the FreeCAD addon uses it too. Leave it empty for none."
	case shareMinutes:
		return "How long FreeCAD stays assigned to one agent. After this long without activity another agent may take it."
	case shareAdvanced:
		return "Allowed devices " + p.value(shareAllowed) + ", port " + p.value(sharePort) + ". Press space to change them."
	case shareAllowed:
		return "IP addresses or subnets, comma-separated. Example: 192.168.1.0/24, 10.0.0.5"
	case sharePort:
		return "The port the listener accepts connections on."
	case shareTest:
		return "Checks that the settings answer from this computer. To test from another device, use Use another computer there."
	}
	return ""
}

func (p *sharePage) footer() string {
	hints := []hint{{"tab", "move"}}
	switch c := p.form.current(); {
	case c.kind == kindToggle && c.on:
		hints = append(hints, hint{"space", "turn off"})
	case c.kind == kindToggle:
		hints = append(hints, hint{"space", "turn on"})
	}
	if p.form.current().id == shareTest {
		hints = append(hints, hint{"enter", "test"})
	} else {
		hints = append(hints, hint{"enter", "save"})
	}
	hints = append(hints, hint{"esc", "back"})
	return footerText(p.size.width(), hints...)
}

func (p *sharePage) View() string {
	width := p.size.width()
	header := headerText("Share this PC")

	if p.phase == sharePhaseLoading {
		body := []string{"  " + tui.SpinFrame(p.spinnerFrame) + " Looking for the installed FreeCAD addon..."}
		return renderScreen(p.p, p.size, header, body, footerText(width, hint{"esc", "back"}))
	}

	body := paragraph("Let agents on other devices use FreeCAD on this computer.", width, nil)
	body = append(body, "")
	if p.loadError != nil {
		body = append(body, paragraph(p.loadError.Error(), width, p.p.fail.Render)...)
		body = append(body, "")
	}
	body = append(body, p.form.view(width)...)

	switch {
	case p.testing:
		body = append(body, "", "  "+tui.SpinFrame(p.spinnerFrame)+" Testing connection...")
	case len(p.testLines) > 0:
		body = append(body, "")
		for _, l := range p.testLines {
			body = append(body, p.resultLines(l.Text, width)...)
		}
	}
	switch {
	case p.saving:
		body = append(body, "", "  "+tui.SpinFrame(p.spinnerFrame)+" Saving...")
	case p.saveOutput != "":
		body = append(body, "")
		for _, l := range strings.Split(strings.TrimRight(p.saveOutput, "\n"), "\n") {
			body = append(body, p.resultLines(l, width)...)
		}
	}

	body = append(body, "")
	body = append(body, paragraph(p.shareHelp(), width, p.p.dim.Render)...)
	body = append(body, "")
	body = append(body, paragraph("Connections are not encrypted; see docs/remote-access.md for an SSH tunnel.", width, p.p.dim.Render)...)
	return renderScreen(p.p, p.size, header, body, p.footer())
}

// resultLines wraps one result line of a test, save or similar and colours it
// by its [ok], [warn] or [fail] tag.
func (p palette) resultLines(text string, width int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	style := func(s ...string) string { return strings.Join(s, "") }
	switch {
	case strings.HasPrefix(text, "[fail]"):
		style = p.fail.Render
	case strings.HasPrefix(text, "[warn]"):
		style = p.warn.Render
	case strings.HasPrefix(text, "[ok]"):
		style = p.ok.Render
	}
	return paragraph(text, width, style)
}

func (p *sharePage) resultLines(text string, width int) []string { return p.p.resultLines(text, width) }

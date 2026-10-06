package main

// The install wizard's "Share this PC with other devices?" steps, shown
// after the addon step when FreeCAD was found: password and session timeout.
// The allowed IPs are not asked: they follow the saved list or the
// default-route subnet, and the result lines point to the Share page for
// changing them. The choices are applied after registration by
// applyShareChoice (settings and credentials) and applyShareListener (autostart and start),
// in the order finishWizard calls them: the share settings, then the addon,
// then the listener, so the listener only starts once the addon it talks to
// is in place.
//
// applyShareChoice and applyShareListener call remote.go's own
// applyShareSettings/applyUnshareSettings and applyShareListenerRegistration
// directly, rather than its applyShare/applyUnshare (which bundle the
// settings and the listener step together for the `share` one-shot command,
// which has no addon-install step to sequence around), so the wizard and the
// command write and report every step identically while still installing
// the addon between the settings and the listener.

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/sairaph/mcp-wizard/flow"
	"github.com/sairaph/mcp-wizard/tui"

	"github.com/sairaph/freecad-mcp/internal/addoninstall"
)

// shareState is the "Share this PC" choice. The password is held in memory
// only, until applyShareChoice.
type shareState struct {
	// Asked is set once the share question was answered; nothing is applied
	// without it.
	Asked bool
	// Initial is remote_enabled as the wizard found it, where the choice
	// starts; On is the answer. On false while Initial is true turns remote
	// access off and removes the listener.
	Initial        bool
	On             bool
	AllowedIPs     string
	Password       string
	TimeoutMinutes int
	Port           int

	// The rest is the steps' own view state; it does not survive past the
	// wizard.
	//
	// primed marks that Initial, On, AllowedIPs, TimeoutMinutes, Port and
	// existingPassword were loaded from the addon settings and the LAN
	// subnets, so revisiting the choice screen (esc from a later screen,
	// then forward again) does not discard what the user already chose.
	primed bool
	// existingPassword is the password already set (Initial true and it was
	// not empty); the password screen shows it as "a password is set" and
	// keeps it when Password is left empty, rather than removing it.
	existingPassword string
	// passwordInput and minutesInput are the text fields of the password and
	// minutes screens; TimeoutMinutes is only updated once the text parses as
	// a valid value.
	passwordInput textinput.Model
	minutesInput  textinput.Model
	// message is the current validation error on the password or minutes
	// screen, or "" before one.
	message string
	// readErr is set when the addon settings could not be read (message then
	// holds the error and recovery guidance): the choice screen refuses Yes
	// while it is set, so enter cannot save the defaults over a file that
	// failed to read for some other reason (a hand-edited auth_token, for
	// example); see shareChoiceStep.Update.
	readErr bool
}

// shareSkipMsg is emitted by a share step's Init when it does not apply, so
// Update can return flow.Skip (the flow.Step interface has no Init
// directive of its own).
type shareSkipMsg struct{}

// shareSteps are the steps after the addon step (and, when FreeCAD was not
// found, after connectSteps) when FreeCAD was found; each skips itself
// otherwise.
func shareSteps(ctx context.Context) []flow.Step[AppState] {
	return []flow.Step[AppState]{
		&shareChoiceStep{},
		&sharePasswordStep{},
		&shareMinutesStep{},
	}
}

// --- Share this PC with other devices? ---

type shareChoiceStep struct{}

func (s *shareChoiceStep) ID() string { return "share-choice" }

func (s *shareChoiceStep) Title(*AppState) string { return "Share this PC" }

func (s *shareChoiceStep) Hints(*AppState) []struct{ Key, Label string } {
	return []struct{ Key, Label string }{{"space", "toggle"}, {"enter", "continue"}, {"esc", "back"}, {"q", "cancel"}}
}

func (s *shareChoiceStep) Init(state *AppState) tea.Cmd {
	if state.Addon.Phase != addonChoosing {
		return func() tea.Msg { return shareSkipMsg{} }
	}
	sh := &state.Share
	if !sh.primed {
		settings := addoninstall.DefaultRemoteSettings()
		if len(state.Addon.Targets) > 0 {
			v, err := addoninstall.ReadRemoteSettings(state.Addon.Targets[0])
			if err != nil {
				// Shown on this screen (below) instead of silently priming
				// from the defaults, which would offer to save over
				// whatever the file actually holds. readErr also blocks Yes
				// (see Update), so enter cannot write those defaults either.
				sh.message = "[fail] " + err.Error() +
					"; fix or delete the file, or open `freecad-mcp` > Share this PC to change these settings."
				sh.readErr = true
			} else {
				settings = v
			}
		}
		sh.Initial = settings.RemoteEnabled
		sh.On = settings.RemoteEnabled
		sh.AllowedIPs = initialAllowedIPs(settings)
		// A password already set is kept unless the user types a new one,
		// whether or not remote access happens to be on right now: the
		// Share page already allows leaving remote access off with a
		// password saved for later, and turning sharing back on here must
		// not silently drop it.
		sh.existingPassword = settings.AuthToken
		sh.TimeoutMinutes = settings.SessionTimeoutMinutes
		sh.Port = settings.ListenerPort
		sh.primed = true
	}
	return nil
}

func (s *shareChoiceStep) Update(msg tea.Msg, state *AppState) (flow.Directive, tea.Cmd) {
	sh := &state.Share
	switch m := msg.(type) {
	case shareSkipMsg:
		return flow.Skip, nil
	case tea.KeyMsg:
		switch m.String() {
		case "q", "ctrl+c":
			return flow.Quit, nil
		case "esc":
			state.Retreating = true
			sh.Password = ""
			return flow.Back, nil
		case " ", "space":
			sh.On = !sh.On
		case "enter":
			// While the settings file failed to read, turning sharing on is
			// refused: Asked would otherwise let applyShareChoice write the
			// (unrelated) defaults over a file that failed to read for some
			// other reason. Off changes nothing when Initial is already
			// false, so it may still proceed.
			if sh.readErr && sh.On {
				return flow.Continue, nil
			}
			sh.Asked = true
			return flow.Next, nil
		}
	}
	return flow.Continue, nil
}

// toggleRow is the wizard's toggle: the cursor, ○ or ● for the state, and the
// label, the same row the addon step and the app's forms draw.
func toggleRow(label string, on bool) string {
	styles := tui.DefaultTheme.Styles()
	mark := styles.Off.Render("○")
	if on {
		mark = styles.On.Render("●")
	}
	return " " + styles.Cursor.Render(">") + " " + mark + " " + label + "\n"
}

// wizardParagraph wraps text to the terminal width and indents it, for the
// wizard's own screens.
func wizardParagraph(state *AppState, text string) string {
	return strings.Join(paragraph(text, screenSize{state.Width, state.Height}.width(), nil), "\n") + "\n"
}

func (s *shareChoiceStep) View(state *AppState) string {
	theme := tui.DefaultTheme
	sh := &state.Share
	var b strings.Builder
	b.WriteString(wizardParagraph(state, "Let agents on other devices use FreeCAD on this computer."))
	b.WriteString("\n")
	b.WriteString(toggleRow("Share this PC with other devices", sh.On))
	b.WriteString("\n")
	b.WriteString(wizardParagraph(state, "Each of those devices needs freecad-mcp installed too. A small listener starts with this computer and opens FreeCAD when a remote agent asks."))
	if sh.message != "" {
		b.WriteString("\n" + wizardParagraph(state, sh.message))
	}
	b.WriteString(tui.Footer(theme, tui.Hints(theme,
		tui.Hint{Key: "space", Label: "toggle"}, tui.Hint{Key: "enter", Label: "continue"},
		tui.Hint{Key: "esc", Label: "back"}, tui.Hint{Key: "q", Label: "cancel"})))
	return tui.Section(theme, s.Title(state), b.String())
}

// shareInput starts a text field of the wizard's share steps.
func shareInput(placeholder string, secret bool, value string) textinput.Model {
	in := newTextField(newPalette(), placeholder, secret)
	in.SetValue(value)
	in.Width = 40
	in.Focus()
	return in
}

// --- Password ---

type sharePasswordStep struct{}

func (s *sharePasswordStep) ID() string { return "share-password" }

func (s *sharePasswordStep) Title(*AppState) string { return "Share this PC" }

func (s *sharePasswordStep) Hints(*AppState) []struct{ Key, Label string } {
	return []struct{ Key, Label string }{{"enter", "continue"}, {"esc", "back"}}
}

func (s *sharePasswordStep) Init(state *AppState) tea.Cmd {
	if !state.Share.On {
		return func() tea.Msg { return shareSkipMsg{} }
	}
	sh := &state.Share
	placeholder := "none"
	if sh.existingPassword != "" {
		placeholder = "unchanged"
	}
	sh.passwordInput = shareInput(placeholder, true, sh.Password)
	return nil
}

func (s *sharePasswordStep) Update(msg tea.Msg, state *AppState) (flow.Directive, tea.Cmd) {
	if _, ok := msg.(shareSkipMsg); ok {
		return flow.Skip, nil
	}
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return flow.Continue, nil
	}
	sh := &state.Share
	switch k.String() {
	case "ctrl+c":
		return flow.Quit, nil
	case "esc":
		state.Retreating = true
		return flow.Back, nil
	case "enter":
		if err := validatePassword(sh.Password); err != nil {
			sh.message = err.Error()
			return flow.Continue, nil
		}
		sh.message = ""
		return flow.Next, nil
	}
	var cmd tea.Cmd
	sh.passwordInput, cmd = sh.passwordInput.Update(msg)
	if sh.Password != sh.passwordInput.Value() {
		sh.Password = sh.passwordInput.Value()
		sh.message = ""
	}
	return flow.Continue, cmd
}

func (s *sharePasswordStep) View(state *AppState) string {
	theme := tui.DefaultTheme
	sh := &state.Share
	var b strings.Builder
	b.WriteString(wizardParagraph(state, "Password other devices must give (optional)."))
	b.WriteString("\n")
	b.WriteString("   Password  " + sh.passwordInput.View() + "\n")
	if sh.message != "" {
		b.WriteString("\n" + wizardParagraph(state, sh.message))
	}
	b.WriteString("\n")
	help := "The same password protects FreeCAD on this computer, and agents here use it too. Without one, anyone on an allowed device can run code in FreeCAD."
	if sh.existingPassword != "" {
		help = "A password is set. Leave this empty to keep it, or type a new one. " + help
	}
	b.WriteString(wizardParagraph(state, help))
	b.WriteString("\n")
	b.WriteString(wizardParagraph(state, "Connections are not encrypted; see docs/remote-access.md for an SSH tunnel."))
	b.WriteString(tui.Footer(theme, tui.Hints(theme,
		tui.Hint{Key: "enter", Label: "continue"}, tui.Hint{Key: "esc", Label: "back"})))
	return tui.Section(theme, s.Title(state), b.String())
}

// --- Minutes ---

type shareMinutesStep struct{}

func (s *shareMinutesStep) ID() string { return "share-minutes" }

func (s *shareMinutesStep) Title(*AppState) string { return "Share this PC" }

func (s *shareMinutesStep) Hints(*AppState) []struct{ Key, Label string } {
	return []struct{ Key, Label string }{{"enter", "continue"}, {"esc", "back"}}
}

func (s *shareMinutesStep) Init(state *AppState) tea.Cmd {
	if !state.Share.On {
		return func() tea.Msg { return shareSkipMsg{} }
	}
	sh := &state.Share
	value := sh.minutesInput.Value()
	if value == "" {
		value = strconv.Itoa(sh.TimeoutMinutes)
	}
	sh.minutesInput = shareInput("30", false, value)
	sh.message = ""
	return nil
}

func (s *shareMinutesStep) Update(msg tea.Msg, state *AppState) (flow.Directive, tea.Cmd) {
	if _, ok := msg.(shareSkipMsg); ok {
		return flow.Skip, nil
	}
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return flow.Continue, nil
	}
	sh := &state.Share
	switch k.String() {
	case "ctrl+c":
		return flow.Quit, nil
	case "esc":
		state.Retreating = true
		return flow.Back, nil
	case "enter":
		n, err := validateMinutes(sh.minutesInput.Value())
		if err != nil {
			sh.message = err.Error()
			return flow.Continue, nil
		}
		sh.TimeoutMinutes = n
		sh.message = ""
		return flow.Next, nil
	}
	var cmd tea.Cmd
	if kept, ok := digitsOnly(k); ok {
		k = kept
	} else {
		return flow.Continue, nil
	}
	before := sh.minutesInput.Value()
	sh.minutesInput, cmd = sh.minutesInput.Update(k)
	if sh.minutesInput.Value() != before {
		sh.message = ""
	}
	return flow.Continue, cmd
}

func (s *shareMinutesStep) View(state *AppState) string {
	theme := tui.DefaultTheme
	sh := &state.Share
	var b strings.Builder
	b.WriteString(wizardParagraph(state, "How long to keep the session assigned to an agent?"))
	b.WriteString("\n")
	b.WriteString("   Minutes  " + sh.minutesInput.View() + "\n")
	if sh.message != "" {
		b.WriteString("\n" + wizardParagraph(state, sh.message))
	}
	b.WriteString("\n")
	b.WriteString(wizardParagraph(state, "With sharing on, several agents may try to use FreeCAD. After this long without activity the session becomes available to another agent."))
	b.WriteString(tui.Footer(theme, tui.Hints(theme,
		tui.Hint{Key: "enter", Label: "continue"}, tui.Hint{Key: "esc", Label: "back"})))
	return tui.Section(theme, s.Title(state), b.String())
}

// --- Apply ---

// applyShareChoice writes the share settings into every located addon
// settings file (addoninstall.UpdateSettings) and the password into the
// credential store, or turns remote access off, printing one [ok] or [fail]
// line per step. It returns an exit code. The listener is not touched here;
// applyShareListener runs after the addon install (see finishWizard).
func applyShareChoice(ctx context.Context, w io.Writer, state *AppState, dryRun bool) int {
	sh := &state.Share
	if !sh.Asked {
		return 0
	}
	switch {
	case sh.On:
		password := sh.Password
		if password == "" && sh.existingPassword != "" {
			// Left empty: keep the password already set, rather than
			// removing it (only the Share page removes one, with an
			// explicit empty save).
			password = sh.existingPassword
		}
		return applyShareSettingsOn(ctx, w, state.Addon.Targets, shareOptions{
			AllowedIPs:     sh.AllowedIPs,
			Password:       password,
			TimeoutMinutes: sh.TimeoutMinutes,
			Port:           sh.Port,
		}, dryRun)
	case sh.Initial:
		return applyShareSettingsOff(w, state.Addon.Targets, dryRun)
	default:
		// Chosen No while remote access was already off: nothing to apply.
		return 0
	}
}

// applyShareSettingsOn writes opts into every target's addon settings and
// mirrors the password into the credential store, through remote.go's own
// applyShareSettings, so the `share` command and the wizard write and report
// the settings step identically.
func applyShareSettingsOn(ctx context.Context, w io.Writer, targets []addoninstall.Target, opts shareOptions, dryRun bool) int {
	if applyShareSettings(ctx, w, targets, opts, dryRun) {
		return 0
	}
	return 1
}

// applyShareSettingsOff turns remote access off in every target's addon
// settings, through remote.go's own applyUnshareSettings.
func applyShareSettingsOff(w io.Writer, targets []addoninstall.Target, dryRun bool) int {
	if applyUnshareSettings(w, targets, dryRun) {
		return 0
	}
	return 1
}

// applyShareListener registers and starts the listener (share on) or stops
// and unregisters it (share turned off), printing the plan's lines. handled
// reports whether it acted on the listener, so finishWizard does not restart
// it again.
func applyShareListener(ctx context.Context, w io.Writer, state *AppState, dryRun bool) (code int, handled bool) {
	sh := &state.Share
	if !sh.Asked || len(state.Addon.Targets) == 0 {
		return 0, false
	}
	switch {
	case sh.On:
		return applyShareListenerOn(w, state.Addon.Targets, sh.Port, sh.AllowedIPs, dryRun)
	case sh.Initial:
		return applyShareListenerOff(w, dryRun)
	default:
		return 0, false
	}
}

// applyShareListenerOn registers the listener (recorded against the first
// located target) and starts or restarts it, through remote.go's own
// applyShareListenerRegistration, so the `share` command and the wizard
// report the listener steps identically.
func applyShareListenerOn(w io.Writer, targets []addoninstall.Target, port int, allowedIPs string, dryRun bool) (int, bool) {
	if dryRun {
		fmt.Fprintln(w, "  would register and start the freecad-mcp listener")
		return 0, true
	}
	if !applyShareListenerRegistration(w, targets) {
		return 1, true
	}
	shareOnResultLine(w, port, allowedIPs)
	return 0, true
}

// applyShareListenerOff stops and removes the listener entry.
func applyShareListenerOff(w io.Writer, dryRun bool) (int, bool) {
	if dryRun {
		fmt.Fprintln(w, "  would stop and remove the listener")
		return 0, true
	}
	if !applyUnshareListener(w) {
		return 1, true
	}
	return 0, true
}

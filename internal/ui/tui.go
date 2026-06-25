package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// TUI is the bubbletea-based terminal user interface.
type TUI struct {
	viewport    viewport.Model
	textarea    textarea.Model
	messages    []string
	sender      func(string) (string, error)
	width       int
	height      int
	busy        bool
	err         error
	ready       bool
}

// NewTUI creates a new TUI instance.
func NewTUI(sender func(string) (string, error)) *TUI {
	ta := textarea.New()
	ta.Placeholder = "Ask YOLO Agent..."
	ta.Focus()
	ta.Prompt = "\u276f "
	ta.SetWidth(80)
	ta.SetHeight(3)
	ta.CharLimit = 4000

	vp := viewport.New(80, 10)

	return &TUI{
		textarea: ta,
		viewport: vp,
		sender:   sender,
	}
}

// Init initializes the TUI.
func (t *TUI) Init() tea.Cmd {
	return tea.EnterAltScreen
}

// Update handles messages and input events.
func (t *TUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		t.width = msg.Width
		t.height = msg.Height

		textareaHeight := 3
		if !t.ready {
			t.textarea.SetWidth(msg.Width - 2)
			t.viewport = viewport.New(msg.Width-4, msg.Height-textareaHeight-4)
			t.viewport.SetContent("Welcome to YOLO Agent. Type a message and press Enter.")
			t.ready = true
		} else {
			t.textarea.SetWidth(msg.Width - 2)
			t.viewport.Width = msg.Width - 4
			t.viewport.Height = msg.Height - textareaHeight - 4
		}

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEsc:
			return t, tea.Quit
		case tea.KeyEnter:
			input := strings.TrimSpace(t.textarea.Value())
			if input == "" {
				return t, nil
			}

			t.appendMessage("You", input)
			t.textarea.Reset()

			// Send message (async)
			return t, func() tea.Msg {
				response, err := t.sender(input)
				if err != nil {
					return errMsg{err: err}
				}
				return responseMsg{content: response}
			}
		}

	case responseMsg:
		t.appendMessage("YOLO", msg.content)
		t.busy = false

	case errMsg:
		t.err = msg.err
		t.appendMessage("Error", msg.err.Error())
		t.busy = false
	}

	// Update textarea and viewport
	ta, cmd := t.textarea.Update(msg)
	t.textarea = ta
	cmds = append(cmds, cmd)

	vp, cmd := t.viewport.Update(msg)
	t.viewport = vp
	cmds = append(cmds, cmd)

	return t, tea.Batch(cmds...)
}

// View renders the TUI.
func (t *TUI) View() string {
	if !t.ready {
		return "Loading YOLO Agent..."
	}

	var b strings.Builder

	// Title
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00D8FF"))
	b.WriteString(titleStyle.Render(" YOLO Agent ") + "\n")

	// Status bar
	status := "ready"
	if t.busy {
		status = "thinking..."
	}
	statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	b.WriteString(statusStyle.Render(fmt.Sprintf(" status: %s | press ESC to quit\n", status)))

	// Viewport
	b.WriteString(t.viewport.View() + "\n")

	// Error display
	if t.err != nil {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000"))
		b.WriteString(errStyle.Render(fmt.Sprintf("Error: %v", t.err)) + "\n")
	}

	// Textarea
	b.WriteString(t.textarea.View())

	return b.String()
}

// appendMessage adds a message to the viewport.
func (t *TUI) appendMessage(sender, content string) {
	roleStyle := lipgloss.NewStyle().Bold(true)
	var colored string
	switch sender {
	case "You":
		colored = roleStyle.Foreground(lipgloss.Color("#00FF00")).Render(sender)
	case "YOLO":
		colored = roleStyle.Foreground(lipgloss.Color("#00D8FF")).Render(sender)
	case "Error":
		colored = roleStyle.Foreground(lipgloss.Color("#FF0000")).Render(sender)
	default:
		colored = roleStyle.Render(sender)
	}

	msg := fmt.Sprintf("%s: %s", colored, content)
	t.messages = append(t.messages, msg)
	t.viewport.SetContent(strings.Join(t.messages, "\n\n"))
	t.viewport.GotoBottom()
}

// responseMsg is a TUI message carrying an assistant response.
type responseMsg struct {
	content string
}

// errMsg is a TUI message carrying an error.
type errMsg struct {
	err error
}

// Run starts the TUI.
func (t *TUI) Run() error {
	p := tea.NewProgram(t, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}

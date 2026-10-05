// internal/main.go
package main

import (
	"bufio"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"

	"charm.land/bubbles/v2/cursor"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type connMsg struct {
	conn net.Conn
}

type chatMsg string

type errMsg struct {
	err error
}

func main() {
	mode := flag.String("mode", "server", `"server" (listen) or "client" (dial)`)
	addr := flag.String("addr", "localhost:8080", "host:port")
	flag.Parse()
	p := tea.NewProgram(initialModel(*mode, *addr))
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Oof: %v\n", err)
		os.Exit(1)
	}
}

type model struct {
	viewport    viewport.Model
	messages    []string
	textarea    textarea.Model
	senderStyle lipgloss.Style

	mode     string
	addr     string
	conn     net.Conn
	incoming chan string

	err error
}

func initialModel(mode, addr string) model {
	ta := textarea.New()
	ta.Placeholder = "Send a message..."
	ta.SetVirtualCursor(false)
	ta.Focus()

	ta.Prompt = "┃ "
	ta.CharLimit = 280

	ta.SetWidth(30)
	ta.SetHeight(3)

	// Remove cursor line styling
	s := ta.Styles()
	s.Focused.CursorLine = lipgloss.NewStyle()
	ta.SetStyles(s)

	ta.ShowLineNumbers = false

	vp := viewport.New(viewport.WithWidth(30), viewport.WithHeight(5))
	vp.SetContent(fmt.Sprintf("Waiting to connect (%s @ %s)...", mode, addr))
	vp.KeyMap.Left.SetEnabled(false)
	vp.KeyMap.Right.SetEnabled(false)

	ta.KeyMap.InsertNewline.SetEnabled(false)

	return model{
		textarea:    ta,
		messages:    []string{},
		viewport:    vp,
		senderStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("5")),
		mode:        mode,
		addr:        addr,
		err:         nil,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, m.connect())
}

func (m model) connect() tea.Cmd {
	addr, mode := m.addr, m.mode
	return func() tea.Msg {
		if mode == "server" {
			ln, err := net.Listen("tcp", addr)
			if err != nil {
				return errMsg{err}
			}
			conn, err := ln.Accept()
			ln.Close()
			if err != nil {
				return errMsg{err}
			}
			return connMsg{conn}
		}
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			return errMsg{err}
		}
		return connMsg{conn}
	}
}

func readLoop(conn net.Conn, ch chan<- string) {
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		ch <- scanner.Text()
	}
	close(ch)
}

func waitForIncoming(ch <-chan string) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return errMsg{fmt.Errorf("peer disconnected")}
		}
		return chatMsg(line)
	}
}

func sendLine(conn net.Conn, text string) tea.Cmd {
	return func() tea.Msg {
		if conn == nil {
			return nil
		}
		if _, err := fmt.Fprintf(conn, "%s\n", text); err != nil {
			return errMsg{err}
		}
		return nil
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.viewport.SetWidth(msg.Width)
		m.textarea.SetWidth(msg.Width)
		m.viewport.SetHeight(msg.Height - m.textarea.Height())
		m.refresh()
		m.viewport.GotoBottom()

	case connMsg:
		m.conn = msg.conn
		m.incoming = make(chan string)
		m.messages = append(m.messages, "-- connected --")
		m.refresh()
		m.viewport.GotoBottom()
		go readLoop(m.conn, m.incoming)       // start the reader goroutine
		return m, waitForIncoming(m.incoming) // start listening for lines

	case chatMsg:
		m.messages = append(m.messages, "Them: "+string(msg))
		m.refresh()
		m.viewport.GotoBottom()
		return m, waitForIncoming(m.incoming) // RE-ISSUE: wait for the next line

	case errMsg:
		m.err = msg.err
		m.messages = append(m.messages, "-- "+msg.err.Error()+" --")
		m.refresh()
		m.viewport.GotoBottom()
		return m, nil // stop here; don't re-subscribe on a dead channel

	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit
		case "enter":
			text := m.textarea.Value()
			if strings.TrimSpace(text) == "" {
				return m, nil
			}
			m.messages = append(m.messages, m.senderStyle.Render("You: ")+text)
			m.refresh()
			m.textarea.Reset()
			m.viewport.GotoBottom()
			return m, sendLine(m.conn, text)
		default:
			// Send all other keypresses to the textarea.
			var cmd tea.Cmd
			m.textarea, cmd = m.textarea.Update(msg)
			return m, cmd
		}

	case cursor.BlinkMsg:
		// Textarea should also process cursor blinks.
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		return m, cmd
	}

	return m, nil
}

// refresh re-renders the message list into the viewport, wrapped to width.
func (m *model) refresh() {
	if len(m.messages) == 0 {
		return
	}
	m.viewport.SetContent(
		lipgloss.NewStyle().Width(m.viewport.Width()).Render(strings.Join(m.messages, "\n")),
	)
}

func (m model) View() tea.View {
	viewportView := m.viewport.View()
	v := tea.NewView(viewportView + "\n" + m.textarea.View())
	c := m.textarea.Cursor()
	if c != nil {
		c.Y += lipgloss.Height(viewportView)
	}
	v.Cursor = c
	v.AltScreen = true
	return v
}

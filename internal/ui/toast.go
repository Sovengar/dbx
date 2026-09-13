package ui

import (
	"strings"
	"time"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/buble/dbx/internal/theme"
)

type ToastLevel int

const (
	ToastSuccess ToastLevel = iota
	ToastError
	ToastInfo
	ToastWarning
)

const (
	toastMinWidth = 20
	toastMaxWidth = 60
)

type Toast struct {
	Message  string
	Level    ToastLevel
	Created  time.Time
	Duration time.Duration
}

type ToastManager struct {
	toasts []Toast
	styles *theme.Styles
	width  int
}

func NewToastManager(styles *theme.Styles) *ToastManager {
	return &ToastManager{
		styles: styles,
	}
}

func (t *ToastManager) SetWidth(w int) {
	t.width = w
}

func (t *ToastManager) Show(message string, level ToastLevel) {
	t.toasts = append(t.toasts, Toast{
		Message:  message,
		Level:    level,
		Created:  time.Now(),
		Duration: 3 * time.Second,
	})
}

func (t *ToastManager) ShowSuccess(message string) {
	t.Show(message, ToastSuccess)
}

func (t *ToastManager) ShowError(message string) {
	t.Show(message, ToastError)
}

func (t *ToastManager) ShowInfo(message string) {
	t.Show(message, ToastInfo)
}

func (t *ToastManager) ShowWarning(message string) {
	t.Show(message, ToastWarning)
}

func (t *ToastManager) Update() {
	now := time.Now()
	var active []Toast
	for _, toast := range t.toasts {
		if now.Sub(toast.Created) < toast.Duration {
			active = append(active, toast)
		}
	}
	t.toasts = active
}

func (t *ToastManager) View() string {
	if len(t.toasts) == 0 {
		return ""
	}
	return strings.Join(t.ViewLines(), "\n")
}

func (t *ToastManager) ViewLines() []string {
	var lines []string

	for _, toast := range t.toasts {
		toastLines := t.renderToast(toast)
		lines = append(lines, toastLines...)
	}

	return lines
}

func (t *ToastManager) renderToast(toast Toast) []string {
	icon := t.getIcon(toast.Level)
	style := t.getStyle(toast.Level)

	toastWidth := t.calculateWidth(toast.Message)
	contentWidth := toastWidth - 4

	wrappedLines := wrapText(toast.Message, contentWidth)

	var result []string
	prefix := icon + " "
	for i, line := range wrappedLines {
		if i == 0 {
			result = append(result, style.Render(prefix+line))
		} else {
			result = append(result, style.Render("  "+line))
		}
	}

	return result
}

func (t *ToastManager) calculateWidth(message string) int {
	iconOverhead := 4
	textLen := displayWidth(message)
	needed := textLen + iconOverhead + 4

	if needed < toastMinWidth {
		needed = toastMinWidth
	}
	if needed > toastMaxWidth {
		needed = toastMaxWidth
	}

	return needed
}

func (t *ToastManager) getIcon(level ToastLevel) string {
	switch level {
	case ToastSuccess:
		return "✓"
	case ToastError:
		return "✗"
	case ToastInfo:
		return "ℹ"
	case ToastWarning:
		return "⚠"
	default:
		return "•"
	}
}

func (t *ToastManager) getStyle(level ToastLevel) lipgloss.Style {
	switch level {
	case ToastSuccess:
		return t.styles.Success
	case ToastError:
		return t.styles.Error
	case ToastInfo:
		return t.styles.Info
	case ToastWarning:
		return t.styles.Warning
	default:
		return t.styles.Text
	}
}

func (t *ToastManager) RenderedToasts() []string {
	var rendered []string

	for _, toast := range t.toasts {
		toastLines := t.renderToast(toast)
		toastWidth := t.calculateWidth(toast.Message)
		bordered := t.styles.Border.Width(toastWidth).Render(strings.Join(toastLines, "\n"))
		rendered = append(rendered, bordered)
	}

	return rendered
}

func displayWidth(s string) int {
	width := 0
	for _, r := range s {
		if r == '\t' {
			width += 4
		} else if isWideRune(r) {
			width += 2
		} else {
			width++
		}
	}
	return width
}

func isWideRune(r rune) bool {
	if unicode.Is(unicode.Han, r) {
		return true
	}
	if unicode.Is(unicode.Hangul, r) {
		return true
	}
	if unicode.Is(unicode.Katakana, r) {
		return true
	}
	if unicode.Is(unicode.Hiragana, r) {
		return true
	}
	if r >= 0x1100 && r <= 0x115F {
		return true
	}
	if r >= 0x2E80 && r <= 0x303E {
		return true
	}
	if r >= 0x3040 && r <= 0x9FFF {
		return true
	}
	if r >= 0xAC00 && r <= 0xD7AF {
		return true
	}
	if r >= 0xF900 && r <= 0xFAFF {
		return true
	}
	if r >= 0xFE30 && r <= 0xFE6F {
		return true
	}
	if r >= 0xFF01 && r <= 0xFF60 {
		return true
	}
	if r >= 0xFFE0 && r <= 0xFFE6 {
		return true
	}
	if r >= 0x20000 && r <= 0x2FA1F {
		return true
	}
	return false
}

func wrapText(text string, maxWidth int) []string {
	if maxWidth <= 0 {
		return []string{text}
	}

	if displayWidth(text) <= maxWidth {
		return []string{text}
	}

	var lines []string
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{text}
	}

	currentLine := words[0]
	for _, word := range words[1:] {
		testLine := currentLine + " " + word
		if displayWidth(testLine) <= maxWidth {
			currentLine = testLine
		} else {
			lines = append(lines, currentLine)
			currentLine = word
		}
	}
	lines = append(lines, currentLine)

	return lines
}

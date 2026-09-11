package ui

import (
	"strings"
	"time"

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
	maxWidth := t.width - 4
	if maxWidth > 60 {
		maxWidth = 60
	}

	for _, toast := range t.toasts {
		icon := t.getIcon(toast.Level)
		style := t.getStyle(toast.Level)

		msg := toast.Message
		if len(msg) > maxWidth-4 {
			msg = msg[:maxWidth-7] + "..."
		}

		lines = append(lines, style.Render(icon+" "+msg))
	}

	return lines
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

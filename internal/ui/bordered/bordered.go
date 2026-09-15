package bordered

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Title alignment constants
const (
	AlignCenter = iota
	AlignLeft
	AlignRight
)

// RenderWithTitle renders a bordered box with a centered title in the top border.
func RenderWithTitle(border lipgloss.Border, borderFg color.Color, title, content string, width int) string {
	return renderBox(border, borderFg, AlignCenter, title, "", content, width)
}

// RenderWithTitleEx renders a bordered box with configurable title alignment and no footer.
func RenderWithTitleEx(border lipgloss.Border, borderFg color.Color, align int, title, content string, width int) string {
	return renderBox(border, borderFg, align, title, "", content, width)
}

// RenderWithTitleAndFooter renders a bordered box with a centered title and footer.
func RenderWithTitleAndFooter(border lipgloss.Border, borderFg color.Color, title, footer, content string, width int) string {
	return renderBox(border, borderFg, AlignCenter, title, footer, content, width)
}

// RenderWithTitleAndFooterEx renders a bordered box with configurable title alignment.
// align: AlignCenter, AlignLeft, or AlignRight.
func RenderWithTitleAndFooterEx(border lipgloss.Border, borderFg color.Color, align int, title, footer, content string, width int) string {
	return renderBox(border, borderFg, align, title, footer, content, width)
}

func renderBox(border lipgloss.Border, borderFg color.Color, align int, title, footer, content string, width int) string {
	if width < 2 {
		width = 2
	}

	topLeft := border.TopLeft
	topRight := border.TopRight
	bottomLeft := border.BottomLeft
	bottomRight := border.BottomRight
	topChar := border.Top
	leftChar := border.Left
	rightChar := border.Right
	bottomChar := border.Bottom

	if topChar == "" {
		topChar = " "
	}
	if leftChar == "" {
		leftChar = " "
	}
	if rightChar == "" {
		rightChar = " "
	}
	if bottomChar == "" {
		bottomChar = " "
	}

	// Calculate display widths (ANSI-safe) — NEVER use len()
	tlW := ansi.StringWidth(topLeft)
	trW := ansi.StringWidth(topRight)

	// Inner width = total width minus corner pieces
	innerWidth := width - tlW - trW
	if innerWidth < 0 {
		innerWidth = 0
	}

	// Style for border characters — applied via ansi.Style.Styled(), NOT lipgloss.Render()
	var borderStyle *ansi.Style
	if borderFg != nil {
		s := ansi.NewStyle().ForegroundColor(borderFg)
		borderStyle = &s
	}

	// Build top line with embedded title
	topLine := buildTopLine(borderStyle, topLeft, topChar, topRight, tlW, trW, innerWidth, align, title)

	// Build content lines
	contentLines := buildContentLines(borderStyle, leftChar, rightChar, content, innerWidth)

	// Build bottom line with optional footer (right-aligned)
	bottomLine := buildBottomLine(borderStyle, bottomLeft, bottomChar, bottomRight, innerWidth, footer)

	var b strings.Builder
	b.WriteString(topLine)
	b.WriteString("\n")
	for i, line := range contentLines {
		b.WriteString(line)
		if i < len(contentLines)-1 {
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	b.WriteString(bottomLine)

	return b.String()
}

// repeatStyled repeats a character n times, optionally applying ANSI style.
func repeatStyled(style *ansi.Style, char string, n int) string {
	s := strings.Repeat(char, n)
	if style != nil {
		return style.Styled(s)
	}
	return s
}

// styledChar wraps a single character with an optional ANSI style.
func styledChar(style *ansi.Style, char string) string {
	if style != nil {
		return style.Styled(char)
	}
	return char
}

// buildTopLine constructs: topLeft + padding + title + padding + topRight
func buildTopLine(style *ansi.Style, topLeft, topChar, topRight string, tlW, trW, innerWidth, align int, title string) string {
	// Strip ANSI from title to get display width — NEVER use len()
	titleDisplay := ansi.Strip(title)
	titleWidth := ansi.StringWidth(string(titleDisplay))

	// If title is wider than inner width, truncate it
	if titleWidth > innerWidth {
		title = ansi.Truncate(title, innerWidth, "")
		titleWidth = innerWidth
	}

	remaining := innerWidth - titleWidth
	var leftPad, rightPad int

	switch align {
	case AlignLeft:
		leftPad = 0
		rightPad = remaining
	case AlignRight:
		leftPad = remaining
		rightPad = 0
	default: // AlignCenter
		leftPad = remaining / 2
		rightPad = remaining - leftPad
	}

	leftPadStr := repeatStyled(style, topChar, leftPad)
	rightPadStr := repeatStyled(style, topChar, rightPad)

	// Apply border color to title text so it matches the border
	titleStyled := styledChar(style, title)

	return styledChar(style, topLeft) + leftPadStr + titleStyled + rightPadStr + styledChar(style, topRight)
}

// buildBottomLine constructs: bottomLeft + padding + footer (right-aligned) + padding + bottomRight
// If footer is empty, produces a plain filled bottom line.
func buildBottomLine(style *ansi.Style, bottomLeft, bottomChar, bottomRight string, innerWidth int, footer string) string {
	if footer == "" {
		bottomFill := repeatStyled(style, bottomChar, innerWidth)
		return styledChar(style, bottomLeft) + bottomFill + styledChar(style, bottomRight)
	}

	// Strip ANSI from footer to get display width
	footerDisplay := ansi.Strip(footer)
	footerWidth := ansi.StringWidth(string(footerDisplay))

	// If footer is wider than inner width, truncate it
	if footerWidth > innerWidth {
		footer = ansi.Truncate(footer, innerWidth, "")
		footerWidth = innerWidth
	}

	remaining := innerWidth - footerWidth
	leftPad := remaining // all padding on the left (right-align)
	leftPadStr := repeatStyled(style, bottomChar, leftPad)

	return styledChar(style, bottomLeft) + leftPadStr + footer + styledChar(style, bottomRight)
}

// buildContentLines wraps content lines to fit inside the bordered area.
func buildContentLines(style *ansi.Style, leftChar, rightChar, content string, innerWidth int) []string {
	rawLines := strings.Split(content, "\n")
	var result []string

	for _, line := range rawLines {
		displayWidth := ansi.StringWidth(line)

		if displayWidth <= innerWidth {
			// Pad to fill the full inner width
			padding := innerWidth - displayWidth
			paddedLine := line + strings.Repeat(" ", padding)
			result = append(result, styledChar(style, leftChar)+paddedLine+styledChar(style, rightChar))
		} else {
			// Wrap long lines
			wrapped := wrapLine(line, innerWidth)
			for _, wl := range wrapped {
				displayWidth := ansi.StringWidth(wl)
				padding := innerWidth - displayWidth
				if padding < 0 {
					padding = 0
				}
				paddedLine := wl + strings.Repeat(" ", padding)
				result = append(result, styledChar(style, leftChar)+paddedLine+styledChar(style, rightChar))
			}
		}
	}

	// Ensure at least one line
	if len(result) == 0 {
		emptyLine := strings.Repeat(" ", innerWidth)
		result = append(result, styledChar(style, leftChar)+emptyLine+styledChar(style, rightChar))
	}

	return result
}

// wrapLine breaks a line into chunks that fit within maxDisplayWidth.
func wrapLine(line string, maxDisplayWidth int) []string {
	if maxDisplayWidth <= 0 {
		return []string{line}
	}

	var chunks []string
	runes := []rune(line)
	currentChunk := ""
	currentWidth := 0

	for _, r := range runes {
		runeStr := string(r)
		runeWidth := ansi.StringWidth(runeStr)

		if currentWidth+runeWidth > maxDisplayWidth {
			chunks = append(chunks, currentChunk)
			currentChunk = runeStr
			currentWidth = runeWidth
		} else {
			currentChunk += runeStr
			currentWidth += runeWidth
		}
	}

	if currentChunk != "" {
		chunks = append(chunks, currentChunk)
	}

	return chunks
}

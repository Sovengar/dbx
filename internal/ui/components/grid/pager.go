package grid

import (
	"fmt"
	"strings"

	"github.com/buble/dbx/internal/theme"
)

type Pager struct {
	styles    *theme.Styles
	page      int
	totalRows int
	pageSize  int
}

func NewPager(styles *theme.Styles, pageSize int) *Pager {
	return &Pager{
		styles:   styles,
		page:     1,
		pageSize: pageSize,
	}
}

func (p *Pager) SetTotalRows(total int) {
	p.totalRows = total
	if p.totalRows == 0 {
		p.page = 1
	}
}

func (p *Pager) TotalPages() int {
	if p.pageSize <= 0 {
		return 1
	}
	pages := p.totalRows / p.pageSize
	if p.totalRows%p.pageSize > 0 {
		pages++
	}
	if pages < 1 {
		pages = 1
	}
	return pages
}

func (p *Pager) Page() int {
	return p.page
}

func (p *Pager) Offset() int {
	return (p.page - 1) * p.pageSize
}

func (p *Pager) Limit() int {
	return p.pageSize
}

func (p *Pager) NextPage() bool {
	if p.page < p.TotalPages() {
		p.page++
		return true
	}
	return false
}

func (p *Pager) PrevPage() bool {
	if p.page > 1 {
		p.page--
		return true
	}
	return false
}

func (p *Pager) FirstPage() {
	p.page = 1
}

func (p *Pager) LastPage() {
	p.page = p.TotalPages()
}

func (p *Pager) Render(width int) string {
	if p.totalRows == 0 {
		return p.styles.Help.Render("  No data")
	}

	totalPages := p.TotalPages()
	startRow := (p.page-1)*p.pageSize + 1
	endRow := p.page * p.pageSize
	if endRow > p.totalRows {
		endRow = p.totalRows
	}

	info := fmt.Sprintf("  %d-%d of %d", startRow, endRow, p.totalRows)
	pageInfo := fmt.Sprintf("Page %d of %d", p.page, totalPages)

	left := p.styles.Help.Render(info)
	right := p.styles.Help.Render(pageInfo)

	padding := width - lipgloss.Width(info) - lipgloss.Width(pageInfo)
	if padding < 0 {
		padding = 0
	}

	return left + strings.Repeat(" ", padding) + right
}

func lipglossWidth(s string) int {
	visible := 0
	inSeq := false
	for _, r := range s {
		if r == '\x1b' {
			inSeq = true
			continue
		}
		if inSeq {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inSeq = false
			}
			continue
		}
		visible++
	}
	return visible
}

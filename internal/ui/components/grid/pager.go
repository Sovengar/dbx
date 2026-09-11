package grid

import (
	"fmt"

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

func (p *Pager) GoToPage(n int) {
	if n < 1 {
		n = 1
	}
	total := p.TotalPages()
	if n > total {
		n = total
	}
	p.page = n
}

func (p *Pager) Render() string {
	if p.totalRows == 0 {
		return p.styles.Help.Render("  No data")
	}

	totalPages := p.TotalPages()
	startRow := (p.page-1)*p.pageSize + 1
	endRow := p.page * p.pageSize
	if endRow > p.totalRows {
		endRow = p.totalRows
	}

	info := fmt.Sprintf("  %d-%d of %d · Page %d of %d", startRow, endRow, p.totalRows, p.page, totalPages)

	return p.styles.Help.Render(info)
}

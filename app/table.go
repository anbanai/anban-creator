package main

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

// displayWidth 计算字符串的显示宽度（中文字符宽度为2）
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hangul, r) ||
			unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) ||
			(r >= 0xFF01 && r <= 0xFF60) || (r >= 0xFFE0 && r <= 0xFFE6) {
			w += 2
		} else {
			w += 1
		}
	}
	return w
}

// truncateByWidth 按显示宽度截断字符串，超出加 …
func truncateByWidth(s string, maxWidth int) string {
	if displayWidth(s) <= maxWidth {
		return s
	}
	w := 0
	var buf strings.Builder
	for _, r := range s {
		cw := 1
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hangul, r) ||
			unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) ||
			(r >= 0xFF01 && r <= 0xFF60) || (r >= 0xFFE0 && r <= 0xFFE6) {
			cw = 2
		}
		if w+cw > maxWidth-1 { // reserve 1 for …
			break
		}
		buf.WriteRune(r)
		w += cw
	}
	return buf.String() + "…"
}

// relativeTime 返回相对时间字符串
func relativeTime(unix int64) string {
	t := time.Unix(unix, 0)
	now := time.Now()
	diff := now.Sub(t)

	switch {
	case diff < time.Minute:
		return "刚刚"
	case diff < time.Hour:
		mins := int(diff.Minutes())
		return fmt.Sprintf("%d分钟前", mins)
	case diff < 24*time.Hour && isSameDay(t, now):
		return "今天 " + t.Format("15:04")
	case isYesterday(t, now):
		return "昨天"
	case t.Year() == now.Year():
		return t.Format("01月02日")
	default:
		return t.Format("2006年01月02日")
	}
}

func isSameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func isYesterday(t, now time.Time) bool {
	yesterday := now.AddDate(0, 0, -1)
	return isSameDay(t, yesterday)
}

// renderTable 渲染 box-drawing 字符表格到 stdout
// headers: 列标题；rows: 数据行，每行列数与 headers 一致
func renderTable(headers []string, rows [][]string) {
	cols := len(headers)
	widths := make([]int, cols)

	// 计算每列最大宽度（含标题）
	for i, h := range headers {
		widths[i] = displayWidth(h)
	}
	for _, row := range rows {
		for i := 0; i < cols && i < len(row); i++ {
			if w := displayWidth(row[i]); w > widths[i] {
				widths[i] = w
			}
		}
	}

	// 辅助：生成横线
	hLine := func(left, mid, right, fill string) string {
		var sb strings.Builder
		sb.WriteString(left)
		for i, w := range widths {
			sb.WriteString(strings.Repeat(fill, w+2))
			if i < cols-1 {
				sb.WriteString(mid)
			}
		}
		sb.WriteString(right)
		return sb.String()
	}

	// 辅助：生成数据行
	dataRow := func(cells []string) string {
		var sb strings.Builder
		sb.WriteString("│")
		for i, w := range widths {
			var cell string
			if i < len(cells) {
				cell = cells[i]
			}
			sb.WriteString(" ")
			sb.WriteString(cell)
			// pad to width
			pad := w - displayWidth(cell)
			sb.WriteString(strings.Repeat(" ", pad))
			sb.WriteString(" │")
		}
		return sb.String()
	}

	top := hLine("┌", "┬", "┐", "─")
	headerSep := hLine("├", "┼", "┤", "─")
	rowSep := hLine("├", "┼", "┤", "─")
	bottom := hLine("└", "┴", "┘", "─")

	fmt.Println("  " + top)
	fmt.Println("  " + dataRow(headers))
	fmt.Println("  " + headerSep)
	for i, row := range rows {
		fmt.Println("  " + dataRow(row))
		if i < len(rows)-1 {
			fmt.Println("  " + rowSep)
		}
	}
	fmt.Println("  " + bottom)
}

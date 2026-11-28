package domain

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// TrackerRow is one data row of a tracker's markdown table. Key is stable while the row's text is: it is a hash of
// the cells, with a -2, -3 ... suffix for an identical row further down.
type TrackerRow struct {
	Key   string
	Cells []string
	line  int
}

// TrackerTable reads a tracker's header and data rows. A text with no table has no header and no rows.
func TrackerTable(text string) (header []string, rows []TrackerRow) {
	seen := map[string]int{}
	for i, l := range strings.Split(text, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), "|") {
			continue
		}
		cells := splitRow(l)
		switch {
		case header == nil:
			header = cells
		case strings.Contains(l, "---") && strings.Trim(strings.ReplaceAll(strings.Join(cells, ""), " ", ""), "-:") == "":
			// the separator line
		default:
			sum := sha1.Sum([]byte(strings.ToLower(strings.Join(cells, "|"))))
			key := "r" + hex.EncodeToString(sum[:])[:8]
			if n := seen[key]; n > 0 {
				seen[key]++
				key += fmt.Sprintf("-%d", n+1)
			} else {
				seen[key] = 1
			}
			rows = append(rows, TrackerRow{Key: key, Cells: cells, line: i})
		}
	}
	return header, rows
}

// RemoveTrackerRows drops the rows with these keys and says how many it dropped.
func RemoveTrackerRows(text string, keys []string) (out string, removed int) {
	_, rows := TrackerTable(text)
	drop := map[int]bool{}
	for _, k := range keys {
		for _, r := range rows {
			if r.Key == k {
				drop[r.line] = true
			}
		}
	}
	if len(drop) == 0 {
		return text, 0
	}
	var kept []string
	for i, l := range strings.Split(text, "\n") {
		if !drop[i] {
			kept = append(kept, l)
		}
	}
	return strings.Join(kept, "\n"), len(drop)
}

// ErrTrackerRow is returned for a row key or a column the list does not have.
var ErrTrackerRow = errors.New("tracker row or column not found")

// SetTrackerCell sets one cell of the row with this key. The column is matched by its header name, ignoring case.
// It returns the new text and the row's new key (the key follows the row's text).
func SetTrackerCell(text, key, column, value string) (out, newKey string, err error) {
	header, rows := TrackerTable(text)
	col := -1
	for j, h := range header {
		if strings.EqualFold(strings.TrimSpace(h), strings.TrimSpace(column)) {
			col = j
			break
		}
	}
	if col < 0 {
		return text, "", fmt.Errorf("%w: no column %q (columns: %s)", ErrTrackerRow, column, strings.Join(header, ", "))
	}
	for _, r := range rows {
		if r.Key != key {
			continue
		}
		cells := append([]string(nil), r.Cells...)
		for len(cells) < len(header) {
			cells = append(cells, "")
		}
		cells[col] = strings.TrimSpace(strings.NewReplacer("|", "/", "\n", " ", "\r", " ").Replace(value))
		lines := strings.Split(text, "\n")
		lines[r.line] = "| " + strings.Join(cells, " | ") + " |"
		out = strings.Join(lines, "\n")
		_, after := TrackerTable(out)
		for _, a := range after {
			if a.line == r.line {
				return out, a.Key, nil
			}
		}
		return out, "", nil
	}
	return text, "", fmt.Errorf("%w: no row %q", ErrTrackerRow, key)
}

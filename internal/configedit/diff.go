package configedit

import "strings"

// Diff is a unified-style line diff of a against b with three lines of context; "" when
// they are the same.
func Diff(a, b string) string {
	if a == b {
		return ""
	}
	x, y := splitLines(a), splitLines(b)
	// Trim the common head and tail so the table below covers only what moved.
	head := 0
	for head < len(x) && head < len(y) && x[head] == y[head] {
		head++
	}
	tail := 0
	for tail < len(x)-head && tail < len(y)-head && x[len(x)-1-tail] == y[len(y)-1-tail] {
		tail++
	}
	mx, my := x[head:len(x)-tail], y[head:len(y)-tail]
	type op struct {
		tag  byte
		line string
	}
	var ops []op
	for _, l := range x[:head] {
		ops = append(ops, op{' ', l})
	}
	if len(mx)*len(my) > 4_000_000 {
		for _, l := range mx {
			ops = append(ops, op{'-', l})
		}
		for _, l := range my {
			ops = append(ops, op{'+', l})
		}
	} else {
		// lcs[i][j] is the longest common run of mx[i:] and my[j:].
		lcs := make([][]int32, len(mx)+1)
		for i := range lcs {
			lcs[i] = make([]int32, len(my)+1)
		}
		for i := len(mx) - 1; i >= 0; i-- {
			for j := len(my) - 1; j >= 0; j-- {
				if mx[i] == my[j] {
					lcs[i][j] = lcs[i+1][j+1] + 1
				} else {
					lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
				}
			}
		}
		i, j := 0, 0
		for i < len(mx) || j < len(my) {
			switch {
			case i < len(mx) && j < len(my) && mx[i] == my[j]:
				ops = append(ops, op{' ', mx[i]})
				i++
				j++
			case j < len(my) && (i == len(mx) || lcs[i][j+1] >= lcs[i+1][j]):
				ops = append(ops, op{'+', my[j]})
				j++
			default:
				ops = append(ops, op{'-', mx[i]})
				i++
			}
		}
	}
	for _, l := range x[len(x)-tail:] {
		ops = append(ops, op{' ', l})
	}
	const ctx = 3
	keep := make([]bool, len(ops))
	for i, o := range ops {
		if o.tag != ' ' {
			for k := max(0, i-ctx); k <= min(len(ops)-1, i+ctx); k++ {
				keep[k] = true
			}
		}
	}
	var sb strings.Builder
	gap := false
	for i, o := range ops {
		if !keep[i] {
			gap = true
			continue
		}
		if gap && sb.Len() > 0 {
			sb.WriteString("@@\n")
		}
		gap = false
		sb.WriteByte(o.tag)
		sb.WriteString(o.line)
		sb.WriteByte('\n')
	}
	return sb.String()
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

package main

import "strings"

// visibleMarkdownOutsideCode masks the bytes of Markdown inline code spans
// before looking for control-plane HTML tags, headings or machine actions.
// Backtick runs must close with the same length; the state spans lines.
// This prevents `air-worker.exe hook <event>` in the real PLAN from becoming
// an unclosed <event> HTML block and prevents whole quoted commands from
// granting a typed native machine-use contract.
func (s *markdownControlSurface) visibleMarkdownOutsideCode(line string) (string, bool) {
	var out strings.Builder
	out.Grow(len(line))
	hadCode := s.inlineRun != 0
	for i := 0; i < len(line); {
		if line[i] == byte(92) && i+1 < len(line) {
			// Markdown backslash-escaped markup is a literal, not a tag
			// or a quoted action. Preserve byte offsets by masking only
			// the escaped marker.
			next := line[i+1]
			if next == '<' || next == byte(96) || next == '>' {
				out.WriteByte(' ')
				out.WriteByte(' ')
				i += 2
				continue
			}
		}
		if line[i] == byte(96) {
			j := i + 1
			for j < len(line) && line[j] == byte(96) {
				j++
			}
			length := j - i
			hadCode = true
			if s.inlineRun == 0 {
				s.inlineRun = length
			} else if s.inlineRun == length {
				s.inlineRun = 0
			}
			for k := i; k < j; k++ {
				out.WriteByte(' ')
			}
			i = j
			continue
		}
		if s.inlineRun > 0 {
			out.WriteByte(' ')
		} else {
			out.WriteByte(line[i])
		}
		i++
	}
	return out.String(), hadCode
}

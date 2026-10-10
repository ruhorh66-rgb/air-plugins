package main

import (
	"errors"
	"strings"
)

func errNativeFeedbackProcedure(msg string) error {
	return errors.New("native feedback procedure: " + msg)
}

// Markdown headings and action markers are not authoritative inside examples.
// Opening fence length must match an equal-or-longer *closing* fence; an
// interior three-backtick line never closes a four-backtick example.
type markdownControlSurface struct {
	fenceChar byte
	fenceLen  int
	htmlBlock string
	comment   bool
}

func markdownFenceLine(line string) (byte, int, bool) {
	s := strings.TrimSpace(line)
	if len(s) < 3 {
		return 0, 0, false
	}
	c := s[0]
	if c != byte(96) && c != '~' {
		return 0, 0, false
	}
	n := 0
	for n < len(s) && s[n] == c {
		n++
	}
	if n < 3 {
		return 0, 0, false
	}
	return c, n, true
}
func (s *markdownControlSurface) hidden(line string, allowMarker string) bool {
	trimmed := strings.TrimSpace(line)
	lower := strings.ToLower(trimmed)
	if s.fenceLen > 0 {
		c, n, ok := markdownFenceLine(trimmed)
		if ok && c == s.fenceChar && n >= s.fenceLen && strings.TrimSpace(trimmed[n:]) == "" {
			s.fenceChar = 0
			s.fenceLen = 0
		}
		return true
	}
	if s.htmlBlock != "" {
		if strings.Contains(lower, "</"+s.htmlBlock+">") {
			s.htmlBlock = ""
		}
		return true
	}
	if s.comment {
		if strings.Contains(trimmed, "-->") {
			s.comment = false
		}
		return true
	}
	if strings.HasPrefix(trimmed, ">") {
		return true
	}
	if c, n, ok := markdownFenceLine(trimmed); ok {
		s.fenceChar = c
		s.fenceLen = n
		return true
	}
	for _, tag := range []string{"pre", "code", "blockquote"} {
		if strings.Contains(lower, "<"+tag) {
			if !strings.Contains(lower, "</"+tag+">") {
				s.htmlBlock = tag
			}
			return true
		}
	}
	if strings.HasPrefix(trimmed, "<!--") && trimmed != allowMarker {
		if !strings.Contains(trimmed, "-->") {
			s.comment = true
		}
		return true
	}
	return false
}

func nativeFeedbackProcedureVisibleLines(content string) ([]string, error) {
	var state markdownControlSurface
	inProc, foundProc, foundPitfalls := false, false, false
	var steps []string
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		if state.hidden(line, "") {
			continue
		}
		trimmed := strings.TrimSpace(line)
		low := strings.ToLower(trimmed)
		if strings.HasPrefix(low, "## ") {
			if low == "## procedure" {
				if foundProc {
					return nil, errNativeFeedbackProcedure("duplicate Procedure section")
				}
				foundProc = true
				inProc = true
				continue
			}
			if inProc {
				if low != "## pitfalls" {
					return nil, errNativeFeedbackProcedure("Procedure is not terminated by Pitfalls")
				}
				foundPitfalls = true
				break
			}
		}
		if inProc {
			steps = append(steps, trimmed)
		}
	}
	if !foundProc || !foundPitfalls {
		return nil, errNativeFeedbackProcedure("Procedure/Pitfalls section absent or hidden")
	}
	return steps, nil
}

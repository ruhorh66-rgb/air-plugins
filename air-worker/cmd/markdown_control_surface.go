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
	fenceChar     byte
	fenceLen      int
	htmlStack     []string
	htmlMalformed bool
	comment       bool
}

func markdownFenceLine(line string) (byte, int, bool) {
	// CommonMark permits at most three leading spaces before a fenced
	// delimiter. Four spaces or a tab make this an indented code example,
	// not a closer for the currently active fence.
	indent := 0
	for indent < len(line) && line[indent] == ' ' {
		indent++
	}
	if indent > 3 || indent >= len(line) || line[indent] == '\t' {
		return 0, 0, false
	}
	s := line[indent:]
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
	if s.fenceLen > 0 {
		c, n, ok := markdownFenceLine(line)
		if ok && c == s.fenceChar && n >= s.fenceLen && strings.TrimSpace(trimmed[n:]) == "" {
			s.fenceChar = 0
			s.fenceLen = 0
		}
		return true
	}
	if len(s.htmlStack) > 0 || s.htmlMalformed {
		// The entire raw HTML subtree remains non-authoritative. Track
		// nested tags rather than closing at the first </div>.
		s.consumeHTMLTags(line)
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
	// Four-space and tab-indented lines are Markdown code examples, not
	// affirmative learned instructions or canonical feedback records.
	prefix := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	if len(prefix) >= 4 || strings.Contains(prefix, "\t") {
		return true
	}
	if c, n, ok := markdownFenceLine(line); ok {
		s.fenceChar = c
		s.fenceLen = n
		return true
	}
	if s.consumeHTMLTags(line) {
		return true
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
			// A hidden continuation can negate an earlier instruction:
			// "2. Do\n    not carry out the preceding step".
			// Never infer a typed safe action from a Procedure containing
			// code, raw HTML, indented/quoted continuations or examples.
			if inProc {
				return nil, errNativeFeedbackProcedure("ambiguous hidden or indented Procedure continuation")
			}
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
			// Do not erase indentation here: the native action binding must
			// reject hidden or indented "1. Call ..." examples.
			steps = append(steps, line)
		}
	}
	if !foundProc || !foundPitfalls {
		return nil, errNativeFeedbackProcedure("Procedure/Pitfalls section absent or hidden")
	}
	return steps, nil
}

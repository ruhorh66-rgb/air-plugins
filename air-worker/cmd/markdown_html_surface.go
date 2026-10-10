package main

import "strings"

// Machine-authoritative feedback is never inferred from arbitrary raw HTML.
// The parser accepts only bounded, COMPLETE tag tokens on one line. Any
// multiline opening tag (including <div class="demo" + newline + >),
// mismatched nesting, or unfinished attribute quote is ambiguous and makes
// subsequent machine credit fail closed. Markdown inline code is removed
// BEFORE calling this parser.
func nativeHTMLName(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

func nativeHTMLNamePart(c byte) bool {
	return nativeHTMLName(c) || c >= '0' && c <= '9' || c == ':' || c == '-'
}

func nativeVoidHTMLElement(name string) bool {
	switch name {
	case "area", "base", "br", "col", "embed", "hr", "img",
		"input", "link", "meta", "param", "source", "track", "wbr":
		return true
	}
	return false
}

func (s *markdownControlSurface) consumeHTMLTags(line string) bool {
	if s.htmlPending != "" {
		if len(s.htmlPending)+len(line)+1 > 4096 {
			s.htmlMalformed = true
			return true
		}
		line = s.htmlPending + string(byte(10)) + line
		s.htmlPending = ""
	}
	found := false
	for i := 0; i < len(line); {
		index := strings.IndexByte(line[i:], '<')
		if index < 0 {
			break
		}
		start := i + index
		j := start + 1
		closing := false
		if j < len(line) && line[j] == '/' {
			closing = true
			j++
		}
		if j >= len(line) || !nativeHTMLName(line[j]) {
			i = start + 1
			continue
		}
		nameStart := j
		for j < len(line) && nativeHTMLNamePart(line[j]) {
			j++
		}
		name := strings.ToLower(line[nameStart:j])
		if j < len(line) && line[j] != ' ' && line[j] != '\t' && line[j] != '\n' && line[j] != '\r' &&
			line[j] != '/' && line[j] != '>' {
			// E.g. <event+foo> is not a supported authority.
			s.htmlMalformed = true
			return true
		}
		found = true
		var quote byte
		end := -1
		for k := j; k < len(line); k++ {
			c := line[k]
			if quote != 0 {
				if c == quote {
					quote = 0
				}
				continue
			}
			if c == '\'' || c == '"' {
				quote = c
				continue
			}
			if c == '>' {
				end = k
				break
			}
			if c == '<' {
				// An unquoted nested opener inside attributes is ambiguous.
				s.htmlMalformed = true
				return true
			}
		}
		if end < 0 {
			if len(line)-start > 4096 {
				s.htmlMalformed = true
			} else {
				s.htmlPending = line[start:]
			}
			return true
		}
		if end-start > 4096 {
			s.htmlMalformed = true
			return true
		}
		raw := line[start : end+1]
		if closing {
			if len(s.htmlStack) == 0 || s.htmlStack[len(s.htmlStack)-1] != name {
				s.htmlMalformed = true
				return true
			}
			s.htmlStack = s.htmlStack[:len(s.htmlStack)-1]
		} else if !strings.HasSuffix(strings.TrimSpace(strings.TrimSuffix(raw, ">")), "/") &&
			!nativeVoidHTMLElement(name) {
			if len(s.htmlStack) >= 32 {
				s.htmlMalformed = true
				return true
			}
			s.htmlStack = append(s.htmlStack, name)
		}
		i = end + 1
	}
	return found
}

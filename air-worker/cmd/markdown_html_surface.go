package main

import (
	"regexp"
	"strings"
)

// A copied feedback heading/row or executable-looking Procedure step is not
// authority when enclosed by ANY real HTML element. Recognize generic tag
// names (not a mutable block-tag whitelist) and keep nesting balanced.
// Unknown/mismatched HTML conservatively hides all following content.
var nativeHTMLTagPattern = regexp.MustCompile("(?i)<(/?)([a-z][a-z0-9:-]*)(?:[ \t]+[^<>]*?)?[ \t]*/?>")

func nativeVoidHTMLElement(name string) bool {
	switch name {
	case "area", "base", "br", "col", "embed", "hr", "img", "input",
		"link", "meta", "param", "source", "track", "wbr":
		return true
	}
	return false
}

func (s *markdownControlSurface) consumeHTMLTags(line string) bool {
	tags := nativeHTMLTagPattern.FindAllStringSubmatch(line, -1)
	if len(tags) == 0 {
		return false
	}
	for _, match := range tags {
		raw := match[0]
		closing := match[1] == "/"
		tag := strings.ToLower(match[2])
		if closing {
			if len(s.htmlStack) == 0 || s.htmlStack[len(s.htmlStack)-1] != tag {
				// Wrong close tag is ambiguous; never credit later text.
				s.htmlMalformed = true
				continue
			}
			s.htmlStack = s.htmlStack[:len(s.htmlStack)-1]
			continue
		}
		if strings.HasSuffix(strings.TrimSpace(raw), "/>") || nativeVoidHTMLElement(tag) {
			continue
		}
		if len(s.htmlStack) >= 32 {
			s.htmlMalformed = true
			continue
		}
		s.htmlStack = append(s.htmlStack, tag)
	}
	return true
}

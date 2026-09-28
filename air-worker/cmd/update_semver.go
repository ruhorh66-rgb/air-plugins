package main

import (
	"fmt"
	"strconv"
	"strings"
)

type updateSemVersion struct {
	Major int
	Minor int
	Patch int
	Pre   []string
}

func parseUpdateSemVersion(raw string) (updateSemVersion, error) {
	s := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(raw), "v"))
	if s == "" {
		return updateSemVersion{}, fmt.Errorf("empty version")
	}
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	var pre []string
	if i := strings.IndexByte(s, '-'); i >= 0 {
		prePart := s[i+1:]
		s = s[:i]
		if prePart == "" {
			return updateSemVersion{}, fmt.Errorf("empty prerelease")
		}
		pre = strings.Split(prePart, ".")
		for _, id := range pre {
			if id == "" {
				return updateSemVersion{}, fmt.Errorf("empty prerelease identifier")
			}
			for _, r := range id {
				if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
					return updateSemVersion{}, fmt.Errorf("invalid prerelease identifier %q", id)
				}
			}
		}
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return updateSemVersion{}, fmt.Errorf("version %q must have major.minor.patch", raw)
	}
	num := make([]int, 3)
	for i, p := range parts {
		if p == "" {
			return updateSemVersion{}, fmt.Errorf("empty version component")
		}
		if len(p) > 1 && p[0] == '0' {
			return updateSemVersion{}, fmt.Errorf("leading zero in version component %q", p)
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return updateSemVersion{}, fmt.Errorf("invalid version component %q", p)
		}
		num[i] = n
	}
	return updateSemVersion{Major: num[0], Minor: num[1], Patch: num[2], Pre: pre}, nil
}

func compareUpdateSemVersion(a, b updateSemVersion) int {
	if a.Major != b.Major {
		return cmpUpdateInt(a.Major, b.Major)
	}
	if a.Minor != b.Minor {
		return cmpUpdateInt(a.Minor, b.Minor)
	}
	if a.Patch != b.Patch {
		return cmpUpdateInt(a.Patch, b.Patch)
	}
	if len(a.Pre) == 0 && len(b.Pre) == 0 {
		return 0
	}
	if len(a.Pre) == 0 {
		return 1
	}
	if len(b.Pre) == 0 {
		return -1
	}
	n := len(a.Pre)
	if len(b.Pre) < n {
		n = len(b.Pre)
	}
	for i := 0; i < n; i++ {
		if a.Pre[i] == b.Pre[i] {
			continue
		}
		an, ae := strconv.Atoi(a.Pre[i])
		bn, be := strconv.Atoi(b.Pre[i])
		switch {
		case ae == nil && be == nil:
			return cmpUpdateInt(an, bn)
		case ae == nil:
			return -1
		case be == nil:
			return 1
		default:
			if a.Pre[i] < b.Pre[i] {
				return -1
			}
			return 1
		}
	}
	return cmpUpdateInt(len(a.Pre), len(b.Pre))
}

func compareUpdateVersionStrings(a, b string) int {
	pa, errA := parseUpdateSemVersion(a)
	pb, errB := parseUpdateSemVersion(b)
	switch {
	case errA != nil && errB != nil:
		return 0
	case errA != nil:
		return -1
	case errB != nil:
		return 1
	default:
		return compareUpdateSemVersion(pa, pb)
	}
}

func cmpUpdateInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

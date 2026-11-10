package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type sourceTraceRead struct {
	path, content string
	start         int
}

// The sealed native output must contain actual successful Read tool results.
// A model assertion or a quote found on disk alone is not evidence of a read.
func parseAndVerifySourceReviewStream(raw string, runErr error, root, taskSHA string) (string, string, error) {
	if runErr != nil {
		return "", "", fmt.Errorf("claude source review process: %w", runErr)
	}
	calls := map[string]string{}
	var reads []sourceTraceRead
	var resultLine string
	initSeen := false
	session := ""
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e struct {
			Type    string            `json:"type"`
			Subtype string            `json:"subtype"`
			Session string            `json:"session_id"`
			Tools   []string          `json:"tools"`
			MCP     []json.RawMessage `json:"mcp_servers"`
			Message struct {
				Content []struct {
					Type      string `json:"type"`
					ID        string `json:"id"`
					Name      string `json:"name"`
					ToolUseID string `json:"tool_use_id"`
					IsError   bool   `json:"is_error"`
					Input     struct {
						FilePath string `json:"file_path"`
					} `json:"input"`
				} `json:"content"`
			} `json:"message"`
			ToolResult struct {
				File struct {
					Path    string `json:"filePath"`
					Content string `json:"content"`
					Start   int    `json:"startLine"`
				} `json:"file"`
			} `json:"tool_use_result"`
		}
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return "", "", errors.New("invalid source review event")
		}
		if e.Type == "system" && e.Subtype == "init" {
			if initSeen || len(e.MCP) != 0 {
				return "", "", errors.New("unexpected MCP server or duplicate init")
			}
			initSeen = true
			session = e.Session
			for _, t := range e.Tools {
				if t != "Read" && t != "Glob" && t != "Grep" && t != "StructuredOutput" {
					return "", "", fmt.Errorf("unexpected available tool %q", t)
				}
			}
		}
		if e.Type == "result" {
			if resultLine != "" {
				return "", "", errors.New("multiple terminal results")
			}
			resultLine = line
		}
		for _, b := range e.Message.Content {
			if b.Type == "tool_use" && b.Name == "Read" && b.ID != "" {
				if _, exists := calls[b.ID]; exists {
					return "", "", errors.New("duplicate read id")
				}
				calls[b.ID] = b.Input.FilePath
			}
			if b.Type != "tool_result" || b.IsError {
				continue
			}
			path, ok := calls[b.ToolUseID]
			f := e.ToolResult.File
			if !ok || path == "" || f.Path == "" || f.Start < 1 {
				continue
			}
			if !filepath.IsAbs(path) {
				path = filepath.Join(root, path)
			}
			if !filepath.IsAbs(f.Path) {
				f.Path = filepath.Join(root, f.Path)
			}
			if !strings.EqualFold(filepath.Clean(path), filepath.Clean(f.Path)) {
				return "", "", errors.New("read result path mismatch")
			}
			reads = append(reads, sourceTraceRead{path: filepath.Clean(f.Path), content: f.Content, start: f.Start})
		}
	}
	if !initSeen || resultLine == "" {
		return "", session, errors.New("missing source review init or terminal result")
	}
	reply, resultSession, err := parseClaudeStructuredSourceResult(resultLine, nil)
	if err != nil {
		return "", resultSession, err
	}
	if session == "" || resultSession != session {
		return "", resultSession, errors.New("source review session mismatch")
	}
	var v sourceReviewVerdict
	if err := json.Unmarshal([]byte(reply), &v); err != nil {
		return "", session, err
	}
	// NOT_PROVEN may be emitted without a successful read; it cannot become PASS.
	if v.Verdict == "NOT_PROVEN" {
		return reply, session, nil
	}
	taskRead := false
	for _, r := range reads {
		b, err := os.ReadFile(r.path)
		if err != nil || sha256Hex(b) != taskSHA || r.start != 1 {
			continue
		}
		normalize := func(x []byte) []byte {
			return bytes.TrimSuffix(bytes.TrimPrefix(bytes.ReplaceAll(x, []byte("\r\n"), []byte("\n")), []byte{0xef, 0xbb, 0xbf}), []byte("\n"))
		}
		if bytes.Equal(normalize(b), normalize([]byte(r.content))) {
			taskRead = true
		}
	}
	if !taskRead {
		return "", session, errors.New("task read not proven by successful tool result")
	}
	citations := append([]sourceReviewLocation(nil), v.Checked...)
	for _, f := range v.Findings {
		citations = append(citations, sourceReviewLocation{Path: f.Path, Line: f.Line, Quote: f.Quote})
	}
	for _, c := range citations {
		rel, err := canonicalReviewPath(root, c.Path)
		if err != nil {
			return "", session, err
		}
		wanted := filepath.Clean(filepath.Join(root, filepath.FromSlash(rel)))
		found := false
		for _, r := range reads {
			if !strings.EqualFold(r.path, wanted) {
				continue
			}
			lines := strings.Split(strings.ReplaceAll(r.content, "\r\n", "\n"), "\n")
			idx := c.Line - r.start
			if idx >= 0 && idx < len(lines) && strings.Contains(lines[idx], strings.TrimSpace(c.Quote)) {
				found = true
				break
			}
		}
		if !found {
			return "", session, fmt.Errorf("citation read not proven: %s:%d", c.Path, c.Line)
		}
	}
	return reply, session, nil
}

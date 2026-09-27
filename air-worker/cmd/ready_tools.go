package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

const readyToolsSectionName = "Готовые средства"

type readyToolEntry struct {
	Tool     string
	Means    string
	Version  string
	Coverage string
}

type readyToolsInfo struct {
	Present bool
	Entries []readyToolEntry
}

var (
	reExternalIntegrationSignal = regexp.MustCompile(`(?i)(?:\b[A-Za-z0-9_.-]+-sdk\b|\bSDK\b|(?:official|официальн\p{L}*|штатн\p{L}*)\s+(?:SDK|CLI|API)\b|(?:external|внешн\p{L}*)\s+(?:tool|instrument|инструмент\p{L}*))`)
	reReadyMeans                = regexp.MustCompile(`(?i)(?:^|[^[:alpha:]])(?:SDK|CLI|API)(?:[^[:alpha:]]|$)`)
	reHomemadeTooling           = regexp.MustCompile(`(?i)(самопис|custom\s+(?:http\s+)?client|HTTP[- ]?клиент|написать\s+(?:свой\s+)?(?:HTTP[- ]?)?клиент|urllib|Invoke-RestMethod|\bcurl\b|(?:прям(?:ой|ое|ая|ые)?|direct)\s+(?:доступ|access).*(?:MySQL|MinIO|Elasticsearch|БД|database))`)
)

func containsAnyFold(value string, needles ...string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, needle := range needles {
		if strings.Contains(value, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

func headerIndex(cols []string, needles ...string) int {
	for i, col := range cols {
		if containsAnyFold(col, needles...) {
			return i
		}
	}
	return -1
}

func isMarkdownSeparatorRow(cols []string) bool {
	if len(cols) == 0 {
		return false
	}
	for _, col := range cols {
		c := strings.TrimSpace(col)
		c = strings.Trim(c, ":")
		if len(c) < 3 || strings.Trim(c, "-") != "" {
			return false
		}
	}
	return true
}

func readReadyTools(planPath string) (readyToolsInfo, error) {
	raw, err := os.ReadFile(planPath)
	if err != nil {
		return readyToolsInfo{}, err
	}
	body, err := markdownSectionText(raw, readyToolsSectionName)
	if err != nil {
		return readyToolsInfo{Present: false}, nil
	}
	info := readyToolsInfo{Present: true}
	lines := strings.Split(body, "\n")
	header := -1
	toolCol, meansCol, versionCol, coverageCol := -1, -1, -1, -1
	for i, line := range lines {
		cols := markdownTableColumns(line)
		if len(cols) < 4 {
			continue
		}
		t := headerIndex(cols, "инструмент", "tool")
		m := headerIndex(cols, "средств", "sdk/cli/api", "sdk, cli, api", "официаль")
		v := headerIndex(cols, "верси", "version")
		c := headerIndex(cols, "покры", "coverage")
		if t >= 0 && m >= 0 && v >= 0 && c >= 0 {
			header, toolCol, meansCol, versionCol, coverageCol = i, t, m, v, c
			break
		}
	}
	if header < 0 {
		return info, fmt.Errorf("раздел %q не содержит таблицу Инструмент / Штатное средство / Версия / Покрытие", readyToolsSectionName)
	}
	sawData := false
	for i := header + 1; i < len(lines); i++ {
		cols := markdownTableColumns(lines[i])
		if len(cols) == 0 {
			if sawData && strings.TrimSpace(lines[i]) == "" {
				break
			}
			continue
		}
		if isMarkdownSeparatorRow(cols) {
			continue
		}
		maxCol := toolCol
		for _, idx := range []int{meansCol, versionCol, coverageCol} {
			if idx > maxCol {
				maxCol = idx
			}
		}
		if len(cols) <= maxCol {
			continue
		}
		entry := readyToolEntry{
			Tool: strings.TrimSpace(cols[toolCol]), Means: strings.TrimSpace(cols[meansCol]),
			Version: strings.TrimSpace(cols[versionCol]), Coverage: strings.TrimSpace(cols[coverageCol]),
		}
		if entry.Tool == "" && entry.Means == "" && entry.Version == "" && entry.Coverage == "" {
			continue
		}
		if entry.Tool == "" || entry.Means == "" || entry.Version == "" || entry.Coverage == "" {
			return info, fmt.Errorf("раздел %q содержит неполную строку: tool=%q means=%q version=%q coverage=%q",
				readyToolsSectionName, entry.Tool, entry.Means, entry.Version, entry.Coverage)
		}
		if !reReadyMeans.MatchString(entry.Means) {
			return info, fmt.Errorf("для %q штатное средство %q не называет SDK/CLI/API", entry.Tool, entry.Means)
		}
		info.Entries = append(info.Entries, entry)
		sawData = true
	}
	if len(info.Entries) == 0 {
		return info, fmt.Errorf("раздел %q не содержит ни одного штатного SDK/CLI/API", readyToolsSectionName)
	}
	return info, nil
}

func planExternalIntegrationSignals(steps []workStep) []string {
	var signals []string
	for _, step := range steps {
		if step.Done || step.Gate {
			continue
		}
		text := step.Title + " " + step.Cmd + " " + step.Judge
		if match := reExternalIntegrationSignal.FindString(text); strings.TrimSpace(match) != "" {
			signals = appendUniqueString(signals, strings.TrimSpace(match))
		}
	}
	return signals
}

func validateReadyTools(planPath string, steps []workStep) []string {
	signals := planExternalIntegrationSignals(steps)
	info, err := readReadyTools(planPath)
	if len(signals) > 0 && !info.Present {
		return []string{fmt.Sprintf("PLAN называет внешний инструмент (%s), но раздел %q отсутствует",
			strings.Join(signals, ", "), readyToolsSectionName)}
	}
	if info.Present && err != nil {
		return []string{err.Error()}
	}
	return nil
}

func homemadeToolingWarnings(steps []workStep, ready readyToolsInfo) []planLintWarning {
	if !ready.Present || len(ready.Entries) == 0 {
		return nil
	}
	var warnings []planLintWarning
	for _, step := range steps {
		if step.Done || step.Gate {
			continue
		}
		text := step.Title + " " + step.Cmd
		if match := strings.TrimSpace(reHomemadeTooling.FindString(text)); match != "" {
			warnings = append(warnings, planLintWarning{
				Step: step.Num, Tier: step.Tier, Rule: "AW-PLAN-LINT-02",
				Detail: fmt.Sprintf("шаг предписывает самописное/прямое управление (%s), хотя PLAN объявляет штатные SDK/CLI/API; нужен доказанный gap и решение ЛПР", match),
			})
		}
	}
	return warnings
}

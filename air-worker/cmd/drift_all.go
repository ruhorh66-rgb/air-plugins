package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const portfolioDriftSchemaVersion = "air-worker.portfolio-drift/v1"

type portfolioHistoryRecord struct {
	Schema       string `json:"schema"`
	At           string `json:"at"`
	Product      string `json:"product"`
	Distance     *int   `json:"distance,omitempty"`
	Source       string `json:"source,omitempty"`
	ClosedSteps  *int   `json:"closed_steps,omitempty"`
	PlanContract string `json:"plan_contract,omitempty"`
	Note         string `json:"note,omitempty"`
}

type portfolioDriftRow struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	Distance             *int   `json:"distance,omitempty"`
	Source               string `json:"source,omitempty"`
	ClosedSteps          *int   `json:"closed_steps,omitempty"`
	PlanContract         string `json:"plan_contract,omitempty"`
	PreviousDistance     *int   `json:"previous_distance,omitempty"`
	PreviousSource       string `json:"previous_source,omitempty"`
	PreviousClosedSteps  *int   `json:"previous_closed_steps,omitempty"`
	PreviousPlanContract string `json:"previous_plan_contract,omitempty"`
	Movement             string `json:"movement"`
	StallMoves           int    `json:"stall_moves"`
}

type portfolioDriftReport struct {
	Schema   string              `json:"schema"`
	At       string              `json:"at"`
	Registry string              `json:"registry"`
	History  string              `json:"history"`
	Recorded bool                `json:"recorded"`
	Rows     []portfolioDriftRow `json:"rows"`
}

func defaultPortfolioHistoryPath(registryPath string) string {
	return filepath.Join(filepath.Dir(registryPath), ".air-worker", "portfolio-drift.jsonl")
}

func readPortfolioHistory(path string) ([]portfolioHistoryRecord, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var rows []portfolioHistoryRecord
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 64*1024), 2*1024*1024)
	line := 0
	for s.Scan() {
		line++
		raw := strings.TrimSpace(s.Text())
		if raw == "" {
			continue
		}
		var row portfolioHistoryRecord
		if err := json.Unmarshal([]byte(raw), &row); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, line, err)
		}
		if row.Schema != portfolioDriftSchemaVersion {
			continue
		}
		rows = append(rows, row)
	}
	return rows, s.Err()
}

func appendPortfolioHistory(path string, rows []portfolioHistoryRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	for _, row := range rows {
		if err := enc.Encode(row); err != nil {
			return err
		}
	}
	return f.Sync()
}

func latestPortfolioHistory(rows []portfolioHistoryRecord, product string) *portfolioHistoryRecord {
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].Product == product {
			copy := rows[i]
			return &copy
		}
	}
	return nil
}

func sameOptionalInt(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func portfolioStallMoves(rows []portfolioHistoryRecord, product, source, planContract string, distance, closed *int) int {
	stall := 0
	for i := len(rows) - 1; i >= 0; i-- {
		row := rows[i]
		if row.Product != product {
			continue
		}
		if row.Source != source || row.PlanContract != planContract ||
			!sameOptionalInt(row.Distance, distance) || !sameOptionalInt(row.ClosedSteps, closed) {
			break
		}
		stall++
	}
	return stall
}

func evaluatePortfolioDrift(row portfolioRow, history []portfolioHistoryRecord) portfolioDriftRow {
	out := portfolioDriftRow{
		ID: row.ID, Name: row.Name,
		Distance: row.EffectiveDistance, Source: row.DistanceSource,
		ClosedSteps: row.ClosedSteps, PlanContract: row.PlanContract,
		Movement: "BASELINE",
	}
	prev := latestPortfolioHistory(history, row.ID)
	if prev == nil {
		if row.EffectiveDistance == nil && row.ClosedSteps == nil {
			out.Movement = "UNKNOWN"
		}
		return out
	}
	out.PreviousDistance = prev.Distance
	out.PreviousSource = prev.Source
	out.PreviousClosedSteps = prev.ClosedSteps
	out.PreviousPlanContract = prev.PlanContract

	if row.EffectiveDistance == nil && row.ClosedSteps == nil {
		out.Movement = "UNKNOWN"
		return out
	}
	if prev.Source != row.DistanceSource || prev.PlanContract != row.PlanContract {
		out.Movement = "BASELINE_SOURCE_CHANGE"
		return out
	}

	regress := false
	forward := false
	if row.EffectiveDistance != nil && prev.Distance != nil {
		if *row.EffectiveDistance > *prev.Distance {
			regress = true
		} else if *row.EffectiveDistance < *prev.Distance {
			forward = true
		}
	}
	if row.ClosedSteps != nil && prev.ClosedSteps != nil {
		if *row.ClosedSteps < *prev.ClosedSteps {
			regress = true
		} else if *row.ClosedSteps > *prev.ClosedSteps {
			forward = true
		}
	}
	switch {
	case regress:
		out.Movement = "REGRESS"
	case forward:
		out.Movement = "FORWARD"
	case (row.EffectiveDistance != nil && prev.Distance != nil) || (row.ClosedSteps != nil && prev.ClosedSteps != nil):
		out.Movement = "STALL"
		out.StallMoves = portfolioStallMoves(history, row.ID, row.DistanceSource, row.PlanContract, row.EffectiveDistance, row.ClosedSteps)
		if out.StallMoves == 0 {
			out.StallMoves = 1
		}
	default:
		out.Movement = "BASELINE"
	}
	return out
}

func buildPortfolioDrift(registryPath, historyPath, note string, record bool) (portfolioDriftReport, error) {
	report, err := buildPortfolioReport(registryPath)
	if err != nil {
		return portfolioDriftReport{}, err
	}
	if historyPath == "" {
		historyPath = defaultPortfolioHistoryPath(report.Registry)
	} else if !filepath.IsAbs(historyPath) {
		historyPath = filepath.Join(filepath.Dir(report.Registry), historyPath)
	}
	historyPath, err = filepath.Abs(historyPath)
	if err != nil {
		return portfolioDriftReport{}, err
	}
	history, err := readPortfolioHistory(historyPath)
	if err != nil {
		return portfolioDriftReport{}, err
	}
	out := portfolioDriftReport{
		Schema:   portfolioDriftSchemaVersion,
		At:       time.Now().UTC().Format(time.RFC3339Nano),
		Registry: report.Registry,
		History:  historyPath,
		Recorded: record,
	}
	for _, row := range report.Rows {
		out.Rows = append(out.Rows, evaluatePortfolioDrift(row, history))
	}
	if record {
		var records []portfolioHistoryRecord
		for _, row := range report.Rows {
			var distance *int
			if row.EffectiveDistance != nil {
				distance = intPtr(*row.EffectiveDistance)
			}
			var closed *int
			if row.ClosedSteps != nil {
				closed = intPtr(*row.ClosedSteps)
			}
			records = append(records, portfolioHistoryRecord{
				Schema: portfolioDriftSchemaVersion,
				At:     out.At, Product: row.ID,
				Distance: distance, Source: row.DistanceSource,
				ClosedSteps: closed, PlanContract: row.PlanContract,
				Note: note,
			})
		}
		if err := appendPortfolioHistory(historyPath, records); err != nil {
			return portfolioDriftReport{}, err
		}
	}
	return out, nil
}

func printPortfolioDrift(report portfolioDriftReport) {
	fmt.Printf("AIR portfolio drift: %d products%s", len(report.Rows), lineEnding)
	fmt.Printf("%-14s | %-9s | %-22s | %-22s | %-7s%s",
		"Product", "distance", "source", "movement", "stalls", lineEnding)
	fmt.Printf("%s%s", strings.Repeat("-", 88), lineEnding)
	for _, row := range report.Rows {
		source := row.Source
		if source == "" {
			source = "-"
		}
		fmt.Printf("%-14s | %-9s | %-22s | %-22s | %-7d%s",
			row.Name, portfolioInt(row.Distance), source, row.Movement, row.StallMoves, lineEnding)
	}
	fmt.Printf("history: %s%s", report.History, lineEnding)
}

func cmdPortfolioDrift(registryPath, historyPath, note string, record, asJSON, quiet bool) int {
	path := defaultPortfolioRegistryPath(registryPath)
	report, err := buildPortfolioDrift(path, historyPath, note, record)
	if err != nil {
		if !quiet {
			fmt.Fprintf(os.Stderr, "drift -all: %v%s", err, lineEnding)
		}
		return 2
	}
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
	} else if !quiet {
		printPortfolioDrift(report)
	}
	return 0
}

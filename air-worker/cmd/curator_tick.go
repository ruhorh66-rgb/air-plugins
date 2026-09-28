package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

func cmdCuratorTick(argv []string) int {
	fs := flag.NewFlagSet("curator tick", flag.ContinueOnError)
	product := fs.String("product", "", "managed product root")
	nowRaw := fs.String("now", "", "override current time RFC3339")
	asJSON := fs.Bool("json", false, "machine-readable output")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	root, err := normalizePlanNodeProduct(*product)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	now := time.Now().UTC()
	if strings.TrimSpace(*nowRaw) != "" {
		now, err = time.Parse(time.RFC3339, strings.TrimSpace(*nowRaw))
		if err != nil {
			fmt.Fprintln(os.Stderr, "curator tick -now:", err)
			return 2
		}
	}
	stats, err := calculatePlanNodeStats(root, now)
	if err != nil {
		fmt.Fprintln(os.Stderr, "curator tick:", err)
		return 2
	}
	if *asJSON {
		doc := map[string]any{
			"schema":       "air-worker.curator.tick/v1",
			"generated_at": now.UTC().Format(time.RFC3339Nano),
			"product":      root,
			"open_nodes":   stats.Open,
			"no_owner":     stats.NoOwner,
			"stale_24h":    stats.Stale24h,
		}
		if err := json.NewEncoder(os.Stdout).Encode(doc); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		return 0
	}
	fmt.Printf("открытых узлов %d, без владельца %d, без движения >24 ч %d%s",
		stats.Open, stats.NoOwner, stats.Stale24h, lineEnding)
	return 0
}

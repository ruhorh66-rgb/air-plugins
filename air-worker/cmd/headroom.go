package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type headroomHealthDoc struct {
	Service string `json:"service"`
	Status  string `json:"status"`
	Ready   bool   `json:"ready"`
	Version string `json:"version"`
}

func headroomPreflight() (headroomHealthDoc, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(headroomBaseURL + "/health")
	if err != nil {
		return headroomHealthDoc{}, fmt.Errorf("headroom health: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return headroomHealthDoc{}, fmt.Errorf("headroom health returned HTTP %d", resp.StatusCode)
	}
	var doc headroomHealthDoc
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return headroomHealthDoc{}, fmt.Errorf("headroom health JSON: %w", err)
	}
	if !doc.Ready || doc.Status != "healthy" {
		return doc, fmt.Errorf("headroom not ready: status=%q ready=%t", doc.Status, doc.Ready)
	}
	return doc, nil
}

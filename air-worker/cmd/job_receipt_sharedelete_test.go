//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestReceiptAtomicReplaceWhileReadersActive(t *testing.T) {
	root := t.TempDir()
	scope := newScope(root, sessionIdentity{Principal: "codex", SessionKey: "receipt-share-test"})
	if err := os.MkdirAll(filepath.Join(root, ".woody", "jobs"), 0700); err != nil {
		t.Fatal(err)
	}
	receipt := &jobReceipt{Product: root, Principal: "codex", Session: "receipt-share-test", Step: "review", Operation: "atomic", Model: strings.Repeat("a", 65536), PID: 1}
	path, _ := jobReceiptPaths(scope, receipt.Step, receipt.Operation)
	if err := writeJobReceipt(path, receipt); err != nil {
		t.Fatal(err)
	}
	var stop atomic.Bool
	var readers sync.WaitGroup
	problems := make(chan error, 4)
	for i := 0; i < 4; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for !stop.Load() {
				if _, err := readJobReceipt(path); err != nil {
					problems <- err
					return
				}
			}
		}()
	}
	for i := 2; i < 102; i++ {
		receipt.PID = i
		if err := writeJobReceipt(path, receipt); err != nil {
			stop.Store(true)
			readers.Wait()
			t.Fatalf("atomic replacement %d: %v", i, err)
		}
	}
	stop.Store(true)
	readers.Wait()
	close(problems)
	for err := range problems {
		t.Errorf("receipt read: %v", err)
	}
	got, err := readJobReceipt(path)
	if err != nil || got.PID != 101 {
		t.Fatalf("final receipt: %+v, %v", got, err)
	}
}

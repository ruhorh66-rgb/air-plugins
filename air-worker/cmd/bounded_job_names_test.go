package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDW32BoundedReceiptNames(t *testing.T) {
	scope := newScope(t.TempDir(), sessionIdentity{Principal: "gpt", SessionKey: "short"})
	r, o := jobReceiptPaths(scope, "step", "operation")
	old := reNonWord.ReplaceAllString(scope.ID.namespace()+"__step__operation", "-")
	if filepath.Base(r) != old+".receipt.json" || filepath.Base(o) != old+".out" {
		t.Fatal("short legacy path changed")
	}
	long := newScope(scope.Root, sessionIdentity{Principal: "gpt", SessionKey: strings.Repeat("s", 120)})
	step, operation := strings.Repeat("a", 100), strings.Repeat("b", 100)
	a, b := jobReceiptPaths(long, step, operation)
	a2, b2 := jobReceiptPaths(long, step, operation)
	if a != a2 || b != b2 || !strings.HasPrefix(filepath.Base(a), "awjob-") || len(filepath.Base(a)) > 255 || len(filepath.Base(b)) > 255 {
		t.Fatal("long mapping is not bounded and deterministic")
	}
	other, _ := jobReceiptPaths(long, step, operation+"c")
	if other == a {
		t.Fatal("different operations collided")
	}
	other, _ = jobReceiptPaths(long, step+"a", operation)
	if other == a {
		t.Fatal("different steps collided")
	}
	otherScope := newScope(scope.Root, sessionIdentity{Principal: "gpt", SessionKey: strings.Repeat("s", 119) + "t"})
	other, _ = jobReceiptPaths(otherScope, step, operation)
	if other == a {
		t.Fatal("different sessions collided")
	}
}

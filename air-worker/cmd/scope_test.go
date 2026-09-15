package main

import (
	"os"
	"strings"
	"sync"
	"testing"
)

// К62: mutable runtime state, locks, gates/receipts и ownership namespaced минимум по
// (product, principal, session, job); две сессии одного продукта работают параллельно;
// global singleton-lock на рабочую сессию запрещён; cross-principal/session mutation
// отклоняется; machine-wide install/tray/version metadata не используется как session lock.
func TestCriterion62Pending(t *testing.T) {
	root := setupSessionProduct(t)

	idA := sessionIdentity{"claude", "sess-a"}
	idB := sessionIdentity{"claude", "sess-b"} // тот же principal, другая session — худший случай для продукт-only замка
	scopeA := newScope(root, idA)
	scopeB := newScope(root, idB)

	// (1) две независимые сессии ОДНОГО продукта стартуют одновременно, не блокируя друг
	// друга — namespace (product, principal, session) держит замки раздельно.
	lockA, okA := acquireLock(scopedLockName("loop", scopeA))
	if !okA {
		t.Fatal("сессия A обязана взять свой замок")
	}
	defer lockA.release()
	lockB, okB := acquireLock(scopedLockName("loop", scopeB))
	if !okB {
		t.Fatal("сессия B заблокирована сессией A на том же продукте — global singleton-lock на сессию запрещён")
	}
	defer lockB.release()

	// (2) регрессия на сам примитив: та же identity дважды подряд — тот же замок, и
	// повторный захват честно отказывает (иначе это не замок вовсе).
	if _, ok := acquireLock(scopedLockName("loop", scopeA)); ok {
		t.Fatal("повторный замок той же сессии обязан отказать")
	}

	// (3) разные продукты с одной и той же identity тоже не сталкиваются — product это
	// отдельная ось namespace, а не довесок к identity.
	scopeOtherProduct := newScope(t.TempDir(), idA)
	lockC, okC := acquireLock(scopedLockName("loop", scopeOtherProduct))
	if !okC {
		t.Fatal("та же identity на другом продукте обязана получить свой замок")
	}
	defer lockC.release()

	// (4) job получает собственный job_id внутри scope.
	job := newJobState(scopeA)
	if job.JobID == "" {
		t.Fatal("job_id пуст")
	}
	jobSame := newJobState(scopeA)
	if jobSame.JobID == job.JobID {
		t.Fatal("два job в одном scope получили одинаковый job_id")
	}

	// (5) cross-scope mutation отказывает с НАЗВАННОЙ причиной — fail-closed, не молчаливый
	// пропуск и не предупреждение.
	if err := assertJobOwnership(job, idB); err == nil {
		t.Fatal("сессия B обязана получить отказ на job сессии A")
	} else if !strings.Contains(err.Error(), "ОТКАЗ") || !strings.Contains(err.Error(), "cross-scope") {
		t.Fatalf("отказ обязан называть причину: %v", err)
	}

	// Владелец своего же job отказа не получает.
	if err := assertJobOwnership(job, idA); err != nil {
		t.Fatalf("владелец job получил отказ на свой же job: %v", err)
	}

	// (6) конкурентный смоук: реальные горутины на разных scope не видят чужого замка.
	var wg sync.WaitGroup
	scopes := []sessionScope{
		newScope(root, sessionIdentity{"claude", "sess-c"}),
		newScope(root, sessionIdentity{"codex", "sess-d"}),
	}
	results := make([]bool, len(scopes))
	for i, sc := range scopes {
		wg.Add(1)
		go func(i int, sc sessionScope) {
			defer wg.Done()
			l, ok := acquireLock(scopedLockName("loop", sc))
			results[i] = ok
			if ok {
				l.release()
			}
		}(i, sc)
	}
	wg.Wait()
	for i, ok := range results {
		if !ok {
			t.Fatalf("сессия %d не стартовала одновременно с другими: %v", i, results)
		}
	}

	// (7) machine-wide install/tray/version metadata не используется как session lock:
	// namespace замка петли строится только из sessionScope (product+identity), а не из
	// чего-либо, что решает installGuard/tray.
	src, err := os.ReadFile("scope.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), "installGuard") || strings.Contains(string(src), "trayctl") {
		t.Fatal("scope.go обязан не зависеть от machine-wide install/tray guard — это read-only host state, не замок сессии")
	}
}

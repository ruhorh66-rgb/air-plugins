# А2.5 · передача: вливание в интеграцию Dev Work32 (16.11.2026)

Ветка `dev/airworker-a2-reviewer-stage-from-rc5`. А2.5 добавлен двумя коммитами поверх `b355dd1`: `5203987` и `b9f3ad6`. Судья Codex: раунд 2 — PASS, NO-OVERWRITE CONFIRMED, RUNNING/UNKNOWN NOT RETRIED CONFIRMED (предварительно).

## Пробное вливание

- База: `integration/selflearning-a123-20261116` на `933733c`. Взята через `git fetch` из дерева Dev Work32 только на чтение; его дерево не менялось.
- Команда: `git merge --no-ff b9f3ad6`. Результат — коммит `033ed30`, дерево `dbe70b69d7f8b1c7d8af0203078e399b35346253`. Пробное дерево снято.
- **Конфликтов нет, ручного разрешения не требуется.** `learn_adapter.go` git слил сам. `jobKey` из А3 живёт в сигнатуре `learningReviewerInvoke` (обёртка вызова и тесты) и не пересекается с правкой А2.5. А2.5 меняет `invokeLearningReviewerCodex` и `recoverLearningReviewerJob` внутри. Правка `jobKey` в `learn_adapter_one_class_test.go:13` в `933733c` уже есть.
- Итог вливания относительно `933733c` совпадает с `b355dd1..b9f3ad6`: одинаковый `git patch-id --stable` `e0127c8c…`, 6 файлов, +343/−34.
- Проверка на `033ed30`: `go vet` rc=0; `go test -run 'A1|A2|^TestA24|^TestA25|A3|^TestA33|^TestA34|OneClass|N102|Reviewer|JobReceipt'` — ok за 68 с. Флаги были `-p 1`, `GOGC=25`, `GOMAXPROCS=2`: на узле почти не осталось свободной памяти (виртуальная машина `vmmem` занимает 24,5 ГБ), и сборка без них падала с out of memory.

## Изменение `learn_adapter.go` при вливании (для сверки)

```diff
diff --git a/air-worker/cmd/learn_adapter.go b/air-worker/cmd/learn_adapter.go
index f4cddc5..c1a303a 100644
--- a/air-worker/cmd/learn_adapter.go
+++ b/air-worker/cmd/learn_adapter.go
@@ -10,6 +10,7 @@ import (
 	"io"
 	"os"
 	"path/filepath"
+	"runtime"
 	"sort"
 	"strings"
 )
@@ -109,32 +110,64 @@ func learningReviewerStep(eventSHA string) string {
 	return "learning-" + eventSHA[:16]
 }
 
-// recoverLearningReviewerJob returns the sealed output of a completed review of
-// this same event instead of starting a second one. Anything that does not
-// prove identity and integrity is refused, never overwritten; a job that is not
-// terminal yet is left to runReceipted, which keeps refusing duplicate starts.
-func recoverLearningReviewerJob(scope sessionScope, step, eventSHA string) ([]byte, bool, error) {
-	receiptPath, outputPath := jobReceiptPaths(scope, step, "learning-reviewer")
-	r, err := readJobReceipt(receiptPath)
-	if errors.Is(err, os.ErrNotExist) {
-		return nil, false, nil
-	}
-	if err != nil {
-		return nil, false, fmt.Errorf("learning reviewer job evidence is unreadable: %w", err)
-	}
-	if r.Status == jobStatusRunning {
-		return nil, false, nil
-	}
-	if r.Status != jobStatusDone || r.Step != step || r.Operation != "learning-reviewer" || r.Role != "learning-reviewer" ||
-		eventSHA == "" || r.ReviewTaskSHA256 != eventSHA || !sameLearningPath(r.Product, scope.Root) ||
-		!sameLearningPath(r.OutputPath, outputPath) || r.OutputSHA256 == "" {
-		return nil, false, errors.New("learning reviewer job evidence does not match this event")
+// learningReviewerAttempts is the learning module's own limit: air-modules
+// v0.1.1 offers a review of the same event again while outcome=error and
+// attempt < 2. AirWorker never starts more attempts than the module offers.
+const learningReviewerAttempts = 2
+
+// learningReviewerAttemptStep names attempt n of one event's review. The first
+// attempt keeps the earlier name, so existing receipts stay where they are.
+func learningReviewerAttemptStep(eventSHA string, n int) string {
+	if n <= 1 {
+		return learningReviewerStep(eventSHA)
 	}
-	out, err := readLearningBounded(outputPath, 32<<20)
-	if err != nil || learnSHA(out) != r.OutputSHA256 {
-		return nil, false, errors.New("learning reviewer job evidence does not match this event")
+	return fmt.Sprintf("%s-a%d", learningReviewerStep(eventSHA), n)
+}
+
+// recoverLearningReviewerJob walks this event's attempt receipts in order and
+// returns either the sealed output of a completed attempt or the step of the
+// attempt to start. An attempt is passed over only when its own receipt proves
+// it terminally failed: FAILED, started, exit code recorded, process tree
+// verified ended, output sealed and intact. A RUNNING attempt yields its own
+// step, so runReceipted keeps refusing a duplicate start. Anything else,
+// including an old FAILED receipt without tree proof, is refused and never
+// overwritten or moved. After the module's limit no process is started.
+func recoverLearningReviewerJob(scope sessionScope, eventSHA string) (out []byte, found bool, step string, err error) {
+	mismatch := errors.New("learning reviewer job evidence does not match this event")
+	for n := 1; n <= learningReviewerAttempts; n++ {
+		step = learningReviewerAttemptStep(eventSHA, n)
+		receiptPath, outputPath := jobReceiptPaths(scope, step, "learning-reviewer")
+		r, err := readJobReceipt(receiptPath)
+		if errors.Is(err, os.ErrNotExist) {
+			return nil, false, step, nil
+		}
+		if err != nil {
+			return nil, false, "", fmt.Errorf("learning reviewer job evidence is unreadable: %w", err)
+		}
+		if r.Status == jobStatusRunning {
+			return nil, false, step, nil
+		}
+		if r.Step != step || r.Operation != "learning-reviewer" || r.Role != "learning-reviewer" ||
+			eventSHA == "" || r.ReviewTaskSHA256 != eventSHA || !sameLearningPath(r.Product, scope.Root) ||
+			!sameLearningPath(r.OutputPath, outputPath) || r.OutputSHA256 == "" {
+			return nil, false, "", mismatch
+		}
+		body, err := readLearningBounded(outputPath, 32<<20)
+		if err != nil || learnSHA(body) != r.OutputSHA256 {
+			return nil, false, "", mismatch
+		}
+		switch {
+		case r.Status == jobStatusDone:
+			return body, true, step, nil
+		// Tree proof exists only where the owned tree runs (Windows); elsewhere
+		// a FAILED attempt is never passed over.
+		case runtime.GOOS == "windows" && r.Status == jobStatusFailed && r.ProcessStarted && r.ExitCode != nil && r.ProcessTreeVerified:
+			continue
+		default:
+			return nil, false, "", mismatch
+		}
 	}
-	return out, true, nil
+	return nil, false, "", errors.New("learning reviewer attempts exhausted for this event")
 }
 
 func cmdLearningAdapter(argv []string) int {
@@ -590,7 +623,6 @@ func validLearningProcedureMarkdown(content string) bool {
 }
 
 func invokeLearningReviewerCodex(eventSHA, product, prompt, outputSchema, model, effort string) (string, error) {
-	step := learningReviewerStep(eventSHA)
 	scope := legacyScope(product)
 	// Retention never blocks a review: its outcome is in reviewer-job-archive.jsonl.
 	if s, on, err := readSharedLearningSettings(product); err == nil && on {
@@ -598,7 +630,7 @@ func invokeLearningReviewerCodex(eventSHA, product, prompt, outputSchema, model,
 	}
 	var out []byte
 	var runErr error
-	if recovered, found, err := recoverLearningReviewerJob(scope, step, eventSHA); err != nil {
+	if recovered, found, step, err := recoverLearningReviewerJob(scope, eventSHA); err != nil {
 		return "", err
 	} else if found {
 		out = recovered
```

## Известное ограничение

Пропуск FAILED-попытки возможен только на Windows. Доказательство завершения дерева процессов (`process_tree_verified`) даёт только управляемое дерево Windows. На других ОС после первой FAILED-попытки восстановление отказывает с `reviewer_job_evidence_mismatch`, и вторая попытка не стартует. Старые FAILED-квитанции без `process_tree_verified`, записанные до А2.5, также остаются на разборе и автоматически не повторяются.
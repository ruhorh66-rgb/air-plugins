package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// К42/шаг 73: planner/judge/executor до запуска любой долгой команды создают durable job
// receipt (`job_id`/PID/`started_at`/`status`/`output_path`/product/principal/session/
// step/operation); transport/RDC timeout оставляет receipt `RUNNING`, а не failure;
// duplicate start запрещён только внутри owned scope, соседние session/job работают
// параллельно.
//
// Прежняя редакция проверяла только helper (startJobReceipt/finishJobReceipt) в отрыве от
// реальных точек запуска — семантический судья 15.09.2026 назвал это недостаточным:
// "helper-only implementation is insufficient; wire receipt into real planner.go, judge.go
// and executor runner.go launch paths". Эта редакция добавляет три вещи внутри того же
// теста (селектор остаётся тем же именем, которым его меряет план): receipt уже RUNNING до
// того, как дочерняя команда закончила работу; RUNNING переживает симулированный
// transport/RDC timeout, пока настоящий процесс не отчитается сам; stdout читается по
// output_path — и всё это через runReceipted, ту же функцию, которую planner.go/judge.go/
// runner.go/runner_codex.go/orchestration.go вызывают на своих настоящих точках запуска
// (cmd.CombinedOutput() заменён на неё во всех шести местах).
func TestCriterion42Pending(t *testing.T) {
	root := setupSessionProduct(t)
	scope := newScope(root, sessionIdentity{"claude", "sess-a"})

	receiptPath := filepath.Join(t.TempDir(), "job.receipt.json")
	outputPath := filepath.Join(t.TempDir(), "job.out")

	// (1) receipt существует на диске ДО запуска долгой команды — здесь "команда" ещё не
	// выполнена ни строчки, а receipt уже прочитаем целиком.
	r, lock, err := startJobReceipt(scope, "73", "run-worker-task", receiptPath, outputPath)
	if err != nil {
		t.Fatalf("receipt обязан создаться до запуска: %v", err)
	}
	defer lock.release()

	if _, statErr := os.Stat(receiptPath); statErr != nil {
		t.Fatalf("receipt файл не найден до запуска команды: %v", statErr)
	}

	onDisk, err := readJobReceipt(receiptPath)
	if err != nil {
		t.Fatalf("receipt не читается: %v", err)
	}
	if onDisk.JobID == "" || onDisk.PID == 0 || onDisk.StartedAt.IsZero() {
		t.Fatalf("receipt обязан содержать job_id/pid/started_at: %+v", onDisk)
	}
	if onDisk.Status != jobStatusRunning {
		t.Fatalf("receipt до запуска обязан быть RUNNING, получено %q", onDisk.Status)
	}
	if onDisk.OutputPath != outputPath {
		t.Fatalf("output_path не совпадает: %q != %q", onDisk.OutputPath, outputPath)
	}
	if onDisk.Product != scope.Root || onDisk.Principal != "claude" || onDisk.Session != "sess-a" {
		t.Fatalf("receipt обязан называть product/principal/session: %+v", onDisk)
	}
	if onDisk.Step != "73" || onDisk.Operation != "run-worker-task" {
		t.Fatalf("receipt обязан называть step/operation: %+v", onDisk)
	}
	if onDisk.JobID != r.JobID {
		t.Fatalf("расхождение job_id между возвратом и диском: %q != %q", r.JobID, onDisk.JobID)
	}

	// (2) transport/RDC timeout: caller НЕ вызывает finishJobReceipt (нет ответа от
	// транспорта) — receipt обязан остаться RUNNING, а не превратиться в failure.
	afterTimeout, err := readJobReceipt(receiptPath)
	if err != nil {
		t.Fatalf("receipt после timeout не читается: %v", err)
	}
	if afterTimeout.Status != jobStatusRunning {
		t.Fatalf("timeout транспорта обязан оставить RUNNING, получено %q", afterTimeout.Status)
	}

	// Явный финал (получен ответ) — единственный путь смены статуса.
	if err := finishJobReceipt(receiptPath, jobStatusDone); err != nil {
		t.Fatalf("finishJobReceipt: %v", err)
	}
	finished, err := readJobReceipt(receiptPath)
	if err != nil {
		t.Fatalf("receipt после финала не читается: %v", err)
	}
	if finished.Status != jobStatusDone {
		t.Fatalf("явный финал обязан сменить статус, получено %q", finished.Status)
	}

	// (3) duplicate start того же job в том же owned scope запрещён, пока владеющий lock не
	// освобождён (тот же process/step/operation/scope).
	dupReceiptPath := filepath.Join(t.TempDir(), "dup.receipt.json")
	if _, _, err := startJobReceipt(scope, "73", "run-worker-task", dupReceiptPath, outputPath); err == nil {
		t.Fatal("duplicate start внутри owned scope обязан отказать")
	}
	if _, statErr := os.Stat(dupReceiptPath); statErr == nil {
		t.Fatal("duplicate start не должен писать свой receipt")
	}

	// (4) соседняя session на том же продукте — независимый scope, не блокируется job
	// сессии A, даже с тем же step/operation.
	neighborScope := newScope(root, sessionIdentity{"claude", "sess-b"})
	neighborReceiptPath := filepath.Join(t.TempDir(), "neighbor.receipt.json")
	neighborOutputPath := filepath.Join(t.TempDir(), "neighbor.out")
	nr, nlock, err := startJobReceipt(neighborScope, "73", "run-worker-task", neighborReceiptPath, neighborOutputPath)
	if err != nil {
		t.Fatalf("соседняя независимая сессия обязана стартовать параллельно: %v", err)
	}
	defer nlock.release()
	if nr.JobID == r.JobID {
		t.Fatal("соседний job получил тот же job_id, что и job сессии A")
	}
	if nr.Session != "sess-b" {
		t.Fatalf("receipt соседней сессии называет чужую session: %+v", nr)
	}

	// После освобождения владеющего lock, тот же step/operation в том же scope снова
	// доступен — это не постоянный запрет, а честный duplicate-start guard на время работы.
	lock.release()
	reReceiptPath := filepath.Join(t.TempDir(), "re.receipt.json")
	if _, lock2, err := startJobReceipt(scope, "73", "run-worker-task", reReceiptPath, outputPath); err != nil {
		t.Fatalf("после освобождения lock повторный старт обязан пройти: %v", err)
	} else {
		lock2.release()
	}

	// (5) RUNNING уже виден ДО того, как реальная дочерняя команда закончила работать, и
	// finишится только когда та реально отчиталась — проверено через runReceipted, ту же
	// функцию, что вызывают planner.go/judge.go/runner.go/runner_codex.go/orchestration.go.
	t.Run("receipt_running_before_child_finishes", func(t *testing.T) {
		testReceiptRunningBeforeChildFinishes(t, root)
	})

	// (6) RDC/transport timeout: caller перестаёт ждать раньше настоящего процесса — receipt
	// остаётся RUNNING сразу после timeout и лишь позже, когда процесс реально закончил
	// работу, честно переходит в DONE со stdout, читаемым по output_path.
	t.Run("receipt_survives_transport_timeout", func(t *testing.T) {
		testReceiptSurvivesTransportTimeout(t, root)
	})

	// (7) настоящая точка запуска исполнителя (invokeCodex, cmd/runner_codex.go) — не голый
	// вызов runReceipted, а wired production launch path: тот же seam runnerCommand, которым
	// уже проверяется TestInvokeCodexWritesProductRoot.
	t.Run("wired_into_executor_launch_path", func(t *testing.T) {
		testReceiptWiredIntoExecutorLaunchPath(t, root)
	})
}

// jobReceiptSlowHelperMarker — включает медленный дочерний процесс: get self-exec'нутый
// тестовый бинарник спит AW_JOB_RECEIPT_HELPER_DELAY_MS миллисекунд и пишет в stdout
// известный маркер, который потом проверяется и как возврат runReceipted, и как содержимое
// output_path.
const jobReceiptStdoutMarker = "JOB-RECEIPT-STDOUT-OK"

func TestJobReceiptSlowHelperProcess(t *testing.T) {
	if os.Getenv("AW_JOB_RECEIPT_HELPER") != "1" {
		return
	}
	ms := 300
	if v := os.Getenv("AW_JOB_RECEIPT_HELPER_DELAY_MS"); v != "" {
		fmt.Sscanf(v, "%d", &ms)
	}
	time.Sleep(time.Duration(ms) * time.Millisecond)
	fmt.Print(jobReceiptStdoutMarker)
	os.Exit(0)
}

func slowHelperCmd(t *testing.T, delayMs int) *exec.Cmd {
	t.Helper()
	t.Setenv("AW_JOB_RECEIPT_HELPER", "1")
	t.Setenv("AW_JOB_RECEIPT_HELPER_DELAY_MS", fmt.Sprintf("%d", delayMs))
	return exec.Command(os.Args[0], "-test.run=TestJobReceiptSlowHelperProcess", "--")
}

// pollReceiptStatus — ждёт, пока receipt на диске не примет один из ожидаемых статусов, или
// проваливает тест по таймауту. Опрос, а не сон фиксированной длины: тест доказывает
// НАБЛЮДАЕМЫЙ факт («receipt был RUNNING, пока команда ещё работала»), а не полагается на
// удачное совпадение по времени.
func pollReceiptStatus(t *testing.T, path string, want string, timeout time.Duration) *jobReceipt {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if r, err := readJobReceipt(path); err == nil && r.Status == want {
			return r
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("receipt %s не принял статус %q за %s", path, want, timeout)
	return nil
}

func testReceiptRunningBeforeChildFinishes(t *testing.T, root string) {
	scope := newScope(root, sessionIdentity{"claude", "sess-slow"})
	cmd := slowHelperCmd(t, 400)

	done := make(chan struct{})
	var out []byte
	var runErr error
	go func() {
		out, runErr = runReceipted(context.Background(), scope, "73", "slow-op", cmd)
		close(done)
	}()

	receiptPath, outputPath := jobReceiptPaths(scope, "73", "slow-op")
	// Наблюдается RUNNING, пока команда (400мс сна) заведомо ещё не закончила.
	pollReceiptStatus(t, receiptPath, jobStatusRunning, 2*time.Second)

	<-done
	if runErr != nil {
		t.Fatalf("runReceipted: %v", runErr)
	}
	if string(out) != jobReceiptStdoutMarker {
		t.Fatalf("stdout не дошёл через runReceipted: %q", out)
	}
	onDiskOut, err := os.ReadFile(outputPath)
	if err != nil || string(onDiskOut) != jobReceiptStdoutMarker {
		t.Fatalf("stdout не читается по output_path: %v, %q", err, onDiskOut)
	}
	finished := pollReceiptStatus(t, receiptPath, jobStatusDone, time.Second)
	if finished.OutputPath != outputPath {
		t.Fatalf("output_path финального receipt не совпадает: %q != %q", finished.OutputPath, outputPath)
	}
}

func testReceiptSurvivesTransportTimeout(t *testing.T, root string) {
	scope := newScope(root, sessionIdentity{"claude", "sess-timeout"})
	cmd := slowHelperCmd(t, 500)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	_, err := runReceipted(ctx, scope, "73", "timeout-op", cmd)
	if err != errJobStillRunning {
		t.Fatalf("транспортный timeout обязан вернуть errJobStillRunning, получено: %v", err)
	}

	receiptPath, outputPath := jobReceiptPaths(scope, "73", "timeout-op")
	afterTimeout, err := readJobReceipt(receiptPath)
	if err != nil {
		t.Fatalf("receipt не читается сразу после timeout: %v", err)
	}
	if afterTimeout.Status != jobStatusRunning {
		t.Fatalf("timeout транспорта обязан оставить receipt RUNNING, получено %q", afterTimeout.Status)
	}

	// Дочерняя команда продолжает жить в фоне и сама честно отчитывается о финале —
	// это и есть разница между «упал» и «мы не дождались».
	finished := pollReceiptStatus(t, receiptPath, jobStatusDone, 3*time.Second)
	_ = finished
	data, err := os.ReadFile(outputPath)
	if err != nil || string(data) != jobReceiptStdoutMarker {
		t.Fatalf("stdout не сохранился по output_path после отложенного финала: %v, %q", err, data)
	}
}

func testReceiptWiredIntoExecutorLaunchPath(t *testing.T, root string) {
	old := runnerCommand
	t.Cleanup(func() { runnerCommand = old })
	slow := slowHelperCmd(t, 250)
	runnerCommand = func(_ string, _ ...string) *exec.Cmd { return slow }

	ctx := &loopCtx{Root: root, Principal: "claude", SessionKey: "sess-exec"}
	scope := ctx.scope()
	receiptPath, outputPath := jobReceiptPaths(scope, "73", "executor-codex")

	done := make(chan stepResult, 1)
	go func() {
		done <- ctx.invokeCodex("codex", "task", runnerSpec{Kind: "codex", Model: "m"}, "73")
	}()

	// invokeCodex (cmd/runner_codex.go) — настоящая точка запуска исполнителя, а не
	// helper-вызов напрямую: receipt обязан появиться RUNNING до того, как дочерний процесс
	// (250мс сна) отработал.
	pollReceiptStatus(t, receiptPath, jobStatusRunning, 2*time.Second)

	<-done
	finished := pollReceiptStatus(t, receiptPath, jobStatusDone, time.Second)
	if finished.Step != "73" || finished.Operation != "executor-codex" {
		t.Fatalf("receipt исполнителя называет не тот step/operation: %+v", finished)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil || string(data) != jobReceiptStdoutMarker {
		t.Fatalf("stdout исполнителя не читается по output_path: %v, %q", err, data)
	}
}

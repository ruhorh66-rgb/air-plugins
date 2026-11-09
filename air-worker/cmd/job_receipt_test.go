package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestJobStdinFingerprint(t *testing.T) {
	command := func(input *strings.Reader) *exec.Cmd {
		cmd := exec.Command("example", "same-argv")
		cmd.Stdin = input
		return cmd
	}
	first := command(strings.NewReader("first prompt"))
	second := command(strings.NewReader("second prompt"))
	firstHash, secondHash := jobStdinFingerprint(first), jobStdinFingerprint(second)
	if firstHash == "" || secondHash == "" || firstHash == secondHash {
		t.Fatalf("distinct stdin must have distinct hashes: %q, %q", firstHash, secondHash)
	}
	if jobCommandFingerprint(first) != jobCommandFingerprint(second) {
		t.Fatal("stdin changed the existing argv fingerprint")
	}

	reader := strings.NewReader("prefix remaining")
	if _, err := reader.Seek(int64(len("prefix ")), 0); err != nil {
		t.Fatal(err)
	}
	cmd := command(reader)
	want := fmt.Sprintf("%x", sha256.Sum256([]byte("remaining")))
	if got := jobStdinFingerprint(cmd); got != want {
		t.Fatalf("remaining stdin hash = %q, want %q", got, want)
	}
	if reader.Len() != len("remaining") {
		t.Fatal("fingerprinting consumed stdin")
	}
	if got := jobStdinFingerprint(command(strings.NewReader(""))); got != fmt.Sprintf("%x", sha256.Sum256(nil)) {
		t.Fatalf("empty stdin was not measured: %q", got)
	}
	if got := jobStdinFingerprint(command(strings.NewReader(strings.Repeat("x", 128<<10)))); got == "" {
		t.Fatal("128 KiB stdin was not measured")
	}
	if got := jobStdinFingerprint(command(strings.NewReader(strings.Repeat("x", (128<<10)+1)))); got != "" {
		t.Fatalf("oversized stdin was measured: %q", got)
	}
}

func TestJobStdinFingerprintUnsupported(t *testing.T) {
	if got := jobStdinFingerprint(nil); got != "" {
		t.Fatalf("nil command hash = %q", got)
	}
	cmd := exec.Command("example")
	if got := jobStdinFingerprint(cmd); got != "" {
		t.Fatalf("nil stdin hash = %q", got)
	}
	var typedNil *strings.Reader
	cmd.Stdin = typedNil
	if got := jobStdinFingerprint(cmd); got != "" {
		t.Fatalf("typed nil stdin hash = %q", got)
	}
	reader := bytes.NewBufferString("must remain unread")
	cmd.Stdin = reader
	if got := jobStdinFingerprint(cmd); got != "" {
		t.Fatalf("unsupported stdin hash = %q", got)
	}
	if reader.Len() != len("must remain unread") {
		t.Fatal("unsupported stdin was read")
	}
}

func TestJobReceiptLegacyStdinSHA256(t *testing.T) {
	var receipt jobReceipt
	if err := json.Unmarshal([]byte(`{"job_id":"legacy","args_sha256":"old"}`), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.JobID != "legacy" || receipt.ArgsSHA256 != "old" || receipt.StdinSHA256 != "" {
		t.Fatalf("legacy receipt changed: %+v", receipt)
	}
}

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

func TestRunReceiptedFailsClosedWhenPIDRegistrationFails(t *testing.T) {
	root := setupSessionProduct(t)
	scope := newScope(root, sessionIdentity{"claude", "sess-pid-failure"})
	cmd := slowHelperCmd(t, 30_000)

	oldWriter := writeJobReceiptFunc
	t.Cleanup(func() { writeJobReceiptFunc = oldWriter })
	writes := 0
	writeJobReceiptFunc = func(path string, receipt *jobReceipt) error {
		writes++
		if writes == 3 {
			return fmt.Errorf("тестовая ошибка регистрации PID")
		}
		return oldWriter(path, receipt)
	}

	_, err := runReceipted(context.Background(), scope, "4", "pid-registration", cmd)
	if err == nil || !strings.Contains(err.Error(), "не удалось зарегистрировать PID") {
		t.Fatalf("ожидалась ошибка регистрации PID, получено: %v", err)
	}
	if cmd.ProcessState == nil || cmd.ProcessState.Exited() == false {
		t.Fatal("дочерний процесс должен быть завершён после срыва регистрации PID")
	}
	receiptPath, _ := jobReceiptPaths(scope, "4", "pid-registration")
	receipt, readErr := readJobReceipt(receiptPath)
	if readErr != nil {
		t.Fatalf("квитанция не читается: %v", readErr)
	}
	if receipt.Status != jobStatusFailed {
		t.Fatalf("квитанция должна быть FAILED после срыва регистрации PID: %+v", receipt)
	}
	if receipt.Status == jobStatusRunning && receipt.ProcessStarted && receipt.PID > 0 {
		t.Fatalf("квитанция не должна оставаться RUNNING с живым PID: %+v", receipt)
	}
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
	runner := runnerSpec{Kind: "codex", Model: "m"}
	operation := runnerReceiptOperation("executor-codex", runner, ctx.iter)
	receiptPath, outputPath := jobReceiptPaths(scope, "73", operation)

	done := make(chan stepResult, 1)
	go func() {
		done <- ctx.invokeCodex("codex", "task", runner, "73")
	}()

	// invokeCodex (cmd/runner_codex.go) — настоящая точка запуска исполнителя, а не
	// helper-вызов напрямую: receipt обязан появиться RUNNING до того, как дочерний процесс
	// (250мс сна) отработал.
	pollReceiptStatus(t, receiptPath, jobStatusRunning, 2*time.Second)

	<-done
	finished := pollReceiptStatus(t, receiptPath, jobStatusDone, time.Second)
	if finished.Step != "73" || finished.Operation != operation {
		t.Fatalf("receipt исполнителя называет не тот step/operation: %+v", finished)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil || string(data) != jobReceiptStdoutMarker {
		t.Fatalf("stdout исполнителя не читается по output_path: %v, %q", err, data)
	}
}

func TestN123TerminalReceiptReplayCannotOverwrite(t *testing.T) {
	root := setupSessionProduct(t)
	scope := newScope(root, sessionIdentity{"gpt", "n123-terminal-replay"})
	dir := t.TempDir()
	receiptPath, outputPath := filepath.Join(dir, "job.receipt.json"), filepath.Join(dir, "job.out")

	first, lock, err := startJobReceipt(scope, "N123", "codex-run", receiptPath, outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := finishJobReceipt(receiptPath, jobStatusDone); err != nil {
		lock.release()
		t.Fatal(err)
	}
	lock.release()
	before, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, secondLock, err := startJobReceipt(scope, "N123", "codex-run", receiptPath, outputPath); err == nil {
		if secondLock != nil {
			secondLock.release()
		}
		t.Fatal("same scoped job was executed again after terminal; must use saved result")
	} else if secondLock != nil {
		secondLock.release()
	}
	after, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("terminal receipt bytes changed during duplicate start")
	}
	r, err := readJobReceipt(receiptPath)
	if err != nil || r.JobID != first.JobID || r.Status != jobStatusDone {
		t.Fatalf("terminal job identity/status not preserved: %+v %v", r, err)
	}
}

func TestN123OrphanOutputRejectsBlindJobRestart(t *testing.T) {
	root := setupSessionProduct(t)
	scope := newScope(root, sessionIdentity{"gpt", "n123-orphan-output"})
	dir := t.TempDir()
	receiptPath, outputPath := filepath.Join(dir, "job.receipt.json"), filepath.Join(dir, "job.out")
	const existing = "unreconciled historical output"
	if err := os.WriteFile(outputPath, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, lock, err := startJobReceipt(scope, "N123", "codex-run", receiptPath, outputPath); err == nil {
		if lock != nil {
			lock.release()
		}
		t.Fatal("orphan output must block a new job until explicit recovery")
	} else if lock != nil {
		lock.release()
	}
	if _, err := os.Stat(receiptPath); !os.IsNotExist(err) {
		t.Fatalf("orphan-output replay created a new receipt: %v", err)
	}
	raw, err := os.ReadFile(outputPath)
	if err != nil || string(raw) != existing {
		t.Fatalf("orphan output changed: %q / %v", raw, err)
	}
}

func TestN123FinishedReceiptIsImmutableAgainstSecondFinalization(t *testing.T) {
	root := setupSessionProduct(t)
	scope := newScope(root, sessionIdentity{"gpt", "n123-finish-once"})
	dir := t.TempDir()
	receiptPath, outputPath := filepath.Join(dir, "job.receipt.json"), filepath.Join(dir, "job.out")
	_, lock, err := startJobReceipt(scope, "N123", "codex-run", receiptPath, outputPath)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.release()
	if err := finishJobReceipt(receiptPath, jobStatusDone); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := finishJobReceipt(receiptPath, jobStatusFailed); err == nil {
		t.Fatal("DONE must not be overwritten by a second FAILED finalization")
	}
	after, err := os.ReadFile(receiptPath)
	if err != nil || string(after) != string(before) {
		t.Fatalf("terminal receipt modified after finalization: %v", err)
	}
}

func TestN123OwnedJobsDirectoryJunctionFailsBeforeLaunch(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows junction fixture only")
	}
	root, outside := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".woody"), 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(outside, "sentinel")
	if err := os.WriteFile(sentinel, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(root, ".woody", "jobs")
	if out, err := exec.Command("cmd.exe", "/c", "mklink", "/J", junction, outside).CombinedOutput(); err != nil {
		t.Skipf("junction creation unavailable; NOT_PROVEN: %v: %s", err, out)
	}
	scope := newScope(root, sessionIdentity{"gpt", "junction"})
	receiptPath, outputPath := jobReceiptPaths(scope, "N123", "junction")
	marker := filepath.Join(root, "child-started")
	command := exec.Command("cmd.exe", "/c", "echo started > \""+marker+"\"")
	_, err := runReceipted(context.Background(), scope, "N123", "junction", command)
	if err == nil {
		t.Error("junction-backed jobs directory was accepted")
	}
	if data, err := os.ReadFile(sentinel); err != nil || string(data) != "untouched" {
		t.Errorf("outside sentinel changed: %q: %v", data, err)
	}
	for _, path := range []string{marker, receiptPath, outputPath} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Errorf("unexpected child marker or job evidence %s: %v", path, err)
		}
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 1 || entries[0].Name() != "sentinel" {
		t.Errorf("outside directory changed: %v: %v", entries, err)
	}
}

func TestN123OversizeNativeOutputCannotRecordUnsealedDone(t *testing.T) {
	root := t.TempDir()
	scope := newScope(root, sessionIdentity{"gpt", "oversize"})
	receiptPath, outputPath := jobReceiptPaths(scope, "N123", "oversize")
	if err := os.MkdirAll(filepath.Dir(receiptPath), 0o700); err != nil {
		t.Fatal(err)
	}
	r, lock, err := startJobReceipt(scope, "N123", "oversize", receiptPath, outputPath)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.release()
	r.ProcessStarted = true
	if err := writeJobReceipt(receiptPath, r); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outputPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(outputPath, (32<<20)+1); err != nil {
		t.Fatal(err)
	}
	zero := 0
	if err := finishJobReceiptWithExit(receiptPath, jobStatusDone, &zero); err == nil {
		t.Error("oversize output must not be accepted as DONE without a seal")
	}
	finished, err := readJobReceipt(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status == jobStatusDone && finished.OutputSHA256 == "" {
		t.Error("receipt recorded DONE without output SHA256")
	}
}

func TestN123ReceiptChildHelper(t *testing.T) {
	marker := os.Getenv("AW_N123_CHILD_MARKER")
	if marker == "" {
		return
	}
	if err := os.WriteFile(marker, []byte("started"), 0o600); err != nil {
		os.Exit(1)
	}
	fmt.Print(jobReceiptStdoutMarker)
	os.Exit(0)
}

func TestN123RacedOwnedOutputHardlinkNeverTruncatesForeignFile(t *testing.T) {
	fixture := t.TempDir()
	root := filepath.Join(fixture, "product")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(fixture, "outside")
	const sentinel = "foreign evidence must survive"
	if err := os.WriteFile(outside, []byte(sentinel), 0o600); err != nil {
		t.Fatal(err)
	}
	scope := newScope(root, sessionIdentity{"gpt", "hardlink-race"})
	_, outputPath := jobReceiptPaths(scope, "N123", "hardlink-race")
	marker := filepath.Join(fixture, "child-started")
	t.Setenv("AW_N123_CHILD_MARKER", marker)
	command := exec.Command(os.Args[0], "-test.run=^TestN123ReceiptChildHelper$")

	oldWriter := writeJobReceiptFunc
	t.Cleanup(func() { writeJobReceiptFunc = oldWriter })
	writes := 0
	var linkErr error
	writeJobReceiptFunc = func(path string, receipt *jobReceipt) error {
		writes++
		if writes == 2 {
			linkErr = os.Link(outside, outputPath)
			if linkErr != nil {
				return linkErr
			}
		}
		return oldWriter(path, receipt)
	}
	_, err := runReceipted(context.Background(), scope, "N123", "hardlink-race", command)
	if linkErr != nil {
		t.Skipf("hardlink creation unavailable; NOT_PROVEN: %v", linkErr)
	}
	if writes < 2 {
		t.Fatalf("injection point was not reached: %v", err)
	}
	if err == nil {
		t.Error("raced hardlink must reject the job")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("child started or marker could not be checked: %v", err)
	}
	for _, path := range []string{outside, outputPath} {
		if data, err := os.ReadFile(path); err != nil || string(data) != sentinel {
			t.Errorf("foreign evidence changed at %s: %q: %v", path, data, err)
		}
	}
}

func TestN123RootedReceiptUpdateAndTerminalSeal(t *testing.T) {
	root := t.TempDir()
	scope := newScope(root, sessionIdentity{"gpt", "rooted-seal"})
	receiptPath, outputPath := jobReceiptPaths(scope, "N123", "rooted-seal")
	marker := filepath.Join(root, "child-started")
	t.Setenv("AW_N123_CHILD_MARKER", marker)
	command := exec.Command(os.Args[0], "-test.run=^TestN123ReceiptChildHelper$")
	oldWriter := writeJobReceiptFunc
	t.Cleanup(func() { writeJobReceiptFunc = oldWriter })
	sawRunning := false
	writeJobReceiptFunc = func(path string, receipt *jobReceipt) error {
		err := oldWriter(path, receipt)
		if err == nil && receipt.Status == jobStatusRunning {
			onDisk, readErr := readJobReceipt(path)
			sawRunning = sawRunning || (readErr == nil && onDisk.Status == jobStatusRunning)
		}
		return err
	}
	output, err := runReceipted(context.Background(), scope, "N123", "rooted-seal", command)
	if err != nil || !sawRunning || string(output) != jobReceiptStdoutMarker {
		t.Fatalf("job did not complete from RUNNING: output=%q running=%v err=%v", output, sawRunning, err)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "started" {
		t.Fatalf("child marker: %q: %v", data, err)
	}
	receipt, err := readJobReceipt(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	seal := fmt.Sprintf("%x", sha256.Sum256(output))
	if receipt.Status != jobStatusDone || receipt.OutputSHA256 != seal || receipt.OutputBytes != int64(len(output)) {
		t.Fatalf("terminal output was not sealed: %+v", receipt)
	}
	beforeReceipt, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeOutput, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runReceipted(context.Background(), scope, "N123", "rooted-seal", exec.Command(os.Args[0], "-test.run=^TestN123ReceiptChildHelper$")); err == nil {
		t.Error("duplicate terminal job was accepted")
	}
	for path, before := range map[string][]byte{receiptPath: beforeReceipt, outputPath: beforeOutput} {
		if after, err := os.ReadFile(path); err != nil || string(after) != string(before) {
			t.Errorf("terminal evidence changed at %s: %v", path, err)
		}
	}
}

func TestN123OwnedRunnerOutputExclusiveLinkAndSeal(t *testing.T) {
	product := t.TempDir()
	outside := filepath.Join(t.TempDir(), "foreign-sentinel.txt")
	const sentinel = "must not be truncated"
	if err := os.WriteFile(outside, []byte(sentinel), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureOwnedJobsDir(product); err != nil {
		t.Fatal(err)
	}
	jobsDir := filepath.Join(product, ".woody", "jobs")
	attacked := filepath.Join(jobsDir, "output-link.out")
	if err := os.Link(outside, attacked); err != nil {
		t.Skipf("hardlink race fixture unavailable, NOT_PROVEN: %v", err)
	}
	if err := writeOwnedJobOutput(product, attacked, []byte("corrupt")); err == nil {
		t.Fatal("canonical runner output overwrote an existing hardlink")
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != sentinel {
		t.Fatalf("foreign sentinel changed: %q err=%v", got, err)
	}
	good := filepath.Join(jobsDir, "safe.out")
	if err := writeOwnedJobOutput(product, good, []byte("safe")); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(good); err != nil || string(got) != "safe" {
		t.Fatalf("rooted output readback failed: %q err=%v", got, err)
	}
	if err := writeOwnedJobOutput(product, good, []byte("overwrite")); err == nil {
		t.Fatal("existing terminal output accepted a second publication")
	}
	if err := writeOwnedJobOutput(product, filepath.Join(product, "foreign.out"), []byte("bad")); err == nil {
		t.Fatal("noncanonical output path was accepted")
	}
}

func TestRequestStartedDoneRequiresSealedOutput(t *testing.T) {
	for _, tc := range []struct {
		name   string
		output string
	}{
		{name: "missing"},
		{name: "oversized"},
		{name: "valid", output: "request output"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			scope := newScope(root, sessionIdentity{"gpt", tc.name})
			receiptPath, outputPath := jobReceiptPaths(scope, "request", "openai")
			if err := os.MkdirAll(filepath.Dir(receiptPath), 0o700); err != nil {
				t.Fatal(err)
			}
			r, lock, err := startJobReceipt(scope, "request", "openai", receiptPath, outputPath)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.release()
			r.RequestStarted = true
			if err := writeJobReceipt(receiptPath, r); err != nil {
				t.Fatal(err)
			}
			if tc.name != "missing" {
				if err := os.WriteFile(outputPath, []byte(tc.output), 0o600); err != nil {
					t.Fatal(err)
				}
				if tc.name == "oversized" {
					if err := os.Truncate(outputPath, (32<<20)+1); err != nil {
						t.Fatal(err)
					}
				}
			}
			err = finishJobReceipt(receiptPath, jobStatusDone)
			finished, readErr := readJobReceipt(receiptPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if tc.name != "valid" {
				if err == nil || finished.Status == jobStatusDone {
					t.Fatalf("unsealed request accepted: err=%v receipt=%+v", err, finished)
				}
				return
			}
			want := fmt.Sprintf("%x", sha256.Sum256([]byte(tc.output)))
			if err != nil || finished.Status != jobStatusDone || finished.OutputSHA256 != want || finished.OutputBytes != int64(len(tc.output)) {
				t.Fatalf("valid request not sealed: err=%v receipt=%+v", err, finished)
			}
		})
	}
}

func TestN123ReturnedOutputMustMatchTerminalSeal(t *testing.T) {
	root := t.TempDir()
	scope := newScope(root, sessionIdentity{"gpt", "tampered-return"})
	if err := os.MkdirAll(filepath.Join(root, ".woody", "jobs"), 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "child-marker")
	t.Setenv("AW_N123_CHILD_MARKER", marker)
	cmd := exec.Command(os.Args[0], "-test.run=^TestN123ReceiptChildHelper$")
	previous := writeJobReceiptFunc
	t.Cleanup(func() { writeJobReceiptFunc = previous })
	tampered := false
	writeJobReceiptFunc = func(path string, receipt *jobReceipt) error {
		if err := previous(path, receipt); err != nil {
			return err
		}
		if receipt.Status == jobStatusDone {
			if err := os.WriteFile(receipt.OutputPath, []byte("tampered output"), 0600); err != nil {
				return err
			}
			tampered = true
		}
		return nil
	}
	data, err := runReceipted(context.Background(), scope, "N123", "tamper-return", cmd)
	if !tampered {
		t.Fatal("terminal output injection did not run")
	}
	if err == nil || len(data) != 0 {
		t.Fatalf("tampered output accepted: data=%q err=%v", data, err)
	}
}

func TestJudgeRejectsTamperedTerminalOutput(t *testing.T) {
	root := t.TempDir()
	scope := newScope(root, sessionIdentity{"gpt", "judge-tampered-output"})
	marker := filepath.Join(root, "child-marker")
	t.Setenv("AW_N123_CHILD_MARKER", marker)
	previous := writeJobReceiptFunc
	t.Cleanup(func() { writeJobReceiptFunc = previous })
	tampered := false
	writeJobReceiptFunc = func(path string, receipt *jobReceipt) error {
		if err := previous(path, receipt); err != nil {
			return err
		}
		if receipt.Status == jobStatusDone {
			if err := os.WriteFile(receipt.OutputPath, []byte("tampered output"), 0600); err != nil {
				return err
			}
			tampered = true
		}
		return nil
	}
	var result judgeResult
	runCommandCheck(root, checkSpec{Command: os.Args[0], Args: []string{"-test.run=^TestN123ReceiptChildHelper$"}}, "tampered", scope, &result)
	if !tampered {
		t.Fatalf("terminal output injection did not run: %+v", result)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "started" {
		t.Fatalf("child marker: %q: %v", data, err)
	}
	if len(result.Passed) != 0 || len(result.Failed) != 1 {
		t.Fatalf("tampered output must fail the check: %+v", result)
	}
}

func TestExitCodeRejectsErrorAfterSuccessfulProcess(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "child-marker")
	t.Setenv("AW_N123_CHILD_MARKER", marker)
	cmd := exec.Command(os.Args[0], "-test.run=^TestN123ReceiptChildHelper$")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if code := exitCode(cmd, fmt.Errorf("synthetic integrity error")); code == 0 {
		t.Error("successful process with an integrity error returned exit code 0")
	}
	if code := exitCode(cmd, nil); code != 0 {
		t.Errorf("successful process without error returned exit code %d", code)
	}
}

func TestJobReceiptRejectsInvalidAcquiredLock(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows lock handles only")
	}
	for _, tc := range []struct {
		name string
		lock *osLock
	}{{"nil", nil}, {"zero handle", &osLock{}}} {
		t.Run(tc.name, func(t *testing.T) {
			old := jobReceiptAcquireLock
			jobReceiptAcquireLock = func(string) (*osLock, bool) { return tc.lock, true }
			t.Cleanup(func() { jobReceiptAcquireLock = old })

			root := setupSessionProduct(t)
			scope := newScope(root, sessionIdentity{"gpt", "invalid-lock"})
			receiptPath, outputPath := jobReceiptPaths(scope, "preflight", "invalid")
			marker := filepath.Join(t.TempDir(), "child-started")
			t.Setenv("AW_N123_CHILD_MARKER", marker)
			cmd := exec.Command(os.Args[0], "-test.run=^TestN123ReceiptChildHelper$")
			called := false
			output, err := runReceiptedWithPreflight(context.Background(), scope, "preflight", "invalid", cmd, func() error {
				called = true
				return nil
			})
			if err == nil || called || len(output) != 0 || cmd.Process != nil {
				t.Fatalf("invalid lock: preflight=%v output=%q process=%v err=%v", called, output, cmd.Process, err)
			}
			message := strings.ToLower(err.Error())
			if !strings.Contains(message, "mutex недоступен") || strings.Contains(message, "уже запущен") || strings.Contains(message, "already running") || strings.Contains(message, "duplicate") {
				t.Fatalf("invalid lock reported an incorrect reason: %v", err)
			}
			for _, path := range []string{receiptPath, outputPath, marker} {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatalf("invalid lock created %s: %v", path, err)
				}
			}
		})
	}
}

func TestJobReceiptPreflightRunsUnderNativeLock(t *testing.T) {
	root := setupSessionProduct(t)
	scope := newScope(root, sessionIdentity{"gpt", "preflight-lock"})
	dir := t.TempDir()
	receiptPath, outputPath := filepath.Join(dir, "receipt.json"), filepath.Join(dir, "output")
	called := 0
	preflight := func() error {
		called++
		if lock, ok := acquireLock(scopedLockName(jobLockKind("preflight", "locked"), scope)); ok {
			lock.release()
			return fmt.Errorf("native job lock was not held")
		}
		for _, path := range []string{receiptPath, outputPath} {
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				return fmt.Errorf("evidence existed during preflight at %s: %v", path, err)
			}
		}
		return nil
	}
	r, lock, err := startJobReceiptWithPreflight(scope, "preflight", "locked", receiptPath, outputPath, preflight)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.release()
	if r == nil || called != 1 {
		t.Fatalf("preflight calls=%d receipt=%+v", called, r)
	}
	if _, secondLock, err := startJobReceiptWithPreflight(scope, "preflight", "locked", filepath.Join(dir, "second.json"), filepath.Join(dir, "second.out"), preflight); err == nil {
		if secondLock != nil {
			secondLock.release()
		}
		t.Fatal("parallel start acquired the original native job lock")
	} else if called != 1 {
		t.Fatalf("busy start called preflight: %d", called)
	}
}

func TestJobReceiptPreflightFailureLeavesNoEvidenceOrChild(t *testing.T) {
	root := setupSessionProduct(t)
	scope := newScope(root, sessionIdentity{"gpt", "preflight-failure"})
	receiptPath, outputPath := jobReceiptPaths(scope, "preflight", "failure")
	marker := filepath.Join(t.TempDir(), "child-started")
	t.Setenv("AW_N123_CHILD_MARKER", marker)
	cmd := exec.Command(os.Args[0], "-test.run=^TestN123ReceiptChildHelper$")
	called := 0
	want := fmt.Errorf("shared preflight refused")
	output, err := runReceiptedWithPreflight(context.Background(), scope, "preflight", "failure", cmd, func() error {
		called++
		return want
	})
	if !errors.Is(err, want) || called != 1 || len(output) != 0 {
		t.Fatalf("preflight failure: calls=%d output=%q err=%v", called, output, err)
	}
	for _, path := range []string{receiptPath, outputPath, marker} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("preflight failure created %s: %v", path, err)
		}
	}
	if cmd.Process != nil {
		t.Fatal("child process started after preflight failure")
	}
	r, lock, err := startJobReceipt(scope, "preflight", "failure", receiptPath, outputPath)
	if err != nil {
		t.Fatalf("ordinary start after preflight failure: %v", err)
	}
	lock.release()
	if r.Status != jobStatusRunning {
		t.Fatalf("ordinary start changed semantics: %+v", r)
	}
}

func TestJobReceiptPreflightRejectsBusySharedLock(t *testing.T) {
	root := setupSessionProduct(t)
	scope := newScope(root, sessionIdentity{"gpt", "shared-busy"})
	receiptPath, outputPath := jobReceiptPaths(scope, "preflight", "busy")
	shared, ok := acquireLock("preflight-shared-busy")
	if !ok {
		t.Fatal("could not arrange busy shared lock")
	}
	defer shared.release()
	called := 0
	_, lock, err := startJobReceiptWithPreflight(scope, "preflight", "busy", receiptPath, outputPath, func() error {
		called++
		if other, ok := acquireLock("preflight-shared-busy"); ok {
			other.release()
			return fmt.Errorf("busy shared lock was acquired")
		}
		return fmt.Errorf("shared lock busy")
	})
	if lock != nil {
		lock.release()
	}
	if err == nil || err.Error() != "shared lock busy" || called != 1 {
		t.Fatalf("busy shared lock: calls=%d err=%v", called, err)
	}
	for _, path := range []string{receiptPath, outputPath} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("busy shared lock created %s: %v", path, err)
		}
	}
}

func TestRunReceiptedNilPreflightKeepsLegacyBehavior(t *testing.T) {
	root := setupSessionProduct(t)
	scope := newScope(root, sessionIdentity{"gpt", "nil-preflight"})
	t.Setenv("AW_N123_CHILD_MARKER", filepath.Join(t.TempDir(), "child-started"))
	cmd := exec.Command(os.Args[0], "-test.run=^TestN123ReceiptChildHelper$")
	output, err := runReceiptedWithPreflight(context.Background(), scope, "preflight", "nil", cmd, nil)
	if err != nil || string(output) != jobReceiptStdoutMarker {
		t.Fatalf("nil preflight output=%q err=%v", output, err)
	}
	receiptPath, outputPath := jobReceiptPaths(scope, "preflight", "nil")
	r, err := readJobReceipt(receiptPath)
	if err != nil || r.Status != jobStatusDone || r.OutputPath != outputPath {
		t.Fatalf("nil preflight receipt=%+v err=%v", r, err)
	}
}

func TestJudgeCrossFenceChild(t *testing.T) {
	started, release := os.Getenv("AW_JUDGE_STARTED"), os.Getenv("AW_JUDGE_RELEASE")
	if started == "" || release == "" {
		return
	}
	if err := os.WriteFile(started, []byte("started"), 0o600); err != nil {
		os.Exit(1)
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if _, err := os.Stat(release); err == nil {
			fmt.Print(jobReceiptStdoutMarker)
			os.Exit(0)
		}
	}
	os.Exit(1)
}

func TestJudgeCrossFenceAfterInterruptedCaller(t *testing.T) {
	for _, reportFirst := range []bool{false, true} {
		name := "judge-first"
		if reportFirst {
			name = "report-first"
		}
		t.Run(name, func(t *testing.T) {
			root := setupSessionProduct(t)
			scope := newScope(root, sessionIdentity{"gpt", name})
			const operation = "cross-fence"
			firstStep, oppositeStep := "judge", "report-judge"
			if reportFirst {
				firstStep, oppositeStep = oppositeStep, firstStep
			}
			firstReceipt, firstOutput := jobReceiptPaths(scope, firstStep, operation)
			oppositeReceipt, oppositeOutput := jobReceiptPaths(scope, oppositeStep, operation)
			started := filepath.Join(t.TempDir(), "started")
			release := filepath.Join(t.TempDir(), "release")
			cmd := exec.Command(os.Args[0], "-test.run=^TestJudgeCrossFenceChild$")
			cmd.Env = append(os.Environ(), "AW_JUDGE_STARTED="+started, "AW_JUDGE_RELEASE="+release)
			ctx, cancel := context.WithCancel(context.Background())
			result := make(chan error, 1)
			go func() {
				_, err := runJudgeCheckReceipted(ctx, scope, operation, cmd, reportFirst)
				result <- err
			}()
			t.Cleanup(func() {
				cancel()
				_ = os.WriteFile(release, nil, 0o600)
				if _, err := os.Stat(firstReceipt); err == nil {
					pollReceiptStatus(t, firstReceipt, jobStatusDone, 10*time.Second)
				}
			})
			pollJudgeMarker(t, started, 5*time.Second)
			initial := pollReceiptStatus(t, firstReceipt, jobStatusRunning, 5*time.Second)
			cancel()
			select {
			case err := <-result:
				if !errors.Is(err, errJobStillRunning) {
					t.Fatalf("interrupted caller: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("interrupted caller did not return")
			}
			oppositeMarker := filepath.Join(t.TempDir(), "opposite-started")
			oppositeCmd := exec.Command(os.Args[0], "-test.run=^TestN123ReceiptChildHelper$")
			oppositeCmd.Env = append(os.Environ(), "AW_N123_CHILD_MARKER="+oppositeMarker)
			if _, err := runJudgeCheckReceipted(context.Background(), scope, operation, oppositeCmd, !reportFirst); err == nil {
				t.Fatal("opposite check started while original child was running")
			}
			for _, path := range []string{oppositeReceipt, oppositeOutput, oppositeMarker} {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatalf("opposite check created %s: %v", path, err)
				}
			}
			if err := os.WriteFile(release, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			finished := pollReceiptStatus(t, firstReceipt, jobStatusDone, 10*time.Second)
			if finished.JobID != initial.JobID || finished.OutputSHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(jobReceiptStdoutMarker))) {
				t.Fatalf("original job was not sealed: initial=%+v finished=%+v", initial, finished)
			}
			if data, err := os.ReadFile(firstOutput); err != nil || string(data) != jobReceiptStdoutMarker {
				t.Fatalf("original output: %q: %v", data, err)
			}
			lockName := scopedLockName(jobLockKind(firstStep, operation), scope)
			for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
				if lock, ok := acquireLock(lockName); ok {
					lock.release()
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("original job lock was not released")
				}
			}
			secondCmd := exec.Command(os.Args[0], "-test.run=^TestN123ReceiptChildHelper$")
			secondCmd.Env = append(os.Environ(), "AW_N123_CHILD_MARKER="+oppositeMarker)
			out, err := runJudgeCheckReceipted(context.Background(), scope, operation, secondCmd, !reportFirst)
			if err != nil || string(out) != jobReceiptStdoutMarker {
				t.Fatalf("opposite check after seal: output=%q err=%v", out, err)
			}
			if data, err := os.ReadFile(oppositeMarker); err != nil || string(data) != "started" {
				t.Fatalf("opposite child marker: %q: %v", data, err)
			}
		})
	}
}

func pollJudgeMarker(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if data, err := os.ReadFile(path); err == nil && string(data) == "started" {
			return
		}
	}
	t.Fatalf("child did not write started marker at %s", path)
}

func TestReportJudgeRespectsHeldCommonLock(t *testing.T) {
	root := setupSessionProduct(t)
	scope := newScope(root, sessionIdentity{"gpt", "held-common-lock"})
	const operation = "held-lock"
	lock, ok := acquireLock(scopedLockName(jobLockKind("judge", operation), scope))
	if !ok {
		t.Fatal("could not hold common judge lock")
	}
	defer lock.release()
	marker := filepath.Join(t.TempDir(), "started")
	cmd := exec.Command(os.Args[0], "-test.run=^TestN123ReceiptChildHelper$")
	cmd.Env = append(os.Environ(), "AW_N123_CHILD_MARKER="+marker)
	if _, err := runJudgeCheckReceipted(context.Background(), scope, operation, cmd, true); err == nil {
		t.Fatal("report check started while common judge lock was held")
	}
	receipt, output := jobReceiptPaths(scope, "report-judge", operation)
	for _, path := range []string{receipt, output, marker} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("held common lock created %s: %v", path, err)
		}
	}
}

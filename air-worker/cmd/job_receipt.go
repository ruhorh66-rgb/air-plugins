package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Шаг 73/К42 — durable job receipt: planner/judge/executor обязаны создать receipt ДО
// запуска любой долгой команды, а не после. Receipt — единственный источник состояния при
// transport/RDC timeout: таймаут транспорта означает, что мы не дождались ответа, а не что
// job упал, поэтому receipt на диске остаётся RUNNING, пока сам процесс не отчитается о
// своём финале через finishJobReceipt.
const (
	jobStatusRunning = "RUNNING"
	jobStatusDone    = "DONE"
	jobStatusFailed  = "FAILED"
)

// jobReceipt — durable запись о долгой команде: всё необходимое, чтобы прочитать финал по
// output_path, не удерживая сам процесс/сессию, которая её запустила.
type jobReceipt struct {
	JobID          string    `json:"job_id"`
	PID            int       `json:"pid"`
	StartedAt      time.Time `json:"started_at"`
	Status         string    `json:"status"`
	OutputPath     string    `json:"output_path"`
	Product        string    `json:"product"`
	Principal      string    `json:"principal"`
	Session        string    `json:"session"`
	Step           string    `json:"step"`
	Operation      string    `json:"operation"`
	Runner         string    `json:"runner,omitempty"`
	Provider       string    `json:"provider,omitempty"`
	Model          string    `json:"model,omitempty"`
	Effort         string    `json:"effort,omitempty"`
	Role           string    `json:"role,omitempty"`
	Sandbox        string    `json:"sandbox,omitempty"`
	ProcessStarted bool      `json:"process_started"`
	RequestStarted bool      `json:"request_started,omitempty"`
	Cost           *float64  `json:"cost,omitempty"`
	OutputSHA256   string    `json:"output_sha256,omitempty"`
	OutputBytes    int64     `json:"output_bytes,omitempty"`
	SourceHead     string    `json:"source_head,omitempty"`
	ArgsSHA256     string    `json:"args_sha256,omitempty"`
	ExitCode       *int      `json:"exit_code,omitempty"`
	RecoveryRef    string    `json:"recovery_ref,omitempty"`
	DeadlineUTC    string    `json:"deadline_utc,omitempty"`
}

// jobLockKind — duplicate-start guard именует замок по (step, operation) внутри scope: тот
// же job в том же owned scope отказывает повторному старту, а другая identity/сессия на том
// же продукте получает свой собственный замок и продолжает работать параллельно
// (scopedLockName уже даёт разные имена разным scope — см. scope.go, К62).
func jobLockKind(step, operation string) string {
	return "job:" + step + ":" + operation
}

// startJobReceipt — receipt пишется на диск ДО запуска долгой команды: caller обязан вызвать
// это перед exec/RPC, а не после получения результата. Duplicate start внутри того же owned
// scope (тот же product+principal+session, тот же step+operation) отказывает без записи
// receipt; независимый scope (другая сессия, другой продукт) получает свой замок и не
// блокируется.
func startJobReceipt(scope sessionScope, step, operation, receiptPath, outputPath string) (*jobReceipt, *osLock, error) {
	canonicalReceipt, canonicalOutput := jobReceiptPaths(scope, step, operation)
	if filepath.Clean(receiptPath) == filepath.Clean(canonicalReceipt) || filepath.Clean(outputPath) == filepath.Clean(canonicalOutput) {
		if err := validateJobDir(scope.Root); err != nil {
			return nil, nil, err
		}
	}
	lock, ok := acquireLock(scopedLockName(jobLockKind(step, operation), scope))
	if !ok {
		return nil, nil, fmt.Errorf("ОТКАЗ: job %s/%s уже запущен в scope %s — duplicate start внутри owned scope запрещён",
			step, operation, scope.key())
	}

	// The path is deterministic across reconnects. A per-process lock alone only
	// excludes concurrent starts; after the terminal result it used to allow
	// overwriting both the receipt and output, losing recovery evidence.
	// Refuse any existing evidence even after the lock has been released.
	// The lock still serializes all workers with the same owned scope/step/operation.
	for _, candidate := range []string{receiptPath, outputPath} {
		if _, statErr := os.Lstat(candidate); statErr == nil {
			lock.release()
			return nil, nil, fmt.Errorf("ОТКАЗ: existing job evidence %s; recover its receipt/result, do not start again", filepath.Base(candidate))
		} else if !os.IsNotExist(statErr) {
			lock.release()
			return nil, nil, fmt.Errorf("inspect job evidence %s: %w", filepath.Base(candidate), statErr)
		}
	}

	r := &jobReceipt{
		JobID:      newJobID(scope),
		PID:        os.Getpid(),
		StartedAt:  time.Now().UTC(),
		Status:     jobStatusRunning,
		OutputPath: outputPath,
		Product:    scope.Root,
		Principal:  scope.ID.Principal,
		Session:    scope.ID.SessionKey,
		Step:       step,
		Operation:  operation,
	}
	r.RecoveryRef = filepath.ToSlash(receiptPath) + "#job_id=" + r.JobID
	if err := writeJobReceiptFunc(receiptPath, r); err != nil {
		lock.release()
		return nil, nil, err
	}
	return r, lock, nil
}

// finishJobReceipt — единственный путь, которым receipt покидает RUNNING: явный финал
// (done/failed) самой команды. Transport/RDC timeout НЕ вызывает эту функцию — это и есть
// контракт К42: таймаут ожидания не трогает receipt, он остаётся RUNNING на диске.
func finishJobReceipt(receiptPath, status string) error {
	return finishJobReceiptWithExit(receiptPath, status, nil)
}

// finishJobReceiptWithExit seals actual child exit status and stdout only after
// Wait has finished. Caller timeout must never mark this job terminal.
func finishJobReceiptWithExit(receiptPath, status string, code *int) error {
	if status != jobStatusDone && status != jobStatusFailed {
		return fmt.Errorf("invalid terminal job status: %q", status)
	}
	if code != nil && status == jobStatusDone && *code != 0 {
		return errors.New("DONE cannot carry a nonzero child process exit code")
	}
	r, err := readJobReceipt(receiptPath)
	if err != nil {
		return err
	}
	if r.Status != jobStatusRunning {
		return fmt.Errorf("job %s already terminal (%s); status cannot be overwritten", r.JobID, r.Status)
	}
	r.OutputSHA256, r.OutputBytes = "", 0
	output, openErr := os.Open(r.OutputPath)
	if openErr == nil {
		defer output.Close()
		info, statErr := output.Stat()
		linkInfo, linkErr := os.Lstat(r.OutputPath)
		if statErr == nil && linkErr == nil && info.Mode().IsRegular() && linkInfo.Mode().IsRegular() && os.SameFile(info, linkInfo) && info.Size() <= (32<<20) {
			h := sha256.New()
			n, readErr := io.Copy(h, io.LimitReader(output, (32<<20)+1))
			if readErr == nil && n == info.Size() && n <= (32<<20) {
				r.OutputSHA256, r.OutputBytes = hex.EncodeToString(h.Sum(nil)), n
			}
		}
	}
	if status == jobStatusDone && (r.ProcessStarted || r.RequestStarted || r.Runner == "router" || code != nil) && r.OutputSHA256 == "" {
		return errors.New("cannot seal terminal output")
	}
	r.Status = status
	r.ExitCode = code
	return writeJobReceiptFunc(receiptPath, r)
}

// writeJobReceiptFunc — seam для проверки отказа записи квитанции на каждом этапе запуска.
var writeJobReceiptFunc = writeJobReceipt

func writeJobReceipt(path string, r *jobReceipt) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	scope := newScope(r.Product, sessionIdentity{Principal: r.Principal, SessionKey: r.Session})
	canonical, _ := jobReceiptPaths(scope, r.Step, r.Operation)
	if filepath.Clean(path) != filepath.Clean(canonical) {
		return writeFileAtomic(path, data)
	}
	root, err := os.OpenRoot(scope.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	jobs, err := root.OpenRoot(filepath.Join(".woody", "jobs"))
	if err != nil {
		return err
	}
	defer jobs.Close()
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	temp := ".receipt-" + hex.EncodeToString(random[:]) + ".tmp"
	f, err := jobs.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer jobs.Remove(temp)
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return jobs.Rename(temp, filepath.Base(path))
}

func readJobReceipt(path string) (*jobReceipt, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r jobReceipt
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// errJobStillRunning — транспорт не дождался ответа команды. Это НЕ ошибка job: сам процесс
// может ещё работать (его завершит горутина ниже и честно допишет receipt), просто ЭТОТ
// вызов ждать дальше не намерен. Caller получает пустой результат и ОБЯЗАН прочитать финал
// позже по output_path/receipt, а не считать job упавшим.
var errJobStillRunning = errors.New("job ещё RUNNING: транспорт не дождался ответа, receipt не тронут")

// jobReceiptPaths — детерминированное расположение receipt/output под owned scope: те же
// (step, operation) в том же scope всегда ведут на те же файлы, так что более поздний
// прогон (после timeout) находит ИМЕННО ЭТОТ job, а не гадает по случайному имени.
func jobReceiptPaths(scope sessionScope, step, operation string) (receiptPath, outputPath string) {
	dir := filepath.Join(scope.Root, ".woody", "jobs")
	base := reNonWord.ReplaceAllString(scope.ID.namespace()+"__"+step+"__"+operation, "-")
	return filepath.Join(dir, base+".receipt.json"), filepath.Join(dir, base+".out")
}

type jobCmdResult struct {
	data []byte
	err  error
}

// runReceipted — durable receipt вокруг ОДНОЙ долгой команды: receipt пишется RUNNING ДО
// cmd.Start (через startJobReceipt — там же duplicate-start guard owned scope), stdout+stderr
// стримятся на диск по output_path, а не копятся в памяти, и финал (DONE/FAILED) пишется
// ТОЛЬКО когда cmd.Wait() реально вернулся — никогда по истечении ctx.
//
// TRANSPORT/RDC TIMEOUT НЕ ТРОГАЕТ RECEIPT (К42). Если ctx завершился раньше, чем команда,
// эта функция возвращает errJobStillRunning и пустой результат — но горутина ниже
// ПРОДОЛЖАЕТ жить и, когда команда реально закончится, сама допишет финальный статус и
// освободит lock. Именно поэтому lock.release() стоит ТОЛЬКО внутри горутины: второй старт
// того же job в том же scope должен продолжать отказывать, пока ПЕРВЫЙ процесс жив, даже
// если caller этого первого уже перестал ждать.
type jobReceiptMeta struct {
	Runner   string
	Provider string
	Model    string
	Effort   string
	Role     string
	Sandbox  string
}

func runReceipted(ctx context.Context, scope sessionScope, step, operation string, cmd *exec.Cmd) ([]byte, error) {
	return runReceiptedWithMeta(ctx, scope, step, operation, cmd, jobReceiptMeta{})
}

// validateJobDir rejects existing linked ancestors before any directory creation.
func validateJobDir(root string) error {
	physicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	for _, part := range []string{".woody", filepath.Join(".woody", "jobs")} {
		path := filepath.Join(root, part)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			break
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return fmt.Errorf("unsafe job directory: %s", path)
		}
		physical, err := filepath.EvalSymlinks(path)
		if err != nil || !pathWithinRoot(physicalRoot, physical) || !strings.EqualFold(filepath.Clean(physical), filepath.Clean(filepath.Join(physicalRoot, part))) {
			return fmt.Errorf("redirected job directory: %s", path)
		}
	}
	return nil
}

// ensureOwnedJobsDir prepares only the canonical owned jobs directory.
func ensureOwnedJobsDir(product string) error {
	if err := validateJobDir(product); err != nil {
		return err
	}
	root, err := os.OpenRoot(product)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.MkdirAll(filepath.Join(".woody", "jobs"), 0o755); err != nil {
		return err
	}
	return validateJobDir(product)
}

// writeOwnedJobOutput refuses existing targets and never opens outside the root.
func writeOwnedJobOutput(product, path string, data []byte) error {
	if filepath.Clean(filepath.Dir(path)) != filepath.Clean(filepath.Join(product, ".woody", "jobs")) {
		return errors.New("job output is not inside canonical owned directory")
	}
	if len(data) > (32 << 20) {
		return errors.New("job output exceeds 32MiB seal limit")
	}
	root, err := os.OpenRoot(product)
	if err != nil {
		return err
	}
	defer root.Close()
	out, err := root.OpenFile(filepath.Join(".woody", "jobs", filepath.Base(path)),
		os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := out.Write(data)
	if writeErr == nil {
		writeErr = out.Sync()
	}
	closeErr := out.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func runReceiptedWithMeta(ctx context.Context, scope sessionScope, step, operation string, cmd *exec.Cmd, meta jobReceiptMeta) ([]byte, error) {
	receiptPath, outputPath := jobReceiptPaths(scope, step, operation)
	if err := validateJobDir(scope.Root); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(scope.Root)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	if err := root.MkdirAll(filepath.Join(".woody", "jobs"), 0o755); err != nil {
		return nil, err
	}
	r, lock, err := startJobReceipt(scope, step, operation, receiptPath, outputPath)
	if err != nil {
		return nil, err
	}
	r.Runner = meta.Runner
	r.Provider = meta.Provider
	r.Model = meta.Model
	r.Effort = meta.Effort
	r.Role = meta.Role
	r.Sandbox = meta.Sandbox
	r.SourceHead = jobSourceHead(scope.Root)
	r.ArgsSHA256 = jobCommandFingerprint(cmd)
	if deadline, ok := ctx.Deadline(); ok {
		r.DeadlineUTC = deadline.UTC().Format(time.RFC3339Nano)
	}
	if err := writeJobReceiptFunc(receiptPath, r); err != nil {
		_ = finishJobReceipt(receiptPath, jobStatusFailed)
		lock.release()
		return nil, err
	}
	outFile, err := root.OpenFile(filepath.Join(".woody", "jobs", filepath.Base(outputPath)), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o666)
	if err != nil {
		_ = finishJobReceipt(receiptPath, jobStatusFailed)
		lock.release()
		return nil, err
	}
	cmd.Stdout = outFile
	cmd.Stderr = outFile
	if err := cmd.Start(); err != nil {
		outFile.Close()
		_ = finishJobReceipt(receiptPath, jobStatusFailed)
		lock.release()
		return nil, err
	}
	// PID известен только после Start: receipt уже RUNNING на диске (см. startJobReceipt),
	// здесь он лишь дополняется настоящим PID вместо нуля placeholder.
	r.PID = cmd.Process.Pid
	r.ProcessStarted = true
	if err := writeJobReceiptFunc(receiptPath, r); err != nil {
		registrationErr := fmt.Errorf("не удалось зарегистрировать PID %d: %w", r.PID, err)
		// Запуск без записи настоящего PID не считается успешным: немедленно убиваем
		// дочерний процесс и дожидаемся его завершения до освобождения job-lock.
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = outFile.Close()
		r.Status = jobStatusFailed
		r.ProcessStarted = false
		_ = writeJobReceiptFunc(receiptPath, r)
		lock.release()
		return nil, registrationErr
	}

	resultCh := make(chan jobCmdResult, 1)
	go func() {
		waitErr := cmd.Wait()
		outFile.Close()
		status := jobStatusDone
		actualExit := 0
		if waitErr != nil {
			status = jobStatusFailed
			actualExit = 1
			var processError *exec.ExitError
			if errors.As(waitErr, &processError) {
				actualExit = processError.ExitCode()
			}
		}
		finishErr := finishJobReceiptWithExit(receiptPath, status, &actualExit)
		lock.release()
		var data []byte
		if finishErr == nil {
			var receipt *jobReceipt
			receipt, finishErr = readJobReceipt(receiptPath)
			if finishErr == nil {
				var output *os.File
				output, finishErr = os.Open(outputPath)
				if finishErr == nil {
					data, finishErr = io.ReadAll(io.LimitReader(output, (32<<20)+1))
					if closeErr := output.Close(); finishErr == nil {
						finishErr = closeErr
					}
				}
				if finishErr == nil {
					hash := sha256.Sum256(data)
					if receipt.Status != status || len(data) > (32<<20) || receipt.OutputBytes != int64(len(data)) || receipt.OutputSHA256 != hex.EncodeToString(hash[:]) {
						finishErr = errors.New("terminal output does not match receipt seal")
					}
				}
			}
		}
		if finishErr != nil {
			data = nil
			if waitErr != nil {
				waitErr = errors.Join(waitErr, finishErr)
			} else {
				waitErr = fmt.Errorf("terminal receipt could not be verified: %w", finishErr)
			}
		}
		resultCh <- jobCmdResult{data: data, err: waitErr}
	}()

	select {
	case res := <-resultCh:
		return res.data, res.err
	case <-ctx.Done():
		return nil, errJobStillRunning
	}
}

// jobCommandFingerprint binds exact executable path and argv to durable job
// identity without exposing prompt content, environment variables or secrets.
// The fingerprint is a measurement only, not a signing/authorization token.
func jobCommandFingerprint(cmd *exec.Cmd) string {
	if cmd == nil {
		return ""
	}
	h := sha256.New()
	for _, part := range append([]string{cmd.Path}, cmd.Args...) {
		_, _ = fmt.Fprintf(h, "%d:", len(part))
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// jobSourceHead is factual if the owned product is a Git repository. Legacy
// disposable products may not have a HEAD; callers must never interpret an
// absent source_head as proof of source identity.
func jobSourceHead(root string) string {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = root
	raw, err := cmd.Output()
	if err != nil {
		return ""
	}
	head := strings.TrimSpace(string(raw))
	if !operationalSHA.MatchString(head) {
		return ""
	}
	return head
}

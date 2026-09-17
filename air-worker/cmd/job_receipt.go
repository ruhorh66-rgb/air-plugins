package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	lock, ok := acquireLock(scopedLockName(jobLockKind(step, operation), scope))
	if !ok {
		return nil, nil, fmt.Errorf("ОТКАЗ: job %s/%s уже запущен в scope %s — duplicate start внутри owned scope запрещён",
			step, operation, scope.key())
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
	if err := writeJobReceipt(receiptPath, r); err != nil {
		lock.release()
		return nil, nil, err
	}
	return r, lock, nil
}

// finishJobReceipt — единственный путь, которым receipt покидает RUNNING: явный финал
// (done/failed) самой команды. Transport/RDC timeout НЕ вызывает эту функцию — это и есть
// контракт К42: таймаут ожидания не трогает receipt, он остаётся RUNNING на диске.
func finishJobReceipt(receiptPath, status string) error {
	r, err := readJobReceipt(receiptPath)
	if err != nil {
		return err
	}
	r.Status = status
	return writeJobReceipt(receiptPath, r)
}

func writeJobReceipt(path string, r *jobReceipt) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data)
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

func runReceiptedWithMeta(ctx context.Context, scope sessionScope, step, operation string, cmd *exec.Cmd, meta jobReceiptMeta) ([]byte, error) {
	receiptPath, outputPath := jobReceiptPaths(scope, step, operation)
	if err := os.MkdirAll(filepath.Dir(receiptPath), 0o755); err != nil {
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
	if err := writeJobReceipt(receiptPath, r); err != nil {
		_ = finishJobReceipt(receiptPath, jobStatusFailed)
		lock.release()
		return nil, err
	}
	outFile, err := os.Create(outputPath)
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
	_ = writeJobReceipt(receiptPath, r)

	resultCh := make(chan jobCmdResult, 1)
	go func() {
		waitErr := cmd.Wait()
		outFile.Close()
		status := jobStatusDone
		if waitErr != nil {
			status = jobStatusFailed
		}
		_ = finishJobReceipt(receiptPath, status)
		lock.release()
		data, _ := os.ReadFile(outputPath)
		resultCh <- jobCmdResult{data: data, err: waitErr}
	}()

	select {
	case res := <-resultCh:
		return res.data, res.err
	case <-ctx.Done():
		return nil, errJobStillRunning
	}
}

package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"time"
)

// SESSION SCOPE — третья ось namespace поверх sessionIdentity (session.go), шаг 83/К62.
//
// До этого шага замок петли (loop.go) был именован ТОЛЬКО путём продукта: одна петля на
// продукт, вообще без учёта того, какая сессия её завела. Вводная ЛПР 14.09.2026 про
// несколько параллельных сессий на одной машине требует большего: две identity на одном
// продукте обязаны мочь работать одновременно, не блокируя и не останавливая друг друга.
//
// sessionScope — составной ключ (product, principal, session): корень продукта плюс
// sessionIdentity, уже гарантирующая, что разные адаптеры/сессии не схлопнутся на одном
// файле (session.go, К44). Легаси-вызовы без identity продолжают запирать продукт целиком
// той же формулой замка, что и раньше (lockName("loop", root)) — это НЕ меняется, чтобы не
// сломать существующие проверки статуса (drift.go/report.go: lockHeld(lockName("loop",
// root))). Именованная identity даёт СВОЙ замок, отдельный от легаси и от других identity.
type sessionScope struct {
	Root string
	ID   sessionIdentity
}

// legacyScope — owned scope для вызовов без явной identity (судья и планировщик пока не
// принимают -principal/-session-key на командной строке). Фиксированная identity "legacy"
// даёт этим вызовам собственный, стабильный owned scope: duplicate-start guard действует
// (тот же продукт не запустит два судейских прогона одной и той же проверки одновременно),
// а разные продукты/сессии с объявленной identity получают свои независимые scope и не
// блокируются легаси-вызовами (см. scopedLockName/newScope — ключ включает root).
func legacyScope(root string) sessionScope {
	return newScope(root, sessionIdentity{Principal: "legacy", SessionKey: "default"})
}

func newScope(root string, id sessionIdentity) sessionScope {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	return sessionScope{Root: filepath.Clean(abs), ID: id}
}

// key — (product, principal, session) одной строкой. Продукт и identity разделены байтом,
// который не встречается ни в пути, ни в identity (namespace() уже ограничена
// [A-Za-z0-9._-]), так что коллизия по конкатенации исключена.
func (s sessionScope) key() string {
	return s.Root + "\x00" + s.ID.namespace()
}

// scopedLockName — замок конкретной сессии на конкретном продукте, а не продукта целиком.
// Две разные identity на одном Root получают РАЗНЫЕ имена замка и потому не блокируют друг
// друга; та же identity на том же Root — тот же замок, и повторный захват честно отказывает.
func scopedLockName(kind string, s sessionScope) string {
	return lockName(kind, s.key())
}

// jobState — минимальное владение job внутри scope: кто (identity) начал job с этим job_id.
// Не долговечное хранилище job (это отдельный шаг) — только запись для cross-scope guard.
type jobState struct {
	JobID      string `json:"job_id"`
	Root       string `json:"root"`
	Principal  string `json:"principal"`
	SessionKey string `json:"session_key"`
}

// newJobID — job получает собственный id поверх scope: время в наносекундах плюс случайный
// хвост, чтобы два job в одном scope подряд не совпали.
func newJobID(s sessionScope) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s__%d__%s", s.ID.namespace(), time.Now().UnixNano(), hex.EncodeToString(b))
}

func newJobState(s sessionScope) jobState {
	return jobState{JobID: newJobID(s), Root: s.Root, Principal: s.ID.Principal, SessionKey: s.ID.SessionKey}
}

// assertJobOwnership — cross-scope mutation fail-closed: несовпадение любой части identity —
// отказ с названной причиной, не предупреждение и не молчаливый пропуск.
func assertJobOwnership(job jobState, id sessionIdentity) error {
	if job.Principal != id.Principal || job.SessionKey != id.SessionKey {
		return fmt.Errorf("ОТКАЗ: job %s принадлежит %s__%s, запрошено %s__%s — cross-scope mutation запрещена",
			job.JobID, job.Principal, job.SessionKey, id.Principal, id.SessionKey)
	}
	return nil
}

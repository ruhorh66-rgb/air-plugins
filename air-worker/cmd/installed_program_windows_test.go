//go:build windows

package main

import (
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeARPStore struct {
	record          airWorkerARPRecord
	exists          bool
	readErr         error
	failWriteOnCall int
	writeCount      int
	deleteCount     int
	corruptReadback bool
	restoreErr      error
}

func (s *fakeARPStore) Read() (airWorkerARPRecord, bool, error) {
	if s.readErr != nil {
		return airWorkerARPRecord{}, false, s.readErr
	}
	value := s.record
	if s.corruptReadback && s.writeCount > 0 {
		value.DisplayVersion = "0.0.0"
	}
	return value, s.exists, nil
}
func (s *fakeARPStore) Write(r airWorkerARPRecord) error {
	s.writeCount++
	s.record = r
	s.exists = true
	if s.writeCount == s.failWriteOnCall {
		return errors.New("simulated partial write")
	}
	return nil
}
func (s *fakeARPStore) Restore(previous airWorkerARPRecord, existed bool) error {
	if s.restoreErr != nil {
		return s.restoreErr
	}
	if existed {
		s.record = previous
		s.exists = true
	} else {
		s.deleteCount++
		s.exists = false
		s.record = airWorkerARPRecord{}
	}
	return nil
}

func testARPRecord(t *testing.T, version string) airWorkerARPRecord {
	t.Helper()
	r, err := newAirWorkerARPRecord(`C:\Users\tester\AppData\Local\air-worker`, version,
		8*1024*1024, 2*1024*1024, time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestN138WindowsARPRegistryIdentityAndVersionGate(t *testing.T) {
	r := testARPRecord(t, "0.11.11-rc.5")
	if r.DisplayName != "AirWorker" || r.Publisher != "AIR OS" ||
		r.DisplayVersion != "0.11.11-rc.5" || r.InstallDate != "20261107" ||
		r.EstimatedSizeKB != 10240 ||
		r.UninstallString != `"C:\Users\tester\AppData\Local\air-worker\bin\air-worker.exe" install -uninstall` {
		t.Fatalf("bad portable per-user Windows program identity: %+v", r)
	}
	if err := validateAirWorkerARPRecord(airWorkerARPRecord{}, r, false); err != nil {
		t.Fatalf("first installation should accept absent ARP: %v", err)
	}
	if err := validateAirWorkerARPRecord(testARPRecord(t, "0.11.11-rc.2"), r, true); err != nil {
		t.Fatalf("valid same-owner upgrade rejected: %v", err)
	}
	if err := validateAirWorkerARPRecord(r, testARPRecord(t, "0.11.11-rc.2"), true); err == nil {
		t.Fatal("registered-program version downgrade was accepted")
	}
	for _, mutate := range []func(*airWorkerARPRecord){
		func(p *airWorkerARPRecord) { p.Publisher = "Foreign Publisher" },
		func(p *airWorkerARPRecord) { p.DisplayName = "Foreign Product" },
		func(p *airWorkerARPRecord) { p.InstallLocation = `D:\Other\Product` },
		func(p *airWorkerARPRecord) { p.DisplayVersion = "unreadable-version" },
	} {
		foreign := r
		mutate(&foreign)
		if err := validateAirWorkerARPRecord(foreign, r, true); err == nil {
			t.Fatalf("foreign or unreadable previous Windows program overwritten: %+v", foreign)
		}
		if err := validateAirWorkerARPRemoval(foreign, true, r.InstallLocation); err == nil {
			t.Fatalf("foreign Windows program removal allowed: %+v", foreign)
		}
	}
	if err := validateAirWorkerARPRemoval(r, true, r.InstallLocation); err != nil {
		t.Fatalf("same-owner Windows program removal rejected: %v", err)
	}
	if err := validateAirWorkerARPRemoval(airWorkerARPRecord{}, false, r.InstallLocation); err != nil {
		t.Fatalf("absent owned registration should be harmless: %v", err)
	}
}

func TestN138WindowsARPInputSafety(t *testing.T) {
	for _, home := range []string{
		"", ".", `C:\`, `\\server\share\air-worker`,
		`C:\Users\tester\AppData\Local\unsafe"command`,
		"C:\\Users\\tester\\bad\ncommand",
	} {
		if _, err := newAirWorkerARPRecord(home, "0.11.11-rc.5", 100, 100, time.Now()); err == nil {
			t.Errorf("dangerous or ambiguous installed-program root accepted: %q", home)
		}
	}
	if _, err := newAirWorkerARPRecord(`C:\Users\tester\AppData\Local\air-worker`,
		"0.11.invalid", 100, 100, time.Now()); err == nil {
		t.Fatal("bad version accepted")
	}
	if _, err := newAirWorkerARPRecord(`C:\Users\tester\AppData\Local\air-worker`,
		"0.11.11-rc.5", -1, 100, time.Now()); err == nil {
		t.Fatal("negative binary size accepted")
	}
}

func TestN138WindowsARPReconcileRollbackWithoutLiveRegistry(t *testing.T) {
	old := testARPRecord(t, "0.11.11-rc.2")
	next := testARPRecord(t, "0.11.11-rc.5")
	store := &fakeARPStore{record: old, exists: true}
	if err := reconcileAirWorkerARP(store, next); err != nil {
		t.Fatalf("owner upgrade failed: %v", err)
	}
	if !store.exists || !sameARPIdentity(store.record, next) || store.writeCount != 1 {
		t.Fatalf("incorrect installed-program upgrade result: %+v", store)
	}
	// Failed half-written update must restore the exact prior record.
	store = &fakeARPStore{record: old, exists: true, failWriteOnCall: 1}
	if err := reconcileAirWorkerARP(store, next); err == nil || !sameARPIdentity(store.record, old) {
		t.Fatalf("partial write not rolled back to previous version: err=%v store=%+v", err, store)
	}
	// First install failure must remove only the newly created own key.
	store = &fakeARPStore{failWriteOnCall: 1}
	if err := reconcileAirWorkerARP(store, next); err == nil ||
		store.exists || store.deleteCount != 1 {
		t.Fatalf("failed first install left an unowned partial key: err=%v store=%+v", err, store)
	}
	store = &fakeARPStore{readErr: errors.New("access denied")}
	if err := reconcileAirWorkerARP(store, next); err == nil ||
		!strings.Contains(err.Error(), "cannot read") || store.writeCount != 0 {
		t.Fatalf("registry read failure treated as absent: err=%v store=%+v", err, store)
	}
}

func TestN138WindowsARPReconcileReadbackMismatchTriggersRestore(t *testing.T) {
	old := testARPRecord(t, "0.11.11-rc.2")
	next := testARPRecord(t, "0.11.11-rc.5")
	store := &fakeARPStore{record: old, exists: true, corruptReadback: true}
	if err := reconcileAirWorkerARP(store, next); err == nil ||
		!strings.Contains(err.Error(), "readback failed") ||
		!sameARPIdentity(store.record, old) {
		t.Fatalf("mismatched installed-program readback was accepted: err=%v store=%+v", err, store)
	}
	foreign := next
	foreign.UninstallString = `"D:\Foreign\setup.exe" /uninstall`
	if err := validateAirWorkerARPRecord(old, foreign, true); err == nil {
		t.Fatal("new ARP record would advertise an executable owned by somebody else")
	}
}

func TestN138WindowsARPFailedRestoreIsNotCalledSuccess(t *testing.T) {
	old := testARPRecord(t, "0.11.11-rc.2")
	store := &fakeARPStore{
		record: old, exists: true,
		failWriteOnCall: 1, restoreErr: errors.New("registry ACL denied restoration"),
	}
	err := reconcileAirWorkerARP(store, testARPRecord(t, "0.11.11-rc.5"))
	if err == nil || !strings.Contains(err.Error(), "restoring prior state also failed") {
		t.Fatalf("failed registry rollback was hidden as successful: %v", err)
	}
}

func TestN138WindowsARPReconcileRejectsForeignWithoutWrite(t *testing.T) {
	old := testARPRecord(t, "0.11.11-rc.2")
	old.DisplayName = "Foreign Product"
	store := &fakeARPStore{record: old, exists: true}
	if err := reconcileAirWorkerARP(store, testARPRecord(t, "0.11.11-rc.5")); err == nil ||
		store.writeCount != 0 || store.deleteCount != 0 {
		t.Fatalf("foreign program was modified: err=%v store=%+v", err, store)
	}
}

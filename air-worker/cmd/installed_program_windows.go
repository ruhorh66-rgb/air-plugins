//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// AIR OS PRODUCTS_INSTALL_REGISTRATION: a native portable plugin still needs
// one honest Windows Installed Apps identity once its OWN uninstall lifecycle
// is implemented and proven. This module contains the isolated, source-only
// registration transaction. Do not call it from a live installer until that
// removal and update/rollback contract is accepted (N-138).
const (
	airWorkerARPSubkey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\AirWorker`
	airWorkerARPName   = "AirWorker"
	airWorkerARPOwner  = "AIR OS"
)

type airWorkerARPRecord struct {
	DisplayName     string
	DisplayVersion  string
	Publisher       string
	InstallLocation string
	UninstallString string
	InstallDate     string
	EstimatedSizeKB uint32
}

// ARP writes are not connected to a live installer in this source-only slice.
// A future native writer must report whether it TOUCHED the exact key: a
// failed preflight/read/ownership check must NEVER trigger deletion on rollback.
type airWorkerARPStore interface {
	Read() (airWorkerARPRecord, bool, error)
	Write(airWorkerARPRecord) (touched bool, err error)
	Restore(previous airWorkerARPRecord, existed bool) error
}

// This is an identity and ownership gate, not a guarantee that -uninstall
// currently deletes owned binaries. N-138 must prove that separately.
func newAirWorkerARPRecord(home, releaseVersion string, cliSize, traySize int64, now time.Time) (airWorkerARPRecord, error) {
	clean := filepath.Clean(strings.TrimSpace(home))
	if !filepath.IsAbs(clean) || clean == "." || strings.ContainsAny(clean, "\x00\r\n\"") ||
		strings.HasPrefix(clean, `\\`) || filepath.Dir(clean) == clean {
		return airWorkerARPRecord{}, fmt.Errorf("invalid per-user installed-program path %q", home)
	}
	if _, err := parseUpdateSemVersion(releaseVersion); err != nil {
		return airWorkerARPRecord{}, fmt.Errorf("invalid installed-program version: %w", err)
	}
	if cliSize <= 0 || traySize <= 0 || cliSize > 2<<30 || traySize > 2<<30 {
		return airWorkerARPRecord{}, errors.New("installed CLI and tray must both have positive bounded sizes")
	}
	sizeKB := uint32((cliSize + traySize + 1023) / 1024)
	return airWorkerARPRecord{
		DisplayName:     airWorkerARPName,
		DisplayVersion:  releaseVersion,
		Publisher:       airWorkerARPOwner,
		InstallLocation: clean,
		UninstallString: "\"" + filepath.Join(clean, "bin", "air-worker.exe") + "\" install -uninstall",
		InstallDate:     now.Format("20060102"),
		EstimatedSizeKB: sizeKB,
	}, nil
}

func validateAirWorkerARPRecord(old, next airWorkerARPRecord, existed bool) error {
	if next.DisplayName != airWorkerARPName || next.Publisher != airWorkerARPOwner ||
		!filepath.IsAbs(next.InstallLocation) || next.EstimatedSizeKB == 0 {
		return errors.New("new installed-program record has invalid AirWorker ownership or size")
	}
	if _, err := parseUpdateSemVersion(next.DisplayVersion); err != nil {
		return fmt.Errorf("new installed-program record has invalid version: %w", err)
	}
	wantUninstall := "\"" + filepath.Join(filepath.Clean(next.InstallLocation), "bin", "air-worker.exe") + "\" install -uninstall"
	if next.UninstallString != wantUninstall {
		return errors.New("new installed-program record has noncanonical uninstall command")
	}
	if !existed {
		return nil
	}
	if old.DisplayName != airWorkerARPName || old.Publisher != airWorkerARPOwner ||
		!samePath(old.InstallLocation, next.InstallLocation) {
		return errors.New("registered Windows program belongs to a different product or location")
	}
	if _, err := parseUpdateSemVersion(old.DisplayVersion); err != nil {
		return errors.New("existing registered program version is unreadable; refuse overwrite")
	}
	if compareUpdateVersionStrings(next.DisplayVersion, old.DisplayVersion) < 0 {
		return fmt.Errorf("registered-program downgrade refused: %s -> %s", old.DisplayVersion, next.DisplayVersion)
	}
	return nil
}

func validateAirWorkerARPRemoval(old airWorkerARPRecord, exists bool, expectedHome string) error {
	if !exists {
		return nil // already absent
	}
	if old.DisplayName != airWorkerARPName || old.Publisher != airWorkerARPOwner ||
		!samePath(old.InstallLocation, expectedHome) {
		return errors.New("cannot remove Windows installed-program key owned by another product/location")
	}
	if _, err := parseUpdateSemVersion(old.DisplayVersion); err != nil {
		return errors.New("refuse to remove registered program with unreadable version")
	}
	wantUninstall := "\"" + filepath.Join(filepath.Clean(expectedHome), "bin", "air-worker.exe") + "\" install -uninstall"
	if old.UninstallString != wantUninstall {
		return errors.New("refuse to remove registered program with foreign uninstall command")
	}
	return nil
}

func sameARPIdentity(a, b airWorkerARPRecord) bool {
	return a.DisplayName == b.DisplayName && a.DisplayVersion == b.DisplayVersion &&
		a.Publisher == b.Publisher && samePath(a.InstallLocation, b.InstallLocation) &&
		a.UninstallString == b.UninstallString && a.InstallDate == b.InstallDate &&
		a.EstimatedSizeKB == b.EstimatedSizeKB
}

// reconcileAirWorkerARP is a rollbackable source-only building block.
// Caller must own the installer mutex and verify the CLI/tray and plugin cache
// BEFORE invoking it. Failure to read previous state never becomes absence.
func reconcileAirWorkerARP(store airWorkerARPStore, next airWorkerARPRecord) error {
	before, exists, err := store.Read()
	if err != nil {
		return fmt.Errorf("cannot read existing Windows installed-program key: %w", err)
	}
	if err := validateAirWorkerARPRecord(before, next, exists); err != nil {
		return err
	}
	restore := func() error {
		return store.Restore(before, exists)
	}
	touched, err := store.Write(next)
	if err != nil {
		// A foreign key may appear after our initial Read but before Write.
		// It is NOT ours; a validation failure before any mutation is never
		// permission to restore/delete that later foreign key.
		if !touched {
			return fmt.Errorf("registry write refused before mutation: %w", err)
		}
		if rollbackErr := restore(); rollbackErr != nil {
			return fmt.Errorf("registry write failed: %v; restoring prior state also failed: %w", err, rollbackErr)
		}
		return fmt.Errorf("registry write failed; previous state restored: %w", err)
	}
	if !touched {
		return errors.New("registry writer returned success without any mutation; no installed-program PASS")
	}
	actual, found, err := store.Read()
	if err != nil || !found || !sameARPIdentity(actual, next) {
		why := fmt.Errorf("registry readback failed or does not match installed package: read=%v found=%t", err, found)
		if rollbackErr := restore(); rollbackErr != nil {
			return fmt.Errorf("%w; restoring prior state also failed: %v", why, rollbackErr)
		}
		return why
	}
	return nil
}

type nativeAirWorkerARPStore struct{}

func openAirWorkerARPKey(access uintptr) (syscall.Handle, bool, error) {
	sub, err := syscall.UTF16PtrFromString(airWorkerARPSubkey)
	if err != nil {
		return 0, false, err
	}
	var h syscall.Handle
	rc, _, _ := procRegOpenKeyExW.Call(uintptr(hkeyCurrentUser), uintptr(unsafe.Pointer(sub)),
		0, access, uintptr(unsafe.Pointer(&h)))
	if rc == 2 {
		return 0, false, nil
	}
	if rc != 0 {
		return 0, false, fmt.Errorf("open Windows installed-program key: Windows error %d", rc)
	}
	return h, true, nil
}

func queryARPString(key syscall.Handle, name string) (string, error) {
	field, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return "", err
	}
	var kind, size uint32
	rc, _, _ := procRegQueryValue.Call(uintptr(key), uintptr(unsafe.Pointer(field)), 0,
		uintptr(unsafe.Pointer(&kind)), 0, uintptr(unsafe.Pointer(&size)))
	if rc != 0 || size == 0 || kind != regSZ || size > 32768 || size%2 != 0 {
		return "", fmt.Errorf("ARP field %s missing, malformed or unavailable (Windows error %d)", name, rc)
	}
	buf := make([]uint16, size/2+1)
	rc, _, _ = procRegQueryValue.Call(uintptr(key), uintptr(unsafe.Pointer(field)), 0,
		uintptr(unsafe.Pointer(&kind)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if rc != 0 || kind != regSZ {
		return "", fmt.Errorf("ARP field %s unreadable (Windows error %d)", name, rc)
	}
	return syscall.UTF16ToString(buf), nil
}

func queryARPDWORD(key syscall.Handle, name string) (uint32, error) {
	field, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	var kind, size uint32 = 0, 4
	var value uint32
	rc, _, _ := procRegQueryValue.Call(uintptr(key), uintptr(unsafe.Pointer(field)), 0,
		uintptr(unsafe.Pointer(&kind)), uintptr(unsafe.Pointer(&value)), uintptr(unsafe.Pointer(&size)))
	if rc != 0 || kind != regDWORD || size != 4 {
		return 0, fmt.Errorf("ARP DWORD %s missing or unreadable (Windows error %d)", name, rc)
	}
	return value, nil
}

func (nativeAirWorkerARPStore) Read() (airWorkerARPRecord, bool, error) {
	h, exists, err := openAirWorkerARPKey(uintptr(keyRead))
	if err != nil || !exists {
		return airWorkerARPRecord{}, exists, err
	}
	defer procRegCloseKey.Call(uintptr(h))
	var record airWorkerARPRecord
	for _, item := range []struct {
		field string
		dst   *string
	}{
		{"DisplayName", &record.DisplayName},
		{"DisplayVersion", &record.DisplayVersion},
		{"Publisher", &record.Publisher},
		{"InstallLocation", &record.InstallLocation},
		{"UninstallString", &record.UninstallString},
	} {
		*item.dst, err = queryARPString(h, item.field)
		if err != nil {
			return airWorkerARPRecord{}, true, err
		}
	}
	// Additional properties are part of exact readback: a missing/malformed
	// size is NOT silently treated as zero and then called a successful install.
	record.InstallDate, err = queryARPString(h, "InstallDate")
	if err != nil {
		return airWorkerARPRecord{}, true, err
	}
	record.EstimatedSizeKB, err = queryARPDWORD(h, "EstimatedSize")
	if err != nil {
		return airWorkerARPRecord{}, true, err
	}
	return record, true, nil
}

// Non-mutating readback is safe before the owner authorizes live upgrade.
func airWorkerARPStatus(home string) (string, error) {
	record, exists, err := (nativeAirWorkerARPStore{}).Read()
	if err != nil {
		return "ARP: unreadable", err
	}
	if !exists {
		return "ARP: no AirWorker registration in current user Installed Apps", nil
	}
	if err := validateAirWorkerARPRemoval(record, true, home); err != nil {
		return "ARP: foreign or mismatched registration", err
	}
	return fmt.Sprintf("ARP: %s version=%s location=%s", record.DisplayName, record.DisplayVersion, record.InstallLocation), nil
}

// Future native writer must measure actual installed files and keep this
// calculation separate from source package size.
func liveAirWorkerARPSizes(home string) (int64, int64, error) {
	cli, err := os.Stat(filepath.Join(home, "bin", "air-worker.exe"))
	if err != nil {
		return 0, 0, err
	}
	tray, err := os.Stat(filepath.Join(home, "bin", "air-worker-tray.exe"))
	if err != nil {
		return 0, 0, err
	}
	return cli.Size(), tray.Size(), nil
}

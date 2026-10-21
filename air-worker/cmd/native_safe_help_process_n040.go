package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os/user"
	"strings"
	"sync"
)

// The module's principal remains the historical feedback principal under its
// v0.1.0 provenance contract. An N040 receipt ALSO binds the *current* native
// process actor to its Windows-token SID, not to a caller-provided CLI flag.
func n040CurrentActorSID() (string, error) {
	u, err := user.Current()
	if err != nil || u == nil || strings.TrimSpace(u.Uid) == "" {
		return "", errors.New("N040 cannot establish current native OS actor")
	}
	return u.Uid, nil
}

// PID equality/inequality cannot demonstrate process generation: Windows may
// reuse numeric PIDs. This random instance ID is drawn once by each compiled
// process. Its signed value distinguishes generations even after PID reuse.
var n040InstanceOnce sync.Once
var n040InstanceValue string
var n040InstanceError error

func n040CurrentProcessInstance() (string, error) {
	n040InstanceOnce.Do(func() {
		var b [16]byte
		if _, err := rand.Read(b[:]); err != nil {
			n040InstanceError = err
			return
		}
		n040InstanceValue = hex.EncodeToString(b[:])
	})
	if n040InstanceError != nil || len(n040InstanceValue) != 32 {
		return "", errors.New("N040 secure native process instance unavailable")
	}
	return n040InstanceValue, nil
}

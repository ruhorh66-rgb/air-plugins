package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func curatorFixtureSkill(name, body string) []byte {
	return []byte("---\nname: " + name + "\ndescription: fixture " + name + "\n---\n\n# " + name + "\n\n" + body + "\n")
}

func curatorFixtureSHA(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func writeCuratorRegistryFixture(t *testing.T, root string, skills []map[string]any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "skills", "air-curator"), 0o755); err != nil {
		t.Fatal(err)
	}
	doc := map[string]any{
		"schema":          "air-worker.curator-skills/v1",
		"product":         "air-worker",
		"source_of_truth": "release-payload",
		"hosts": map[string]any{
			"windows": map[string]any{"status": "required"},
			"claude":  map[string]any{"status": "managed_mirror"},
			"codex":   map[string]any{"status": "managed_mirror"},
			"hermes":  map[string]any{"status": "not_applicable", "reason": "fixture no skill discovery payload"},
			"android": map[string]any{"status": "not_applicable", "reason": "fixture no Android payload"},
		},
		"skills": skills,
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, '\n')
	if err := os.WriteFile(filepath.Join(root, curatorSkillRegistryRel), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func curatorFixtureEntry(id, rel, role string, body []byte, triggers ...string) map[string]any {
	return map[string]any{
		"id":       id,
		"path":     filepath.ToSlash(rel),
		"sha256":   curatorFixtureSHA(body),
		"role":     role,
		"load":     "on_trigger",
		"triggers": triggers,
		"mirrors": map[string]string{
			"claude": filepath.ToSlash(filepath.Join("skills", id, "SKILL.md")),
			"codex":  filepath.ToSlash(filepath.Join("skills", id, "SKILL.md")),
		},
	}
}

func writeCuratorFixtureSkill(t *testing.T, root, rel string, body []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCuratorSkillPackageHealthRequiresRegisteredSource(t *testing.T) {
	root := t.TempDir()
	body := curatorFixtureSkill("air-curator-resume-interrupted-worker", "resume")
	entry := curatorFixtureEntry(
		"air-curator-resume-interrupted-worker",
		"skills/air-curator-resume-interrupted-worker/SKILL.md",
		"resume-interrupted-worker",
		body,
		"resume interrupted worker",
	)
	writeCuratorRegistryFixture(t, root, []map[string]any{entry})

	health := inspectCuratorSkillPackage(root)
	if health.Status != curatorSkillStatusFail || len(health.Violations) == 0 {
		t.Fatalf("missing registered source must fail package health: %#v", health)
	}
	if !strings.Contains(strings.ToLower(strings.Join(health.Violations, " ")), "missing") {
		t.Fatalf("missing source reason not explicit: %#v", health.Violations)
	}
}

func TestCuratorSkillPackageHealthRejectsMirrorDestinationAndMarkerCollisions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(map[string]string, map[string]string)
		want   string
	}{
		{
			name: "destination",
			mutate: func(first, second map[string]string) {
				second["codex"] = first["codex"]
			},
			want: "destination collision",
		},
		{
			name: "marker",
			mutate: func(first, second map[string]string) {
				first["codex"] = "skills/shared/a.md"
				second["codex"] = "skills/shared/b.md"
			},
			want: "marker collision",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			bodyA := curatorFixtureSkill("skill-a", "safe a")
			bodyB := curatorFixtureSkill("skill-b", "safe b")
			first := curatorFixtureEntry("skill-a", "skills/skill-a/SKILL.md", "role-a", bodyA, "trigger a")
			second := curatorFixtureEntry("skill-b", "skills/skill-b/SKILL.md", "role-b", bodyB, "trigger b")
			firstMirrors := first["mirrors"].(map[string]string)
			secondMirrors := second["mirrors"].(map[string]string)
			tc.mutate(firstMirrors, secondMirrors)
			writeCuratorRegistryFixture(t, root, []map[string]any{first, second})
			health := inspectCuratorSkillPackage(root)
			if health.Status != curatorSkillStatusFail {
				t.Fatalf("mirror collision must fail package health: %#v", health)
			}
			if !strings.Contains(strings.ToLower(strings.Join(health.Violations, " ")), tc.want) {
				t.Fatalf("reason must mention %q: %#v", tc.want, health.Violations)
			}
		})
	}
}

func TestCuratorSkillPackageHealthRejectsHashNameAndSecretDrift(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content []byte
		mutate  func(map[string]any)
		want    string
	}{
		{
			name:    "hash",
			content: curatorFixtureSkill("skill-a", "safe"),
			mutate: func(e map[string]any) {
				e["sha256"] = strings.Repeat("0", 64)
			},
			want: "sha",
		},
		{
			name:    "frontmatter-name",
			content: curatorFixtureSkill("other-name", "safe"),
			mutate:  func(map[string]any) {},
			want:    "frontmatter",
		},
		{
			name:    "secret",
			content: curatorFixtureSkill("skill-a", "api_key = \""+"sk-"+strings.Repeat("x", 32)+"\""),
			mutate:  func(map[string]any) {},
			want:    "secret",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			rel := "skills/skill-a/SKILL.md"
			writeCuratorFixtureSkill(t, root, rel, tc.content)
			entry := curatorFixtureEntry("skill-a", rel, "test-role", tc.content, "test trigger")
			tc.mutate(entry)
			writeCuratorRegistryFixture(t, root, []map[string]any{entry})
			health := inspectCuratorSkillPackage(root)
			if health.Status != curatorSkillStatusFail {
				t.Fatalf("expected package health fail: %#v", health)
			}
			if !strings.Contains(strings.ToLower(strings.Join(health.Violations, " ")), tc.want) {
				t.Fatalf("reason must mention %q: %#v", tc.want, health.Violations)
			}
		})
	}
}

func TestCuratorSkillMirrorSyncIsIdempotentAndProtectsUnmanagedDrift(t *testing.T) {
	root := t.TempDir()
	body := curatorFixtureSkill("air-curator-review-worker-result", "review evidence")
	rel := "skills/air-curator-review-worker-result/SKILL.md"
	writeCuratorFixtureSkill(t, root, rel, body)
	entry := curatorFixtureEntry("air-curator-review-worker-result", rel, "review-worker-result", body, "review worker result")
	writeCuratorRegistryFixture(t, root, []map[string]any{entry})

	config := t.TempDir()
	first, err := syncCuratorSkillMirrors(root, "codex", config)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].State != curatorMirrorSynced {
		t.Fatalf("first sync: %#v", first)
	}
	dst := filepath.Join(config, "skills", "air-curator-review-worker-result", "SKILL.md")
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Fatal("mirror bytes differ from release-owned source")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dst), curatorSkillMirrorMarkerName)); err != nil {
		t.Fatalf("managed marker missing: %v", err)
	}

	second, err := syncCuratorSkillMirrors(root, "codex", config)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || second[0].State != curatorMirrorCurrent {
		t.Fatalf("second sync must be idempotent: %#v", second)
	}

	if err := os.WriteFile(dst, []byte("user changed mirror\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := syncCuratorSkillMirrors(root, "codex", config); err == nil {
		t.Fatal("managed mirror changed outside delivery must fail instead of overwrite")
	}
	after, _ := os.ReadFile(dst)
	if string(after) != "user changed mirror\n" {
		t.Fatalf("drifted mirror was overwritten: %q", after)
	}
}

func TestCuratorSkillCleanSessionResolveByRoleAndTrigger(t *testing.T) {
	root := t.TempDir()
	specs := []struct {
		id, role, trigger string
	}{
		{"air-curator", "curator", "air curator patrol"},
		{"air-curator-resume-interrupted-worker", "resume-interrupted-worker", "resume interrupted worker"},
		{"air-curator-review-worker-result", "review-worker-result", "review worker result"},
	}
	var entries []map[string]any
	for _, s := range specs {
		body := curatorFixtureSkill(s.id, "fixture "+s.role)
		rel := filepath.ToSlash(filepath.Join("skills", s.id, "SKILL.md"))
		writeCuratorFixtureSkill(t, root, rel, body)
		e := curatorFixtureEntry(s.id, rel, s.role, body, s.trigger)
		if s.id == "air-curator" {
			e["load"] = "startup"
		}
		entries = append(entries, e)
	}
	writeCuratorRegistryFixture(t, root, entries)

	for _, host := range []string{"claude", "codex"} {
		config := t.TempDir()
		if _, err := syncCuratorSkillMirrors(root, host, config); err != nil {
			t.Fatalf("%s sync: %v", host, err)
		}
		for _, s := range specs {
			if _, err := os.Stat(filepath.Join(config, "skills", s.id, "SKILL.md")); err != nil {
				t.Fatalf("%s missing clean-session mirror %s: %v", host, s.id, err)
			}
		}
	}
	reg, err := loadCuratorSkillRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := resolveCuratorSkill(reg, "resume-interrupted-worker", ""); err != nil || got.ID != "air-curator-resume-interrupted-worker" {
		t.Fatalf("role resolution: %#v %v", got, err)
	}
	if got, err := resolveCuratorSkill(reg, "", "please review worker result and release evidence"); err != nil || got.ID != "air-curator-review-worker-result" {
		t.Fatalf("trigger resolution: %#v %v", got, err)
	}
	if got, err := resolveCuratorSkill(reg, "", "air curator: please review worker result and release evidence"); err != nil || got.ID != "air-curator-review-worker-result" {
		t.Fatalf("most-specific trigger resolution: %#v %v", got, err)
	}
	ctx := curatorSkillStartupContext(reg)
	for _, s := range specs {
		if !strings.Contains(ctx, s.id) || !strings.Contains(ctx, s.role) {
			t.Fatalf("startup context missing %s/%s: %s", s.id, s.role, ctx)
		}
	}
}

func TestCuratorSkillStartupBootstrapsCleanClaudeMirror(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	config := t.TempDir()
	t.Setenv("AIR_WORKER_PLUGIN_ROOT", root)
	t.Setenv("CLAUDE_CONFIG_DIR", config)

	res, err := curatorSkillStartup(hookInput{SessionID: "clean-session", Principal: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"air-curator", "air-curator-resume-interrupted-worker", "air-curator-review-worker-result"} {
		if !strings.Contains(res.Context, id) {
			t.Fatalf("startup context missing %s: %s", id, res.Context)
		}
		path := filepath.Join(config, "skills", id, "SKILL.md")
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("startup did not deliver %s: %v", id, err)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(path), curatorSkillMirrorMarkerName)); err != nil {
			t.Fatalf("startup did not mark %s as managed: %v", id, err)
		}
	}
}

func TestCuratorSkillStartupFailsClosedWhenDeclaredPluginPayloadIsBroken(t *testing.T) {
	root := t.TempDir()
	body := curatorFixtureSkill("skill-a", "safe")
	entry := curatorFixtureEntry("skill-a", "skills/skill-a/SKILL.md", "test-role", body, "test trigger")
	writeCuratorRegistryFixture(t, root, []map[string]any{entry})
	t.Setenv("AIR_WORKER_PLUGIN_ROOT", root)
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	_, err := curatorSkillStartup(hookInput{SessionID: "broken-session", Principal: "claude"})
	var hookErr *curatorSkillHookError
	if err == nil || !errors.As(err, &hookErr) {
		t.Fatalf("declared broken plugin payload must fail with curatorSkillHookError: %T %v", err, err)
	}
}

func TestCuratorSkillSourceParticipatesInPayloadSnapshotIdentity(t *testing.T) {
	root := t.TempDir()
	body := curatorFixtureSkill("skill-a", "first")
	rel := "skills/skill-a/SKILL.md"
	writeCuratorFixtureSkill(t, root, rel, body)
	entry := curatorFixtureEntry("skill-a", rel, "test-role", body, "test trigger")
	writeCuratorRegistryFixture(t, root, []map[string]any{entry})

	before, _, err := payloadSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), curatorFixtureSkill("skill-a", "changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	after, _, err := payloadSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("release-owned skill change must change payload snapshot identity")
	}
	health := inspectCuratorSkillPackage(root)
	if health.Status != curatorSkillStatusFail {
		t.Fatalf("changed skill without registry SHA update must fail health: %#v", health)
	}
}

func TestSelfcheckTreatsCuratorSkillPackageAndMirrorDriftAsViolations(t *testing.T) {
	modified := false
	live := distributionBinaryIdentity{
		Path: "live", Version: "0.11.7", Revision: "rev", SHA256: "same", Readable: true, Executable: true, Modified: &modified,
	}
	health := curatorSkillHealth{
		Schema: "air-worker.curator-skill-health/v1", Status: curatorSkillStatusFail,
		Violations: []string{"registered skill source missing: skill-a"},
	}
	profile := distributionProfile{
		Host: "codex", ConfigDir: "cfg", Active: true, HasState: true, Canonical: true, PluginEnabled: true,
		InstallPath: "plugin", Version: "0.11.7", Revision: "rev", PayloadSHA256: "payload",
		CacheBinary: distributionBinaryIdentity{
			Path: "cache", Version: "0.11.7", Revision: "rev", SHA256: "same", Readable: true, Executable: true, Modified: &modified,
		},
		CuratorSkills: &health,
		CuratorMirrors: []curatorSkillMirrorState{{
			Host: "codex", ConfigDir: "cfg", SkillID: "skill-a", MirrorPath: "cfg/skills/skill-a/SKILL.md", State: curatorMirrorDrift,
		}},
	}
	_, violations, _ := compareDistributionIdentity(live, []distributionProfile{profile})
	text := strings.Join(violations, "\n")
	if !strings.Contains(text, "skill package violation") || !strings.Contains(text, "mirror drift") {
		t.Fatalf("selfcheck did not surface package+mirror violations: %v", violations)
	}
}

func TestCuratorPluginRootExplicitAndDeclaredRootsAreAuthoritative(t *testing.T) {
	canonical, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	invalid := t.TempDir()
	t.Setenv("AIR_WORKER_PLUGIN_ROOT", canonical)
	if got, err := curatorPluginRoot(invalid); err == nil {
		t.Fatalf("explicit invalid plugin root must fail, got fallback %s", got)
	}

	t.Setenv("AIR_WORKER_PLUGIN_ROOT", invalid)
	t.Setenv("CLAUDE_PLUGIN_ROOT", canonical)
	if got, err := curatorPluginRoot(""); err == nil {
		t.Fatalf("declared AIR_WORKER_PLUGIN_ROOT must be authoritative, got fallback %s", got)
	}
}

func TestCuratorSkillStatusReturnsFailureOnDrift(t *testing.T) {
	root := t.TempDir()
	body := curatorFixtureSkill("skill-a", "safe")
	rel := "skills/skill-a/SKILL.md"
	writeCuratorFixtureSkill(t, root, rel, body)
	entry := curatorFixtureEntry("skill-a", rel, "test-role", body, "test trigger")
	writeCuratorRegistryFixture(t, root, []map[string]any{entry})

	claude := t.TempDir()
	codex := t.TempDir()
	if _, err := syncCuratorSkillMirrors(root, "claude", claude); err != nil {
		t.Fatal(err)
	}
	if _, err := syncCuratorSkillMirrors(root, "codex", codex); err != nil {
		t.Fatal(err)
	}
	if rc := cmdCuratorSkills([]string{
		"status", "-plugin-root", root, "-claude-config", claude, "-codex-config", codex, "-json",
	}); rc != 0 {
		t.Fatalf("clean status rc=%d want 0", rc)
	}

	drift := filepath.Join(codex, "skills", "skill-a", "SKILL.md")
	if err := os.WriteFile(drift, []byte("host drift\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rc := cmdCuratorSkills([]string{
		"status", "-plugin-root", root, "-claude-config", claude, "-codex-config", codex, "-json",
	}); rc == 0 {
		t.Fatal("drifted mirror status must return nonzero")
	}
}

func TestCuratorSkillResolveRejectsMissingRegisteredSource(t *testing.T) {
	root := t.TempDir()
	body := curatorFixtureSkill("skill-a", "safe")
	entry := curatorFixtureEntry("skill-a", "skills/skill-a/SKILL.md", "test-role", body, "test trigger")
	writeCuratorRegistryFixture(t, root, []map[string]any{entry})

	if rc := cmdCuratorSkills([]string{
		"resolve", "-plugin-root", root, "-role", "test-role", "-json",
	}); rc == 0 {
		t.Fatal("resolve must not return success for a missing registered source")
	}
}

func TestCurrentCuratorProfileRequiresRegistry(t *testing.T) {
	root := t.TempDir()
	config := t.TempDir()
	health, _, err := inspectCuratorProfileSkills(root, "codex", config, true, "0.11.7")
	if err != nil {
		t.Fatal(err)
	}
	if health == nil || health.Status != curatorSkillStatusFail {
		t.Fatalf("current 0.11.7 package without registry must fail health: %#v", health)
	}
}

func TestLegacyCuratorProfileAllowsMissingRegistry(t *testing.T) {
	health, mirrors, err := inspectCuratorProfileSkills(t.TempDir(), "codex", t.TempDir(), true, "0.11.5")
	if err != nil {
		t.Fatal(err)
	}
	if health != nil || mirrors != nil {
		t.Fatalf("0.11.5 missing registry must remain legacy-compatible: health=%#v mirrors=%#v", health, mirrors)
	}
}

func TestCanonicalCuratorSkillRegistryHealthAndApplicability(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	health := inspectCuratorSkillPackage(root)
	if health.Status != curatorSkillStatusPass || len(health.Violations) != 0 {
		t.Fatalf("canonical registry must be healthy: %#v", health)
	}
	reg, err := loadCuratorSkillRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := map[string]bool{
		"air-curator":                           true,
		"air-curator-resume-interrupted-worker": true,
		"air-curator-review-worker-result":      true,
	}
	for _, skill := range reg.Skills {
		delete(wantIDs, skill.ID)
	}
	if len(wantIDs) != 0 {
		t.Fatalf("canonical registry missing skills: %v", wantIDs)
	}
	for host, status := range map[string]string{
		"windows": curatorHostRequired,
		"claude":  curatorHostManagedMirror,
		"codex":   curatorHostManagedMirror,
		"hermes":  curatorHostNotApplicable,
		"android": curatorHostNotApplicable,
	} {
		h, ok := reg.Hosts[host]
		if !ok || h.Status != status {
			t.Fatalf("host %s status=%#v want %s", host, h, status)
		}
		if status == curatorHostNotApplicable && strings.TrimSpace(h.Reason) == "" {
			t.Fatalf("host %s NOT_APPLICABLE needs evidence reason", host)
		}
	}
}

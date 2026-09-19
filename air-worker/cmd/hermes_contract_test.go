package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func hermesRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate Hermes contract test source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), ".."))
}

func readContractFile(t *testing.T, root string, parts ...string) []byte {
	t.Helper()
	path := filepath.Join(append([]string{root}, parts...)...)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func yamlContractScalar(t *testing.T, text, key string) string {
	t.Helper()
	prefix := key + ":"
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, prefix)), "\"'")
		}
	}
	t.Fatalf("missing YAML key %q", key)
	return ""
}

func inlineYAMLList(t *testing.T, text, key string) []string {
	t.Helper()
	raw := yamlContractScalar(t, text, key)
	if len(raw) < 2 || raw[0] != '[' || raw[len(raw)-1] != ']' {
		t.Fatalf("%s must be an inline list, got %q", key, raw)
	}
	body := strings.TrimSpace(raw[1 : len(raw)-1])
	if body == "" {
		return nil
	}
	parts := strings.Split(body, ",")
	for i := range parts {
		parts[i] = strings.Trim(strings.TrimSpace(parts[i]), "\"'")
	}
	return parts
}

func TestCriterion78HermesPluginContract(t *testing.T) {
	root := hermesRepoRoot(t)
	pluginRoot := filepath.Join(root, ".hermes", "plugins", "air-worker")
	manifestBytes := readContractFile(t, pluginRoot, "plugin.yaml")
	manifest := string(manifestBytes)
	if got := yamlContractScalar(t, manifest, "name"); got != "air-worker" {
		t.Fatalf("plugin name = %q", got)
	}
	if got := yamlContractScalar(t, manifest, "version"); got != version {
		t.Fatalf("Hermes plugin version %q does not match binary %q", got, version)
	}
	tools := inlineYAMLList(t, manifest, "provides_tools")
	if len(tools) != 1 || tools[0] != "air_worker" {
		t.Fatalf("Hermes manifest must declare exactly one air_worker tool: %#v", tools)
	}
	requiredHooks := []string{"pre_llm_call", "pre_tool_call", "post_tool_call", "pre_verify", "subagent_start", "subagent_stop", "on_session_start", "on_session_end"}
	hooks := inlineYAMLList(t, manifest, "provides_hooks")
	sort.Strings(hooks)
	sort.Strings(requiredHooks)
	if strings.Join(hooks, "\x00") != strings.Join(requiredHooks, "\x00") {
		t.Fatalf("Hermes hooks = %#v, want %#v", hooks, requiredHooks)
	}

	registration := string(readContractFile(t, pluginRoot, "__init__.py"))
	if strings.Count(registration, "ctx.register_tool(") != 1 || !strings.Contains(registration, `name="air_worker"`) {
		t.Fatal("plugin must register exactly one air_worker tool")
	}
	for _, hook := range requiredHooks {
		if !strings.Contains(registration, fmt.Sprintf(`("%s",`, hook)) {
			t.Fatalf("plugin does not register required hook %q", hook)
		}
	}

	schema := string(readContractFile(t, pluginRoot, "schemas.py"))
	if strings.Count(schema, `"name": "air_worker"`) != 1 || !strings.Contains(schema, `"enum": ["status", "run", "verify"]`) || !strings.Contains(schema, `"additionalProperties": False`) {
		t.Fatal("air_worker schema must expose only status/run/verify and reject unknown properties")
	}

	skill := readContractFile(t, pluginRoot, "skills", "operate-air-worker", "SKILL.md")
	if bytes.HasPrefix(skill, []byte{0xef, 0xbb, 0xbf}) {
		t.Fatal("Hermes skill must not contain a UTF-8 BOM")
	}
	if len(skill) > 2048 || bytes.Count(skill, []byte("\n")) > 20 {
		t.Fatalf("Hermes skill is not short: %d bytes", len(skill))
	}
	if !bytes.HasPrefix(skill, []byte("---\n")) || !bytes.Contains(skill, []byte("\n---\n")) {
		t.Fatal("Hermes skill frontmatter is missing or not first")
	}
}

type hermesProfileContract struct {
	SchemaVersion   string `json:"schema_version"`
	ContractVersion string `json:"contract_version"`
	Profile         struct {
		Name        string `json:"name"`
		Mode        string `json:"mode"`
		Enforcement bool   `json:"enforcement"`
	} `json:"profile"`
	Plugin struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"plugin"`
	Tool struct {
		Name          string `json:"name"`
		SchemaVersion string `json:"schema_version"`
	} `json:"tool"`
	CapabilityRequirements struct {
		SafePromptTransport struct {
			Accepted []string `json:"accepted"`
			Required bool     `json:"required"`
		} `json:"safe_prompt_transport"`
		OneShot struct {
			Required bool `json:"required"`
		} `json:"oneshot"`
		MaxTurns struct {
			Required bool `json:"required"`
		} `json:"max_turns"`
		RunBudget struct {
			Required bool `json:"required"`
		} `json:"run_budget"`
		UsageReceipt struct {
			Required               bool `json:"required"`
			MachineReadable        bool `json:"machine_readable"`
			CombinedWithQueryFile  bool `json:"combined_with_query_file"`
			DeferredUntilSupported bool `json:"deferred_until_supported"`
		} `json:"usage_receipt"`
	} `json:"capability_requirements"`
	ReleasePolicy struct {
		FailClosedOnMissingCapability bool   `json:"fail_closed_on_missing_capability"`
		Activation                    string `json:"activation"`
	} `json:"release_policy"`
}

func TestCriterion79HermesProfileContracts(t *testing.T) {
	root := hermesRepoRoot(t)
	profilesRoot := filepath.Join(root, ".hermes", "profiles")
	type expectedProfile struct {
		name        string
		mode        string
		enforcement bool
	}
	expected := []expectedProfile{
		{"airworker-hermes-v1-shadow", "shadow", false},
		{"airworker-hermes-v1-enforce", "enforce", true},
	}
	contracts := make([]hermesProfileContract, 0, len(expected))
	for _, want := range expected {
		dir := filepath.Join(profilesRoot, want.name)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read profile %s: %v", want.name, err)
		}
		allowed := map[string]bool{"config.yaml": true, "distribution.yaml": true, "profile-contract.json": true}
		for _, entry := range entries {
			if entry.IsDir() || !allowed[entry.Name()] {
				t.Fatalf("%s contains non-contract state %q", want.name, entry.Name())
			}
		}
		var got hermesProfileContract
		if err := json.Unmarshal(readContractFile(t, dir, "profile-contract.json"), &got); err != nil {
			t.Fatalf("parse %s profile contract: %v", want.name, err)
		}
		contracts = append(contracts, got)
		if got.SchemaVersion != "air-worker.hermes-profile/v1" || got.ContractVersion != "1.0.0" {
			t.Fatalf("%s contract versions are invalid: %+v", want.name, got)
		}
		if got.Profile.Name != want.name || got.Profile.Mode != want.mode || got.Profile.Enforcement != want.enforcement {
			t.Fatalf("%s identity/enforcement mismatch: %+v", want.name, got.Profile)
		}
		if got.Plugin.Name != "air-worker" || got.Plugin.Version != version || got.Tool.Name != "air_worker" || got.Tool.SchemaVersion != "air-worker.tool/v1" {
			t.Fatalf("%s plugin/tool identity mismatch", want.name)
		}
		caps := got.CapabilityRequirements
		acceptedValues := append([]string(nil), caps.SafePromptTransport.Accepted...)
		sort.Strings(acceptedValues)
		accepted := strings.Join(acceptedValues, ",")
		if !caps.SafePromptTransport.Required || accepted != "query-file,stdin" || !caps.OneShot.Required || !caps.MaxTurns.Required || !caps.RunBudget.Required {
			t.Fatalf("%s lacks bounded safe prompt requirements", want.name)
		}
		if caps.UsageReceipt.Required || caps.UsageReceipt.MachineReadable || caps.UsageReceipt.CombinedWithQueryFile || !caps.UsageReceipt.DeferredUntilSupported {
			t.Fatalf("%s does not defer the unsupported combined usage receipt", want.name)
		}
		if !got.ReleasePolicy.FailClosedOnMissingCapability || got.ReleasePolicy.Activation != "explicit-only" {
			t.Fatalf("%s is not explicit-only and fail-closed", want.name)
		}
		distribution := string(readContractFile(t, dir, "distribution.yaml"))
		if got := yamlContractScalar(t, distribution, "hermes_requires"); got != ">=0.21.3" {
			t.Fatalf("%s Hermes version floor = %q", want.name, got)
		}
		config := strings.ReplaceAll(string(readContractFile(t, dir, "config.yaml")), "\r\n", "\n")
		if strings.TrimSpace(config) != "plugins:\n  enabled:\n    - air-worker" {
			t.Fatalf("%s config is not a minimal plugin allow-list", want.name)
		}
		forbidden := map[string]bool{".env": true, "auth.json": true, "soul.md": true, "memory.md": true, "user.md": true, "state.db": true, "hermes_state.db": true, "memories": true, "sessions": true, "cron": true, "logs": true, "home": true, "local": true}
		err = filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if path != dir && forbidden[strings.ToLower(entry.Name())] {
				return fmt.Errorf("forbidden copied state: %s", path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if contracts[0].SchemaVersion != contracts[1].SchemaVersion || contracts[0].ContractVersion != contracts[1].ContractVersion || contracts[0].Tool != contracts[1].Tool || contracts[0].Plugin != contracts[1].Plugin {
		t.Fatal("shadow and enforce profiles do not share the same plugin/tool contract")
	}
}

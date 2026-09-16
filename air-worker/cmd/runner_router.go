package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const routerExecutorTimeout = 20 * time.Minute

type routerRouteEvidence struct {
	Provider string  `json:"provider,omitempty"`
	Model    string  `json:"model,omitempty"`
	Cost     float64 `json:"cost,omitempty"`
	Finish   string  `json:"finish,omitempty"`
}

type openCodeMessage struct {
	Info struct {
		Role   string `json:"role"`
		Finish string `json:"finish"`
		Error  any    `json:"error"`
		Time   struct {
			Completed int64 `json:"completed"`
		} `json:"time"`
	} `json:"info"`
	Parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"parts"`
}

func freeLoopbackPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func resolvePythonTool() (string, error) {
	if v := strings.TrimSpace(os.Getenv("AIR_PYTHON_EXE")); v != "" {
		if _, err := os.Stat(v); err == nil {
			return v, nil
		}
	}
	for _, name := range []string{"python", "python3"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	for _, cand := range []string{`R:\_tools\python314\python.exe`, `C:\Python314\python.exe`} {
		if _, err := os.Stat(cand); err == nil {
			return cand, nil
		}
	}
	return "", fmt.Errorf("python не найден для AirLLMRouter")
}

func resolveRouterScript() (string, error) {
	if v := strings.TrimSpace(os.Getenv("AIR_LLM_ROUTER_PY")); v != "" {
		if _, err := os.Stat(v); err == nil {
			return v, nil
		}
	}
	candidates := []string{
		`R:\-4-\shared-components\releases\air-llm-router\v0.3.0\05_RELEASES\v0.3.0\air_llm_router.py`,
		`E:\-5-\012_Apps\AirLLMRouter_Wiki\05_RELEASES\v0.3.0\air_llm_router.py`,
	}
	for _, cand := range candidates {
		if _, err := os.Stat(cand); err == nil {
			return cand, nil
		}
	}
	return "", fmt.Errorf("AirLLMRouter v0.3.0 не найден; задайте AIR_LLM_ROUTER_PY")
}

func routerBridgeScript(root string) (string, error) {
	candidates := []string{
		filepath.Join(root, "tools", "router_stream_bridge.py"),
	}
	if self, err := os.Executable(); err == nil {
		base := filepath.Dir(self)
		candidates = append(candidates,
			filepath.Join(base, "tools", "router_stream_bridge.py"),
			filepath.Join(filepath.Dir(base), "tools", "router_stream_bridge.py"),
		)
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("router_stream_bridge.py не найден")
}

func routerHTTP(client *http.Client, method, endpoint string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, endpoint, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return err
		}
	}
	return nil
}

func waitRouterHTTP(client *http.Client, endpoint string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := routerHTTP(client, http.MethodGet, endpoint, nil, nil); err == nil {
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for %s", endpoint)
}

func appendEnv(base []string, pairs ...string) []string {
	out := append([]string{}, base...)
	keys := map[string]bool{}
	for _, p := range pairs {
		if i := strings.IndexByte(p, '='); i > 0 {
			keys[strings.ToUpper(p[:i])] = true
		}
	}
	filtered := out[:0]
	for _, p := range out {
		key := p
		if i := strings.IndexByte(p, '='); i > 0 {
			key = p[:i]
		}
		if !keys[strings.ToUpper(key)] {
			filtered = append(filtered, p)
		}
	}
	return append(filtered, pairs...)
}

func normalizeProxyURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "://") {
		return raw
	}
	return "http://" + raw
}

func parseSystemProxy(raw string) (httpProxy, httpsProxy string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	if !strings.Contains(raw, "=") {
		p := normalizeProxyURL(raw)
		return p, p
	}
	for _, part := range strings.Split(raw, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(kv[0])) {
		case "http":
			httpProxy = normalizeProxyURL(kv[1])
		case "https":
			httpsProxy = normalizeProxyURL(kv[1])
		}
	}
	if httpProxy == "" {
		httpProxy = httpsProxy
	}
	if httpsProxy == "" {
		httpsProxy = httpProxy
	}
	return httpProxy, httpsProxy
}

// systemProxyFallbackEnv не задаёт сетевую политику модели/provider. Он лишь
// переносит уже активный proxy ОС в сетевой subprocess, когда родительский
// процесс был запущен до настройки proxy и потому не имеет HTTP(S)_PROXY.
func systemProxyFallbackEnv() []string {
	if strings.TrimSpace(os.Getenv("HTTP_PROXY")) != "" || strings.TrimSpace(os.Getenv("HTTPS_PROXY")) != "" {
		return nil
	}
	if runtime.GOOS != "windows" {
		return nil
	}
	enabled, ok := userDWORDIn(`Software\Microsoft\Windows\CurrentVersion\Internet Settings`, "ProxyEnable")
	if !ok || enabled == 0 {
		return nil
	}
	raw := userEnvVarIn(`Software\Microsoft\Windows\CurrentVersion\Internet Settings`, "ProxyServer")
	httpProxy, httpsProxy := parseSystemProxy(raw)
	var pairs []string
	if httpProxy != "" {
		pairs = append(pairs, "HTTP_PROXY="+httpProxy, "http_proxy="+httpProxy)
	}
	if httpsProxy != "" {
		pairs = append(pairs, "HTTPS_PROXY="+httpsProxy, "https_proxy="+httpsProxy)
	}
	return pairs
}

func lastRouteEvidence(path string) routerRouteEvidence {
	data, err := os.ReadFile(path)
	if err != nil {
		return routerRouteEvidence{}
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		var ev routerRouteEvidence
		if json.Unmarshal([]byte(strings.TrimSpace(lines[i])), &ev) == nil {
			return ev
		}
	}
	return routerRouteEvidence{}
}

func stopProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}

func readLogTail(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	text := strings.TrimSpace(string(data))
	if len(text) > 3000 {
		text = text[len(text)-3000:]
	}
	return text
}

func routerExecutorConfig(baseURL string) map[string]any {
	return map[string]any{
		"$schema":     "https://opencode.ai/config.json",
		"model":       "airrouter/air-auto",
		"small_model": "airrouter/air-auto",
		"provider": map[string]any{
			"airrouter": map[string]any{
				"npm":  "@ai-sdk/openai-compatible",
				"name": "AIR LLM Router",
				"options": map[string]any{
					"baseURL": baseURL,
					"apiKey":  "air-local",
				},
				"models": map[string]any{
					"air-auto": map[string]any{
						"name":  "AIR Auto",
						"limit": map[string]any{"context": 128000, "output": 8192},
					},
				},
			},
		},
	}
}

func writeRouterConfig(path, baseURL string) error {
	raw, err := json.MarshalIndent(routerExecutorConfig(baseURL), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func startLogged(cmd *exec.Cmd, logPath string) error {
	logFile, err := os.Create(logPath)
	if err != nil {
		return err
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return err
	}
	go func() {
		_ = cmd.Wait()
		_ = logFile.Close()
	}()
	return nil
}

func openCodeServerEnv(configPath, dataDir, cacheDir, modelsURL string) []string {
	configHome := filepath.Join(filepath.Dir(configPath), "config-home")
	_ = os.MkdirAll(configHome, 0o755)
	return appendEnv(os.Environ(),
		"OPENCODE_CONFIG="+configPath,
		"OPENCODE_CONFIG_DIR="+filepath.Dir(configPath),
		"OPENCODE_DISABLE_AUTOUPDATE=true",
		"OPENCODE_DISABLE_DEFAULT_PLUGINS=true",
		"OPENCODE_MODELS_URL="+modelsURL,
		"XDG_CONFIG_HOME="+configHome,
		"XDG_DATA_HOME="+dataDir,
		"XDG_CACHE_HOME="+cacheDir,
		"NO_PROXY=127.0.0.1,localhost",
		"no_proxy=127.0.0.1,localhost",
	)
}

func routerBridgeEnv(routerScript, receiptPath string, port int) []string {
	pairs := []string{
		"AIR_LLM_ROUTER_PY=" + routerScript,
		"AIR_ROUTER_BRIDGE_PORT=" + strconv.Itoa(port),
		"AIR_ROUTER_BRIDGE_RECEIPTS=" + receiptPath,
		"PYTHONUTF8=1",
		"NO_PROXY=127.0.0.1,localhost",
		"no_proxy=127.0.0.1,localhost",
	}
	pairs = append(pairs, systemProxyFallbackEnv()...)
	return appendEnv(os.Environ(), pairs...)
}

func (c *loopCtx) invokeRouter(openCodePath, prompt string, runner runnerSpec, stepID string) (result stepResult) {
	scope := c.scope()
	receiptPath, outputPath := jobReceiptPaths(scope, stepID, "executor-router")
	if err := os.MkdirAll(filepath.Dir(receiptPath), 0o755); err != nil {
		return stepResult{Subtype: "runner_error", Detail: err.Error()}
	}
	receipt, lock, err := startJobReceipt(scope, stepID, "executor-router", receiptPath, outputPath)
	if err != nil {
		return stepResult{Subtype: "runner_error", Detail: err.Error()}
	}
	receipt.Runner = "router"
	_ = writeJobReceipt(receiptPath, receipt)
	finished := false
	defer func() {
		if !finished {
			_ = finishJobReceipt(receiptPath, jobStatusFailed)
		}
		lock.release()
	}()

	pythonPath, err := resolvePythonTool()
	if err != nil {
		return stepResult{Subtype: "no_runner", Detail: err.Error()}
	}
	routerScript, err := resolveRouterScript()
	if err != nil {
		return stepResult{Subtype: "no_runner", Detail: err.Error()}
	}
	bridgeScript, err := routerBridgeScript(c.Root)
	if err != nil {
		return stepResult{Subtype: "no_runner", Detail: err.Error()}
	}

	workDir, err := os.MkdirTemp(filepath.Join(c.Root, ".woody"), "router-exec-")
	if err != nil {
		return stepResult{Subtype: "runner_error", Detail: err.Error()}
	}
	defer os.RemoveAll(workDir)
	bridgePort, err := freeLoopbackPort()
	if err != nil {
		return stepResult{Subtype: "runner_error", Detail: err.Error()}
	}
	serverPort, err := freeLoopbackPort()
	if err != nil {
		return stepResult{Subtype: "runner_error", Detail: err.Error()}
	}
	routeReceipt := filepath.Join(workDir, "route.jsonl")
	configPath := filepath.Join(workDir, "opencode.json")
	bridgeURL := fmt.Sprintf("http://127.0.0.1:%d", bridgePort)
	serverURL := fmt.Sprintf("http://127.0.0.1:%d", serverPort)
	if err := writeRouterConfig(configPath, bridgeURL+"/v1"); err != nil {
		return stepResult{Subtype: "runner_error", Detail: err.Error()}
	}

	bridgeCmd := exec.Command(pythonPath, bridgeScript)
	bridgeCmd.Dir = c.Root
	bridgeCmd.Env = routerBridgeEnv(routerScript, routeReceipt, bridgePort)
	if err := startLogged(bridgeCmd, filepath.Join(workDir, "bridge.log")); err != nil {
		return stepResult{Subtype: "runner_error", Detail: "bridge: " + err.Error()}
	}
	defer stopProcess(bridgeCmd)

	client := &http.Client{Timeout: 60 * time.Second}
	if err := waitRouterHTTP(client, bridgeURL+"/v1/models", 10*time.Second); err != nil {
		return stepResult{Subtype: "runner_error", Detail: err.Error()}
	}

	serverCmd := exec.Command(openCodePath, "serve", "--pure", "--hostname", "127.0.0.1", "--port", strconv.Itoa(serverPort), "--print-logs", "--log-level", "DEBUG")
	serverCmd.Dir = c.Root
	serverCmd.Env = openCodeServerEnv(configPath, filepath.Join(workDir, "data"), filepath.Join(workDir, "cache"), bridgeURL)
	if err := startLogged(serverCmd, filepath.Join(workDir, "opencode.log")); err != nil {
		return stepResult{Subtype: "runner_error", Detail: "opencode: " + err.Error()}
	}
	defer stopProcess(serverCmd)
	if err := waitRouterHTTP(client, serverURL+"/provider", 75*time.Second); err != nil {
		return stepResult{Subtype: "runner_error", Detail: err.Error() + "\nopencode.log:\n" + readLogTail(filepath.Join(workDir, "opencode.log"))}
	}

	qdir := url.QueryEscape(c.Root)
	var session struct {
		ID string `json:"id"`
	}
	if err := routerHTTP(client, http.MethodPost, serverURL+"/session?directory="+qdir,
		map[string]any{"title": "air-worker router " + stepID}, &session); err != nil {
		return stepResult{Subtype: "runner_error", Detail: "session create: " + err.Error()}
	}
	if session.ID == "" {
		return stepResult{Subtype: "runner_error", Detail: "OpenCode session id is empty"}
	}
	promptBody := map[string]any{
		"agent": "build",
		"model": map[string]any{"providerID": "airrouter", "modelID": "air-auto"},
		"parts": []map[string]any{{"type": "text", "text": prompt}},
	}
	if err := routerHTTP(client, http.MethodPost,
		serverURL+"/session/"+session.ID+"/prompt_async?directory="+qdir, promptBody, nil); err != nil {
		return stepResult{Subtype: "runner_error", Detail: "prompt_async: " + err.Error()}
	}

	ctx, cancel := context.WithTimeout(context.Background(), routerExecutorTimeout)
	defer cancel()
	turns := 0
	detail := ""
	for {
		select {
		case <-ctx.Done():
			return stepResult{Session: session.ID, Subtype: "runner_timeout", Detail: ctx.Err().Error()}
		case <-time.After(500 * time.Millisecond):
		}
		var messages []openCodeMessage
		if err := routerHTTP(client, http.MethodGet,
			serverURL+"/session/"+session.ID+"/message?directory="+qdir, nil, &messages); err != nil {
			continue
		}
		turns = 0
		for _, msg := range messages {
			if msg.Info.Role == "assistant" {
				turns++
			}
		}
		if len(messages) == 0 {
			continue
		}
		last := messages[len(messages)-1]
		if last.Info.Role != "assistant" || last.Info.Time.Completed == 0 {
			continue
		}
		if last.Info.Error != nil {
			raw, _ := json.Marshal(last.Info.Error)
			return stepResult{Session: session.ID, Subtype: "runner_error", Detail: string(raw)}
		}
		if last.Info.Finish != "stop" {
			continue
		}
		var texts []string
		for _, part := range last.Parts {
			if part.Type == "text" && strings.TrimSpace(part.Text) != "" {
				texts = append(texts, strings.TrimSpace(part.Text))
			}
		}
		detail = strings.Join(texts, "\n")
		break
	}

	ev := lastRouteEvidence(routeReceipt)
	if ev.Provider == "" || ev.Model == "" {
		return stepResult{Session: session.ID, Subtype: "unparsed", Detail: "Router route evidence is empty"}
	}
	receipt.Provider = ev.Provider
	receipt.Model = ev.Model
	cost := ev.Cost
	receipt.Cost = &cost
	_ = writeJobReceipt(receiptPath, receipt)
	_ = os.WriteFile(outputPath, []byte(detail+"\n"), 0o644)
	if err := finishJobReceipt(receiptPath, jobStatusDone); err != nil {
		return stepResult{Session: session.ID, Subtype: "runner_error", Detail: err.Error()}
	}
	finished = true
	return stepResult{
		Ok: true, Cost: &cost, Turns: &turns, Session: session.ID,
		Subtype: "success", Detail: detail,
	}
}

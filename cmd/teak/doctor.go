package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"teak/internal/config"
	"teak/internal/lsp"
	"teak/internal/toolpath"
)

const doctorUsageText = `Usage:
  teak doctor [--json] [--root <directory>]

Options:
  --json                 Emit a machine-readable report
  --root <directory>     Diagnose this project directory
  -h, --help             Show this help and exit
`

type doctorOptions struct {
	json bool
	root string
}

type doctorCheck struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Detail     string `json:"detail"`
	Hint       string `json:"hint,omitempty"`
	Version    string `json:"version,omitempty"`
	Capability string `json:"capability,omitempty"`
}

type doctorAction struct {
	Component string `json:"component"`
	Name      string `json:"name"`
	State     string `json:"state"`
	Action    string `json:"action"`
	Hint      string `json:"hint,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

type doctorReport struct {
	Version      string              `json:"version"`
	GoVersion    string              `json:"go_version"`
	OS           string              `json:"os"`
	Arch         string              `json:"arch"`
	Workspace    string              `json:"workspace"`
	ConfigPath   string              `json:"config_path"`
	Checks       []doctorCheck       `json:"checks"`
	Languages    []doctorLanguage    `json:"languages,omitempty"`
	LanguageScan *doctorLanguageScan `json:"language_scan,omitempty"`
	Actions      []doctorAction      `json:"actions,omitempty"`
}

type doctorLanguage struct {
	LanguageID      string   `json:"language_id"`
	Extensions      []string `json:"extensions"`
	Files           int      `json:"files"`
	Server          string   `json:"server"`
	State           string   `json:"state"`
	Path            string   `json:"path,omitempty"`
	Version         string   `json:"version,omitempty"`
	VersionProbe    string   `json:"version_probe,omitempty"`
	ProtocolProbe   string   `json:"protocol_probe,omitempty"`
	CapabilityProbe string   `json:"capability_probe,omitempty"`
	Capabilities    []string `json:"capabilities,omitempty"`
	Hint            string   `json:"hint,omitempty"`
}

type doctorLanguageScan struct {
	ScannedFiles int   `json:"scanned_files"`
	Truncated    bool  `json:"truncated"`
	DurationMS   int64 `json:"duration_ms"`
}

type doctorToolProbe struct {
	Index int
	Name  string
}

type doctorLanguageProbe struct {
	Indexes  []int
	Server   lsp.ServerConfig
	Protocol bool
}

const (
	doctorMaxScannedFiles = 20_000
	doctorMaxScanDepth    = 12
	doctorScanTimeout     = 2 * time.Second
	// Tool probes are individually bounded by toolpath, but the doctor must
	// also have a total budget so a project with many configured servers cannot
	// make startup diagnostics take one timeout per executable.
	doctorToolProbeTimeout = 3 * time.Second
)

var doctorSkippedDirectories = map[string]struct{}{
	".git":         {},
	".glyphrun":    {},
	".codemap":     {},
	".vecgrep":     {},
	".cache":       {},
	"node_modules": {},
	"vendor":       {},
	"target":       {},
	"dist":         {},
	"build":        {},
}

func isDoctorCommand(args []string) bool {
	return len(args) > 0 && args[0] == "doctor"
}

func parseDoctorArgs(args []string) (doctorOptions, bool, error) {
	var opts doctorOptions
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json":
			opts.json = true
		case "--root":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return doctorOptions{}, false, fmt.Errorf("--root requires a directory")
			}
			opts.root = args[i+1]
			i++
		case "-h", "--help":
			return doctorOptions{}, true, nil
		default:
			return doctorOptions{}, false, fmt.Errorf("unknown doctor option %q", args[i])
		}
	}
	return opts, false, nil
}

// runDoctorCLI is deliberately independent from terminal startup. It must be
// usable from CI, Glyphrun, and a shell where Teak is not attached to a TTY.
func runDoctorCLI(args []string, stdout, stderr io.Writer, buildVersion string) int {
	opts, showHelp, err := parseDoctorArgs(args)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Error: %v\n%s", err, doctorUsageText)
		return 2
	}
	if showHelp {
		_, _ = io.WriteString(stdout, doctorUsageText)
		return 0
	}

	report := collectDoctor(opts.root, buildVersion, toolpath.Default())
	if opts.json {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			_, _ = fmt.Fprintf(stderr, "Error: write doctor report: %v\n", err)
			return 1
		}
	} else {
		if err := writeDoctorReport(stdout, report); err != nil {
			_, _ = fmt.Fprintf(stderr, "Error: write doctor report: %v\n", err)
			return 1
		}
	}

	for _, check := range report.Checks {
		if check.Status == "fail" {
			return 1
		}
	}
	return 0
}

func collectDoctor(root, buildVersion string, resolver *toolpath.Resolver) doctorReport {
	if buildVersion == "" {
		buildVersion = developmentVersion
	}
	if resolver == nil {
		resolver = toolpath.Default()
	}

	workspace, workspaceErr := doctorWorkspace(root)
	report := doctorReport{
		Version:    buildVersion,
		GoVersion:  runtime.Version(),
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		Workspace:  workspace,
		ConfigPath: config.ConfigPath(),
	}

	if workspaceErr != nil {
		report.Checks = append(report.Checks, doctorCheck{
			Name:   "workspace",
			Status: "fail",
			Detail: workspaceErr.Error(),
		})
	} else {
		report.Checks = append(report.Checks, doctorCheck{
			Name:   "workspace",
			Status: "pass",
			Detail: workspace,
		})
	}

	cfg, configErr := config.Load()
	if configErr != nil {
		report.Checks = append(report.Checks, doctorCheck{
			Name:   "config",
			Status: "fail",
			Detail: fmt.Sprintf("%s: %v", config.ConfigPath(), configErr),
			Hint:   "fix the configuration or remove it to restore defaults",
		})
	} else {
		report.Checks = append(report.Checks, doctorCheck{
			Name:   "config",
			Status: "pass",
			Detail: config.ConfigPath(),
		})
	}

	tools := []string{"git", "rg", "codemap", "vecgrep", "hitspec", "glyph"}
	var lspConfigs []lsp.ServerConfig
	if configErr == nil {
		lspConfigs = lspConfigsFromConfig(cfg)
		for _, configured := range cfg.LSP {
			tools = append(tools, configured.Command)
		}
		if cfg.Agent.Enabled {
			tools = append(tools, cfg.Agent.Command)
		}
	}
	if workspaceErr == nil && len(lspConfigs) > 0 {
		scanCtx, cancel := context.WithTimeout(context.Background(), doctorScanTimeout)
		report.Languages, report.LanguageScan = detectDoctorLanguagesContext(scanCtx, workspace, lspConfigs, resolver)
		cancel()
		for _, language := range report.Languages {
			tools = append(tools, language.Server)
		}
	}

	probeCtx, cancelProbes := context.WithTimeout(context.Background(), doctorToolProbeTimeout)
	defer cancelProbes()

	seen := make(map[string]struct{}, len(tools))
	probes := make([]doctorToolProbe, 0, len(tools))
	for _, name := range tools {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}

		path, err := resolver.Resolve(name)
		if err != nil {
			hint := toolpath.Hint(name)
			if missing, ok := err.(*toolpath.MissingToolError); ok {
				hint = missing.Hint
			}
			report.Checks = append(report.Checks, doctorCheck{
				Name:   "tool:" + name,
				Status: "warn",
				Detail: "not available",
				Hint:   hint,
			})
			continue
		}
		check := doctorCheck{
			Name:   "tool:" + name,
			Status: "pass",
			Detail: path,
		}
		if language, ok := doctorLanguageProbeForTool(report.Languages, name); ok {
			applyDoctorLanguageProbe(&check, language)
		} else if resolver.HasVersionProbe(name) {
			probes = append(probes, doctorToolProbe{Index: len(report.Checks), Name: name})
		}
		report.Checks = append(report.Checks, check)
	}
	probeDoctorTools(probeCtx, resolver, report.Checks, probes)
	report.Actions = buildDoctorActions(report.Checks, report.Languages)

	return report
}

const maxDoctorActions = 64

func buildDoctorActions(checks []doctorCheck, languages []doctorLanguage) []doctorAction {
	actions := make([]doctorAction, 0)
	seen := make(map[string]struct{})
	appendAction := func(action doctorAction) {
		if len(actions) >= maxDoctorActions {
			return
		}
		key := action.Component + "\x00" + action.Name + "\x00" + action.Action
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		actions = append(actions, action)
	}

	for _, check := range checks {
		if check.Status == "pass" {
			continue
		}
		component := "check"
		name := check.Name
		action := "repair"
		state := check.Status
		if check.Name == "config" {
			component = "config"
			name = "config"
			state = "invalid"
		} else if strings.HasPrefix(check.Name, "tool:") {
			component = "tool"
			name = strings.TrimPrefix(check.Name, "tool:")
			detail := strings.ToLower(check.Detail)
			if strings.Contains(detail, "not available") {
				state = "missing"
				action = "install"
			} else if strings.Contains(detail, "version probe failed") {
				state = "failed"
			} else if strings.Contains(detail, "version probe timed out") {
				state = "timed_out"
			} else if strings.Contains(detail, "capability probe failed") {
				state = "unsupported"
				action = "upgrade"
			}
		}
		hint := check.Hint
		if hint == "" {
			hint = check.Detail
		}
		appendAction(doctorAction{
			Component: component,
			Name:      name,
			State:     state,
			Action:    action,
			Hint:      hint,
			Detail:    check.Detail,
		})
	}

	for _, language := range languages {
		state := strings.ToLower(strings.TrimSpace(language.State))
		if state == "available" {
			continue
		}
		action := "repair"
		if state == "missing" {
			action = "install"
		}
		hint := language.Hint
		if hint == "" {
			hint = "run teak doctor again after repairing the language server"
		}
		appendAction(doctorAction{
			Component: "lsp",
			Name:      language.LanguageID,
			State:     state,
			Action:    action,
			Hint:      hint,
			Detail:    fmt.Sprintf("%s (%d detected files)", language.Server, language.Files),
		})
	}
	return actions
}

// probeDoctorTools runs independent version checks concurrently while keeping
// the report slice in its original deterministic order. The caller owns the
// shared context, so one total deadline bounds both successful and hung tools.
func probeDoctorTools(ctx context.Context, resolver *toolpath.Resolver, checks []doctorCheck, probes []doctorToolProbe) {
	if resolver == nil || len(probes) == 0 {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var wait sync.WaitGroup
	wait.Add(len(probes))
	for _, probe := range probes {
		go func(probe doctorToolProbe) {
			defer wait.Done()
			if probe.Index < 0 || probe.Index >= len(checks) {
				return
			}
			check := &checks[probe.Index]
			version, probeErr := resolver.Version(ctx, probe.Name)
			if probeErr != nil {
				check.Status = "warn"
				detail := "version probe failed"
				if errors.Is(probeErr, context.DeadlineExceeded) || errors.Is(probeErr, context.Canceled) {
					detail = "version probe timed out"
				}
				check.Detail = fmt.Sprintf("%s; %s: %v", check.Detail, detail, probeErr)
				check.Hint = "verify or reinstall the resolved executable before relying on it"
				return
			}
			check.Version = version
			check.Detail = fmt.Sprintf("%s (%s)", check.Detail, version)
			if capability, capabilityErr := probeDoctorCapability(ctx, resolver, probe.Name); capabilityErr != nil {
				check.Status = "warn"
				check.Detail = fmt.Sprintf("%s; capability probe failed: %v", check.Detail, capabilityErr)
				check.Hint = doctorCapabilityHint(probe.Name)
				return
			} else if capability != "" {
				check.Capability = capability
			}
		}(probe)
	}
	wait.Wait()
}

// probeDoctorCapability verifies the bounded contract that makes a resolved
// executable safe for Teak's health/control-plane paths. A version string only
// proves that a binary starts; it does not prove that the installed release
// supports the bounded capability Teak actually invokes.
func probeDoctorCapability(ctx context.Context, resolver *toolpath.Resolver, name string) (string, error) {
	var args []string
	var marker string
	var capability string
	switch name {
	case "codemap":
		args = []string{"structural-manifest", "--help"}
		marker = "structural-manifest"
		capability = "structural-manifest"
	case "vecgrep":
		args = []string{"status", "--help"}
		marker = "--lightweight"
		capability = "lightweight-status"
	case "hitspec":
		args = []string{"validate", "--help"}
		marker = "validate"
		capability = "validate"
	default:
		return "", nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cmd, err := resolver.Command(ctx, name, args...)
	if err != nil {
		return "", err
	}
	stdout := &headlessOutputBuffer{limit: 64 << 10, onLimit: func() { _ = toolpath.TerminateCommand(cmd) }}
	stderr := &headlessOutputBuffer{limit: 64 << 10, onLimit: func() { _ = toolpath.TerminateCommand(cmd) }}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		if stderr.Len() > 0 {
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return "", err
	}
	if stdout.truncated || stderr.truncated {
		return "", fmt.Errorf("capability help output exceeds 65536 bytes")
	}
	if !strings.Contains(strings.ToLower(stdout.String()+"\n"+stderr.String()), strings.ToLower(marker)) {
		return "", fmt.Errorf("installed release does not advertise %s", marker)
	}
	return capability, nil
}

func doctorCapabilityHint(name string) string {
	switch name {
	case "codemap":
		return "upgrade codemap to a release with structural-manifest"
	case "vecgrep":
		return "upgrade vecgrep to a release with status --lightweight"
	case "hitspec":
		return "upgrade hitspec to a release with the validate command"
	default:
		return "verify the installed tool release before relying on it"
	}
}

// doctorLanguageProbeForTool lets the general tool inventory reuse a probe
// already performed while inspecting a detected language. Without this
// bridge, a configured LSP was checked once as a language server and again as
// a generic executable in the same doctor run.
func doctorLanguageProbeForTool(languages []doctorLanguage, name string) (doctorLanguage, bool) {
	for _, language := range languages {
		if language.Server != name {
			continue
		}
		switch language.VersionProbe {
		case "ready", "failed", "timed_out":
			return language, true
		}
		switch language.ProtocolProbe {
		case "ready", "failed", "timed_out":
			return language, true
		}
	}
	return doctorLanguage{}, false
}

func applyDoctorLanguageProbe(check *doctorCheck, language doctorLanguage) {
	if check == nil {
		return
	}
	switch language.VersionProbe {
	case "ready":
		check.Version = language.Version
		if language.Version != "" {
			check.Detail = fmt.Sprintf("%s (%s)", check.Detail, language.Version)
		}
	case "failed", "timed_out":
		check.Status = "warn"
		detail := "version probe failed"
		if language.VersionProbe == "timed_out" {
			detail = "version probe timed out"
		}
		check.Detail = fmt.Sprintf("%s; %s", check.Detail, detail)
		check.Hint = language.Hint
	}
	if language.ProtocolProbe == "ready" {
		if !strings.Contains(check.Detail, "LSP initialize handshake") {
			check.Detail += " (LSP initialize handshake)"
		}
		return
	}
	if language.ProtocolProbe == "failed" || language.ProtocolProbe == "timed_out" {
		check.Status = "warn"
		detail := "protocol probe failed"
		if language.ProtocolProbe == "timed_out" {
			detail = "protocol probe timed out"
		}
		check.Detail = fmt.Sprintf("%s; %s", check.Detail, detail)
		check.Hint = language.Hint
	}
}

func doctorWorkspace(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve current directory: %w", err)
		}
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve workspace %q: %w", root, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("workspace %q: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace %q is not a directory", abs)
	}
	return abs, nil
}

func writeDoctorReport(w io.Writer, report doctorReport) error {
	var body strings.Builder
	fmt.Fprintf(&body, "teak doctor %s\n", report.Version)
	fmt.Fprintf(&body, "Runtime: %s (%s/%s)\n", report.GoVersion, report.OS, report.Arch)
	fmt.Fprintf(&body, "Workspace: %s\n", report.Workspace)
	fmt.Fprintf(&body, "Config: %s\n\n", report.ConfigPath)

	counts := map[string]int{"pass": 0, "warn": 0, "fail": 0}
	checks := append([]doctorCheck(nil), report.Checks...)
	sort.SliceStable(checks, func(i, j int) bool { return checks[i].Name < checks[j].Name })
	for _, check := range checks {
		counts[check.Status]++
		fmt.Fprintf(&body, "%-4s %-18s %s\n", strings.ToUpper(check.Status), check.Name, check.Detail)
		if check.Capability != "" {
			fmt.Fprintf(&body, "     %-18s capability: %s\n", "", check.Capability)
		}
		if check.Hint != "" {
			fmt.Fprintf(&body, "     %-18s hint: %s\n", "", check.Hint)
		}
	}
	fmt.Fprintf(&body, "\nSummary: %d passed, %d warnings, %d failures\n", counts["pass"], counts["warn"], counts["fail"])
	if len(report.Actions) > 0 {
		fmt.Fprintln(&body, "\nActions:")
		for _, action := range report.Actions {
			fmt.Fprintf(&body, "- %s/%s [%s] %s\n", action.Component, action.Name, action.Action, action.Hint)
		}
	}
	if len(report.Languages) > 0 {
		fmt.Fprintln(&body, "\nDetected languages:")
		for _, language := range report.Languages {
			detail := fmt.Sprintf("%s files=%d server=%s state=%s", language.LanguageID, language.Files, language.Server, language.State)
			if language.VersionProbe != "" {
				detail += " version_probe=" + language.VersionProbe
			}
			if language.ProtocolProbe != "" {
				detail += " protocol_probe=" + language.ProtocolProbe
			}
			if language.CapabilityProbe != "" {
				detail += " capability_probe=" + language.CapabilityProbe
			}
			if len(language.Capabilities) > 0 {
				detail += " capabilities=" + strings.Join(language.Capabilities, ",")
			}
			if language.Path != "" {
				detail += " (" + language.Path + ")"
			}
			fmt.Fprintln(&body, detail)
			if language.Hint != "" {
				fmt.Fprintf(&body, "  hint: %s\n", language.Hint)
			}
		}
	}
	if report.LanguageScan != nil && report.LanguageScan.Truncated {
		fmt.Fprintf(&body, "\nLanguage scan: truncated after %d files (%d ms)\n", report.LanguageScan.ScannedFiles, report.LanguageScan.DurationMS)
	}
	_, err := io.WriteString(w, body.String())
	return err
}

func lspConfigsFromConfig(cfg config.Config) []lsp.ServerConfig {
	userConfigs := make([]lsp.ServerConfig, 0, len(cfg.LSP))
	for _, user := range cfg.LSP {
		userConfigs = append(userConfigs, lsp.ServerConfig{
			Extensions: append([]string(nil), user.Extensions...),
			Command:    user.Command,
			Args:       append([]string(nil), user.Args...),
			LanguageID: user.LanguageID,
			Env:        cloneStringMap(user.Env),
		})
	}
	return lsp.MergeConfigs(lsp.DefaultConfigs(), userConfigs)
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	clone := make(map[string]string, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

func detectDoctorLanguages(root string, configs []lsp.ServerConfig, resolver *toolpath.Resolver) []doctorLanguage {
	languages, _ := detectDoctorLanguagesContext(context.Background(), root, configs, resolver)
	return languages
}

// doctorLanguageProbeKey identifies probe work that can be safely shared by
// detected language entries. Both probe kinds depend on the command's
// environment; protocol probes additionally depend on launch arguments.
func doctorLanguageProbeKey(server lsp.ServerConfig, protocol bool) string {
	var key strings.Builder
	if protocol {
		key.WriteString("protocol")
	} else {
		key.WriteString("version")
	}
	key.WriteByte(0)
	key.WriteString(server.Command)
	for _, arg := range server.Args {
		key.WriteByte(0)
		key.WriteString(arg)
	}
	if len(server.Env) > 0 {
		keys := make([]string, 0, len(server.Env))
		for name := range server.Env {
			keys = append(keys, name)
		}
		sort.Strings(keys)
		for _, name := range keys {
			key.WriteByte(0)
			key.WriteString(name)
			key.WriteByte('=')
			key.WriteString(server.Env[name])
		}
	}
	return key.String()
}

func detectDoctorLanguagesContext(ctx context.Context, root string, configs []lsp.ServerConfig, resolver *toolpath.Resolver) ([]doctorLanguage, *doctorLanguageScan) {
	if resolver == nil {
		resolver = toolpath.Default()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	started := time.Now()
	scan := &doctorLanguageScan{}

	byExtension := make(map[string]int)
	for index, server := range configs {
		for _, extension := range server.Extensions {
			extension = strings.ToLower(strings.TrimSpace(extension))
			if extension != "" {
				byExtension[extension] = index
			}
		}
	}
	counts := make([]int, len(configs))
	scannedFiles := 0
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			scan.Truncated = true
			return fs.SkipAll
		}
		if walkErr != nil {
			scan.Truncated = true
			return nil
		}
		if entry.IsDir() {
			if path == root {
				return nil
			}
			if _, skip := doctorSkippedDirectories[entry.Name()]; skip {
				return filepath.SkipDir
			}
			relative, err := filepath.Rel(root, path)
			if err == nil && strings.Count(relative, string(filepath.Separator)) >= doctorMaxScanDepth {
				scan.Truncated = true
				return filepath.SkipDir
			}
			return nil
		}
		if scannedFiles >= doctorMaxScannedFiles {
			scan.Truncated = true
			return fs.SkipAll
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		scannedFiles++
		if index, ok := byExtension[strings.ToLower(filepath.Ext(entry.Name()))]; ok {
			counts[index]++
		}
		return nil
	})
	scan.ScannedFiles = scannedFiles
	scan.DurationMS = time.Since(started).Milliseconds()

	languages := make([]doctorLanguage, 0)
	languageProbes := make([]doctorLanguageProbe, 0)
	probeIndexes := make(map[string]int)
	for index, count := range counts {
		if count == 0 {
			continue
		}
		server := configs[index]
		language := doctorLanguage{
			LanguageID:   server.LanguageID,
			Extensions:   append([]string(nil), server.Extensions...),
			Files:        count,
			Server:       server.Command,
			State:        "missing",
			VersionProbe: "missing",
		}
		path, err := resolver.Resolve(server.Command)
		if err == nil {
			language.State = "available"
			language.Path = path
			protocolProbe := !resolver.HasVersionProbe(server.Command)
			probe := doctorLanguageProbe{
				Indexes:  []int{len(languages)},
				Server:   server,
				Protocol: protocolProbe,
			}
			key := doctorLanguageProbeKey(server, protocolProbe)
			if existing, ok := probeIndexes[key]; ok {
				languageProbes[existing].Indexes = append(languageProbes[existing].Indexes, len(languages))
			} else {
				probeIndexes[key] = len(languageProbes)
				languageProbes = append(languageProbes, probe)
			}
			if protocolProbe {
				language.VersionProbe = "unsupported"
				language.ProtocolProbe = "pending"
				language.CapabilityProbe = "pending"
				language.Hint = "no safe version probe is configured; verifying the LSP initialize handshake"
			}
		} else if missing, ok := err.(*toolpath.MissingToolError); ok {
			language.Hint = missing.Hint
		} else {
			language.Hint = err.Error()
		}
		languages = append(languages, language)
	}

	// Probes are independent, so run them concurrently while sharing the scan
	// context's deadline. The result slice is still assembled in config order,
	// and the stable sort below keeps the public report deterministic.
	var probes sync.WaitGroup
	probes.Add(len(languageProbes))
	for _, languageProbe := range languageProbes {
		go func(languageProbe doctorLanguageProbe) {
			defer probes.Done()
			server := languageProbe.Server
			outcome := struct {
				state           string
				version         string
				versionProbe    string
				protocolProbe   string
				capabilityProbe string
				capabilities    []string
				hint            string
			}{state: "failed"}
			if ctx.Err() != nil {
				outcome.state = "timed_out"
				if languageProbe.Protocol {
					outcome.protocolProbe = "timed_out"
					outcome.capabilityProbe = "timed_out"
				} else {
					outcome.versionProbe = "timed_out"
				}
				outcome.hint = "language discovery timed out before the server probe completed; retry doctor"
			} else if languageProbe.Protocol {
				probeResult, probeErr := lsp.ProbeProtocolCapabilities(ctx, server, root)
				if probeErr != nil {
					outcome.protocolProbe = "failed"
					outcome.capabilityProbe = "failed"
					if ctx.Err() != nil {
						outcome.state = "timed_out"
						outcome.protocolProbe = "timed_out"
						outcome.capabilityProbe = "timed_out"
						outcome.hint = "language discovery timed out before the LSP handshake completed; retry doctor"
					} else {
						outcome.hint = fmt.Sprintf("language server handshake failed: %v", probeErr)
					}
				} else {
					outcome.state = "available"
					outcome.protocolProbe = "ready"
					outcome.capabilityProbe = "ready"
					outcome.capabilities = append([]string(nil), probeResult.Capabilities...)
				}
			} else if ctx.Err() == nil {
				version, probeErr := resolver.VersionWithEnv(ctx, server.Command, server.Env)
				if probeErr != nil {
					outcome.versionProbe = "failed"
					if ctx.Err() != nil || errors.Is(probeErr, context.DeadlineExceeded) {
						outcome.state = "timed_out"
						outcome.versionProbe = "timed_out"
						outcome.hint = "language discovery timed out before the server probe completed; retry doctor"
					} else {
						outcome.hint = fmt.Sprintf("language server probe failed: %v", probeErr)
					}
				} else {
					outcome.state = "available"
					outcome.version = version
					outcome.versionProbe = "ready"
				}
			} else {
				outcome.state = "timed_out"
				outcome.versionProbe = "timed_out"
				outcome.hint = "language discovery timed out before the server probe completed; retry doctor"
			}
			for _, index := range languageProbe.Indexes {
				language := &languages[index]
				language.State = outcome.state
				language.Version = outcome.version
				if outcome.versionProbe != "" {
					language.VersionProbe = outcome.versionProbe
				}
				if outcome.protocolProbe != "" {
					language.ProtocolProbe = outcome.protocolProbe
				}
				if outcome.capabilityProbe != "" {
					language.CapabilityProbe = outcome.capabilityProbe
				}
				language.Capabilities = append([]string(nil), outcome.capabilities...)
				language.Hint = outcome.hint
			}
		}(languageProbe)
	}
	probes.Wait()
	sort.SliceStable(languages, func(i, j int) bool {
		if languages[i].Files != languages[j].Files {
			return languages[i].Files > languages[j].Files
		}
		return languages[i].LanguageID < languages[j].LanguageID
	})
	return languages, scan
}

// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Configuration model.
//
// Keys are flag names. A config file says `"concurrency": 8` because the flag
// is --concurrency, so the file needs no parallel schema, covers every flag the
// command exposes, and picks up new flags automatically. The alternative — a
// struct mirroring 30-odd flags — is what left the previous config supporting
// five settings out of thirty-one.
//
// Precedence, highest first:
//
//  1. an explicitly-passed flag
//  2. the selected profile
//  3. the defaults block
//  4. the flag's built-in default
//
// Environment variables are deliberately not a layer here. Adding CORRAL_* is
// easy but it is another surface to document and test, and nothing in the tool
// needs it yet; flags plus a file cover the reported use cases.
type configFile struct {
	// Defaults apply to every command and every owner.
	Defaults map[string]any `json:"defaults,omitempty"`
	// Profiles are named sets of owners plus overrides for them.
	Profiles map[string]profile `json:"profiles,omitempty"`
}

// profile is a named set of owners plus per-profile setting overrides. Settings
// are keyed by flag name in Settings; the legacy top-level snake_case fields
// are still parsed so existing config files keep working.
type profile struct {
	Owners   []string       `json:"owners"`
	Settings map[string]any `json:"settings,omitempty"`

	// Deprecated legacy fields, retained for backward compatibility with
	// configs written before v0.0.21. Prefer "settings" keyed by flag name.
	LegacyBaseDir     string `json:"base_dir,omitempty"`
	LegacyLayout      string `json:"layout,omitempty"`
	LegacyProtocol    string `json:"protocol,omitempty"`
	LegacyConcurrency int    `json:"concurrency,omitempty"`
	LegacyLimit       int    `json:"limit,omitempty"`
}

// effectiveSettings folds the legacy fields into the flag-name-keyed map so the
// rest of the code only deals with one shape. Explicit "settings" entries win.
func (p profile) effectiveSettings() map[string]any {
	out := map[string]any{}
	if p.LegacyBaseDir != "" {
		out["base-dir"] = p.LegacyBaseDir
	}
	if p.LegacyLayout != "" {
		out["layout"] = p.LegacyLayout
	}
	if p.LegacyProtocol != "" {
		out["protocol"] = p.LegacyProtocol
	}
	if p.LegacyConcurrency != 0 {
		out["concurrency"] = p.LegacyConcurrency
	}
	if p.LegacyLimit != 0 {
		out["limit"] = p.LegacyLimit
	}
	for k, v := range p.Settings {
		out[k] = v
	}
	return out
}

// settingSource records where an effective value came from, for `config --explain`.
type settingSource struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Source string `json:"source"`
}

// applySettings writes cfg values onto cmd's flags, skipping any flag the user
// passed explicitly. Returns what it applied, for --explain.
//
// A key that matches no flag on this command is an error rather than a silent
// no-op: a typo'd setting that is quietly ignored is worse than one that fails,
// because the user believes it took effect.
func applySettings(cmd *cobra.Command, values map[string]any, source string) ([]settingSource, error) {
	applied := make([]settingSource, 0, len(values))
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		flag := cmd.Flags().Lookup(key)
		if flag == nil {
			// A setting that names a flag belonging to a *different* command is
			// not an error: one config file serves the whole CLI, so `mcp` and
			// `config` legitimately see keys like "concurrency" that mean
			// nothing to them. A key that matches no flag anywhere is a typo,
			// and silently ignoring it is worse than failing, because the user
			// believes it took effect.
			if knownFlagNames()[key] {
				continue
			}
			return nil, fmt.Errorf("%s: unknown setting %q\n\nSettings are named after flags. "+
				"Run `corralctl --help` to see the available names",
				source, key)
		}
		if flag.Changed {
			continue // an explicit flag always wins
		}
		str, err := settingToString(values[key])
		if err != nil {
			return nil, fmt.Errorf("%s: setting %q: %w", source, key, err)
		}
		if err := flag.Value.Set(str); err != nil {
			return nil, fmt.Errorf("%s: setting %q = %v: %w", source, key, values[key], err)
		}
		applied = append(applied, settingSource{Key: key, Value: str, Source: source})
	}
	return applied, nil
}

// settingToString renders a JSON scalar as the string pflag expects. Arrays are
// joined with commas so the CSV-style flags (--languages) accept a JSON list,
// which is the shape a user naturally reaches for.
func settingToString(v any) (string, error) {
	switch t := v.(type) {
	case string:
		return t, nil
	case bool:
		return strconv.FormatBool(t), nil
	case float64: // encoding/json decodes every number as float64
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10), nil
		}
		return strconv.FormatFloat(t, 'f', -1, 64), nil
	case int: // the deprecated legacy profile fields are typed int
		return strconv.Itoa(t), nil
	case int64:
		return strconv.FormatInt(t, 10), nil
	case []any:
		parts := make([]string, 0, len(t))
		for _, e := range t {
			s, err := settingToString(e)
			if err != nil {
				return "", err
			}
			parts = append(parts, s)
		}
		return strings.Join(parts, ","), nil
	case nil:
		return "", fmt.Errorf("value is null")
	default:
		return "", fmt.Errorf("unsupported value type %T", v)
	}
}

// configuredDefaults loads the config file and applies its defaults block to
// cmd. A missing file is not an error: the tool is usable with no config at all.
func configuredDefaults(cmd *cobra.Command) ([]settingSource, error) {
	cfg, err := loadConfigOptional(configPath)
	if err != nil {
		return nil, err
	}
	if len(cfg.Defaults) == 0 {
		return nil, nil
	}
	return applySettings(cmd, cfg.Defaults, "config defaults")
}

// loadConfigOptional is loadConfig with a missing file treated as empty.
func loadConfigOptional(path string) (configFile, error) {
	explicit := path != ""
	if path == "" {
		path = defaultConfigPath()
	}
	cfg, err := loadConfig(path)
	if err != nil {
		if !explicit && os.IsNotExist(errorCause(err)) {
			return configFile{}, nil
		}
		return configFile{}, err
	}
	return cfg, nil
}

// errorCause unwraps to the innermost error so os.IsNotExist can inspect it.
func errorCause(err error) error {
	for {
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return err
		}
		next := u.Unwrap()
		if next == nil {
			return err
		}
		err = next
	}
}

// defaultConfigTemplate is written by `corralctl config init`. Every setting is
// commented out so the file documents the surface without changing behaviour
// until something is uncommented.
func defaultConfigTemplate(cmd *cobra.Command) string {
	var b strings.Builder
	b.WriteString("{\n")
	b.WriteString("  \"//\": [\n")
	b.WriteString("    \"corral configuration. Settings are named after flags, so\",\n")
	b.WriteString("    \"\\\"concurrency\\\": 8 is the same as passing --concurrency 8.\",\n")
	b.WriteString("    \"Precedence: explicit flag > profile > defaults > built-in default.\",\n")
	b.WriteString("    \"Run `corralctl config --explain` to see where each value came from.\"\n")
	b.WriteString("  ],\n")
	b.WriteString("  \"defaults\": {\n")

	var lines []string
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Name == "help" || f.Name == "config" {
			return
		}
		lines = append(lines, fmt.Sprintf("    \"//%s\": %q", f.Name, f.Usage))
	})
	sort.Strings(lines)
	b.WriteString(strings.Join(lines, ",\n"))
	b.WriteString("\n  },\n")
	b.WriteString("  \"profiles\": {\n")
	b.WriteString("    \"example\": {\n")
	b.WriteString("      \"owners\": [\"your-github-username\"],\n")
	b.WriteString("      \"settings\": {\n")
	b.WriteString("        \"protocol\": \"ssh\"\n")
	b.WriteString("      }\n")
	b.WriteString("    }\n")
	b.WriteString("  }\n}\n")
	return b.String()
}

// writeConfigFile is a seam so the write-failure path is testable without
// depending on permission bits, which do not behave the same on every
// platform. Same reasoning as readConfigFile.
var writeConfigFile = os.WriteFile

// writeConfigTemplate creates the config file, refusing to overwrite an
// existing one.
func writeConfigTemplate(path, body string) error {
	if path == "" {
		path = defaultConfigPath()
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("refusing to overwrite the existing config at %s", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if err := writeConfigFile(path, []byte(body), 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	_, _ = fmt.Fprintf(os.Stdout, "Wrote %s\n", path)
	return nil
}

// knownFlagNames is every flag name the CLI defines, across the root command
// and all subcommands. Used to tell a setting meant for another command apart
// from a typo.
func knownFlagNames() map[string]bool {
	knownFlagsOnce.Do(func() {
		knownFlagsSet = map[string]bool{}
		var walk func(c *cobra.Command)
		walk = func(c *cobra.Command) {
			c.Flags().VisitAll(func(f *pflag.Flag) { knownFlagsSet[f.Name] = true })
			c.PersistentFlags().VisitAll(func(f *pflag.Flag) { knownFlagsSet[f.Name] = true })
			for _, sub := range c.Commands() {
				walk(sub)
			}
		}
		walk(rootCmd)
	})
	return knownFlagsSet
}

var (
	knownFlagsOnce sync.Once
	knownFlagsSet  map[string]bool
)

// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/cobra"
)

var (
	execConcurrency  int
	execLanguages    string
	execExcludeLangs string
	execVisibility   string
)

var execCmd = &cobra.Command{
	Use:   "exec <command>",
	Short: "Execute a command across all organised local repositories",
	Long:  `Runs a shell command concurrently across all repositories found under the base directory.`,
	Args:  cobra.MinimumNArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if execConcurrency < 1 {
			return fmt.Errorf("--concurrency must be >= 1")
		}
		execVisibility = strings.ToLower(strings.TrimSpace(execVisibility))
		if execVisibility != "all" && execVisibility != "public" && execVisibility != "private" {
			return fmt.Errorf("--visibility must be one of: all, public, private")
		}
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		commandStr := args[0]
		bDir := baseDir
		if bDir == "" {
			bDir = defaultBaseDir()
		}

		// Find all Git repos under bDir
		repos, err := findLocalRepos(bDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: failed to scan directory: %v\n", err)
			osExit(1)
			return
		}

		// Filter repos
		filtered := filterLocalRepos(repos)
		if len(filtered) == 0 {
			fmt.Println("No matching repositories found.")
			return
		}

		if dryRun {
			fmt.Printf("Dry-run: Would execute %q in:\n", commandStr)
			for _, r := range filtered {
				fmt.Printf("  - %s\n", r)
			}
			return
		}

		// Run commands concurrently
		runExecCommands(cmdContext(cmd), filtered, commandStr, execConcurrency)
	},
}

type localRepoInfo struct {
	Path       string
	Visibility string // "public", "private"
	Language   string
}

// findLocalRepos walks baseDir looking for directories containing a .git folder
func findLocalRepos(baseDir string) ([]localRepoInfo, error) {
	var repos []localRepoInfo
	err := filepath.WalkDir(baseDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && d.Name() == ".git" {
			repoDir := filepath.Dir(path)
			rel, err := filepath.Rel(baseDir, repoDir)
			if err == nil {
				parts := strings.Split(filepath.ToSlash(rel), "/")
				var vis, lang string
				if len(parts) >= 3 {
					vis = strings.ToLower(parts[0])
					lang = strings.ToLower(parts[1])
				} else if len(parts) == 2 {
					lang = strings.ToLower(parts[0])
				}
				repos = append(repos, localRepoInfo{
					Path:       repoDir,
					Visibility: vis,
					Language:   lang,
				})
			}
			return filepath.SkipDir
		}
		return nil
	})
	return repos, err
}

func filterLocalRepos(repos []localRepoInfo) []string {
	var out []string
	includeLang := toLookupSet(parseCSV(execLanguages))
	excludeLang := toLookupSet(parseCSV(execExcludeLangs))

	for _, r := range repos {
		if execVisibility == "public" && r.Visibility != "public" {
			continue
		}
		if execVisibility == "private" && r.Visibility != "private" {
			continue
		}
		if len(includeLang) > 0 {
			if _, ok := includeLang[r.Language]; !ok {
				continue
			}
		}
		if len(excludeLang) > 0 {
			if _, ok := excludeLang[r.Language]; ok {
				continue
			}
		}
		out = append(out, r.Path)
	}
	return out
}

func toLookupSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(values))
	for _, v := range values {
		out[strings.ToLower(strings.TrimSpace(v))] = struct{}{}
	}
	return out
}

func runExecCommands(ctx context.Context, repoPaths []string, commandStr string, concurrency int) {
	jobs := make(chan string, len(repoPaths))
	for _, p := range repoPaths {
		jobs <- p
	}
	close(jobs)

	var wg sync.WaitGroup
	var outMu sync.Mutex

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case path, ok := <-jobs:
					if !ok {
						return
					}
					repoName := filepath.Base(path)

					shell := "sh"
					shellFlag := "-c"
					if os.Getenv("SHELL") != "" {
						shell = os.Getenv("SHELL")
					} else if os.Getenv("COMSPEC") != "" {
						shell = os.Getenv("COMSPEC")
						shellFlag = "/c"
					}

					cmd := exec.CommandContext(ctx, shell, shellFlag, commandStr)
					cmd.Dir = path
					cmd.Env = os.Environ()

					out, err := cmd.CombinedOutput()

					outMu.Lock()
					fmt.Printf("\n--- [%s] ---\n", repoName)
					if len(out) > 0 {
						fmt.Print(string(out))
					}
					if err != nil {
						fmt.Printf("Command failed: %v\n", err)
					}
					outMu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
}

func init() {
	execCmd.Flags().IntVarP(&execConcurrency, "concurrency", "c", 4, "number of concurrent operations")
	execCmd.Flags().StringVar(&execLanguages, "languages", "", "comma-separated language filter")
	execCmd.Flags().StringVar(&execExcludeLangs, "exclude-languages", "", "comma-separated language exclude filter")
	execCmd.Flags().StringVar(&execVisibility, "visibility", "all", "filter repositories by visibility: all, public, private")
	rootCmd.AddCommand(execCmd)
}

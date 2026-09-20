package cli

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/environment"
	"devbox/internal/fsutil"
	"devbox/internal/harness"
	"github.com/spf13/cobra"
)

type completionSource func(*cobra.Command) []string

// Completion deliberately bypasses Store.Open and record readers: even reading
// through the normal store can create lock files. Suggestions are lookup hints,
// not validated authority to operate on the named resource.
func completionHome(cmd *cobra.Command) string {
	userHome, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	explicit, _ := cmd.Flags().GetString("home")
	home, err := config.Home(explicit, os.Getenv("DEVBOX_HOME"), userHome)
	if err != nil {
		return ""
	}
	return home
}

func completionDirectories(cmd *cobra.Command, directory string) []string {
	root, err := fsutil.Path(completionHome(cmd), directory)
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if directory == "profiles" {
			if config.Name.MatchString(name) {
				names = append(names, name)
			}
		} else if strings.HasPrefix(name, environment.ContainerPrefix) {
			p, err := fsutil.Path(root, filepath.Join(name, "session.json"))
			if err == nil {
				if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() {
					names = append(names, name)
				}
			}
		}
	}
	return names
}

func completeProfiles(cmd *cobra.Command) []string {
	return completionDirectories(cmd, "profiles")
}

func completeSessions(cmd *cobra.Command) []string {
	return completionDirectories(cmd, "sessions")
}

func completeHarnesses(cmd *cobra.Command) []string {
	home := completionHome(cmd)
	if home == "" {
		return nil
	}
	registry, err := harness.Enumerate(home)
	if err != nil {
		return nil
	}
	var names []string
	for _, h := range registry.Valid {
		names = append(names, h.Definition.Name)
	}
	return names
}

func completionContainers(runtime docker.Runtime) completionSource {
	return func(cmd *cobra.Command) []string {
		p, err := fsutil.Path(completionHome(cmd), "state/installation-id")
		if err != nil {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		installation := strings.TrimSpace(string(b))
		id, err := hex.DecodeString(installation)
		if err != nil || len(id) != 16 {
			return nil
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), time.Second)
		defer cancel()
		containers, err := runtime.Inventory(ctx, installation)
		if err != nil {
			return nil
		}
		var names []string
		for _, c := range containers {
			names = append(names, strings.TrimPrefix(c.Name, "/"))
		}
		return names
	}
}

func completionMatches(values, used []string, prefix string) []string {
	var matches []string
	for _, value := range values {
		if strings.HasPrefix(value, prefix) && !slices.Contains(used, value) && !strings.ContainsFunc(value, unicode.IsControl) {
			matches = append(matches, value)
		}
	}
	sort.Strings(matches)
	return slices.Compact(matches)
}

func completeFlag(source completionSource) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, _ []string, prefix string) ([]string, cobra.ShellCompDirective) {
		return completionMatches(source(cmd), nil, prefix), cobra.ShellCompDirectiveNoFileComp
	}
}

func completeTarget(source completionSource, index int, multiple bool) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
		for _, flag := range []string{"all", "stopped", "orphaned"} {
			if selected, _ := cmd.Flags().GetBool(flag); selected {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
		}
		if flag := cmd.Flags().Lookup("older-than"); flag != nil && flag.Changed {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		if len(args) < index || (!multiple && len(args) > index) {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return completionMatches(source(cmd), args, prefix), cobra.ShellCompDirectiveDefault
	}
}

func bindCompletions(root *cobra.Command, runtime docker.Runtime) {
	containers := completionContainers(runtime)
	slots := func(cmd *cobra.Command) []string {
		values := []string{".project"}
		for _, profile := range completeProfiles(cmd) {
			values = append(values, ".profile-"+profile, ".profile-"+profile+".project")
		}
		return values
	}
	_ = root.RegisterFlagCompletionFunc("profile", completeFlag(completeProfiles))
	_ = root.MarkPersistentFlagDirname("home")
	var visit func(*cobra.Command)
	visit = func(cmd *cobra.Command) {
		path := strings.TrimPrefix(cmd.CommandPath(), root.Name()+" ")
		switch path {
		case "open", "start", "recreate", "status", "ssh", "shell", "exec", "stop", "logs":
			cmd.ValidArgsFunction = completeTarget(completeSessions, 0, false)
		case "delete":
			cmd.ValidArgsFunction = completeTarget(func(cmd *cobra.Command) []string {
				return append(completeSessions(cmd), containers(cmd)...)
			}, 0, true)
		case "copy":
			cmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
				if len(args) == 1 {
					return nil, cobra.ShellCompDirectiveFilterDirs
				}
				return completeTarget(completeSessions, 0, false)(cmd, args, prefix)
			}
		case "network inspect", "network env":
			cmd.ValidArgsFunction = completeTarget(completeSessions, 0, false)
		case "network connect", "network disconnect":
			cmd.ValidArgsFunction = completeTarget(completeSessions, 1, false)
		case "profile init", "profile config", "profile set", "profile delete":
			cmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
				if len(args) != 0 {
					return nil, cobra.ShellCompDirectiveNoFileComp
				}
				return completeFlag(completeProfiles)(cmd, args, prefix)
			}
		case "profile create":
			cmd.ValidArgsFunction = cobra.NoFileCompletions
		case "create", "project create", "project init", "project config":
			cmd.ValidArgsFunction = func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
				if len(args) == 0 {
					return nil, cobra.ShellCompDirectiveFilterDirs
				}
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
		}
		for flag, source := range map[string]completionSource{
			"profile": completeProfiles, "from-profile": completeProfiles,
			"from": slots, "to": slots, "harness": completeHarnesses,
			"sort": func(*cobra.Command) []string { return []string{"name", "last-active"} },
		} {
			if cmd == root || cmd.Flags().Lookup(flag) == nil {
				continue
			}
			if path == "project init" && flag == "harness" {
				source = func(cmd *cobra.Command) []string { return append(completeHarnesses(cmd), "inherit") }
			}
			_ = cmd.RegisterFlagCompletionFunc(flag, completeFlag(source))
		}
		for _, child := range cmd.Commands() {
			visit(child)
		}
	}
	visit(root)
}

package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"devbox/internal/harness"
	"devbox/internal/resource"
	"github.com/spf13/cobra"
)

type resourceFactory func(*cobra.Command) (*resource.Service, error)

func resourceCommands(factory resourceFactory) []*cobra.Command {
	groups := []*cobra.Command{}
	for _, kind := range []string{"profile", "project"} {
		kind := kind
		group := &cobra.Command{Use: kind, Short: "Manage " + kind + " configuration"}
		owner := func(s *resource.Service, name string) (resource.Owner, error) {
			if kind == "profile" {
				return s.Profile(name)
			}
			return s.Project(name)
		}
		argument := "<name>"
		if kind == "project" {
			argument = "<folder>"
		}
		var from string
		var createJSON bool
		create := &cobra.Command{Use: "create " + argument, Short: "Create sparse configuration without choosing a harness", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			s, err := factory(cmd)
			if err != nil {
				return err
			}
			o, err := owner(s, args[0])
			if err != nil {
				return err
			}
			result, err := s.Create(cmd.Context(), o, from)
			return renderResource(cmd, result, err, createJSON, s.Home)
		}}
		if kind == "project" {
			create.Flags().StringVar(&from, "from-profile", "", "Copy supported profile artifacts once, without profile inheritance")
		}
		create.Flags().BoolVar(&createJSON, "json", false, "Print the result and next steps as JSON")
		group.AddCommand(create)
		var selected string
		var artifacts []string
		var initJSON bool
		init := &cobra.Command{Use: "init " + argument, Short: "Select a harness and optionally seed missing artifacts", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			s, err := factory(cmd)
			if err != nil {
				return err
			}
			o, err := owner(s, args[0])
			if err != nil {
				return err
			}
			options := resource.InitOptions{Harness: selected, Artifacts: artifacts}
			reader := promptReader(cmd)
			if interactive(cmd) && !initJSON {
				options.ChooseHarness = func(choices []string, issues []harness.Issue) (string, error) {
					for _, issue := range issues {
						fmt.Fprintf(cmd.ErrOrStderr(), "Unavailable harness %s: %v\n", issue.Name, issue.Err)
					}
					return chooseOne(reader, cmd.OutOrStdout(), "Select a harness", choices)
				}
				if !cmd.Flags().Changed("harness") && !cmd.Flags().Changed("artifact") {
					options.ChooseArtifacts = func(choices []string) ([]string, error) {
						return chooseMany(reader, cmd.OutOrStdout(), "Optional artifacts (Enter for none)", choices)
					}
				}
			}
			result, err := s.Init(cmd.Context(), o, options)
			return renderResource(cmd, result, err, initJSON, s.Home)
		}}
		init.Flags().StringVar(&selected, "harness", "", "Select a registry harness (projects also accept inherit)")
		init.Flags().StringSliceVar(&artifacts, "artifact", nil, "Seed missing harness-config, setup.sh, entrypoint.sh, or Dockerfile (repeatable)")
		init.Flags().BoolVar(&initJSON, "json", false, "Print the result and next steps as JSON; never prompt")
		group.AddCommand(init, configCommand(factory, kind))
		if kind == "profile" {
			group.AddCommand(profileList(factory), profileSet(factory), profileDelete(factory))
		}
		groups = append(groups, group)
	}
	global := &cobra.Command{Use: "global", Short: "Manage machine-local configuration"}
	global.AddCommand(configCommand(factory, "global"))
	groups = append(groups, global)
	return groups
}
func interactive(cmd *cobra.Command) bool {
	f, ok := cmd.InOrStdin().(*os.File)
	return ok && terminal(f)
}
func shellQuote(value string) string {
	if value != "" && !strings.ContainsAny(value, " \t\r\n'\"\\$`;&|<>()*?[]{}!") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
func scopedSteps(cmd *cobra.Command, steps []resource.Step, home string) []resource.Step {
	// Preserve an explicit installation selection, not the result of normal
	// default/environment resolution. Those resolve naturally on the next command.
	flag := cmd.Flag("home")
	explicit := flag != nil && flag.Changed
	result := make([]resource.Step, 0, len(steps))
	for _, step := range steps {
		args := append([]string(nil), step.Command...)
		if explicit {
			args = append([]string{step.Command[0], "--home", home}, step.Command[1:]...)
		}
		result = append(result, resource.Step{Command: args, Reason: step.Reason})
	}
	return result
}
func stepsText(steps []resource.Step) string {
	var out strings.Builder
	for _, step := range steps {
		args := make([]string, len(step.Command))
		for i, arg := range step.Command {
			args[i] = shellQuote(arg)
		}
		fmt.Fprintf(&out, "  %s\n", strings.Join(args, " "))
	}
	return out.String()
}
func resourceError(cmd *cobra.Command, err error, home string) error {
	var actionable *resource.Error
	if errors.As(err, &actionable) && len(actionable.Next) > 0 {
		return fmt.Errorf("%w\n\nNext:\n%s", err, stepsText(scopedSteps(cmd, actionable.Next, home)))
	}
	return err
}
func renderResource(cmd *cobra.Command, result resource.Result, err error, asJSON bool, home string) error {
	if err != nil {
		return resourceError(cmd, err, home)
	}
	result.Next = scopedSteps(cmd, result.Next, home)
	if asJSON {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Configured %s\n", result.Path)
	if result.Harness != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Harness: %s\n", result.Harness)
	}
	for _, path := range result.Created {
		fmt.Fprintf(cmd.OutOrStdout(), "Created %s\n", path)
	}
	for _, path := range result.Skipped {
		fmt.Fprintf(cmd.OutOrStdout(), "Kept existing %s\n", path)
	}
	if len(result.Next) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "\nNext:\n%s", stepsText(result.Next))
	}
	return nil
}
func profileList(factory resourceFactory) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{Use: "list", Short: "List profiles, including invalid configurations", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := factory(cmd)
		if err != nil {
			return err
		}
		profiles, err := s.Profiles()
		if err != nil {
			return err
		}
		if asJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(profiles)
		}
		if len(profiles) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No profiles.\n\nNext:")
			fmt.Fprint(cmd.OutOrStdout(), stepsText(scopedSteps(cmd, []resource.Step{{Command: []string{"devbox-neo", "profile", "create", "default"}}}, s.Home)))
			return nil
		}
		for _, p := range profiles {
			marker := " "
			if p.Default {
				marker = "*"
			}
			state := p.Harness
			if state == "" {
				state = "no harness selected"
			}
			if p.Error != "" {
				state = "invalid: " + p.Error
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s (%s)\n", marker, p.Name, state)
		}
		return nil
	}}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print profile entries as JSON")
	return cmd
}
func profileSet(factory resourceFactory) *cobra.Command {
	var clear bool
	cmd := &cobra.Command{Use: "set [name]", Short: "Select or clear the default profile", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, err := factory(cmd)
		if err != nil {
			return err
		}
		if clear && len(args) > 0 {
			return fmt.Errorf("--clear does not accept a profile name")
		}
		name := ""
		if len(args) > 0 {
			name = args[0]
		} else if !clear {
			if !interactive(cmd) {
				return fmt.Errorf("provide a profile name or --clear")
			}
			profiles, err := s.Profiles()
			if err != nil {
				return err
			}
			choices := []string{"(no default)"}
			for _, p := range profiles {
				if p.Error == "" {
					choices = append(choices, p.Name)
				}
			}
			name, err = chooseOne(promptReader(cmd), cmd.OutOrStdout(), "Default profile", choices)
			if err != nil {
				return err
			}
			if name == choices[0] {
				name = ""
			}
		}
		if err = s.SetDefault(cmd.Context(), name); err != nil {
			return resourceError(cmd, err, s.Home)
		}
		if name == "" {
			cmd.Println("Default profile cleared.")
		} else {
			cmd.Printf("Default profile: %s\n", name)
		}
		return nil
	}}
	cmd.Flags().BoolVar(&clear, "clear", false, "Clear the default profile without prompting")
	return cmd
}
func chooseOne(reader *bufio.Reader, out io.Writer, title string, choices []string) (string, error) {
	if len(choices) == 0 {
		return "", fmt.Errorf("no valid choices are available")
	}
	fmt.Fprintln(out, title)
	for i, choice := range choices {
		fmt.Fprintf(out, "  %d. %s\n", i+1, choice)
	}
	fmt.Fprint(out, "> ")
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || n < 1 || n > len(choices) {
		return "", fmt.Errorf("select a number from 1 to %d", len(choices))
	}
	return choices[n-1], nil
}
func chooseMany(reader *bufio.Reader, out io.Writer, title string, choices []string) ([]string, error) {
	fmt.Fprintln(out, title)
	for i, choice := range choices {
		fmt.Fprintf(out, "  %d. %s\n", i+1, choice)
	}
	fmt.Fprint(out, "> ")
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return nil, err
	}
	if strings.TrimSpace(line) == "" {
		return nil, nil
	}
	selected := []string{}
	seen := map[int]bool{}
	for _, part := range strings.Split(strings.TrimSpace(line), ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || n < 1 || n > len(choices) {
			return nil, fmt.Errorf("invalid artifact selection")
		}
		if !seen[n] {
			selected = append(selected, choices[n-1])
			seen[n] = true
		}
	}
	return selected, nil
}

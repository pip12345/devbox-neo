package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func bindCompletionScripts(root *cobra.Command) {
	root.InitDefaultCompletionCmd()
	for _, command := range root.Commands() {
		if command.Name() != "completion" {
			continue
		}
		for _, shell := range command.Commands() {
			shell.RunE = generateCompletionScript
		}
	}
}

func generateCompletionScript(cmd *cobra.Command, _ []string) error {
	root := cmd.Root()
	noDescriptions, _ := cmd.Flags().GetBool("no-descriptions")
	includeDescriptions := !noDescriptions && !root.CompletionOptions.DisableDescriptions
	return writeCompletionScript(root, cmd.OutOrStdout(), cmd.Name(), includeDescriptions)
}

func writeCompletionScript(root *cobra.Command, out io.Writer, shell string, includeDescriptions bool) error {
	switch shell {
	case "bash":
		return root.GenBashCompletionV2(out, includeDescriptions)
	case "zsh":
		if includeDescriptions {
			return root.GenZshCompletion(out)
		}
		return root.GenZshCompletionNoDesc(out)
	case "fish":
		return root.GenFishCompletion(out, includeDescriptions)
	case "powershell":
		if includeDescriptions {
			return root.GenPowerShellCompletionWithDesc(out)
		}
		return root.GenPowerShellCompletion(out)
	default:
		return fmt.Errorf("unsupported completion shell %q", shell)
	}
}

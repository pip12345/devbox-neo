package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// Cobra's dynamic handlers invoke the command the user typed, so sharing them
// preserves a dbx shortcut's executable and flags. Only shell registrations are
// added: dbx is not a Cobra command alias and its shell definition is untouched.
func bindCompletionScripts(root *cobra.Command) {
	root.InitDefaultCompletionCmd()
	for _, command := range root.Commands() {
		if command.Name() != "completion" {
			continue
		}
		for _, shell := range command.Commands() {
			shell.Long += "\nThe loaded script also registers completion for an existing dbx shortcut; it does not define that shortcut.\n"
			shell.RunE = generateCompletionScript
		}
	}
}

func generateCompletionScript(cmd *cobra.Command, _ []string) error {
	root := cmd.Root()
	out := cmd.OutOrStdout()
	noDescriptions, _ := cmd.Flags().GetBool("no-descriptions")
	includeDescriptions := !noDescriptions && !root.CompletionOptions.DisableDescriptions
	var err error
	var registration string
	switch cmd.Name() {
	case "bash":
		err = root.GenBashCompletionV2(out, includeDescriptions)
		registration = "\ncomplete -o default -F __start_devbox-neo dbx\n"
	case "zsh":
		// compinit reads the first line without sourcing the file. Include dbx
		// there too so it can trigger autoload before devbox-neo has been used.
		if _, err := io.WriteString(out, "#compdef devbox-neo dbx\ncompdef _devbox-neo dbx\n"); err != nil {
			return err
		}
		if includeDescriptions {
			err = root.GenZshCompletion(out)
		} else {
			err = root.GenZshCompletionNoDesc(out)
		}
	case "fish":
		err = root.GenFishCompletion(out, includeDescriptions)
		// Share handlers without redirecting requests to the canonical executable;
		// the shortcut itself supplies any implicit flags.
		registration = `
complete -c dbx -e
complete -c dbx -n '__devbox_neo_clear_perform_completion_once_result'
complete -c dbx -n 'not __devbox_neo_requires_order_preservation && __devbox_neo_prepare_completions' -f -a '$__devbox_neo_comp_results'
complete -k -c dbx -n '__devbox_neo_requires_order_preservation && __devbox_neo_prepare_completions' -f -a '$__devbox_neo_comp_results'
`
	case "powershell":
		if includeDescriptions {
			err = root.GenPowerShellCompletionWithDesc(out)
		} else {
			err = root.GenPowerShellCompletion(out)
		}
		registration = "\nRegister-ArgumentCompleter -CommandName 'dbx' -ScriptBlock ${__devbox_neoCompleterBlock}\n"
	default:
		return fmt.Errorf("unsupported completion shell %q", cmd.Name())
	}
	if err != nil {
		return err
	}
	_, err = io.WriteString(out, registration)
	return err
}

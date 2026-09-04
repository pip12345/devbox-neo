package cli

import (
	"github.com/spf13/cobra"
)

var Version = "dev"

func New() *cobra.Command {
	root := &cobra.Command{Use: "devbox-rewrite", Short: "Persistent development environments (scratch rewrite)", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(&cobra.Command{Use: "version", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { cmd.Println(Version); return nil }})
	return root
}

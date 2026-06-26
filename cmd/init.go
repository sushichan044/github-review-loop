package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newInitCmd(d deps) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create a default review-loop config in the current repository",
		Long: `Create .github/review-loop.yml at the current repository root.

The file is a commented template. Edit it to list the reviewers you want to
loop, then commit it so collaborators and CI use the same policy.

It refuses to overwrite an existing config.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runInit(d)
		},
	}
}

func runInit(d deps) error {
	path, err := d.initConfig()
	if err != nil {
		return fmt.Errorf("could not initialize config: %w", err)
	}

	_, err = fmt.Fprintf(d.out, "Created %s\n", path)
	return err
}

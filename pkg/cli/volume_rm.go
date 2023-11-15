package cli

import (
	"fmt"

	cli "github.com/acorn-io/runtime/pkg/cli/builder"
	"github.com/spf13/cobra"
)

func NewVolumeDelete(c CommandContext) *cobra.Command {
	cmd := cli.Command(&VolumeDelete{client: c.ClientFactory}, cobra.Command{
		Use:               "rm [VOLUME_NAME...]",
		Example:           `acorn volume rm my-volume`,
		SilenceUsage:      true,
		Short:             "Delete a volume",
		ValidArgsFunction: newCompletion(c.ClientFactory, volumesCompletion).complete,
	})
	return cmd
}

type VolumeDelete struct {
	client ClientFactory
}

func (a *VolumeDelete) Run(cmd *cobra.Command, args []string) error {
	c, err := a.client.CreateDefault()
	if err != nil {
		return err
	}

	for _, volume := range args {
		deleted, err := c.VolumesDelete(cmd.Context(), volume)
		if err != nil {
			return fmt.Errorf("deleting %s: %w", volume, err)
		}
		if len(deleted) > 0 {
			for _, v := range deleted {
				fmt.Println(v.Name)
			}
		} else {
			fmt.Printf("Error: No such volume: %s\n", volume)
		}
	}

	return nil
}

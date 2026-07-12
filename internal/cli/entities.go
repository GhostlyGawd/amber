package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func cmdEntities() *cobra.Command {
	var etype string
	c := &cobra.Command{
		Use:   "entities",
		Short: "Known entities with memory counts",
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := openEnv()
			if err != nil {
				return err
			}
			defer e.Close()
			if etype != "" && etype != "person" && etype != "project" && etype != "org" && etype != "other" {
				return fmt.Errorf("type must be person|project|org|other")
			}
			ents, err := e.Store.ListEntities(etype)
			if err != nil {
				return err
			}
			if flagFormat == "json" {
				return jsonOut(ents)
			}
			if len(ents) == 0 {
				fmt.Println("no entities yet")
				return nil
			}
			for _, en := range ents {
				fmt.Printf("%4d  %-8s %s\n", en.Count, en.Type, en.Name)
			}
			return nil
		},
	}
	c.Flags().StringVar(&etype, "type", "", "filter: person|project|org|other")
	return c
}

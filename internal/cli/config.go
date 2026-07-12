package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ghostlygawd/amber/internal/config"
)

func cmdConfig() *cobra.Command {
	c := &cobra.Command{
		Use:   "config [get <key> | set <key> <value>]",
		Short: "Show or change configuration",
		Long: `Without arguments, prints the effective configuration (defaults +
config file + AMBER_* environment overrides). Keys use dotted paths,
e.g. ` + "`amber config set digest.posture auto`" + `.

The config file never contains secrets: API keys are referenced by
environment-variable name only.`,
		Args: cobra.MaximumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			dir, _, err := config.ResolveStoreDir(config.Scope(flagScope), cwd)
			if err != nil {
				return err
			}
			cfg, err := config.Load(dir)
			if err != nil {
				return err
			}
			switch {
			case len(args) == 0:
				if flagFormat == "json" {
					return jsonOut(cfg)
				}
				fmt.Printf("# %s/config.json (effective; env overrides applied)\n", dir)
				for _, k := range config.Keys() {
					v, _ := cfg.Get(k)
					fmt.Printf("%-28s %s\n", k, v)
				}
				return nil
			case args[0] == "get" && len(args) == 2:
				v, err := cfg.Get(args[1])
				if err != nil {
					return err
				}
				fmt.Println(v)
				return nil
			case args[0] == "set" && len(args) == 3:
				if err := cfg.Set(args[1], args[2]); err != nil {
					return err
				}
				if err := config.Save(dir, cfg); err != nil {
					return err
				}
				fmt.Printf("%s = %s\n", args[1], args[2])
				return nil
			default:
				return fmt.Errorf("usage: amber config | amber config get <key> | amber config set <key> <value>")
			}
		},
	}
	return c
}

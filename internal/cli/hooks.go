package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ghostlygawd/amber/internal/hooks"
)

func cmdHooks() *cobra.Command {
	root := &cobra.Command{
		Use:   "hooks",
		Short: "Claude Code integration",
	}
	var (
		project    bool
		withSkill  bool
		digestMem  bool
		slimNative bool
	)
	install := &cobra.Command{
		Use:   "install",
		Short: "Wire SessionStart recall + SessionEnd digest into Claude Code",
		Long: `Adds two hooks to Claude Code settings (previewed, backed up,
idempotent):

  SessionStart  amber hook session-start   inject a budgeted briefing
  SessionEnd    amber hook session-end     digest the session transcript

Also installs the amber-memory skill (--skill, default on), offers a
one-time digest of existing MEMORY.md/CLAUDE.md (--digest-memory-files),
and can slim native auto-memory files to prevent double context
(--slim-native; originals are backed up).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			plan, err := hooks.BuildPlan(project)
			if err != nil {
				return err
			}
			if plan.AlreadyWired {
				fmt.Printf("hooks already installed in %s\n", plan.SettingsPath)
			} else {
				fmt.Printf("will edit %s:\n", plan.SettingsPath)
				fmt.Println("--- before ---")
				fmt.Println(plan.Before)
				fmt.Println("--- after ---")
				fmt.Println(plan.After)
				if !confirm("apply?") {
					return fmt.Errorf("aborted (use --yes to apply without prompting)")
				}
				if err := hooks.Apply(plan); err != nil {
					return err
				}
				fmt.Printf("hooks installed (backup written alongside)\n")
			}
			if withSkill {
				if err := hooks.InstallSkill(plan.SkillPath); err != nil {
					return err
				}
				fmt.Printf("skill installed: %s\n", plan.SkillPath)
			}
			if digestMem {
				if err := digestMemoryFiles(slimNative); err != nil {
					return err
				}
			} else {
				cwd, _ := os.Getwd()
				if files := hooks.FindMemoryFiles(cwd); len(files) > 0 {
					fmt.Printf("\nfound %d existing memory/instruction file%s:\n", len(files), plural(len(files), "", "s"))
					for _, f := range files {
						fmt.Println("  " + f)
					}
					fmt.Println("one-time migration: amber hooks install --digest-memory-files")
				}
			}
			return nil
		},
	}
	install.Flags().BoolVar(&project, "project", false, "install into ./.claude/settings.json instead of ~/.claude/settings.json")
	install.Flags().BoolVar(&withSkill, "skill", true, "install the amber-memory skill")
	install.Flags().BoolVar(&digestMem, "digest-memory-files", false, "digest existing MEMORY.md/CLAUDE.md into the store now")
	install.Flags().BoolVar(&slimNative, "slim-native", false, "after digesting, replace MEMORY.md files with a pointer stub (originals backed up)")
	root.AddCommand(install)
	return root
}

func digestMemoryFiles(slim bool) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	files := hooks.FindMemoryFiles(cwd)
	if len(files) == 0 {
		fmt.Println("no memory/instruction files found")
		return nil
	}
	for _, f := range files {
		fmt.Printf("\n― digesting %s\n", f)
		c := cmdDigest()
		if err := c.RunE(c, []string{f}); err != nil {
			fmt.Fprintf(os.Stderr, "  skipped: %v\n", err)
			continue
		}
		if slim && isMemoryMD(f) {
			backup, err := hooks.SlimNative(f)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  slim failed: %v\n", err)
				continue
			}
			fmt.Printf("  slimmed %s (original: %s)\n", f, backup)
		}
	}
	return nil
}

func isMemoryMD(path string) bool {
	base := len(path) >= 9 && path[len(path)-9:] == "MEMORY.md"
	return base
}

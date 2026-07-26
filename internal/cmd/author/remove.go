package author

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/saucelabs/saucectl/internal/authoring"
	"github.com/spf13/cobra"
)

// removeFlags are flags common to both `remove testsuite` and `remove
// testcase`. It's deliberately leaner than sharedFlags -- removal has no use
// for authoring-only settings like --max-steps, --poll-interval, --tags,
// --target-file, or --url.
type removeFlags struct {
	lockfile    string
	configOut   string
	concurrency int
}

func addRemoveFlags(cmd *cobra.Command, f *removeFlags) {
	flags := cmd.Flags()
	flags.StringVar(&f.lockfile, "lockfile", authoring.DefaultLockfileName, "Path to the authoring lockfile that tracks which spec produced which test case.")
	flags.StringVar(&f.configOut, "config-out", "", "If set, rewrite the saucectl run config file (kind: authoring) at this path to reflect the lockfile after removal.")
	flags.IntVar(&f.concurrency, "concurrency", 2, "The sauce::concurrency value to write into the file given by --config-out.")
}

// RemoveCommand returns the `author remove` command, a parent for two
// single-resource deletion subcommands -- the inverse of `author add`:
//
//	saucectl author remove testsuite <name>
//	saucectl author remove testcase <spec.md>
//	saucectl author remove testcase <spec.md> --test-suite <name>
func RemoveCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "remove",
		Short:        "Delete a test suite or test case",
		SilenceUsage: true,
	}

	cmd.AddCommand(removeTestSuiteCommand(), removeTestCaseCommand())

	return cmd
}

func removeTestSuiteCommand() *cobra.Command {
	var force bool
	flags := &removeFlags{}

	cmd := &cobra.Command{
		Use:   "testsuite <name>",
		Short: "Delete a test suite",
		Long: `remove testsuite deletes a test suite by name.

It does not delete the test cases that belong to it -- they're left behind as
standalone test cases (the same state as one created via 'add testcase' with
no --test-suite), rather than being destroyed, since this API's cascade
behavior on suite deletion hasn't been confirmed and silently losing authored
test cases would be worse than leaving a few unassigned ones behind.

If the suite still has test cases in it, this refuses to proceed unless
--force is given, since removing a non-empty suite is more likely to be a
mistake than genuinely intended. Any lockfile entries that pointed at this
suite are updated to show no suite membership.`,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 || args[0] == "" {
				return fmt.Errorf("expected exactly one argument: the test suite name")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			ctx := cmd.Context()

			suites, err := authoringService.ListTestSuites(ctx, authoring.ListTestSuiteOptions{Search: name})
			if err != nil {
				return fmt.Errorf("failed to look up test suite %q: %w", name, err)
			}

			var suite *authoring.TestSuite
			for i := range suites {
				if strings.EqualFold(suites[i].Name, name) {
					suite = &suites[i]
					break
				}
			}
			if suite == nil {
				return fmt.Errorf("no test suite named %q found", name)
			}

			if suite.TestCaseCount > 0 && !force {
				return fmt.Errorf("test suite %q still has %d test case(s); pass --force to delete it anyway (its test cases will not be deleted, just left unassigned)", name, suite.TestCaseCount)
			}

			if err := authoringService.DeleteTestSuite(ctx, suite.ID); err != nil {
				return fmt.Errorf("failed to delete test suite %q: %w", name, err)
			}

			lf, err := authoring.LoadLockfile(flags.lockfile)
			if err != nil {
				return fmt.Errorf("test suite %q was deleted, but failed to read lockfile %s to update stale entries: %w", name, flags.lockfile, err)
			}

			lockfileChanged := false
			for specPath, entry := range lf.Entries {
				if entry.TestSuiteID != suite.ID {
					continue
				}
				entry.TestSuiteID = ""
				entry.TestSuiteName = ""
				entry.SyncedAt = time.Now().UTC()
				lf.Entries[specPath] = entry
				lockfileChanged = true
			}
			if lockfileChanged {
				if err := lf.Save(flags.lockfile); err != nil {
					return fmt.Errorf("test suite %q was deleted, but failed to write lockfile %s: %w", name, flags.lockfile, err)
				}
			}

			if err := writeRunConfigIfSet(cmd, flags.configOut, flags.concurrency, lf); err != nil {
				return err
			}

			fmt.Printf("removed    test suite %q (%s)\n", name, suite.ID)
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Delete the suite even if it still has test cases in it.")
	addRemoveFlags(cmd, flags)

	return cmd
}

func removeTestCaseCommand() *cobra.Command {
	flags := &removeFlags{}
	var testSuite string

	cmd := &cobra.Command{
		Use:   "testcase <spec.md>",
		Short: "Delete a test case, or unassign it from a suite",
		Long: `remove testcase deletes the test case that was authored from a spec file, and
drops its entry from the lockfile entirely.

With --test-suite <name>, it instead only unassigns the test case from that
suite -- the test case itself is kept alive as a standalone test case (the
inverse of 'add testcase <spec.md> --test-suite <name>' run against an
existing standalone test case), and its lockfile entry is kept, just with no
suite recorded.`,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 || args[0] == "" {
				return fmt.Errorf("expected exactly one argument: the spec file to remove")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRemoveTestCase(cmd, filepath.Clean(args[0]), testSuite, flags)
		},
	}

	cmd.Flags().StringVar(&testSuite, "test-suite", "", "If set, only unassign the test case from this suite rather than deleting it outright.")
	addRemoveFlags(cmd, flags)

	return cmd
}

func runRemoveTestCase(cmd *cobra.Command, specPath, testSuite string, flags *removeFlags) error {
	ctx := cmd.Context()

	lf, err := authoring.LoadLockfile(flags.lockfile)
	if err != nil {
		return fmt.Errorf("failed to read lockfile %s: %w", flags.lockfile, err)
	}

	entry, ok := lf.Entries[specPath]
	if !ok {
		return fmt.Errorf("no authored test case found for %s in %s", specPath, flags.lockfile)
	}

	if testSuite != "" {
		suiteID, err := resolveSuiteIDByName(ctx, testSuite)
		if err != nil {
			return err
		}
		if entry.TestSuiteID == "" {
			return fmt.Errorf("%s is not currently assigned to any suite", specPath)
		}
		if entry.TestSuiteID != suiteID {
			return fmt.Errorf("%s is in suite %q, not %q", specPath, entry.TestSuiteName, testSuite)
		}

		if _, err := authoringService.UpdateTestSuite(ctx, suiteID, authoring.UpdateTestSuiteOptions{
			RemoveTestCases: []string{entry.TestCaseID},
		}); err != nil {
			return fmt.Errorf("failed to remove test case %s from suite %q: %w", entry.TestCaseID, testSuite, err)
		}

		entry.TestSuiteID = ""
		entry.TestSuiteName = ""
		entry.SyncedAt = time.Now().UTC()
		lf.Entries[specPath] = entry

		if err := lf.Save(flags.lockfile); err != nil {
			return fmt.Errorf("failed to write lockfile %s: %w", flags.lockfile, err)
		}
		if err := writeRunConfigIfSet(cmd, flags.configOut, flags.concurrency, lf); err != nil {
			return err
		}

		fmt.Printf("unassigned %s -> %s (removed from suite %q)\n", specPath, entry.TestCaseID, testSuite)
		return nil
	}

	// No --test-suite: delete the test case outright.
	if entry.TestSuiteID != "" {
		if _, err := authoringService.UpdateTestSuite(ctx, entry.TestSuiteID, authoring.UpdateTestSuiteOptions{
			RemoveTestCases: []string{entry.TestCaseID},
		}); err != nil {
			return fmt.Errorf("failed to remove test case %s from suite %q before deleting it: %w", entry.TestCaseID, entry.TestSuiteName, err)
		}
	}

	if err := authoringService.DeleteTestCase(ctx, entry.TestCaseID); err != nil {
		return fmt.Errorf("removed test case %s from its suite but failed to delete it: %w", entry.TestCaseID, err)
	}

	delete(lf.Entries, specPath)
	if err := lf.Save(flags.lockfile); err != nil {
		return fmt.Errorf("failed to write lockfile %s: %w", flags.lockfile, err)
	}
	if err := writeRunConfigIfSet(cmd, flags.configOut, flags.concurrency, lf); err != nil {
		return err
	}

	fmt.Printf("removed    %s (%s)\n", specPath, entry.TestCaseID)
	return nil
}

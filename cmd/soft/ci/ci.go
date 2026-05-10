// Package ci provides the `soft ci` command group: admin-side
// management of the CI subsystem (runners, runs, log inspection,
// timeout enforcement, retention rotation).
//
// The command group constructs a ci.Service backed by the SQL store
// and the production adapters (real clock, crypto/rand tokens, HTTP
// dispatcher, YAML workflow source). Authorisation is implicit: the
// CLI is intended for an operator on the host, so every action is
// performed as an admin.
package ci

import (
	"fmt"
	"strconv"

	"charm.land/log/v2"
	"github.com/charmbracelet/soft-serve/cmd"
	"github.com/charmbracelet/soft-serve/pkg/backend"
	"github.com/charmbracelet/soft-serve/pkg/ci"
	"github.com/charmbracelet/soft-serve/pkg/ci/adapters/backendaccess"
	"github.com/charmbracelet/soft-serve/pkg/ci/adapters/cryptotokens"
	"github.com/charmbracelet/soft-serve/pkg/ci/adapters/httpdispatch"
	"github.com/charmbracelet/soft-serve/pkg/ci/adapters/realclock"
	"github.com/charmbracelet/soft-serve/pkg/ci/adapters/sqlstore"
	"github.com/charmbracelet/soft-serve/pkg/ci/adapters/yamlworkflows"
	"github.com/charmbracelet/soft-serve/pkg/config"
	"github.com/charmbracelet/soft-serve/pkg/db"
	"github.com/spf13/cobra"
)

// admin is the implicit caller for all CLI-issued actions.
var adminCaller = ci.UserInfo{Role: "admin"}

// Command is the root `soft ci` cobra command.
var Command = &cobra.Command{
	Use:   "ci",
	Short: "Manage the CI subsystem",
	Long:  "Manage the Soft Serve CI subsystem: register runners, inspect runs and logs, enforce timeouts, rotate expired runs.",
}

func init() {
	Command.AddCommand(
		runnerCommand(),
		runCommand(),
		syncCommand(),
		enforceTimeoutsCommand(),
		rotateCommand(),
	)
}

// --- Service construction -------------------------------------------------

// serviceFromContext builds a ci.Service backed by the production
// adapters. It is used by every leaf RunE so each command has a
// fresh, fully wired service.
func serviceFromContext(c *cobra.Command) (*ci.Service, error) {
	ctx := c.Context()
	cfg := config.FromContext(ctx)
	dbx := db.FromContext(ctx)
	be := backend.FromContext(ctx)
	if dbx == nil || be == nil {
		return nil, fmt.Errorf("ci: backend not initialised")
	}

	store := sqlstore.New(dbx)
	source := yamlworkflows.New(be)
	dispatcher := httpdispatch.New(nil, callbackBaseURL(cfg))
	tokens := cryptotokens.New()
	access := backendaccess.New(be)
	clock := realclock.New()
	logger := log.FromContext(ctx).WithPrefix("ci")

	return ci.NewService(ci.DefaultConfig(), store, source, dispatcher, tokens, access, clock, logger), nil
}

// callbackBaseURL is the base URL the runner uses to report back to
// Soft Serve. It is included in dispatch payloads. Currently best-
// effort: derived from the configured HTTP listen address. Phase 4
// (HTTP RunnerDispatchAdapter wiring) will replace this with the
// canonical public URL from config.
func callbackBaseURL(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	return cfg.HTTP.PublicURL
}

// --- Runner subcommands ---------------------------------------------------

func runnerCommand() *cobra.Command {
	runner := &cobra.Command{
		Use:   "runner",
		Short: "Manage runner registrations",
	}
	runner.AddCommand(
		runnerRegisterCommand(),
		runnerListCommand(),
		runnerRemoveCommand(),
	)
	return runner
}

func runnerListCommand() *cobra.Command {
	return &cobra.Command{
		Use:                "list",
		Short:              "List registered runners",
		Args:               cobra.NoArgs,
		PersistentPreRunE:  cmd.InitBackendContext,
		PersistentPostRunE: cmd.CloseDBContext,
		RunE: func(c *cobra.Command, _ []string) error {
			ctx := c.Context()
			store := sqlstore.New(db.FromContext(ctx))
			registrations, err := store.ListRunnerRegistrations(ctx)
			if err != nil {
				return fmt.Errorf("list runners: %w", err)
			}
			c.Printf("%-20s  %s\n", "NAME", "DISPATCH_URL")
			for _, registration := range registrations {
				c.Printf("%-20s  %s\n", registration.Name, registration.DispatchURL)
			}
			return nil
		},
	}
}

func runnerRegisterCommand() *cobra.Command {
	return &cobra.Command{
		Use:                "register NAME DISPATCH_URL",
		Short:              "Register a runner. Prints the generated secret token once.",
		Args:               cobra.ExactArgs(2),
		PersistentPreRunE:  cmd.InitBackendContext,
		PersistentPostRunE: cmd.CloseDBContext,
		RunE: func(c *cobra.Command, args []string) error {
			service, err := serviceFromContext(c)
			if err != nil {
				return err
			}
			registration, err := service.RegisterRunner(c.Context(), adminCaller, args[0], args[1])
			if err != nil {
				return fmt.Errorf("register runner: %w", err)
			}
			c.Printf("Registered runner %q.\n", registration.Name)
			c.Printf("Dispatch URL: %s\n", registration.DispatchURL)
			c.Printf("Secret token: %s\n", registration.SecretToken)
			c.Println()
			c.Println("Store this token now; it is shown only at registration time.")
			return nil
		},
	}
}

func runnerRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:                "remove NAME",
		Short:              "Remove a runner registration",
		Args:               cobra.ExactArgs(1),
		PersistentPreRunE:  cmd.InitBackendContext,
		PersistentPostRunE: cmd.CloseDBContext,
		RunE: func(c *cobra.Command, args []string) error {
			service, err := serviceFromContext(c)
			if err != nil {
				return err
			}
			if err := service.RemoveRunner(c.Context(), adminCaller, args[0]); err != nil {
				return fmt.Errorf("remove runner: %w", err)
			}
			c.Printf("Removed runner %q.\n", args[0])
			return nil
		},
	}
}

// --- Run subcommands ------------------------------------------------------

func runCommand() *cobra.Command {
	run := &cobra.Command{
		Use:   "run",
		Short: "Inspect and control runs",
	}
	run.AddCommand(
		runListCommand(),
		runGetCommand(),
		runCancelCommand(),
		runLogsCommand(),
	)
	return run
}

func runListCommand() *cobra.Command {
	return &cobra.Command{
		Use:                "list",
		Short:              "List all runs",
		Args:               cobra.NoArgs,
		PersistentPreRunE:  cmd.InitBackendContext,
		PersistentPostRunE: cmd.CloseDBContext,
		RunE: func(c *cobra.Command, _ []string) error {
			ctx := c.Context()
			store := sqlstore.New(db.FromContext(ctx))
			runs, err := store.ListRuns(ctx)
			if err != nil {
				return fmt.Errorf("list runs: %w", err)
			}
			c.Printf("%-6s  %-12s  %-20s  %-20s  %s\n", "ID", "STATUS", "REPO", "WORKFLOW", "RUNS_ON")
			for _, r := range runs {
				c.Printf("%-6d  %-12s  %-20s  %-20s  %s\n", r.ID, r.Status, r.RepoName, r.WorkflowName, r.RunsOn)
			}
			return nil
		},
	}
}

func runGetCommand() *cobra.Command {
	return &cobra.Command{
		Use:                "get ID",
		Short:              "Show a run by ID",
		Args:               cobra.ExactArgs(1),
		PersistentPreRunE:  cmd.InitBackendContext,
		PersistentPostRunE: cmd.CloseDBContext,
		RunE: func(c *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("parse run id: %w", err)
			}
			ctx := c.Context()
			store := sqlstore.New(db.FromContext(ctx))
			r, err := store.GetRun(ctx, id)
			if err != nil {
				return fmt.Errorf("get run: %w", err)
			}
			c.Printf("ID:        %d\n", r.ID)
			c.Printf("Repo:      %s\n", r.RepoName)
			c.Printf("Workflow:  %s\n", r.WorkflowName)
			c.Printf("Runs on:   %s\n", r.RunsOn)
			c.Printf("Status:    %s\n", r.Status)
			c.Printf("Triggered: %s\n", r.TriggeredByEvent)
			c.Printf("Created:   %s\n", r.CreatedAt)
			if r.StartedAt != nil {
				c.Printf("Started:   %s\n", *r.StartedAt)
			}
			if r.FinishedAt != nil {
				c.Printf("Finished:  %s\n", *r.FinishedAt)
			}
			if r.FailureReason != nil {
				c.Printf("Failure:   %s\n", *r.FailureReason)
			}
			if r.Container != nil {
				c.Printf("Container: %s\n", *r.Container)
			}
			return nil
		},
	}
}

func runCancelCommand() *cobra.Command {
	return &cobra.Command{
		Use:                "cancel ID",
		Short:              "Cancel a run by ID",
		Args:               cobra.ExactArgs(1),
		PersistentPreRunE:  cmd.InitBackendContext,
		PersistentPostRunE: cmd.CloseDBContext,
		RunE: func(c *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("parse run id: %w", err)
			}
			service, err := serviceFromContext(c)
			if err != nil {
				return err
			}
			if err := service.CancelRun(c.Context(), adminCaller, id); err != nil {
				return fmt.Errorf("cancel run: %w", err)
			}
			c.Printf("Canceled run %d.\n", id)
			return nil
		},
	}
}

func runLogsCommand() *cobra.Command {
	return &cobra.Command{
		Use:                "logs ID",
		Short:              "Print the log lines of a run by ID",
		Args:               cobra.ExactArgs(1),
		PersistentPreRunE:  cmd.InitBackendContext,
		PersistentPostRunE: cmd.CloseDBContext,
		RunE: func(c *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("parse run id: %w", err)
			}
			ctx := c.Context()
			store := sqlstore.New(db.FromContext(ctx))
			logs, err := store.ListLogEntriesByRun(ctx, id)
			if err != nil {
				return fmt.Errorf("list logs: %w", err)
			}
			for _, entry := range logs {
				c.Println(entry.Line)
			}
			return nil
		},
	}
}

// --- Maintenance subcommands ---------------------------------------------

func syncCommand() *cobra.Command {
	return &cobra.Command{
		Use:                "sync REPO",
		Short:              "Reconcile workflows for REPO from its magic folder at HEAD",
		Args:               cobra.ExactArgs(1),
		PersistentPreRunE:  cmd.InitBackendContext,
		PersistentPostRunE: cmd.CloseDBContext,
		RunE: func(c *cobra.Command, args []string) error {
			service, err := serviceFromContext(c)
			if err != nil {
				return err
			}
			if err := service.SyncWorkflowsOnPush(c.Context(), args[0]); err != nil {
				return fmt.Errorf("sync workflows: %w", err)
			}
			c.Printf("Synced workflows for %q.\n", args[0])
			return nil
		},
	}
}

func enforceTimeoutsCommand() *cobra.Command {
	return &cobra.Command{
		Use:                "enforce-timeouts",
		Short:              "Run the pickup-timeout pass once",
		Args:               cobra.NoArgs,
		PersistentPreRunE:  cmd.InitBackendContext,
		PersistentPostRunE: cmd.CloseDBContext,
		RunE: func(c *cobra.Command, _ []string) error {
			service, err := serviceFromContext(c)
			if err != nil {
				return err
			}
			if err := service.EnforceTimeouts(c.Context()); err != nil {
				return fmt.Errorf("enforce timeouts: %w", err)
			}
			return nil
		},
	}
}

func rotateCommand() *cobra.Command {
	return &cobra.Command{
		Use:                "rotate",
		Short:              "Run the run-retention rotation pass once",
		Args:               cobra.NoArgs,
		PersistentPreRunE:  cmd.InitBackendContext,
		PersistentPostRunE: cmd.CloseDBContext,
		RunE: func(c *cobra.Command, _ []string) error {
			service, err := serviceFromContext(c)
			if err != nil {
				return err
			}
			if err := service.RotateExpiredRuns(c.Context()); err != nil {
				return fmt.Errorf("rotate runs: %w", err)
			}
			return nil
		},
	}
}

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

type container struct {
	Deps *deps
}

type deps struct {
	// Filled by initDeps in di.go
	Config   *appConfig
	Services *services
}

type appConfig struct {
	Pretty  bool
	Timeout time.Duration
}

type services struct {
	// Slices required by CLI subcommands will be filled in di.go
	Admin *adminFacade
	Repo  *repoFacade
	Cache *cacheFacade
	Sys   *systemFacade
}

// Root command flags
var (
	flagPretty  bool
	flagTimeout time.Duration
)

func main() {
	root := &cobra.Command{
		Use:   "cli",
		Short: "Go License Admin CLI",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return ensureDeps(cmd.Context())
		},
	}

	// Global flags
	root.PersistentFlags().BoolVar(&flagPretty, "pretty", false, "Pretty-print JSON output")
	root.PersistentFlags().DurationVar(&flagTimeout, "timeout", 10*time.Second, "Command timeout")

	// Wire subcommands in separate files
	root.AddCommand(
		newTenantCmd(),
		newLicenseCmd(),
		newProductCmd(),
		newCacheCmd(),
		newSystemCmd(),
	)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

var appContainer container

func ensureDeps(ctx context.Context) error {
	if appContainer.Deps != nil {
		return nil
	}
	// honor timeout and cancellation
	ctx, cancel := context.WithTimeout(withSignals(ctx), flagTimeout)
	defer cancel()
	d, err := initDeps(ctx, &appConfig{
		Pretty:  flagPretty,
		Timeout: flagTimeout,
	})
	if err != nil {
		return err
	}
	appContainer.Deps = d
	return nil
}

func withSignals(ctx context.Context) context.Context {
	cctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-cctx.Done()
		stop()
	}()
	return cctx
}

// jsonOut writes a JSON object to stdout respecting --pretty.
func jsonOut(v any) {
	enc := json.NewEncoder(os.Stdout)
	if appContainer.Deps != nil && appContainer.Deps.Config.Pretty {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(v); err != nil {
		log.Printf("encode json: %v", err)
		fmt.Println(`{"error":"encode_failed"}`)
	}
}

// jsonErr writes a JSON error and returns a cobra-friendly error.
func jsonErr(msg string, err error) error {
	if err != nil {
		jsonOut(map[string]any{"error": msg, "detail": err.Error()})
		return fmt.Errorf("%s: %w", msg, err)
	}
	jsonOut(map[string]any{"error": msg})
	return fmt.Errorf("%s", msg)
}


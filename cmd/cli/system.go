package main

import (
	"context"
	"runtime"
	"time"

	"github.com/spf13/cobra"
)

func newSystemCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "system",
		Short: "System and debug commands",
	}
	cmd.AddCommand(
		newSystemStatsCmd(),
		newSystemHealthCmd(),
		newSystemConfigCmd(),
	)
	return cmd
}

func newSystemStatsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "stats",
		Short: "Show basic system stats",
		RunE: func(cmd *cobra.Command, args []string) error {
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			out := map[string]any{
				"go": map[string]any{
					"version": runtime.Version(),
					"goroutines": runtime.NumGoroutine(),
				},
				"mem": map[string]any{
					"alloc_bytes": m.Alloc,
					"sys_bytes":   m.Sys,
				},
				"queue_depth": 0,
			}
			jsonOut(out)
			return nil
		},
	}
	return c
}

func newSystemHealthCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "health",
		Short: "Check DB (and Redis if configured)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Second)
			defer cancel()
			dbOK := true
			if err := appContainer.Deps.Services.Repo.DB.Ping(ctx); err != nil {
				dbOK = false
			}
			redisOK := appContainer.Deps.Services.Cache.L2 != nil
			jsonOut(map[string]any{
				"db":    map[string]any{"ok": dbOK},
				"redis": map[string]any{"configured": appContainer.Deps.Services.Cache.L2 != nil, "ok": redisOK},
				"status": func() string {
					if dbOK {
						return "up"
					}
					return "down"
				}(),
			})
			if !dbOK {
				return jsonErr("unhealthy", nil)
			}
			return nil
		},
	}
	return c
}

func newSystemConfigCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "config",
		Short: "Show selected runtime config",
		RunE: func(cmd *cobra.Command, args []string) error {
			appCfg := appContainer.Deps.Services.Sys.Config
			cacheCfg := appContainer.Deps.Services.Sys.CacheConf
			jsonOut(map[string]any{
				"app": map[string]any{
					"name": appCfg.AppName,
					"mode": appCfg.AppMode,
					"env":  appCfg.AppEnv,
					"port": appCfg.AppPort,
				},
				"cache": map[string]any{
					"l1_max_entries": cacheCfg.L1MaxEntries,
					"redis_url_set":  cacheCfg.RedisURL != "",
				},
			})
			return nil
		},
	}
	return c
}


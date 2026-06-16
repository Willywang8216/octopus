package cmd

import (
	"context"
	"time"

	"github.com/bestruirui/octopus/internal/conf"
	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/spf13/cobra"
)

var (
	purgeLogs    bool
	purgeBefore  int
	runVacuum    bool
	commandTO    time.Duration
)

var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Database maintenance tools",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		conf.Load(cfgFile)
		log.SetLevel(conf.AppConfig.Log.Level)
		if err := db.InitDB(conf.AppConfig.Database.Type, conf.AppConfig.Database.Path, conf.IsDebug()); err != nil {
			log.Errorf("database init error: %v", err)
			return
		}
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		_ = db.Close()
	},
}

var dbPurgeCmd = &cobra.Command{
	Use:   "purge",
	Short: "Purge non-essential data (e.g. history logs)",
	Run: func(cmd *cobra.Command, args []string) {
		ctx, cancel := context.WithTimeout(context.Background(), commandTO)
		defer cancel()

		if purgeLogs {
			if purgeBefore > 0 {
				cutoff := time.Now().Add(-time.Duration(purgeBefore) * 24 * time.Hour)
				if err := op.RelayLogPurgeBefore(ctx, cutoff); err != nil {
					log.Errorf("purge relay logs before %s failed: %v", cutoff.Format(time.RFC3339), err)
					return
				}
			} else {
				if err := op.RelayLogPurgeAll(ctx); err != nil {
					log.Errorf("purge relay logs failed: %v", err)
					return
				}
			}
		}

		if runVacuum {
			if err := op.Vacuum(ctx); err != nil {
				log.Errorf("vacuum failed: %v", err)
				return
			}
		}
	},
}

var dbVacuumCmd = &cobra.Command{
	Use:   "vacuum",
	Short: "Run SQLite VACUUM (shrinks data.db after deletes)",
	Run: func(cmd *cobra.Command, args []string) {
		ctx, cancel := context.WithTimeout(context.Background(), commandTO)
		defer cancel()
		if err := op.Vacuum(ctx); err != nil {
			log.Errorf("vacuum failed: %v", err)
			return
		}
	},
}

func init() {
	dbCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is ./data/config.json)")
	dbCmd.PersistentFlags().DurationVar(&commandTO, "timeout", 10*time.Minute, "command timeout")

	dbPurgeCmd.Flags().BoolVar(&purgeLogs, "logs", true, "purge history logs (relay_logs table)")
	dbPurgeCmd.Flags().IntVar(&purgeBefore, "logs-before-days", 0, "only purge logs older than N days (0 = purge all)")
	dbPurgeCmd.Flags().BoolVar(&runVacuum, "vacuum", true, "run SQLite VACUUM after purge")

	dbCmd.AddCommand(dbPurgeCmd)
	dbCmd.AddCommand(dbVacuumCmd)
	rootCmd.AddCommand(dbCmd)
}

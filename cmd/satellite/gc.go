// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package main

import (
	"github.com/spf13/cobra"
	"github.com/zeebo/errs"
	"go.uber.org/zap"

	"storj.io/private/process"
	"storj.io/private/version"
	"storj.io/storj/pkg/revocation"
	"storj.io/storj/satellite"
	"storj.io/storj/satellite/metainfo"
	"storj.io/storj/satellite/satellitedb"
)

func cmdGCRun(cmd *cobra.Command, args []string) (err error) {
	ctx, _ := process.Ctx(cmd)
	log := zap.L()

	runCfg.Debug.Address = *process.DebugAddrFlag

	identity, err := runCfg.Identity.Load()
	if err != nil {
		log.Error("Failed to load identity.", zap.Error(err))
		return errs.New("Failed to load identity: %+v", err)
	}

	db, err := satellitedb.Open(ctx, log.Named("db"), runCfg.Database, satellitedb.Options{ApplicationName: "satellite-gc"})
	if err != nil {
		return errs.New("Error starting master database on satellite GC: %+v", err)
	}
	defer func() {
		err = errs.Combine(err, db.Close())
	}()

	pointerDB, err := metainfo.OpenStore(ctx, log.Named("pointerdb"), runCfg.Metainfo.DatabaseURL, "satellite-gc")
	if err != nil {
		return errs.New("Error creating pointerDB connection GC: %+v", err)
	}
	defer func() {
		err = errs.Combine(err, pointerDB.Close())
	}()

	metabaseDB, err := metainfo.OpenMetabase(ctx, log.Named("metabase"), runCfg.Metainfo.DatabaseURL)
	if err != nil {
		return errs.New("Error creating metabase connection: %+v", err)
	}
	defer func() {
		err = errs.Combine(err, metabaseDB.Close())
	}()

	revocationDB, err := revocation.OpenDBFromCfg(ctx, runCfg.Server.Config)
	if err != nil {
		return errs.New("Error creating revocation database GC: %+v", err)
	}
	defer func() {
		err = errs.Combine(err, revocationDB.Close())
	}()

	peer, err := satellite.NewGarbageCollection(log, identity, db, pointerDB, metabaseDB, revocationDB, version.Build, &runCfg.Config, process.AtomicLevel(cmd))
	if err != nil {
		return err
	}

	_, err = peer.Version.Service.CheckVersion(ctx)
	if err != nil {
		return err
	}

	if err := process.InitMetricsWithHostname(ctx, log, nil); err != nil {
		log.Warn("Failed to initialize telemetry batcher on satellite GC", zap.Error(err))
	}

	err = pointerDB.MigrateToLatest(ctx)
	if err != nil {
		return errs.New("Error creating pointerDB tables GC: %+v", err)
	}

	err = db.CheckVersion(ctx)
	if err != nil {
		log.Error("Failed satellite database version check.", zap.Error(err))
		return errs.New("Error checking version for satellitedb: %+v", err)
	}

	runError := peer.Run(ctx)
	closeError := peer.Close()
	return errs.Combine(runError, closeError)
}

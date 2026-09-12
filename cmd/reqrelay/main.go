package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/ardanlabs/conf/v3"
	"github.com/logocomune/requestinspector-relay/internal/app"
	"github.com/logocomune/requestinspector-relay/internal/config"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	configureLogging()
	os.Exit(run())
}

func configureLogging() {
	log.SetFlags(log.LstdFlags)
}

func run() int {
	cfg, info, err := config.Load(version)
	if err != nil {
		if errors.Is(err, conf.ErrHelpWanted) || errors.Is(err, conf.ErrVersionWanted) {
			fmt.Print(info)
			return 0
		}
		log.Printf("startup failed: %v", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	runtime, err := app.Start(ctx, cfg, app.BuildInfo{Version: version, Commit: commit, Date: date})
	if err != nil {
		log.Printf("startup failed: %v", err)
		return 1
	}
	fmt.Print(startupBanner(version, date, runtime))
	for _, message := range udpStartupMessages(cfg, runtime) {
		fmt.Println(message)
	}
	if err := runtime.Wait(); err != nil {
		log.Printf("shutdown failed: %v", err)
		return 1
	}
	log.Print("RequestInspector Relay stopped")
	return 0
}

func udpStartupMessages(cfg config.Config, runtime *app.Runtime) []string {
	if !cfg.UDP.Enabled {
		return nil
	}
	messages := []string{"UDP listener: " + runtime.UDPAddress}
	if cfg.UDP.Mode == config.ModeProxy {
		messages = append(messages, "UDP proxy upstream: "+cfg.UDP.Upstream)
	}
	return messages
}

func startupBanner(version, date string, runtime *app.Runtime) string {
	return fmt.Sprintf(
		"+--------------------------------------------------+\n"+
			"|  REQUESTINSPECTOR RELAY                          |\n"+
			"|  HTTP REQUEST INSPECTOR / REVERSE PROXY          |\n"+
			"+--------------------------------------------------+\n"+
			"VERSION  %s\n"+
			"BUILT    %s\n"+
			"Ingest: http://%s\n"+
			"Web interface: http://%s\n",
		version,
		date,
		runtime.TrafficAddress,
		runtime.ManagementAddress,
	)
}

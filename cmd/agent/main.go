package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nimweo/pulse-agent/internal/collector"
	"github.com/nimweo/pulse-agent/internal/model"
	"github.com/nimweo/pulse-agent/internal/transport"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	hostname, _ := os.Hostname()
	client := transport.New("http://localhost:3000/ingest")
	batch := &collector.Batch{}
	core := collector.NewCore()

	collectTick := time.NewTicker(time.Second)
	defer collectTick.Stop()
	sendTick := time.NewTicker(10 * time.Second) // na test 10s, w produkcji 60s
	defer sendTick.Stop()

	slog.Info("agent wystartował")

	for {
		select {
		case <-ctx.Done():
			slog.Info("zamykanie")
			return

		case <-collectTick.C:
			if err := core.Collect(ctx, batch); err != nil {
				slog.Error("collect", "err", err)
			}

		case <-sendTick.C:
			cs, ps := batch.Drain()
			if len(cs) == 0 && len(ps) == 0 {
				continue
			}
			payload := model.Payload{
				AgentVersion: "dev",
				Hostname:     hostname,
				Core:         cs,
				Points:       ps,
			}
			if err := client.Send(ctx, payload); err != nil {
				slog.Error("send", "err", err, "dropped", len(cs)+len(ps))
				continue // na razie gubimy — spool dopiszesz później
			}
			slog.Info("wysłano", "core", len(cs), "points", len(ps))
		}
	}
}

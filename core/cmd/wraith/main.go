// Command wraith runs WRAITH Core.
//
//	wraith [flags]            run the server
//	wraith tail [ws-url]      print the live stream (default ws://127.0.0.1:8088/api/v1/ws)
//	wraith validate [--strict] [--require-checksum] [file]
//	                          validate newline-delimited events (stdin by default);
//	                          --strict treats non-protocol lines as errors
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/MoriartyPuth/wraith/core/internal/api"
	"github.com/MoriartyPuth/wraith/core/internal/capture"
	"github.com/MoriartyPuth/wraith/core/internal/capture/serial"
	"github.com/MoriartyPuth/wraith/core/internal/config"
	"github.com/MoriartyPuth/wraith/core/internal/detection"
	"github.com/MoriartyPuth/wraith/core/internal/events"
	"github.com/MoriartyPuth/wraith/core/internal/ingest"
	"github.com/MoriartyPuth/wraith/core/internal/storage"
	"github.com/MoriartyPuth/wraith/core/internal/stream"
	"github.com/MoriartyPuth/wraith/core/internal/system"
	"github.com/MoriartyPuth/wraith/core/internal/transport"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "validate" {
		os.Exit(validate(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "tail" {
		os.Exit(tail(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println("wraith", system.Version, "protocol", events.ProtocolVersion)
		return
	}
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "wraith:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := config.Load(args)
	if err != nil {
		return err
	}
	log := newLogger(cfg)
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := storage.Open(ctx, cfg.DBPath)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer store.Close()
	log.Info("database ready", "path", cfg.DBPath)

	// Retention: prune events (and their observations) older than the policy.
	var prunedEvents, prunedObs atomic.Uint64
	go store.RunPruner(ctx, time.Duration(cfg.EventRetentionDays)*24*time.Hour,
		time.Duration(cfg.PruneIntervalMinutes)*time.Minute, time.Now, func(r storage.PruneResult) {
			prunedEvents.Add(uint64(r.Events))
			prunedObs.Add(uint64(r.Observations))
			log.Info("pruned old data", "events", r.Events, "observations", r.Observations,
				"retention_days", cfg.EventRetentionDays)
		})
	if cfg.EventRetentionDays > 0 {
		log.Info("retention", "event_retention_days", cfg.EventRetentionDays, "prune_interval_min", cfg.PruneIntervalMinutes)
	} else {
		log.Info("retention disabled: events are kept indefinitely")
	}

	hub := stream.NewHub(log, cfg.AllowedHosts)
	pipe := ingest.New(store, detection.NewEngine(), hub, log)
	providers := []capture.CaptureProvider{
		serial.New(serial.Options{Port: cfg.SerialPort, Baud: cfg.SerialBaud,
			OnReject: pipe.Rejected, OnStatus: pipe.ProviderStatus}),
	}
	mgr := capture.NewManager(providers...)

	// Ingestion runs on its own context so it can drain after providers stop.
	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	var ingesting sync.WaitGroup
	for _, p := range providers {
		ingesting.Add(1)
		go func(p capture.CaptureProvider) {
			defer ingesting.Done()
			pipe.Run(runCtx, p.Events())
		}(p)
	}
	for name, err := range mgr.StartAll(ctx) {
		log.Warn("capture provider not running", "provider", name, "reason", err)
	}
	for _, st := range mgr.Statuses() {
		log.Info("capture provider", "provider", st.Name, "state", st.State, "msg", st.Message)
	}
	if cfg.TXEnabled {
		log.Warn("deauth test transmission ENABLED: POST /api/v1/tx forwards deauth/disassoc bursts to the sensor; use only on networks you are authorized to test (docs/tx.md)")
	}

	handler := api.NewRouter(api.Deps{
		Store: store, Capture: mgr, Log: log, Started: time.Now(), AllowedHosts: cfg.AllowedHosts,
		Stream:    hub,
		TXEnabled: cfg.TXEnabled,
		RuntimeStats: func() map[string]uint64 {
			return map[string]uint64{
				"ingested": pipe.Ingested.Load(), "duplicates": pipe.Duplicates.Load(),
				"seq_gaps": pipe.Gaps.Load(), "sensor_restarts": pipe.Restarts.Load(),
				"ingest_failures": pipe.Failures.Load(), "stream_clients": uint64(hub.Clients()),
				"pruned_events": prunedEvents.Load(), "pruned_observations": prunedObs.Load(),
				"findings_deduped": pipe.Deduped.Load(),
			}
		},
	})
	if cfg.DashboardDir != "" {
		handler = withDashboard(handler, cfg.DashboardDir)
	}
	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", "http://"+cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
		close(errc)
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	// 1. stop capture (closes provider event channels)
	mgr.StopAll()
	// 2. let ingestion drain what was already read
	drained := make(chan struct{})
	go func() { ingesting.Wait(); close(drained) }()
	select {
	case <-drained:
	case <-time.After(5 * time.Second):
		log.Warn("ingestion did not drain in time")
		cancelRun()
	}
	// 3. close live clients (hijacked connections are not covered by Shutdown)
	hub.Close()
	// 4. stop HTTP; the deferred store.Close runs last
	shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shCtx)
}

// withDashboard serves /api/* from the API and everything else from dir,
// falling back to index.html for client-side routes.
func withDashboard(apiH http.Handler, dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.URL.Path) >= 5 && r.URL.Path[:5] == "/api/" {
			apiH.ServeHTTP(w, r)
			return
		}
		if _, err := os.Stat(dir + r.URL.Path); r.URL.Path != "/" && err != nil {
			http.ServeFile(w, r, dir+"/index.html")
			return
		}
		files.ServeHTTP(w, r)
	})
}

func newLogger(cfg config.Config) *slog.Logger {
	var lvl slog.Level
	_ = lvl.UnmarshalText([]byte(cfg.LogLevel))
	opts := &slog.HandlerOptions{Level: lvl}
	if cfg.LogFormat == "json" {
		return slog.New(slog.NewJSONHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stderr, opts))
}

// validate checks newline-delimited events with the same framing and
// validation the serial provider will use. Exit status 1 if any event line is
// invalid; non-JSON lines are reported but not fatal.
func validate(args []string) int {
	strict, requireCRC := false, false
	for len(args) > 0 && strings.HasPrefix(args[0], "--") {
		switch args[0] {
		case "--strict":
			strict = true
		case "--require-checksum":
			requireCRC = true
		default:
			fmt.Fprintln(os.Stderr, "unknown flag", args[0])
			return 2
		}
		args = args[1:]
	}
	var in io.Reader = os.Stdin
	if len(args) > 0 {
		f, err := os.Open(args[0])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		defer f.Close()
		in = f
	}
	lr := transport.NewLineReader(in, events.MaxMessageSize)
	seqs := transport.NewSeqTracker(4096)
	var ok, bad, logs int
	for n := 1; ; n++ {
		line, err := lr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			fmt.Printf("line %d: FAIL %v\n", n, err)
			bad++
			break
		}
		switch line.Kind {
		case transport.LineLog:
			logs++
			if strict {
				fmt.Printf("line %d: FAIL non-protocol line (strict)\n", n)
				bad++
			}
			continue
		case transport.LineOversize:
			fmt.Printf("line %d: FAIL oversize (%d bytes)\n", n, line.Size)
			bad++
			continue
		}
		payload, crc := transport.SplitChecksum(line.Data)
		if crc == transport.ChecksumMismatch || (requireCRC && crc == transport.ChecksumAbsent) {
			fmt.Printf("line %d: FAIL checksum %s\n", n, map[transport.Checksum]string{transport.ChecksumMismatch: "mismatch", transport.ChecksumAbsent: "missing"}[crc])
			bad++
			continue
		}
		ev, err := events.Parse(payload)
		if err != nil {
			fmt.Printf("line %d: FAIL %v\n", n, err)
			bad++
			continue
		}
		v := seqs.Track(ev.SensorID, ev.EventID, ev.Seq)
		note := ""
		switch {
		case v.Duplicate:
			note = " (duplicate)"
		case v.Restart:
			note = " (sensor restart)"
		case v.Gap > 0:
			note = fmt.Sprintf(" (seq gap: %d missing)", v.Gap)
		}
		fmt.Printf("line %d: ok %s %s%s\n", n, ev.Type, ev.EventID, note)
		ok++
	}
	fmt.Printf("valid=%d invalid=%d non-protocol=%d\n", ok, bad, logs)
	if bad > 0 {
		return 1
	}
	return 0
}

package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"ghost-protocol/config"
	"ghost-protocol/db"
	"ghost-protocol/proxy"
)

const banner = `
  ░██████╗░██╗░░██╗░█████╗░░██████╗████████╗
  ██╔════╝░██║░░██║██╔══██╗██╔════╝╚══██╔══╝
  ██║░░██╗░███████║██║░░██║╚█████╗░░░░██║░░░
  ██║░░╚██╗██╔══██║██║░░██║░╚═══██╗░░░██║░░░
  ╚██████╔╝██║░░██║╚█████╔╝██████╔╝░░░██║░░░
  ░╚═════╝░╚═╝░░╚═╝░╚════╝░╚═════╝░░░░╚═╝░░░

  ██████╗░██████╗░░█████╗░████████╗░█████╗░░█████╗░░█████╗░██╗░░░░░
  ██╔══██╗██╔══██╗██╔══██╗╚══██╔══╝██╔══██╗██╔══██╗██╔══██╗██║░░░░░
  ██████╔╝██████╔╝██║░░██║░░░██║░░░██║░░██║██║░░╚═╝██║░░██║██║░░░░░
  ██╔═══╝░██╔══██╗██║░░██║░░░██║░░░██║░░██║██║░░██╗██║░░██║██║░░░░░
  ██║░░░░░██║░░██║╚█████╔╝░░░██║░░░╚█████╔╝╚█████╔╝╚█████╔╝███████╗
  ╚═╝░░░░░╚═╝░░╚═╝░╚════╝░░░░╚═╝░░░░╚════╝░░╚════╝░░╚════╝░╚══════╝

  v1.0  ·  Local LLM Reverse Proxy  ·  github.com/deeyeti/ghost-protocol
`

func main() {
	cfgPath := flag.String("config", "config.json", "Path to config.json")
	dbPath := flag.String("db", "ghost.db", "Path to SQLite database file")
	flag.Parse()

	fmt.Print(banner)

	// --- Load config ---
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ❌ Config error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  ✅ Config loaded from %s\n", *cfgPath)

	// --- Open database ---
	database, err := db.Open(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ❌ Database error: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()
	fmt.Printf("  ✅ Database ready at %s\n", *dbPath)

	// --- Build proxy server ---
	srv, err := proxy.New(*cfg, database)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ❌ Proxy init error: %v\n", err)
		os.Exit(1)
	}

	// --- Graceful shutdown on SIGINT/SIGTERM ---
	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit
		fmt.Println("\n\n  👋 Ghost Protocol shutting down. Goodbye.\n")
		os.Exit(0)
	}()

	fmt.Printf("  📡 Cloud:  %s → %s\n", cfg.Cloud.Provider, cfg.Cloud.Model)
	fmt.Printf("  🏠 Local:  %s → %s\n", cfg.Local.BaseURL, cfg.Local.Model)
	fmt.Printf("  🔒 Redaction patterns: %d\n", len(cfg.Redaction.Patterns))
	fmt.Printf("  💾 Cache threshold: %.2f cosine similarity\n\n", cfg.Cache.SimilarityThreshold)

	// --- Start listening ---
	if err := srv.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "  ❌ Server error: %v\n", err)
		os.Exit(1)
	}
}

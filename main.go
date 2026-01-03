package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name:  "mkvfixer",
		Usage: "Batch remux MKV files to standardize audio/video language tags",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "recursive",
				Aliases: []string{"r"},
				Usage:   "Recursively process subdirectories",
			},
			&cli.StringFlag{
				Name:  "config",
				Usage: "Path to configuration file",
				Value: "config.json",
			},
			&cli.IntFlag{
				Name:    "workers",
				Aliases: []string{"n"},
				Usage:   "Number of concurrent workers",
				Value:   4,
			},
			&cli.BoolFlag{
				Name:    "check-only",
				Aliases: []string{"c", "dry-run"},
				Usage:   "Only check files and populate cache, do not remux",
			},
		},
		Commands: []*cli.Command{
			{
				Name:  "all",
				Usage: "Process both audio and subtitles (default)",
				Action: func(c *cli.Context) error {
					return runProcessing(c, ActionAll)
				},
			},
			{
				Name:  "audio",
				Usage: "Process only audio tracks",
				Action: func(c *cli.Context) error {
					return runProcessing(c, ActionAudio)
				},
			},
			{
				Name:  "subtitle",
				Usage: "Process only subtitle tracks",
				Action: func(c *cli.Context) error {
					return runProcessing(c, ActionSubtitle)
				},
			},
		},
		Action: func(c *cli.Context) error {
			// Default action is All
			return runProcessing(c, ActionAll)
		},
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func runProcessing(c *cli.Context, actionType ActionType) error {
	// Create context that listens for the interrupt signal from the OS.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 1. Load Config
	configPath := c.String("config")
	cfg, err := loadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config from %s: %v", configPath, err)
	}
	fmt.Printf("Loaded config: %+v\n", cfg)

	// 2. Get Directory from Args
	targetDir := c.Args().First()
	if targetDir == "" {
		// Default to current directory if none provided
		targetDir = "."
	}

	isRecursive := c.Bool("recursive")
	fmt.Printf("Scanning directory: %s (Recursive: %v) [Action: %s]\n", targetDir, isRecursive, actionType)
	// 3. Load Cache
	cachePath := ".mkvfixer.cache"
	fileCache, err := NewCache(cachePath)
	if err != nil {
		fmt.Printf("Warning: Could not load cache: %v\n", err)
		fileCache, _ = NewCache(cachePath)
	} else {
		fmt.Printf("Loaded cache with %d items\n", len(fileCache.Items))
	}

	// Ensure we save the cache on exit (even on error/panic)
	defer func() {
		if err := fileCache.Save(); err != nil {
			fmt.Printf("Warning: Failed to save cache: %v\n", err)
		} else {
			fmt.Println("Cache saved.")
		}
	}()

	// 4. Define the processing function (walker)
	// Output Check: Single file?
	info, err := os.Stat(targetDir)
	if err != nil {
		return fmt.Errorf("failed to stat target: %v", err)
	}

	numWorkers := c.Int("workers")
	if !info.IsDir() {
		fmt.Println("Detected single file input. Forcing workers=1.")
		numWorkers = 1
	} else if numWorkers < 1 {
		numWorkers = 1
	}
	fmt.Printf("Starting %d workers...\n", numWorkers)

	jobs := make(chan string, numWorkers*2)
	var wg sync.WaitGroup

	checkOnly := c.Bool("check-only")
	// Worker function
	worker := func(id int) {
		defer wg.Done()
		for path := range jobs {
			// Check context before starting new work
			select {
			case <-ctx.Done():
				return
			default:
			}

			// CACHE CHECK
			// Use actionType-prefixed key for cache
			absPath, _ := filepath.Abs(path)
			cacheKey := fmt.Sprintf("%s:%s", actionType, absPath)

			// Pass cacheKey and original path to Check
			if cached, _ := fileCache.Check(cacheKey, path); cached {
				continue
			}

			finalPath, err := remuxFile(ctx, path, cfg, checkOnly, actionType)
			if err != nil {
				fmt.Printf("Worker %d: Failed to process %s: %v\n", id, filepath.Base(path), err)
			} else {
				// Success (remuxed OR skipped as compliant)
				if finalPath != "" {
					// Use same cacheKey for Update
					if err := fileCache.Update(cacheKey, finalPath); err != nil {
						fmt.Printf("Worker %d: Warning - Failed to update cache for %s: %v\n", id, filepath.Base(finalPath), err)
					}
				}
			}
		}
	}

	// Start workers
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go worker(i)
	}

	// Walk and send jobs
	err = filepath.WalkDir(targetDir, func(path string, info os.DirEntry, err error) error {
		// Stop walking if interrupted
		select {
		case <-ctx.Done():
			return filepath.SkipDir
		default:
		}

		if err != nil {
			return err
		}
		if info.IsDir() {
			if !isRecursive && path != targetDir {
				return filepath.SkipDir
			}
			return nil
		}

		if strings.ToLower(filepath.Ext(path)) == ".mkv" {
			jobs <- path
		}
		return nil
	})

	close(jobs) // Signal workers to finish
	wg.Wait()   // Wait for workers to cleanup

	if ctx.Err() != nil {
		fmt.Println("\nProcess interrupted. Cleaning up...")
		// Return nil or specific error?
		// We want defer to save cache, which happens on return.
		return nil
	}

	if err != nil {
		return fmt.Errorf("error walking directory: %v", err)
	}
	// Saved by defer

	fmt.Println("Batch processing complete.")
	return nil
}

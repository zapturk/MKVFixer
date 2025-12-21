package main

import (
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
		},
		Action: func(c *cli.Context) error {
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
			fmt.Printf("Scanning directory: %s (Recursive: %v)\n", targetDir, isRecursive)
			// 3. Load Cache
			// Where to store it? Ideally in current dir or home.
			// Let's store in the current directory for simplicity/portability with the files.
			// Or maybe the user wants it hidden. ".mkvfixer.cache"
			cachePath := ".mkvfixer.cache"
			fileCache, err := NewCache(cachePath)
			if err != nil {
				// Warn but proceed?
				fmt.Printf("Warning: Could not load cache: %v\n", err)
				// Create new empty cache
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
			// Worker Pool Setup
			numWorkers := c.Int("workers")
			if numWorkers < 1 {
				numWorkers = 1
			}
			fmt.Printf("Starting %d workers...\n", numWorkers)

			jobs := make(chan string, numWorkers*2)
			var wg sync.WaitGroup

			// Worker function
			worker := func(id int) {
				defer wg.Done()
				for path := range jobs {
					// CACHE CHECK
					if cached, _ := fileCache.Check(path); cached {
						continue
					}

					finalPath, err := remuxFile(path, cfg)
					if err != nil {
						fmt.Printf("Worker %d: Failed to process %s: %v\n", id, path, err)
					} else {
						// Success (remuxed OR skipped as compliant)
						if finalPath != "" {
							if err := fileCache.Update(finalPath); err != nil {
								fmt.Printf("Worker %d: Warning - Failed to update cache for %s: %v\n", id, finalPath, err)
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
			// Wait for workers or interrupt
			done := make(chan struct{})
			go func() {
				wg.Wait()
				close(done)
			}()

			// Handle interrupts
			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

			select {
			case <-done:
				// Finished normally
			case <-sigChan:
				fmt.Println("\nInterrupt received. Stopping...")
				// We can just exit, the defer will handle saving.
				// But we should probably stop feeding jobs?
				// For now, let's just break and let defer save.
			}

			if err != nil {
				return fmt.Errorf("error walking directory: %v", err)
			}
			// Saved by defer

			fmt.Println("Batch processing complete.")
			return nil
		},
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

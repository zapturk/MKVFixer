package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
				Name:    "config",
				Aliases: []string{"c"},
				Usage:   "Path to configuration file",
				Value:   "config.json",
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

			// 4. Define the processing function (walker)
			processPath := func(path string, info os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if info.IsDir() {
					// If not recursive and this is a subdir, skip it
					if !isRecursive && path != targetDir {
						return filepath.SkipDir
					}
					return nil
				}

				// Check extension
				isMkv := strings.ToLower(filepath.Ext(path)) == ".mkv"

				// CACHE CHECK
				if isMkv {
					// Check if cached
					if cached, _ := fileCache.Check(path); cached {
						// Optional: Debug mode to see this?
						// fmt.Printf("Skipping cached: %s\n", path)
						return nil
					}

					// We removed the isRemux check logic in favor of cache,
					// BUT we should probably still avoid processing our own output if it wasn't caught by cache for some reason
					// (e.g. first run interrupted).
					// Actually, the plan said "Replace the simplistic check... allow for cleaner filenames".
					// So let's rely on cache + internal logic (remuxFile checks tracks).
					// If a file is named "-remux.mkv" but isn't in cache, we might process it.
					// If it's already fixed, remuxFile returns "Skipped".

					finalPath, err := remuxFile(path, cfg)
					if err != nil {
						fmt.Printf("Failed to process %s: %v\n", path, err)
					} else {
						// Success (remuxed OR skipped as compliant)
						// Update cache with the FINAL path
						if finalPath != "" {
							fileCache.Update(finalPath)
							// Save periodically or after every file?
							// Save after every file is safer for interrupts, but slower.
							// Given this is a batch process on potentially large files, saving logic is cheap relative to IO.
							fileCache.Save()
						}
					}
				}
				return nil
			}

			// 4. Walk the directory
			// WalkDir is efficient and works for both recursive and single-dir (via logic above)
			err = filepath.WalkDir(targetDir, processPath)
			if err != nil {
				return fmt.Errorf("error walking directory: %v", err)
			}

			fmt.Println("Batch processing complete.")
			return nil
		},
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

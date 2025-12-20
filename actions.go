package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// remuxFile contains the core logic for a single file, returns the "final" path on success/skip
func remuxFile(inputFile string, cfg *Config) (string, error) {
	fmt.Printf("Processing: %s\n", inputFile)
	// A. Inspect file
	cmd := exec.Command("mkvmerge", "-J", inputFile)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("mkvmerge inspection failed: %w", err)
	}

	var info MkvInfo
	if err := json.Unmarshal(output, &info); err != nil {
		return "", fmt.Errorf("json parsing failed: %w", err)
	}

	// Check requirements:
	// 1. Video must be cfg.VideoLanguage
	// 2. ONLY 'eng' subtitles should be kept -> ONLY cfg.SubtitleLanguages
	hasVideo := false
	needsFix := false

	// Helper to check if lang is in list
	isInList := func(lang string, list []string) bool {
		for _, l := range list {
			if l == lang {
				return true
			}
		}
		return false
	}

	for _, track := range info.Tracks {
		if track.Type == "video" {
			hasVideo = true
			if track.Properties.Language != cfg.VideoLanguage {
				needsFix = true
			}
		}
		if track.Type == "audio" {
			// If audio is NOT in the allowed list, we need to fix (remux to remove it)
			// OR if audio IS in the list but not marked default when it should be
			if !isInList(track.Properties.Language, cfg.AudioLanguages) {
				needsFix = true
			} else {
				// It IS in the list. Check default flag compliance.
				// If this track's language is the target DefaultAudio, it SHOULD be default.
				// Otherwise, it SHOULD NOT be default.
				shouldBeDefault := track.Properties.Language == cfg.DefaultAudio
				if track.Properties.DefaultTrack != shouldBeDefault {
					needsFix = true
				}
			}
		}
		if track.Type == "subtitles" {
			if !isInList(track.Properties.Language, cfg.SubtitleLanguages) {
				needsFix = true
			}
		}
	}

	if hasVideo && !needsFix {
		fmt.Printf("Skipping %s: Already meets requirements (Video=%s, Subs=%v)\n", inputFile, cfg.VideoLanguage, cfg.SubtitleLanguages)
		return inputFile, nil
	}

	// B. Build output filename
	ext := filepath.Ext(inputFile)
	baseName := strings.TrimSuffix(inputFile, ext)
	outputFile := fmt.Sprintf("%s-remux%s", baseName, ext)

	args := []string{"-o", outputFile}

	// C. Track Logic
	var keepAudioIds []string
	var keepSubtitleIds []string

	for _, track := range info.Tracks {
		// Filter Audio
		if track.Type == "audio" {
			if isInList(track.Properties.Language, cfg.AudioLanguages) {
				keepAudioIds = append(keepAudioIds, fmt.Sprintf("%d", track.ID))
			}
		}
		// Filter Subtitles
		if track.Type == "subtitles" {
			if isInList(track.Properties.Language, cfg.SubtitleLanguages) {
				keepSubtitleIds = append(keepSubtitleIds, fmt.Sprintf("%d", track.ID))
			}
		}
	}

	// Handle Audio: keep explicit list
	if len(keepAudioIds) > 0 {
		args = append(args, "--audio-tracks", strings.Join(keepAudioIds, ","))
	} else {
		// If no audio matches config, mkvmerge defaults to keeping all? No, we should probably keep none?
		// Be careful here. If user config is wrong, they lose all audio.
		// Let's assume strict compliance.
		args = append(args, "--no-audio")
	}

	// Handle Subtitles: keep explicit list
	if len(keepSubtitleIds) > 0 {
		args = append(args, "--subtitle-tracks", strings.Join(keepSubtitleIds, ","))
	} else {
		args = append(args, "--no-subtitles")
	}

	for _, track := range info.Tracks {
		// Set Video Language
		if track.Type == "video" {
			args = append(args, "--language", fmt.Sprintf("%d:%s", track.ID, cfg.VideoLanguage))
		}

		// Handle Audio Defaults
		if track.Type == "audio" {
			// Only mess with flags if we are keeping this track
			if isInList(track.Properties.Language, cfg.AudioLanguages) {
				if track.Properties.Language == cfg.DefaultAudio {
					args = append(args, "--default-track", fmt.Sprintf("%d:yes", track.ID))
				} else {
					args = append(args, "--default-track", fmt.Sprintf("%d:no", track.ID))
				}
			}
		}
	}

	args = append(args, inputFile)

	// D. Execute Remux
	remuxCmd := exec.Command("mkvmerge", args...)
	// Connect stdout/stderr if you want to see mkvmerge progress bars,
	// otherwise keep it silent or log to file.
	// remuxCmd.Stdout = os.Stdout

	if err := remuxCmd.Run(); err != nil {
		// Clean up partial file if failed
		os.Remove(outputFile)
		return "", fmt.Errorf("remux command failed: %w", err)
	}

	// E. Remove old file
	if err := os.Remove(inputFile); err != nil {
		return "", fmt.Errorf("could not delete original file: %w", err)
	}

	fmt.Printf("Success: %s -> %s\n", inputFile, outputFile)
	return outputFile, nil
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

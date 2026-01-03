package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ActionType defines the type of cleanup to perform
type ActionType string

const (
	ActionAll      ActionType = "all"
	ActionAudio    ActionType = "audio"
	ActionSubtitle ActionType = "subtitle"
)

// remuxFile contains the core logic for a single file, returns the "final" path on success/skip
func remuxFile(ctx context.Context, inputFile string, cfg *Config, checkOnly bool, actionType ActionType) (string, error) {
	fmt.Printf("Processing: %s\n", filepath.Base(inputFile))
	// A. Inspect file
	cmd := exec.CommandContext(ctx, "mkvmerge", "-J", inputFile)
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
	// 2. Audio/Subtitle checks depending on actionType
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
			if actionType == ActionAll || actionType == ActionAudio {
				_, fixNeeded := determineTargetVideoLanguage(&track, &info, cfg.VideoLanguage)
				if fixNeeded {
					needsFix = true
				}
			}
		}
		if track.Type == "audio" {
			if actionType == ActionAll || actionType == ActionAudio {
				// If audio is NOT in the allowed list, we need to fix (remux to remove it)
				// OR if audio IS in the list but not marked default when it should be
				if !isInList(track.Properties.Language, cfg.AudioLanguages) {
					needsFix = true
				} else {
					// It IS in the list. Check default flag compliance.
					shouldBeDefault := track.Properties.Language == cfg.DefaultAudio
					if track.Properties.DefaultTrack != shouldBeDefault {
						needsFix = true
					}
				}
			}
		}
		if track.Type == "subtitles" {
			if actionType == ActionAll || actionType == ActionSubtitle {
				if !isInList(track.Properties.Language, cfg.SubtitleLanguages) {
					needsFix = true
				}
			}
		}
	}

	if hasVideo && !needsFix {
		fmt.Printf("Skipping %s: Already meets requirements (Video=%s, Subs=%v) -> Adding to cache\n", filepath.Base(inputFile), cfg.VideoLanguage, cfg.SubtitleLanguages)
		return inputFile, nil
	}

	if checkOnly {
		fmt.Printf("Skipping %s: Needs fixing (check-only mode)\n", filepath.Base(inputFile))
		return "", nil // Return empty string so it's NOT added to cache and NOT reported as error
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
			if actionType == ActionAll || actionType == ActionAudio {
				if isInList(track.Properties.Language, cfg.AudioLanguages) {
					keepAudioIds = append(keepAudioIds, fmt.Sprintf("%d", track.ID))
				}
			}
		}
		// Filter Subtitles
		if track.Type == "subtitles" {
			if actionType == ActionAll || actionType == ActionSubtitle {
				if isInList(track.Properties.Language, cfg.SubtitleLanguages) {
					keepSubtitleIds = append(keepSubtitleIds, fmt.Sprintf("%d", track.ID))
				}
			}
		}
	}

	// Handle Audio: keep explicit list
	if actionType == ActionAll || actionType == ActionAudio {
		if len(keepAudioIds) > 0 {
			args = append(args, "--audio-tracks", strings.Join(keepAudioIds, ","))
		} else {
			// If no audio matches config, strict compliance means no audio.
			args = append(args, "--no-audio")
		}
	}

	// Handle Subtitles: keep explicit list
	if actionType == ActionAll || actionType == ActionSubtitle {
		if len(keepSubtitleIds) > 0 {
			args = append(args, "--subtitle-tracks", strings.Join(keepSubtitleIds, ","))
		} else {
			args = append(args, "--no-subtitles")
		}
	}

	for _, track := range info.Tracks {
		// Set Video Language
		if track.Type == "video" {
			if actionType == ActionAll || actionType == ActionAudio {
				targetLang, _ := determineTargetVideoLanguage(&track, &info, cfg.VideoLanguage)
				args = append(args, "--language", fmt.Sprintf("%d:%s", track.ID, targetLang))
			}
		}

		// Handle Audio Defaults
		if track.Type == "audio" {
			if actionType == ActionAll || actionType == ActionAudio {
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
	}

	args = append(args, inputFile)

	// D. Execute Remux
	remuxCmd := exec.CommandContext(ctx, "mkvmerge", args...)
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

	fmt.Printf("Success: %s -> %s\n", filepath.Base(inputFile), filepath.Base(outputFile))
	return outputFile, nil
}

// determineTargetVideoLanguage logic to handle "und"
// Returns (targetLanguage, needsFix)
func determineTargetVideoLanguage(videoTrack *Track, info *MkvInfo, preferredLang string) (string, bool) {
	current := videoTrack.Properties.Language

	// If it's already what we want, all good.
	if current == preferredLang {
		return preferredLang, false
	}

	// If it's specific language but NOT preferred, we definitely need fix (to force it to preferred)
	// UNLESS it's "und" or "unk", in which case we try to be smart.
	// Standard MKV uses "und". We'll handle "unk" just in case.
	if current != "und" && current != "unk" {
		return preferredLang, true // Force change to preferred
	}

	// It IS "und".
	// Strategy:
	// 1. If we have an audio track that is preferredLang, assume video is also that.
	// 2. Else, assume video is same as FIRST audio track found (likely the main one).
	// 3. Else (no audio?), keep as und.

	hasPreferredAudio := false
	var firstAudioLang string

	for _, t := range info.Tracks {
		if t.Type == "audio" {
			if firstAudioLang == "" {
				firstAudioLang = t.Properties.Language
			}
			if t.Properties.Language == preferredLang {
				hasPreferredAudio = true
			}
		}
	}

	if hasPreferredAudio {
		return preferredLang, true
	}

	if firstAudioLang != "" {
		return firstAudioLang, true
	}

	// Fallback: stay und
	return "und", false
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

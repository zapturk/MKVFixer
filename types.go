package main

// Config holds the user preferences
type Config struct {
	VideoLanguage     string   `json:"video_language"`
	AudioLanguages    []string `json:"audio_languages"`
	DefaultAudio      string   `json:"default_audio_language"`
	SubtitleLanguages []string `json:"subtitle_languages"`
}

// Structures for parsing mkvmerge JSON
type TrackProperties struct {
	Language     string `json:"language"`
	DefaultTrack bool   `json:"default_track"`
}

type Track struct {
	ID         int             `json:"id"`
	Type       string          `json:"type"`
	Properties TrackProperties `json:"properties"`
}

type MkvInfo struct {
	Tracks []Track `json:"tracks"`
}

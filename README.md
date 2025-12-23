# MKVFixer

MKVFixer is a high-performance, concurrent CLI tool designed to batch process MKV files. It standardizes video, audio, and subtitle tracks according to a user-defined configuration, ensuring your media library is consistent.

## Features

- **Batch Processing**: Scans directories for `.mkv` files and processes them automatically.
- **Recursive Scanning**: Optionally traverse subdirectories (`-r`, `--recursive`).
- **Concurrent Processing**: Uses worker pools to process multiple files simultaneously for faster execution (`-n`, `--workers`).
- **Smart Remediation**: Inspects files first and only remuxes if they don't meet the configuration criteria.
- **Caching**: Maintains a local `.mkvfixer.cache` file to track processed files and skip them in future runs, significantly speeding up subsequent executions.
- **Strict Compliance**:
  - Removes audio and subtitle languages not in your allowlist.
  - Enforces the correct "Default" track flag for your preferred audio language.
  - Sets the video track language.

## Dependencies

MKVFixer relies on `mkvmerge`, which is part of the [MKVToolNix](https://mkvtoolnix.download/) suite.

### macOS
```bash
brew install mkvtoolnix
```

### Linux (Ubuntu/Debian)
```bash
sudo apt-get install mkvtoolnix
```

## Installation

Clone the repository and build the binary:

```bash
git clone https://github.com/zapturk/MKVFixer.git
cd MKVFixer
go build -o mkvfixer
```

## Configuration

Create a `config.json` file in your working directory (or specify one with `--config`).

**Example `config.json`:**
```json
{
    "video_language": "eng",
    "audio_languages": ["eng", "jpn"],
    "default_audio_language": "eng",
    "subtitle_languages": ["eng"]
}
```

- `video_language`: The language code to enforce for the video track.
- `audio_languages`: A list of audio languages to keep. All others will be removed.
- `default_audio_language`: The audio language that should be flagged as "Default".
- `subtitle_languages`: A list of subtitle languages to keep. All others will be removed.

## Usage

```bash
./mkvfixer [global options] [command] [directory]
```

### Commands

- `all`: Process both audio and subtitles (default if no command specified).
- `audio`: Process only audio tracks.
- `subtitle`: Process only subtitle tracks.

If no directory is specified, it runs in the current directory.

### Options

- `-r`, `--recursive`: Recursively process subdirectories.
- `-n`, `--workers`: Number of concurrent workers (default: 4).
- `-c`, `--check-only`: Only check files and populate cache for compliant ones, do not remux/modify files.
- `--config`: Path to configuration file (default: `config.json`).
- `-h`, `--help`: Show help message.

### Examples

**Standard run in current directory:**
```bash
./mkvfixer
```

**Recursive run with 8 workers:**
```bash
./mkvfixer -r -n 8 /path/to/media/library
```

**Using a custom config file:**
```bash
./mkvfixer --config my_config.json /path/to/media/library
```

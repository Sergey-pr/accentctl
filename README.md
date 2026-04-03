# accentctl

A CLI tool for [Accent](https://www.accent.reviews/) the open-source translation management platform.

## Install

**Homebrew**
```sh
brew install sergey-pr/tap/accentctl
```

**Go**
```sh
go install github.com/sergey-pr/accentctl@latest
```

**Download** grab a pre-built binary from the [releases page](https://github.com/sergey-pr/accentctl/releases).

## Configuration

Create an `accent.json` file in your project root (or run `accentctl init`):

```json
{
  "apiUrl": "https://your.accent.instance",
  "apiKey": "your-api-key",
  "files": [
    {
      "language": "en",
      "format": "json",
      "source": "localization/en/*.json",
      "target": "localization/%slug%/%original_file_name%",
      "hooks": {
        "afterExport": ["prettier --write localization/"]
      }
    }
  ]
}
```

YAML and TOML are also supported (`accent.yaml`, `accent.toml`).

### Keeping your API key out of version control

Run this once per project it saves the key to `accent.local.json` which is gitignored:

```sh
accentctl key set your-api-key
```

You can then commit `accent.json` without any secrets. The local file overrides `accent.json` values, so you can omit `apiKey` from the committed config entirely.

**Environment variables** override both config files:
- `ACCENT_API_KEY`
- `ACCENT_API_URL`

### Target template placeholders

| Placeholder | Description |
|---|---|
| `%slug%` | Language slug (e.g. `fr`, `de`) |
| `%original_file_name%` | Source filename with extension |

### Hooks

Run shell commands before or after each operation:

- `beforeSync` / `afterSync`
- `beforeExport` / `afterExport`
- `beforeAddTranslations` / `afterAddTranslations`

## Commands

### `accentctl export`

Download translations from Accent and write them to your local filesystem.

```sh
accentctl export
accentctl export --order-by key
```

### `accentctl sync`

Upload your source files to Accent. By default runs a **dry-run** showing what would change.

```sh
accentctl sync                    # dry-run (peek)
accentctl sync --write            # apply + export updated files
accentctl sync --write --add-translations
accentctl sync --write --add-translations --merge-type force
```

| Flag | Default | Description |
|---|---|---|
| `--write` | false | Apply changes and write exported files |
| `--add-translations` | false | Upload existing translations |
| `--sync-type` | `smart` | How Accent handles incoming keys (see below) |
| `--merge-type` | `smart` | How Accent merges uploaded translations (see below) |
| `--order-by` | `index` | Key order in exported files (see below) |

**`--sync-type`**

| Value | Behaviour |
|---|---|
| `smart` | New keys are added; keys missing from the upload are removed from Accent |
| `passive` | New keys are added; existing keys are never removed |

**`--merge-type`** (only with `--add-translations`)

| Value | Behaviour |
|---|---|
| `smart` | Uploaded translations are added for untranslated keys; already-translated keys are left untouched |
| `passive` | Uploaded translations are only added when there is no existing translation at all |
| `force` | Uploaded translations overwrite any existing translation |

**`--order-by`**

| Value | Behaviour |
|---|---|
| `index` | File insertion order (default) |
| `-index` | Reverse insertion order |
| `key` | Alphabetical ascending |
| `-key` | Alphabetical descending |
| `updated` | Last updated ascending |
| `-updated` | Last updated descending |


### `accentctl stats`

Display translation progress for all languages.

```sh
accentctl stats
```

### `accentctl init`

Create an `accent.json` config file.

```sh
accentctl init
```

## Improvements over accent-cli

`accentctl` is a Go rewrite of the official TypeScript [accent-cli](https://github.com/mirego/accent-cli). Key differences:

| Feature | accent-cli | accentctl |
|---|---|---|
| **Single binary** | Requires Node.js runtime | Zero dependencies, single static binary |
| **Install** | `npm install -g accent-cli` | `brew install` / `go install` / download |
| **Local API key** | Must put key in committed config | `accentctl key set <key>` writes `accent.local.json` (gitignored) |
| **`--verbose` flag** | No HTTP logging | `--verbose` / `-v` shows every request, status, and error body |
| **`key` ordering** | Delegated to server (not reliable) | Client-side recursive JSON sort — works for flat, nested, and colon/period keys |
| **Large file support** | 502 on large uploads | Automatically uploads only new keys in batches of 500 |
| **Language discovery** | Requires explicit language in config | Auto-discovers languages from the filesystem via target template |
| **`%document_path%` placeholder** | Supported | Supported |
| **Parallel operations** | Sequential | Parallel exports and syncs via `errgroup` |
| **Order-by on sync export** | Not exposed | `--order-by` applies to both `export` and the export step of `sync --write` |

## Shell completions

```sh
# bash
accentctl completion bash > /etc/bash_completion.d/accentctl

# zsh
accentctl completion zsh > "${fpath[1]}/_accentctl"

# fish
accentctl completion fish > ~/.config/fish/completions/accentctl.fish
```

## License

MIT see [LICENSE](LICENSE).

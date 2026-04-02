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
accentctl export --order-by key-asc
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
| `index` | Keys appear in the order they were created in Accent |
| `key-asc` | Keys are sorted alphabetically |

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

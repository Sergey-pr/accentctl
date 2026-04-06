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

Run this once per project to save the key to `accent.local.json` which should be gitignored:

```sh
accentctl key set your-api-key
```

You can commit `accent.json` without any secrets. The local file overrides `accent.json` values. The `init` command also prompts for an API key and saves it to `accent.local.json` automatically.

**Environment variables** override both config files:
- `ACCENT_API_KEY`
- `ACCENT_API_URL`

### Target template placeholders

| Placeholder            | Description                     |
|------------------------|---------------------------------|
| `%slug%`               | Language slug (e.g. `fr`, `de`) |
| `%original_file_name%` | Source filename with extension  |

### Hooks

Run shell commands before or after each operation:

- `beforeExport` / `afterExport`

## Commands

### `accentctl push`

Upload new source keys to Accent in chunks, then force-push translations for those new keys to all target languages, then pull updated files.

```sh
accentctl push
accentctl push --force
accentctl push --order-by key
```

| Flag         | Default | Description                                                                |
|--------------|---------|----------------------------------------------------------------------------|
| `--force`    | false   | Delete all server keys first, re-upload everything, force all translations |
| `--order-by` | `key`   | Key order in exported files (see below)                                    |

### `accentctl pull`

Download translations from Accent and write them to your local filesystem.

```sh
accentctl pull
accentctl pull --order-by -key
```

| Flag         | Default | Description                             |
|--------------|---------|-----------------------------------------|
| `--order-by` | `key`   | Key order in exported files (see below) |

### `accentctl cleanup`

Remove keys from Accent that are no longer present in your local source files.

```sh
accentctl cleanup
```

### `accentctl status`

Show how many keys need pushing or deleting for each language file, compared to the current Accent state.

```sh
accentctl status
```

### `accentctl init`

Interactively create an `accent.json` config file.

```sh
accentctl init
```

### `accentctl key set`

Save an API key to `accent.local.json` (gitignored).

```sh
accentctl key set your-api-key
```

---

**`--order-by` values**

| Value      | Behaviour               |
|------------|-------------------------|
| `index`    | File insertion order    |
| `-index`   | Reverse insertion order |
| `key`      | Alphabetical ascending  |
| `-key`     | Alphabetical descending |
| `updated`  | Last updated ascending  |
| `-updated` | Last updated descending |

## Improvements over accent-cli

`accentctl` is a Go rewrite of the official TypeScript [accent-cli](https://github.com/mirego/accent-cli). Key differences:

| Feature                | accent-cli                   | accentctl                                                      |
|------------------------|------------------------------|----------------------------------------------------------------|
| **Single binary**      | Requires Node.js runtime     | Zero dependencies, single static binary                        |
| **`key` ordering**     | Not working with nested keys | Client-side recursive JSON sort works for flat and nested keys |
| **Large file support** | OOM on large uploads         | Automatically uploads only new keys in batches                 |

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

# accentctl

A CLI tool for [Accent](https://www.accent.reviews/), the open-source translation management platform.

## Install

**Homebrew**
```sh
brew install sergey-pr/tap/accentctl
```

**Go**
```sh
go install github.com/sergey-pr/accentctl@latest
```

**Binary**: grab a pre-built one from the [releases page](https://github.com/sergey-pr/accentctl/releases).

## Configuration

Create an `accent.json` file in your project root, or run `accentctl init`:

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
        "afterPull": ["prettier --write localization/"]
      }
    }
  ]
}
```

The config file itself may also be YAML or TOML (`accent.yaml`, `accent.toml`).

You can run `accentctl` from any subdirectory of your project: it searches upward
for the config file and operates from that directory, so `source` and `target`
paths always resolve against the project root.

### A note on `format`

`format` is the format of your **localization** files, and it is passed through to
Accent on every request. Only `pull` is format-agnostic: it streams whatever Accent
returns straight to disk.

`sync`, `cleanup` and `status` **support `"json"` only**: they read your local files
with a JSON parser to diff keys against the server. Configuring any other format makes
those three fail immediately with a clear message rather than a confusing parse error.
If you need them for another format, please open an issue.

### Keeping your API key out of version control

Run this once per project to save the key to `accent.local.json` (mode `0600`), which
should be gitignored:

```sh
accentctl key set --stdin < key.txt
```

Prefer `--stdin`: passing the key as an argument (`accentctl key set your-api-key`)
leaves the secret in your shell history and in the process list. That form still works
for convenience.

You can commit `accent.json` without any secrets. Values in the local file override
`accent.json`. The `init` command also prompts for an API key and saves it to
`accent.local.json` automatically.

**Environment variables** override both config files:
- `ACCENT_API_KEY`
- `ACCENT_API_URL`
- `ACCENT_REQUEST_DELAY`

### Request throttling

accentctl pauses between API requests so a large sync cannot hammer your Accent
instance. The default is `1.5s`; set `requestDelay` to tune it:

```json
{
  "requestDelay": "500ms"
}
```

The value is a duration string — `"0s"` disables the pause entirely. A number
without units is read as nanoseconds and rejected. Commands that export many
documents (`status`, `pull`) spend most of their time in this pause, so lowering
it speeds them up considerably — check what your instance tolerates first.

### Target template placeholders

| Placeholder            | Description                        |
|------------------------|------------------------------------|
| `%slug%`               | Language slug (e.g. `fr`, `de`)    |
| `%original_file_name%` | Source filename with extension     |
| `%document_path%`      | Source filename without extension  |

### Hooks

Shell commands that run around `accentctl sync` or `accentctl pull`, defined per file
entry in the config. Hooks run through `sh -c` on macOS and Linux, and through
`cmd /c` on Windows. They run in sequence, and the first one to exit non-zero aborts
the command.

> **Hooks run arbitrary shell commands from `accent.json`.** That is the point of the
> feature, but it means the config is executable content: treat an `accent.json` from an
> untrusted repository the same way you would treat its `Makefile` or npm `postinstall`
> script, and read it before running `accentctl` inside that project.

> **On Windows, avoid double quotes inside a hook.** `cmd /c` uses different quoting
> rules from the ones Go applies when building the command line, so a hook like
> `prettier --write "loc/**/*.json"` reaches the program with the quotes escaped as
> `\"`. Unquoted hooks (`prettier --write loc/`) and hooks that delegate to a script or
> task runner (`npm run format`) work fine.

| Hook          | Command  | When it runs                                  |
|---------------|----------|-----------------------------------------------|
| `beforeSync`  | `sync`   | Before uploading source keys                  |
| `afterSync`   | `sync`   | After all files are pulled at the end of sync |
| `beforePull`  | `pull`   | Before downloading files from Accent          |
| `afterPull`   | `pull`   | After all files for the entry are written     |

```json
{
  "hooks": {
    "beforeSync": [
      "echo starting sync"
    ],
    "afterSync": [
      "echo finished sync"
    ],
    "beforePull": [
      "echo starting pull"
    ],
    "afterPull": [
      "echo finished pull"
    ]
  }
}
```

## Commands

Global flags, available on every command:

| Flag              | Description                                     |
|-------------------|-------------------------------------------------|
| `--verbose`, `-v` | Log HTTP requests and responses                 |
| `--version`       | Print the version (releases only; `dev` if built from source) |

### `accentctl sync`

Uploads new source keys to Accent in chunks, force-pushes translations for those
new keys to all target languages, then pulls the updated files back.

```sh
accentctl sync
accentctl sync --force
accentctl sync --translations-only
accentctl sync --order-by key
```

| Flag                  | Default | Description                                                                      |
|-----------------------|---------|----------------------------------------------------------------------------------|
| `--force`             | false   | Deletes all server keys first, re-uploads everything, forces all translations    |
| `--translations-only` | false   | Pushes local translations with a passive merge; uploads no keys, deletes nothing |
| `--yes`               | false   | Skips the `--force` confirmation prompt (for non-interactive use)                |
| `--order-by`          | `key`   | Key order in exported files                                                      |

**`--order-by` values** (shared with `pull`)

| Value      | Behaviour               |
|------------|-------------------------|
| `index`    | File insertion order    |
| `-index`   | Reverse insertion order |
| `key`      | Alphabetical ascending  |
| `-key`     | Alphabetical descending |
| `updated`  | Last updated ascending  |
| `-updated` | Last updated descending |

#### Recovering an interrupted sync

`sync` uploads keys first and pushes their translations second. If it dies between
those two phases, the keys exist on Accent but their translations were never sent.
Re-running `sync` does not fix this: Accent creates the new key in every language
as soon as it is synced, so the new-key diff finds nothing left to do.

Run this instead:

```sh
accentctl sync --translations-only
```

It uploads no keys and deletes nothing. It pushes every local translation with a
passive merge, which fills only the strings no reviewer has corrected in Accent,
so it is safe to re-run and will not overwrite work done in the web UI.

> **Run it before any `sync` or `pull`.** Both end by pulling the server's copy over
> your local files, and an untranslated key comes back holding the *source* text,
> which overwrites the local translation that `--translations-only` needs to push.
> If that already happened, recover the local files from version control first.

### `accentctl pull`

Downloads translations from Accent and writes them to your local filesystem.

```sh
accentctl pull
accentctl pull --order-by -key
```

| Flag         | Default | Description                                    |
|--------------|---------|------------------------------------------------|
| `--order-by` | `key`   | Key order in exported files (values as `sync`) |

Languages are discovered from your local filesystem: `pull` only fetches a language
that already has a matching local file. To start tracking a language added on the
Accent side, create its file first, e.g. an empty `{}` at `localization/de/app.json`.

### `accentctl cleanup`

Removes keys from Accent that are no longer present in your local source files,
then pulls the updated files back down.

```sh
accentctl cleanup
accentctl cleanup --order-by index
```

| Flag         | Default | Description                                            |
|--------------|---------|--------------------------------------------------------|
| `--order-by` | `key`   | Key order in the files pulled afterwards (as `sync`)   |

### `accentctl status`

Shows how many keys need pushing or deleting for each language file, compared to the current Accent state.

```sh
accentctl status
```

### `accentctl init`

Interactively creates an `accent.json` config file.

```sh
accentctl init
```

### `accentctl key set`

Saves an API key to `accent.local.json` with mode `0600`.

```sh
accentctl key set --stdin < key.txt
echo your-api-key | accentctl key set --stdin
accentctl key set your-api-key
```

| Flag      | Default | Description                                             |
|-----------|---------|---------------------------------------------------------|
| `--stdin` | false   | Read the key from stdin, keeping it out of shell history |

## Improvements over accent-cli

`accentctl` is a Go rewrite of the official [accent-cli](https://github.com/mirego/accent-cli). Key differences:

| Feature                   | accent-cli                                                            | accentctl                                                      |
|---------------------------|-----------------------------------------------------------------------|----------------------------------------------------------------|
| **Single binary**         | Requires Node.js runtime                                              | Zero dependencies, single static binary                        |
| **`key` ordering**        | Not working with nested keys                                          | Client-side recursive JSON sort works for flat and nested keys |
| **Large file support**    | Uploads translations in one batch (can cause memory issues on server) | Uploads translations in chunks                                 |
| **Environment variables** | API key and host live in the config file only                         | `ACCENT_API_KEY` / `ACCENT_API_URL` override the config        |

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

MIT. See [LICENSE](LICENSE).

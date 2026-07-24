package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/sergey-pr/accentctl/internal/api"
	"github.com/sergey-pr/accentctl/internal/config"
	"github.com/sergey-pr/accentctl/internal/constants"
	"github.com/sergey-pr/accentctl/internal/helpers"
	"github.com/sergey-pr/accentctl/internal/output"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Add new keys to Accent and force their translations",
	Long: `Uploads new source keys in chunks and force-pushes translations for
those new keys to all target languages.

With --force: uploads all source keys and force-pushes all translations
for all languages.

With --translations-only: uploads no source keys and deletes nothing. It
pushes every local translation with a passive merge, which fills only the
strings no reviewer has corrected in Accent. Use it to recover when a sync
was interrupted after its keys were uploaded but before their translations
were pushed.`,
	Example: `  accentctl sync
  accentctl sync --force
  accentctl sync --translations-only
  accentctl sync --order-by key`,
	RunE: runSync,
}

var (
	syncOrderBy          string
	syncForce            bool
	syncYes              bool
	syncTranslationsOnly bool
)

func init() {
	syncCmd.Flags().StringVar(&syncOrderBy,
		"order-by", "key", "Order of pulled keys: index, -index, key, -key, updated, -updated")
	syncCmd.Flags().BoolVar(&syncForce,
		"force", false, "Upload all source keys and force all translations for all languages")
	syncCmd.Flags().BoolVar(&syncYes,
		"yes", false, "Skip the --force confirmation prompt (for non-interactive use)")
	syncCmd.Flags().BoolVar(&syncTranslationsOnly,
		"translations-only", false, "Push local translations with a passive merge without uploading keys or deleting anything")
}

func runSync(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if err := requireJSONFormat(cfg, "sync"); err != nil {
		return err
	}

	if syncForce && syncTranslationsOnly {
		return fmt.Errorf("--force and --translations-only are mutually exclusive: --force re-uploads and overwrites everything, --translations-only changes no keys and overwrites no reviewed translations")
	}

	if syncForce {
		if err := confirmForceSync(cfg); err != nil {
			return err
		}
	}

	client := newClient(cfg)

	for _, file := range cfg.Files {
		if err := runHooks(file.Hooks.BeforeSync); err != nil {
			return fmt.Errorf("beforeSync hook failed: %w", err)
		}
	}

	if syncTranslationsOnly {
		return runSyncTranslationsOnly(client, cfg)
	}

	progress, err := runFullSync(client, cfg)
	if err != nil && progress.needsRecovery() {
		return fmt.Errorf("%w\n\n%s", err, syncRecoveryHint)
	}
	return err
}

// Re-running sync is the instinctive reaction to a failure and the destructive
// one: its pull phase overwrites the local translations the recovery needs.
const syncRecoveryHint = `Keys were uploaded but their translations were not all pushed.
Run 'accentctl sync --translations-only' to finish the job.
Do not run sync or pull first: both end by pulling the server's copy over your
local files, and a key that is untranslated on the server comes back holding the
source text, overwriting the local translation that the recovery needs.`

// syncProgress records how far a sync got. Keys on the server without their
// translations is the state that needs recovery.
type syncProgress struct {
	keysUploaded       bool
	translationsPushed bool
}

func (p syncProgress) needsRecovery() bool {
	return p.keysUploaded && !p.translationsPushed
}

func runFullSync(client *api.Client, cfg *config.Config) (syncProgress, error) {
	var progress syncProgress

	output.Section("Syncing files")

	type fileNewKeys struct {
		file   config.File
		keySet map[string]bool
	}
	var results []fileNewKeys

	for _, file := range cfg.Files {
		sources, err := file.Sources()
		if err != nil {
			return progress, err
		}

		keySet := map[string]bool{}
		for _, src := range sources {
			documentPath := helpers.DocumentName(src)
			language := helpers.SourceLanguage(file, src)

			if syncForce {
				if err := deleteAllKeysChunked(client, src, documentPath, file.Format, language); err != nil {
					return progress, err
				}
			}

			newNodes, uploaded, err := syncFileChunked(client, src, documentPath, file.Format, language, syncOrderBy, syncForce)
			if uploaded {
				progress.keysUploaded = true
			}
			if err != nil {
				return progress, err
			}
			for _, n := range newNodes {
				keySet[helpers.NodeKey(n.Path)] = true
			}
		}
		results = append(results, fileNewKeys{file, keySet})
	}

	output.Section("Adding translations")
	for _, r := range results {
		if syncForce {
			if err := helpers.AddAllTranslations(client, r.file, "force", verbose); err != nil {
				return progress, err
			}
		} else if len(r.keySet) > 0 {
			if err := helpers.AddTranslationsForNewKeys(client, r.file, r.keySet, verbose); err != nil {
				return progress, err
			}
		}
	}
	progress.translationsPushed = true

	return progress, finishSync(client, cfg)
}

// runSyncTranslationsOnly recovers a sync that died between uploading keys and
// pushing translations; the passive merge keeps reviewer-corrected strings, so it is safe to re-run.
func runSyncTranslationsOnly(client *api.Client, cfg *config.Config) error {
	output.Section("Adding translations")
	for _, file := range cfg.Files {
		if err := helpers.AddAllTranslations(client, file, "passive", verbose); err != nil {
			return err
		}
	}
	return finishSync(client, cfg)
}

// finishSync pulls the server's copy over local files and runs afterSync hooks.
func finishSync(client *api.Client, cfg *config.Config) error {
	output.Section("Pulling updated files")
	for _, file := range cfg.Files {
		if err := pullFile(client, file, syncOrderBy); err != nil {
			return err
		}
	}

	for _, file := range cfg.Files {
		if err := runHooks(file.Hooks.AfterSync); err != nil {
			return fmt.Errorf("afterSync hook failed: %w", err)
		}
	}
	return nil
}

// confirmForceSync gates the destructive --force path: it wants --yes or an
// interactive "yes", and refuses to run on a non-TTY without --yes.
func confirmForceSync(cfg *config.Config) error {
	docCount := 0
	for _, file := range cfg.Files {
		sources, err := file.Sources()
		if err != nil {
			return err
		}
		docCount += len(sources)
	}

	output.Warn(fmt.Sprintf("sync --force deletes ALL keys for %d document(s) on %s before re-uploading.", docCount, cfg.APIURL))

	if syncYes {
		return nil
	}

	if !isatty.IsTerminal(os.Stdin.Fd()) && !isatty.IsCygwinTerminal(os.Stdin.Fd()) {
		return fmt.Errorf("refusing to run destructive --force without confirmation; re-run with --yes to proceed in a non-interactive environment")
	}

	fmt.Print("Type 'yes' to continue: ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return fmt.Errorf("reading confirmation: %w", err)
	}
	if answer := strings.TrimSpace(strings.ToLower(line)); answer != "y" && answer != "yes" {
		return fmt.Errorf("aborted")
	}
	return nil
}

// deleteAllKeysChunked wipes a document by uploading ever-smaller files via
// smart sync, ending with an empty object that clears the last keys.
func deleteAllKeysChunked(client *api.Client, src, documentPath, format, language string) error {
	existingData, err := serverExport(client, documentPath, format, language)
	if err != nil {
		return fmt.Errorf("%s: could not fetch existing keys: %w", src, err)
	}

	allNodes := helpers.ServerNodes(existingData)
	if len(allNodes) == 0 {
		output.Info(fmt.Sprintf("%s: no keys on server", src))
		return nil
	}

	total := len(allNodes)
	nChunks := (total + constants.ChunkSize - 1) / constants.ChunkSize
	output.Info(fmt.Sprintf("%s: deleting %d keys in %d chunk(s)", src, total, nChunks))

	opts := api.SyncOptions{SyncType: "smart"}
	for chunk := 1; chunk <= nChunks; chunk++ {
		remaining := allNodes[min(chunk*constants.ChunkSize, total):]
		tmpName, err := helpers.WriteNodesTempFile(remaining, "accentctl-del-*.json")
		if err != nil {
			return fmt.Errorf("%s: %w", src, err)
		}
		if verbose {
			output.Info(fmt.Sprintf("chunk %d/%d: %s", chunk, nChunks, tmpName))
		}
		err = syncChunk(client, src, documentPath, format, language, tmpName, chunk, nChunks, opts)
		_ = os.Remove(tmpName)
		if err != nil {
			return err
		}
	}
	return nil
}

// syncFileChunked uploads keys missing from the server in cumulative chunks;
// force treats every local key as new. uploaded stays true next to an error so the caller knows keys already landed.
func syncFileChunked(client *api.Client, src, documentPath, format, language, orderBy string, force bool) (newNodes []helpers.NodeEntry, uploaded bool, err error) {
	var existing []byte
	if !force {
		existing, err = serverExport(client, documentPath, format, language)
		if err != nil {
			return nil, false, fmt.Errorf("%s: could not fetch existing keys: %w", src, err)
		}
	}

	chunks, newNodes, err := helpers.NewKeysChunksWithNodes(src, existing, constants.ChunkSize)
	if err != nil {
		return nil, false, fmt.Errorf("%s: chunking failed: %w", src, err)
	}

	defer func() {
		for _, p := range chunks {
			if p != src {
				_ = os.Remove(p)
			}
		}
	}()

	if len(chunks) == 0 {
		output.Info(fmt.Sprintf("%s: no new keys", src))
		return nil, false, nil
	}

	output.Info(fmt.Sprintf("%s: %d keys -> %d chunk(s)", src, len(newNodes), len(chunks)))

	opts := api.SyncOptions{SyncType: "passive", OrderBy: orderBy}

	output.Section(fmt.Sprintf("Syncing %s - %d chunk(s)", src, len(chunks)))
	for i, chunk := range chunks {
		if verbose {
			output.Info(fmt.Sprintf("chunk %d/%d: %s", i+1, len(chunks), chunk))
		}
		err := client.Sync(chunk, documentPath, format, language, opts)
		if err != nil {
			return nil, uploaded, fmt.Errorf("%s chunk %d/%d: %w", src, i+1, len(chunks), err)
		}
		uploaded = true
		if verbose {
			output.FileSync(fmt.Sprintf("%s [chunk %d/%d]", src, i+1, len(chunks)))
		} else {
			output.ChunkProgress(src, i+1, len(chunks))
		}
	}
	return newNodes, uploaded, nil
}

// Copyright 2026 Adam Harris. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"contentful-pp-cli/internal/store"
	"github.com/spf13/cobra"
)

func newImagesCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "images",
		Short: "Build images.ctfassets.net URLs for assets in the local mirror",
	}
	cmd.AddCommand(newImagesURLCmd(flags))
	return cmd
}

type imageURLOpts struct {
	width   int
	height  int
	fit     string
	format  string
	quality int
}

func newImagesURLCmd(flags *rootFlags) *cobra.Command {
	var opts imageURLOpts
	var bulk, srcset, dbPath string

	cmd := &cobra.Command{
		Use:   "url <asset-id>",
		Short: "Build an images.ctfassets.net URL with transforms",
		Long: `Build an images.ctfassets.net URL for an asset, applying any of the standard
transform parameters (--width/--height/--fit/--format/--quality).

Bulk mode: --bulk @file.txt reads asset ids from a newline-delimited file,
emitting one URL per line.

Srcset mode: --srcset 320,640,1280 emits a <picture>/<source> HTML block with
one URL per requested width.`,
		Example:     "  contentful-pp-cli images url 5K8aXghc8eUqYKGm0IqsAU --width 1200 --fit fill\n  contentful-pp-cli images url 5K8aXghc8eUqYKGm0IqsAU --srcset 320,640,1280\n  contentful-pp-cli images url --bulk @asset-ids.txt --format webp",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && bulk == "" {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}
			if err := validateImageOpts(opts); err != nil {
				return err
			}

			path := dbPath
			if path == "" {
				path = defaultDBPath("contentful-pp-cli")
			}
			if !dbExists(path) {
				return fmt.Errorf("local store %s not found — run 'contentful-pp-cli sync' first", path)
			}
			st, err := store.OpenReadOnly(path)
			if err != nil {
				return fmt.Errorf("opening local store at %s: %w", path, err)
			}
			defer st.Close()

			var ids []string
			if bulk != "" {
				idList, err := readIDFile(bulk)
				if err != nil {
					return err
				}
				ids = idList
			} else {
				ids = []string{args[0]}
			}

			var widths []int
			if srcset != "" {
				for _, w := range strings.Split(srcset, ",") {
					w = strings.TrimSpace(w)
					if w == "" {
						continue
					}
					n, err := strconv.Atoi(w)
					if err != nil {
						return fmt.Errorf("invalid --srcset width %q", w)
					}
					widths = append(widths, n)
				}
			}

			type assetURL struct {
				ID     string   `json:"id"`
				URL    string   `json:"url,omitempty"`
				Srcset []string `json:"srcset,omitempty"`
				Error  string   `json:"error,omitempty"`
			}
			var results []assetURL
			for _, id := range ids {
				meta, err := lookupAssetMeta(st, id)
				if err != nil {
					results = append(results, assetURL{ID: id, Error: err.Error()})
					continue
				}
				if len(widths) > 0 {
					var urls []string
					for _, w := range widths {
						copy := opts
						copy.width = w
						urls = append(urls, buildAssetURL(meta, copy))
					}
					results = append(results, assetURL{ID: id, Srcset: urls})
				} else {
					results = append(results, assetURL{ID: id, URL: buildAssetURL(meta, opts)})
				}
			}

			if flags.asJSON || flags.csv {
				return printJSONFiltered(cmd.OutOrStdout(), results, flags)
			}

			// Plaintext output: bulk → one URL per line; single → URL or HTML block.
			anyError := false
			for _, r := range results {
				if r.Error != "" {
					fmt.Fprintf(cmd.ErrOrStderr(), "%s: %s\n", r.ID, r.Error)
					anyError = true
					continue
				}
				if len(r.Srcset) > 0 {
					fmt.Fprintln(cmd.OutOrStdout(), renderPictureBlock(r.ID, r.Srcset, widths))
					continue
				}
				fmt.Fprintln(cmd.OutOrStdout(), r.URL)
			}
			// Single-id mode with a single error: typed not-found exit so
			// agents and shells can distinguish "asset missing" from a
			// successful empty bulk run.
			if len(ids) == 1 && anyError {
				return notFoundErr(fmt.Errorf("asset %s not found in local mirror — run 'contentful-pp-cli sync' or check the asset ID", ids[0]))
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&opts.width, "width", 0, "Image width (?w=)")
	cmd.Flags().IntVar(&opts.height, "height", 0, "Image height (?h=)")
	cmd.Flags().StringVar(&opts.fit, "fit", "", "Fit mode: pad, fill, scale, crop, thumb")
	cmd.Flags().StringVar(&opts.format, "format", "", "Output format: jpg, png, webp, avif, gif")
	cmd.Flags().IntVar(&opts.quality, "quality", 0, "Output quality 1-100 (?q=)")
	cmd.Flags().StringVar(&bulk, "bulk", "", "File of asset ids (prefix with @)")
	cmd.Flags().StringVar(&srcset, "srcset", "", "Comma-separated widths to build a <picture>/<source> block")
	cmd.Flags().StringVar(&dbPath, "db", "", "Override local DB path")
	return cmd
}

func validateImageOpts(opts imageURLOpts) error {
	if opts.fit != "" {
		switch opts.fit {
		case "pad", "fill", "scale", "crop", "thumb":
		default:
			return usageErr(fmt.Errorf("invalid --fit %q (allowed: pad, fill, scale, crop, thumb)", opts.fit))
		}
	}
	if opts.format != "" {
		switch opts.format {
		case "jpg", "png", "webp", "avif", "gif":
		default:
			return usageErr(fmt.Errorf("invalid --format %q (allowed: jpg, png, webp, avif, gif)", opts.format))
		}
	}
	if opts.quality != 0 && (opts.quality < 1 || opts.quality > 100) {
		return usageErr(fmt.Errorf("--quality must be 1-100"))
	}
	return nil
}

type imgAssetMeta struct {
	SpaceID  string
	AssetID  string
	Token    string // the per-asset random token in the URL
	FileName string
}

func lookupAssetMeta(st *store.Store, id string) (imgAssetMeta, error) {
	for _, table := range []string{"assets", "delivery_assets"} {
		rows, err := st.Query(fmt.Sprintf(`SELECT data FROM %s WHERE id = ?`, table), id)
		if err != nil {
			continue
		}
		for rows.Next() {
			var raw []byte
			if err := rows.Scan(&raw); err != nil {
				continue
			}
			rows.Close()
			return parseAssetURL(raw, id)
		}
		rows.Close()
	}
	return imgAssetMeta{}, fmt.Errorf("asset %s not found in local mirror", id)
}

// parseAssetURL extracts space/asset/token/filename from the canonical
// images.ctfassets.net URL embedded in fields.file.<locale>.url. Contentful's
// public URL pattern is //images.ctfassets.net/<space>/<assetID>/<token>/<fileName>.
func parseAssetURL(raw []byte, assetID string) (imgAssetMeta, error) {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return imgAssetMeta{}, err
	}
	sys, _ := obj["sys"].(map[string]any)
	space := ""
	if sp, ok := sys["space"].(map[string]any); ok {
		if sysSp, ok := sp["sys"].(map[string]any); ok {
			space = stringField(sysSp, "id")
		}
	}
	fields, _ := obj["fields"].(map[string]any)
	file, _ := fields["file"].(map[string]any)
	for _, locVal := range file {
		f, ok := locVal.(map[string]any)
		if !ok {
			continue
		}
		urlStr, _ := f["url"].(string)
		fileName, _ := f["fileName"].(string)
		if urlStr == "" {
			continue
		}
		// urlStr like //images.ctfassets.net/<space>/<assetID>/<token>/<fileName>
		clean := strings.TrimPrefix(urlStr, "https:")
		clean = strings.TrimPrefix(clean, "//")
		parts := strings.Split(clean, "/")
		// host, space, asset, token, filename...
		if len(parts) < 5 {
			continue
		}
		return imgAssetMeta{
			SpaceID:  parts[1],
			AssetID:  parts[2],
			Token:    parts[3],
			FileName: fileName,
		}, nil
	}
	if space != "" {
		// We have the space but no token; we can't build a usable URL without
		// the token, so the caller treats this as an error.
		return imgAssetMeta{}, fmt.Errorf("asset %s has no file URL (still processing or unpublished)", assetID)
	}
	return imgAssetMeta{}, fmt.Errorf("asset %s has no usable file metadata", assetID)
}

func buildAssetURL(m imgAssetMeta, opts imageURLOpts) string {
	base := fmt.Sprintf("https://images.ctfassets.net/%s/%s/%s/%s", m.SpaceID, m.AssetID, m.Token, m.FileName)
	q := url.Values{}
	if opts.width > 0 {
		q.Set("w", strconv.Itoa(opts.width))
	}
	if opts.height > 0 {
		q.Set("h", strconv.Itoa(opts.height))
	}
	if opts.fit != "" {
		q.Set("fit", opts.fit)
	}
	if opts.format != "" {
		q.Set("fm", opts.format)
	}
	if opts.quality > 0 {
		q.Set("q", strconv.Itoa(opts.quality))
	}
	if len(q) == 0 {
		return base
	}
	return base + "?" + q.Encode()
}

func renderPictureBlock(id string, urls []string, widths []int) string {
	var b strings.Builder
	b.WriteString("<picture>\n")
	b.WriteString("  <source srcset=\"")
	for i, u := range urls {
		if i > 0 {
			b.WriteString(", ")
		}
		w := widths[i]
		b.WriteString(fmt.Sprintf("%s %dw", u, w))
	}
	b.WriteString("\">\n")
	if len(urls) > 0 {
		b.WriteString(fmt.Sprintf("  <img src=%q alt=%q>\n", urls[len(urls)-1], id))
	}
	b.WriteString("</picture>")
	return b.String()
}

func readIDFile(spec string) ([]string, error) {
	path := strings.TrimPrefix(spec, "@")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			ids = append(ids, line)
		}
	}
	return ids, nil
}

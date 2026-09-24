# kodi-metadata-tmdb-cli

[English](README.md) · [简体中文](README.zh-CN.md)

## Origin and major changes

This project is a modified and refactored version of [fengqi/kodi-metadata-tmdb-cli](https://github.com/fengqi/kodi-metadata-tmdb-cli). Thanks to the original author and contributors. The original [GPL-3.0 license](LICENSE) is retained; this repository maintains an extended version of that project.

Major changes:

- Separate metadata providers, model judgment and output; support the TMDb API and TheTVDB web scraping without a TheTVDB user API key.
- Add named LLM instances and dedicated scraper settings. A general LLM extracts file identity, while an OpenAI-compatible model or TypeSafe Jev judges candidates.
- Search sources in configured order and cross-check model-provided record hints against real search candidates. An exact source/type/record match skips model judgment; otherwise judge basic work candidates. A source failure advances to the next source; only exhaustion of website sources and input-only AI description ends metadata acquisition in failure.
- Target Kodi, Jellyfin, Emby and Silo Server with unified NFO output, correcting runtime, episode coordinates, website identities, artwork and disc-folder paths. Reader contracts were checked against official documentation and source code; actual server imports have not been certified.
- Limit the scope to movies and TV shows; remove music-video processing, ffmpeg/ffprobe, Kodi JSON-RPC control and unused rule-based parsing.
- Accept exactly one media directory through `--path`, process it once and exit. Discover mixed media without preset movie/show roots; the general LLM classifies and extracts in batches of at most 50 files, then the program builds movie and whole-show tasks.
- Fetch the episodes needed by each show task in source batches. If all applicable website sources fail to produce usable metadata, describe local titles using only directory and filename text and program-generated local identities.
- Keep explicit directory, keyword and temporary-suffix filters, DVD/Blu-ray handling, manual source and episode constraints, fact caching, atomic writes, error propagation, proxy isolation and connection deadlines.

Movie and TV processing uses one fixed workflow:

**Single-directory discovery → batched general-LLM classification → movie or whole-show task → real basic search candidates → exact identity cross-check or configured judgment → selected work and episode details → NFO and artwork.** After all applicable website sources fail to produce usable metadata, input-only AI description is attempted.

Only movies and TV shows are supported. TMDb supports movies and TV; TheTVDB currently supports TV shows and episodes through public HTML, without a TheTVDB API key.

Judgment requests omit absent manual references and preserve actual source and work constraints. Title clues may be translated names or aliases; matching does not require same-named fields to be textually identical.

## Named models and scraper settings

Define independent instances in `llms`. Each `name` is a unique reference for tasks, `type` is `openai` (OpenAI-compatible chat completions) or `jev` (TypeSafe System One), and `model` identifies the server-side model. Each instance owns its URL, credentials, proxy and timeout.

`scraper.extract_llm` references an `openai` instance; `scraper.select_llm` references an `openai` or `jev` instance. They may share one general instance or use independent services. Unselected instances are never called and do not need working credentials.

`scraper` also owns `providers`, `cache_hours`, `jev_match_threshold`, and `nfo_field.tag/genre`. Jev uses Noul match probability, not Choice confidence, with a default threshold of 0.8. Explicit Jev credentials take precedence over `TYPESAFE_API_KEY`. Temperature is allowed only on `openai` instances.

Duplicate names, missing references, unsupported types, Jev extraction and unknown fields are errors. `tmdb.retry_count`, formerly the number of network-failure retries, is removed and rejected because failed requests are not implicitly retried; orchestration advances to the next metadata method; retry backoff and retry statistics are removed with it. The old `ai`, `jev`, `metadata`, `kodi`, `ffmpeg`, music-video and unused naming-mode configuration is removed. Move NFO flags from `collector` to `scraper`. See [example.config.json](example.config.json) and [the complete design](docs/metadata-providers.md) for the new configuration.

## Retrieval and judgment

The general LLM classifies untyped discovered media in batches of at most 50 files and extracts title, original title/aliases, year, season, episode and episode title. Each file must be covered exactly once; invalid type, path, show grouping or missing coverage fails before source requests. It may provide `tmdb_id` (a TMDb movie or series record number for the identified media type) and `thetvdb_id` (a TheTVDB series record number), as positive integer strings based on explicit markers or model knowledge. These are unverified hints, remain empty when unknown, and must never identify people, seasons or episodes. Unknown season/episode values stay `null`; season zero means specials. The program groups validated TV files into one task per show before applying manual constraints.

Providers are processed in `scraper.providers` order. Unless a manual explicit work reference applies, each supported source performs complete paginated title/year searches even when the model supplied a record hint. A known year remains a required condition; the program does not automatically widen the search to omit it. A hint confirms a real candidate only when source, movie/show type and record number all match; this skips the configured judge. Unknown or unmatched hints are normal and leave the actual candidates for judgment. Searches deduplicate record references and reject incomplete, invalid or excessive candidate sets instead of truncating them.

The configured Jev or OpenAI-compatible judge receives only basic work evidence: title, original title, year, movie/show type and source record reference. It receives no complete file list, season/episode coordinates, groups, episode plot, actors or artwork catalogs. Each source with unconfirmed candidates is judged once, including a single candidate. Jev uses a Choice with `none` and a Noul match question per candidate; the selected candidate must reach `scraper.jev_match_threshold` (default `0.8`, configurable in `(0,1]`). Choice confidence is relative separability, not absolute match probability.

After a work is confirmed, the program retrieves the selected movie's full facts or the selected show's facts and required seasons/episodes, preserving batch limits, caching and necessary TheTVDB episode-detail requests. Unselected candidates do not trigger full detail or episode retrieval. Models never generate factual website metadata.

Empty searches, judgment rejection/low probability, and search/judgment/detail HTTP or protocol failures are logged with redacted causes, then the next applicable source is attempted. A source succeeds only after the complete required metadata is available. If all website sources fail, input-only AI description is attempted. Initial discovery/classification errors, cancellation, manual constraints and final artwork/NFO output errors remain terminal. Requests are not implicitly retried, and completed files are not rolled back.

If all applicable website sources fail to produce usable metadata, the general LLM's `DescribeLocal` task returns only input-supported titles, plots and genres; unknown facts remain empty. The program generates stable `local` object identifiers, never website record numbers or source URLs. If AI description also fails, the CLI exits nonzero. An explicit manual source or work constraint cannot be replaced by another source or local output.

## Manual constraints and cache

Use `.metadata/source.json` in a show directory, for example:

```json
{"provider":"thetvdb","kind":"show","id":"371065"}
```

Here `id` identifies the show at TheTVDB. A `slug` such as `26882341-show` can locate its page instead. A provider-only reference constrains searching to that source; a model hint cannot bypass the search. Movies use `.metadata/<full-video-filename>.source.json` with `kind: "movie"`.

Existing manual TMDb text files remain supported: movie file-specific or directory `id.txt`, show-root `id.txt`, video-directory `season.txt`, season/show `group.txt`, and `join.txt`. A manual explicit movie/show reference directly retrieves its basic work record for single-candidate judgment; model hints cannot override it or skip that judgment. Manual settings do not bypass extraction. Conflicts and missing manual records terminate the process. A join mapping only transforms known season/episode coordinates, never an unknown episode. TMDb group positions use the sorted episode list, including sparse Order values.

Normalized search results and factual details are cached under `.metadata/cache/`. Cache identity includes source configuration, language, record type and reference, group and episode coordinates. Search caching is isolated from earlier results that ignored or relaxed a known year. Detail and whole-show cache versions remain unchanged; whole-show results retain their separate `series-*` namespace. A provider's in-memory snapshot lasts only for one CLI invocation; `scraper.cache_hours` controls reuse across invocations through the disk cache, and `0` disables that disk cache. Extraction and work confirmation still run for each task; judgment is skipped only for an exact model-hint match against a real search candidate. Old response caches are not migrated or deleted. Keep manual source files when clearing response caches.

DVD/Blu-ray directories produce identical NFO content at the movie root and the disc index location. Movie and episode runtime uses source minutes; unknown runtime, unsupported language tags and source-wide show counts are omitted. Movie logos use `<basename>-logo.png` and retain their `clearlogo` NFO reference.

Each local artwork path gets one selected image, downloaded completely before replacement; a matching source URL avoids repeated downloads. NFO preserves verified source identities and does not substitute a show's identity for an episode's. Failed single-run processing exits nonzero.

The output targets Jellyfin, Emby, Silo Server and Kodi. This tool writes source metadata and downloads source artwork; it does not probe media streams or extract video frames. Actual imports into all four applications have not yet been verified.

Remove obsolete media-root and runtime fields from existing configurations. `collector.movies_dir` and `collector.shows_dir` formerly selected typed media roots; `--path` supplies the run directory and AI classifies its contents. `collector.run_mode`, `collector.watcher`, `collector.cron_scan`, `collector.cron_scan_boot` and `collector.cron_seconds` formerly selected background or scheduled execution; this CLI now runs once. `-mode` is rejected. Remove the obsolete `ffmpeg` configuration (external tool paths and process limit) and `collector.music_videos_dir` (music-video roots) as well. `collector` now contains only explicit filters.

## Run and build

Set Kodi sources to **Local information only**. The tool does not send Kodi JSON-RPC commands.

```sh
cp example.config.json config.json
# 运行前编辑数据源和所选模型凭据。
./kodi-tmdb-linux-amd64 -config config.json --path "/media/混合媒体库"
```

`--path` must appear exactly once and name an existing directory. Positional paths, repeated `--path` and `-mode` fail; there is no default current directory. `-config` selects the config file; `-version` and help remain available. Processing ends after this scan. The CLI exits nonzero if all metadata methods fail, a common execution prerequisite fails, or final output fails; previous successful files are not rolled back. Go 1.27+ is required for source builds:

```sh
go test ./...
go test -race ./...
go vet ./...
make linux-amd64
```

The merged baseline passed race tests, vet and platform builds. The ordered-continuation fix is merged and verified on master. The first three real samples passed for 60 videos, including Super.Science through input-only AI after both website candidate lists were rejected. The fourth sample stopped before website requests because episode coordinates were unknown. Overall acceptance of the ten res.txt shows and the res2.txt collection remains incomplete; see the [validation record](docs/validation-2026-09-24.md). Controlled tests do not establish real model accuracy or actual server import. See [Chinese usage](README.zh-CN.md), [design](docs/metadata-providers.md), and [audit boundaries](docs/provider-audit.md).

Sources: [TMDb](https://www.themoviedb.org/), [TheTVDB](https://thetvdb.com/), [TypeSafe API](https://docs.typesafe.ai/api). [GPL-3.0](LICENSE).

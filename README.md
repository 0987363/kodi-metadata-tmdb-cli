# kodi-metadata-tmdb-cli

[English](README.md) · [简体中文](README.zh-CN.md)

## Origin and major changes

This project is a modified and refactored version of [fengqi/kodi-metadata-tmdb-cli](https://github.com/fengqi/kodi-metadata-tmdb-cli). Thanks to the original author and contributors. The original [GPL-3.0 license](LICENSE) is retained; this repository maintains an extended version of that project.

Major changes:

- Separate metadata providers, model judgment and output; support the TMDb API and TheTVDB web scraping without a user API key.
- Add named LLM instances and dedicated scraper settings. A general LLM extracts file identity, while an OpenAI-compatible model or TypeSafe Jev judges candidates.
- Process sources in configured order and stop on a confirmed match, with direct record lookup, complete pagination, deduplication and explicit failures.
- Target Kodi, Jellyfin, Emby and Silo Server with unified NFO output, correcting runtime, episode coordinates, website identities, artwork and disc-folder paths. Reader contracts were checked against official documentation and source code; actual server imports have not been certified.
- Limit the scope to movies and TV shows; remove music-video processing, ffmpeg/ffprobe, Kodi JSON-RPC control and unused rule-based parsing.
- Fix scan/watch filtering, nested and relative library roots, and DVD/Blu-ray handling while retaining manual source and episode constraints.
- Strengthen fact caching, atomic writes, error propagation, proxy isolation and connection deadlines, with unit tests and real CLI runs against controlled services.

Movie and TV processing uses one fixed workflow:

**General LLM extraction → factual candidates from TMDb / TheTVDB → configured Jev or general-LLM judgment → NFO and artwork.**

Only movies and TV shows are supported. TMDb supports movies and TV; TheTVDB currently supports TV shows and episodes through public HTML, without a TheTVDB API key.

## Named models and scraper settings

Define independent instances in `llms`. Each `name` is a unique reference for tasks, `type` is `openai` (OpenAI-compatible chat completions) or `jev` (TypeSafe System One), and `model` identifies the server-side model. Each instance owns its URL, credentials, proxy and timeout.

`scraper.extract_llm` references an `openai` instance; `scraper.select_llm` references an `openai` or `jev` instance. They may share one general instance or use independent services. Unselected instances are never called and do not need working credentials.

`scraper` also owns `providers`, `cache_hours`, `jev_match_threshold`, and `nfo_field.tag/genre`. Jev uses Noul match probability, not Choice confidence, with a default threshold of 0.8. Explicit Jev credentials take precedence over `TYPESAFE_API_KEY`. Temperature is allowed only on `openai` instances.

Duplicate names, missing references, unsupported types, Jev extraction and unknown fields are errors. The old `ai`, `jev`, `metadata`, `kodi`, `ffmpeg`, music-video and unused naming-mode configuration is removed. Move NFO flags from `collector` to `scraper`. See [example.config.json](example.config.json) and [the complete design](docs/metadata-providers.md) for the new configuration.

## Retrieval and judgment

The general LLM extracts title, aliases, year, season, episode and episode title. It may provide `tmdb_id` (TMDb movie/series identity) and `thetvdb_id` (TheTVDB series identity), as positive integer strings, empty when unknown. These must not identify people, seasons or episodes. Unknown season/episode values stay `null`; season zero means specials.

Providers are processed in `scraper.providers` order. For each supported source, an identity fetches details directly; otherwise its complete paginated title queries provide candidates. The selected model judges only that source. A confirmed match stops the pipeline; an empty result, none or low Jev match probability advances to the next source. Transport and schema failures stop with an error. Searches deduplicate identities and reject incomplete or excessive candidate sets instead of truncating them.

The configured judge receives the same compact actual candidate details, original file context, extracted clues and manual constraints. A TV option binds one source's show and target episode. For Jev, one request for the current source contains a Choice with a `none` option and a Noul match question for each candidate. The selected candidate is accepted only if its Noul match probability reaches `scraper.jev_match_threshold` (default `0.8`, configurable in `(0,1]`). Choice confidence measures relative separability and is not used as absolute match probability.

Even a single candidate is judged by the selected service. Unknown choices, malformed responses and request failures stop without writing metadata. `none` or low Jev match probability can advance to the next source; only a confirmed candidate produces output. Verified external identifiers are supplied as evidence; actors and artwork catalogs are omitted from the model request. No model generates factual NFO content.

## Manual constraints and cache

Use `.metadata/source.json` in a show directory, for example:

```json
{"provider":"thetvdb","kind":"show","id":"371065"}
```

Here `id` identifies the show at TheTVDB. A `slug` such as `26882341-show` can locate its page instead. A provider-only reference constrains the source while allowing an extracted identifier or a search. Movies use `.metadata/<full-video-filename>.source.json` with `kind: "movie"`.

Existing manual TMDb text files remain supported: movie file-specific or directory `id.txt`, show-root `id.txt`, video-directory `season.txt`, season/show `group.txt`, and `join.txt`. Manual settings constrain candidates without bypassing extraction or the selected judge. Conflicts are errors. A join mapping only transforms known season/episode coordinates, never an unknown episode. TMDb group positions use the sorted episode list, including sparse Order values.

Normalized search results and factual details are cached under `.metadata/cache/`. Cache identity includes source configuration, language, record type and reference, group and episode coordinates. Extraction and the selected judgment stage still run for each processing request. `cache_hours: 0` disables the disk response cache; TheTVDB's all-season listing also has a one-hour process cache. Old response caches are not migrated or deleted. Keep manual source files when clearing response caches.

DVD/Blu-ray directories produce identical NFO content at the movie root and the disc index location. Movie and episode runtime uses source minutes; unknown runtime, unsupported language tags and source-wide show counts are omitted. Movie logos use `<basename>-logo.png` and retain their `clearlogo` NFO reference.

Each local artwork path gets one selected image, downloaded completely before replacement; a matching source URL avoids repeated downloads. NFO preserves verified source identities and does not substitute a show's identity for an episode's. Failed single-run processing exits nonzero.

The output targets Jellyfin, Emby, Silo Server and Kodi. This tool writes source metadata and downloads source artwork; it does not probe media streams or extract video frames. Actual imports into all four applications have not yet been verified.

Remove the obsolete `ffmpeg` configuration (external tool paths and process limit) and `collector.music_videos_dir` (music-video roots) from existing configuration files.

## Run and build

Set Kodi sources to **Local information only**. The tool does not send Kodi JSON-RPC commands.

```sh
cp example.config.json config.json
# 运行前编辑媒体目录、数据源及所选模型凭据。
./kodi-tmdb-linux-amd64 -config config.json -mode 2
```

Modes: `1` daemon, `2` scan once, `3` current-directory scan. `-mode` overrides configuration. Go 1.27+ is required for source builds:

```sh
go test ./...
go test -race ./...
go vet ./...
make linux-amd64
```

See [Chinese usage](README.zh-CN.md), [design](docs/metadata-providers.md), and [audit/verification boundaries](docs/provider-audit.md). Controlled HTTP tests establish contracts and behavior; they do not establish real model accuracy on your media library.

Sources: [TMDb](https://www.themoviedb.org/), [TheTVDB](https://thetvdb.com/), [TypeSafe API](https://docs.typesafe.ai/api). [GPL-3.0](LICENSE).

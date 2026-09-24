# kodi-metadata-tmdb-cli

[English](README.md) · [简体中文](README.zh-CN.md)

## 项目来源与主要改动

本项目基于 [fengqi/kodi-metadata-tmdb-cli](https://github.com/fengqi/kodi-metadata-tmdb-cli) 进行修改与重构，感谢原项目作者及贡献者。保留原项目的 [GPL-3.0 许可证](LICENSE)；本仓库维护的是在其基础上扩展的版本。

主要改动：

- 将单一 TMDb 流程拆分为数据源、模型判断和输出模块，接入 TMDb API 与无需用户 API 密钥的 TheTVDB 网页抓取。
- 新增具名多 LLM 配置和独立刮削设置；通用 LLM 提取文件信息，候选判断可选择 OpenAI 兼容模型或 TypeSafe Jev。
- 按配置的数据源顺序获取并判断，确认匹配后停止；支持网站作品编号直取、完整分页和候选去重，异常时不伪造结果。
- 将 NFO 输出目标扩展为 Kodi、Jellyfin、Emby、Silo Server，修复时长、季集、网站编号、图片及原盘输出契约。四端读取依据官方文档与源码核查，未宣称完成服务器实际导入验收。
- 仅保留电影和剧集，移除音乐视频、ffmpeg/ffprobe、Kodi JSON-RPC 远程控制及不再使用的规则解析代码。
- 修复扫描与监听过滤、嵌套和相对媒体根、DVD/Blu-ray 目录处理；保留人工来源与季集约束。
- 完善事实缓存、原子写入、错误传播、代理隔离与连接超时，并补充单元测试及受控服务下的真实 CLI 回归。

仅支持电影和剧集的命令行元数据工具，NFO 输出面向 Jellyfin、Emby、Silo Server 和 Kodi。在线电影/剧集使用 **通用 LLM 提取 → TMDb / TheTVDB 获取事实候选 → 配置的判断器（Jev 或通用 LLM）→ NFO 与图片输出**。仅写入来源元数据并下载来源图片，不探测媒体流、不从视频截图。

## 工作流程

1. 通用 LLM 读取文件名及目录上下文，提取标题、别名、年份、季集号、单集标题，以及已知的来源作品编号。
2. 按 scraper.providers 顺序处理适用来源。已有当前来源作品编号时直取；否则汇总标题查询的完整分页结果并去重，候选超限或分页异常报错，不在来源内选首条。
3. 当前来源的候选实际详情取得后，交给配置的判断器。剧集候选绑定同来源的节目与目标单集；模型不能凭空生成元数据。
4. `jev` 使用 Choice/Noul；`llm` 返回结构化候选键或 `none`。有效选择通过校验后立即停止来源遍历，再写 NFO 和图片；未匹配才进入下一适用来源。

来源顺序就是优先顺序，首个确认匹配的来源生效；人工指定来源会限制可访问范围。电影目前仅由 TMDb 支持，TheTVDB 使用电视剧和单集的公开 HTML。

即使只有一个候选，也要交给所选判断器。提取、判断协议或来源请求失败均明确报错，不会自动切换判断器，没有规则、算法或首条结果回退。当前来源无结果、模型返回 none 或 Jev 未达匹配门槛，才进入下一来源。来源的明确未找到可排除对应候选；不符合人工分组的搜索候选可被排除，人工明确指定的作品与分组冲突仍报错。

## 环境与运行

- Go 1.27+：仅源码构建需要。
- 启用 TMDb 时需要自己的 TMDb API key；TheTVDB HTML 无需 API key。
- 在线电影/剧集始终需要通用 LLM；仅选择 `jev` 判断时额外需要 TypeSafe 凭据。
- Kodi 媒体源可设置为 **Local information only**，读取本地 NFO；本工具不再提供 JSON-RPC 远程控制。

```sh
cp example.config.json config.json
# 编辑来源、所选模型凭据和媒体目录后运行。
./kodi-tmdb-linux-amd64 -config config.json -mode 2
```

运行模式：`1` 常驻、`2` 扫描所有配置目录一次、`3` 只扫描属于配置根目录的当前目录。`-mode` 覆盖 `collector.run_mode`。配置文件优先从工作目录读取，再从可执行文件目录读取。单次运行有文件处理失败时返回非零退出码。

## 模型与刮削配置

`llms` 配置多个独立实例，每项 `name` 是供任务引用的唯一名称，`type` 指协议（`openai` 或 `jev`），`model` 是服务端模型名。地址、凭据、代理和超时均属于该实例。

`scraper.extract_llm` 指定文件信息提取实例，只允许 `openai`；`scraper.select_llm` 指定候选判断实例，允许 `openai` 或 `jev`。两项可引用同一个通用实例，也可使用不同地址或账号；单纯定义实例不会发起请求。

```json
{
  "llms": [
    {"name":"extract","type":"openai","base_url":"https://llm.example.com/v1","api_key":"YOUR_KEY","model":"YOUR_MODEL","timeout_seconds":30},
    {"name":"judge","type":"jev","base_url":"https://api.typesafe.ai/v1","api_key":"YOUR_TYPESAFE_KEY","model":"jev-latest","timeout_seconds":30}
  ],
  "scraper": {
    "providers":["thetvdb","tmdb"],
    "extract_llm":"extract",
    "select_llm":"judge",
    "cache_hours":24,
    "jev_match_threshold":0.8,
    "nfo_field":{"tag":true,"genre":true}
  }
}
```

`openai` 表示兼容聊天补全协议，不限定服务商；`temperature` 仅允许该类型设置。Jev 使用 TypeSafe `/v1/systemone` 协议，显式凭据优先，空凭据读取 `TYPESAFE_API_KEY`；仅被选择的实例需要具备完整连接参数。Jev 的门槛是所选候选的 Noul 匹配概率，不是 Choice 相对置信度。

模型名称重复、引用不存在、类型不支持、Jev 用于提取、未知配置字段均报错。旧 `ai`、`jev`、`metadata` 根配置已由 `llms`、`scraper` 替代；原 `collector.nfo_field` 移入 `scraper.nfo_field`。删除旧 `kodi`、`ffmpeg`、音乐视频目录和未生效的命名模式/过滤开关；不提供新旧配置并行入口。完整可编辑示例见 [example.config.json](example.config.json)。

## 数据源与事实缓存

`tmdb` 配置官方 API 地址、密钥、图片地址、语言、分级、代理、超时和有限重试；`thetvdb` 配置站点、网页语言、代理、超时和请求间隔，无需用户 API 密钥。TheTVDB 名称定位使用网站公开搜索请求，详情使用 HTML，单集采用官方播出顺序。

`cache_hours` 是来源响应有效小时数，`0` 禁用磁盘响应缓存。缓存包含来源配置、媒体类型、作品引用、语言、分组与季集坐标；不会跳过本轮提取和判断。TheTVDB 官方季集列表另有一小时进程缓存。

通用 LLM 提供的 `tmdb_id` 是 TMDb 电影/节目记录编号，`thetvdb_id` 是 TheTVDB 节目记录编号；未知留空，不接受演员、季或单集编号。未知季集号为 `null`，季号 `0` 表示特别篇。模型不生成最终事实。

## 人工约束

节目根目录 `.metadata/source.json`：

```json
{"provider":"thetvdb","kind":"show","id":"371065"}
```

`provider` 指数据源，`kind` 指对象类型，`id` 指该网站内的节目记录编号。也可使用 `slug` 指定节目页面路径片段，例如 `26882341-show`；它不是数字编号。只写 `{"provider":"thetvdb"}` 表示限定来源，仍可使用 LLM 提供的同源编号或执行搜索。

电影使用 `.metadata/<完整视频文件名>.source.json`，对象类型为 `movie`。人工约束限定候选，不跳过 LLM 提取和所选判断器核验。

| 文件 | 语义 |
| --- | --- |
| 电影旁 `tmdb/<完整视频文件名>.id.txt` | 该电影的 TMDb 作品编号，文件名包含视频扩展名 |
| 电影旁 `tmdb/id.txt` | 目录级 TMDb 电影编号；多部电影同目录时应分别指定 |
| 节目根目录 `tmdb/id.txt` | TMDb 节目编号 |
| 视频目录 `tmdb/season.txt` | 明确的季号；`0` 表示特别篇 |
| 季目录或节目根目录 `tmdb/group.txt` | TMDb Episode Group 分组记录编号，季目录优先，来源固定为 TMDb |
| 视频目录 `tmdb/join.txt` | 原季、新季、起始集号；`1,2,13` 将已知 S01E01 映射为 S02E13，只应用一次 |

人工来源冲突会报错。`join` 只可变换已知季集号；不会补造 LLM 未识别的集号。分组内文件集号是按 `Order` 排序后的序位，不要求 `Order` 连续。

## 输出与刷新

- 普通电影写 `<视频名>.nfo` 与海报；原盘在电影根目录和索引目录写相同 NFO；节目写 `tvshow.nfo`，单集写 `<视频名>.nfo`。
- NFO 保存当前对象自身及已验证的跨站编号；电影、单集标称时长以分钟输出，未知省略。不输出无效语言标签或网站节目总量，单集零季号保留。普通电影 Logo 使用 `<视频名>-logo.png`，NFO 也记录 clearlogo 来源 URL。
- 图片每个目的路径选择一张，完整下载后再替换；来源 URL 未变且文件完整时跳过。NFO 可保留图片候选列表。
- `.metadata/cache/` 保存来源响应，`.metadata/artwork/` 保存图片来源记录。人工 `source.json` 与 `tmdb/*.txt` 不属于可清空的响应缓存。
- 本次缓存键版本已更新；旧缓存不迁移，不删除真实媒体目录中的旧文件。TheTVDB 全集还在进程内缓存一小时。
- 所有适用来源均未匹配，或遇请求/协议错误时，不进入 NFO 写入。确认匹配后才写 NFO/图片，输出失败返回非零退出码；已成功写入的其他文件不自动回滚。

其余 `collector` 定时扫描、监听、跳过目录/关键字、多根目录、`scraper.nfo_field.tag/genre`保持原职责；NFO 输出不需要媒体服务器账号或控制接口。

## 开发与验证

```sh
go test ./...
go test -race ./...
go vet ./...
make linux-amd64
```

[设计与契约](docs/metadata-providers.md) · [整改与验收报告](docs/provider-audit.md)

受控 HTTP 回放验证流程和输出，不等于真实模型准确率验证，也不代表已通过四种媒体软件的实际导入验收。所选判断服务与通用 LLM 需使用你的账户凭据验证；中文媒体判断效果应按实际样本检查。

数据来源：[TMDb](https://www.themoviedb.org/) · [TheTVDB](https://thetvdb.com/)。决策协议：[TypeSafe 官方 API](https://docs.typesafe.ai/api)。许可证：[GPL-3.0](LICENSE)。

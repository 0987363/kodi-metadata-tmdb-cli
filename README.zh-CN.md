# kodi-metadata-tmdb-cli

[English](README.md) · [简体中文](README.zh-CN.md)

## 项目来源与主要改动

本项目基于 [fengqi/kodi-metadata-tmdb-cli](https://github.com/fengqi/kodi-metadata-tmdb-cli) 进行修改与重构，感谢原项目作者及贡献者。保留原项目的 [GPL-3.0 许可证](LICENSE)；本仓库维护的是在其基础上扩展的版本。

主要改动：

- 将单一 TMDb 流程拆分为数据源、模型判断和输出模块，接入 TMDb API 与无需用户 API 密钥的 TheTVDB 网页抓取。
- 新增具名多 LLM 配置和独立刮削设置；通用 LLM 提取文件信息，候选判断可选择 OpenAI 兼容模型或 TypeSafe Jev。
- 按配置顺序搜索并核验作品；模型编号命中同来源、同类型的真实搜索候选时跳过判断，否则判断基本作品候选。完整分页与候选去重保留，单个网站来源未成功时继续下一来源，全部网站来源未成功后再进行 AI 本地整理。
- 将 NFO 输出目标扩展为 Kodi、Jellyfin、Emby、Silo Server，修复时长、季集、网站编号、图片及原盘输出契约。四端读取依据官方文档与源码核查，未宣称完成服务器实际导入验收。
- 仅保留电影和剧集，移除音乐视频、ffmpeg/ffprobe、Kodi JSON-RPC 远程控制及不再使用的规则解析代码。
- 统一为 `--path` 指定单个目录、处理一次即退出；递归发现混合媒体，由通用 LLM 每批最多 50 个文件分类提取，再组织电影或整剧任务。
- 按整剧任务批量获取来源季集事实；全部适用网站来源均未取得可用元数据时，只依据目录和文件名整理本地标题、简介与分类，并由程序生成本地标识。
- 保留明确目录、关键词与临时后缀过滤、DVD/Blu-ray 目录处理及人工来源与季集约束；完善事实缓存、原子写入、错误传播、代理隔离与连接超时。

仅支持电影和剧集的命令行元数据工具，NFO 输出面向 Jellyfin、Emby、Silo Server 和 Kodi。流程为 **单目录发现 → 通用 LLM 批量分类提取 → 电影或整剧任务 → 真实基本搜索候选 → 编号交叉核验或模型判断 → 选中作品及季集详情 → NFO 与来源图片**。只有全部适用网站来源均未取得可用元数据时，才进入受限本地整理。工具不探测媒体流、不从视频截图。

## 工作流程

1. 程序递归发现 `--path` 目录内的媒体单元，只执行明确过滤；通用 LLM 每批最多 50 个文件分类提取标题、原名/别名、年份、已知网站作品编号与季集。程序校验完整覆盖、路径、类型和作品归属后，组织电影或整剧任务。
2. 分类后应用人工来源、作品编号、季集及分组约束。人工明确电影/节目引用直取基本作品记录并做单候选判断，模型提示不能覆盖它。没有人工明确引用时，按 `scraper.providers` 顺序用片名和年份完整搜索并去重；已知年份作为必要条件，不自动放宽为无年份搜索，模型有编号也必须先搜索。
3. 模型编号与真实搜索候选的来源、电影/节目对象类型、记录编号都相同，直接确认候选并跳过判断器。编号未知或未命中时，仅把基本作品候选交给配置的 Jev 或通用判断模型；即使只有一个候选也须判断。
4. 判断只包含作品标题、原名、年份、类型与来源引用，不携带完整文件清单、季集、分组、单集剧情、演员或图库。Jev 使用 Choice/Noul；通用判断模型返回候选键或 `none`，候选键只指本次候选，不是网站编号。
5. 确认作品后获取完整电影事实，或节目及本轮所需多季多集事实；保留批量接口、缓存、TMDb 每批 20 个追加对象限制和 TheTVDB 必要详情补取。完整可用元数据取得后才停止来源遍历，校验后写 NFO/图片。
6. 当前来源搜索为空、判断拒绝/低分、来源或判断请求/解析错误、完整详情缺失，均记录原因后尝试下一适用来源。全部网站来源未成功后调用 AI 本地整理，AI 也失败才以元数据获取失败退出。共同输入/分类错误、人工硬约束、用户取消及图片/NFO 输出错误仍终止；不隐式重试。

来源顺序就是优先顺序；人工指定来源限制可访问范围，人工明确作品引用保留硬约束。电影目前仅由 TMDb 支持，TheTVDB 使用电视剧和单集的公开 HTML。正常未知的可选字段保持空值，不属于异常。

来源编排边界逐源记录脱敏失败原因；collector 记录最终终止错误，main 只负责非零退出，避免重复记错。网站成功作品先输出，本轮网站尝试结束后统一批量整理需要本地信息的作品；已完成文件不自动回滚。

## 环境与运行

- Go 1.27+：仅源码构建需要。
- 启用 TMDb 时需要自己的 TMDb API key；TheTVDB HTML 无需 API key。
- 分类及本地整理需要通用 LLM；仅选择 `jev` 判断时额外需要 TypeSafe 凭据。
- Kodi 媒体源可设置为 **Local information only**，读取本地 NFO；本工具不再提供 JSON-RPC 远程控制。

```sh
cp example.config.json config.json
# 编辑来源和所选模型凭据后运行。
./kodi-tmdb-linux-amd64 -config config.json --path "/media/混合媒体库"
```

`--path` 必须且只能指定一次现有目录；位置参数、重复 `--path`、旧 `-mode` 均报错，不默认扫描当前目录。处理结束即退出，不监听或定时扫描。`-config` 指定配置文件，默认优先从工作目录读取，再从可执行文件目录读取；`-version` 与帮助入口保留。网站来源和 AI 本地整理全部未成功时才以元数据获取失败退出；共同输入/分类、人工硬约束、用户取消及最终输出错误仍非零退出，已写入文件不自动回滚。

判断请求在没有人工指定时省略空约束；已有人工来源和作品约束完整保留。中英文片名作为同一作品的名称线索比较，不要求同名字段文字相同。

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

模型名称重复、引用不存在、类型不支持、Jev 用于提取、未知配置字段均报错。旧 `ai`、`jev`、`metadata` 根配置已由 `llms`、`scraper` 替代；原 `collector.nfo_field` 移入 `scraper.nfo_field`。旧 `collector.movies_dir`、`collector.shows_dir` 原来同时提供媒体根和预设类型，现由 `--path` 提供目录、AI 判断类型。旧 `collector.run_mode`、`collector.watcher`、`collector.cron_scan`、`collector.cron_scan_boot`、`collector.cron_seconds` 原控制常驻、监听及周期扫描，因只执行一次而移除。`tmdb.retry_count` 原定义网络失败重试次数；因不对同一失败请求隐式重试，改由编排层尝试下一获取方式而移除，同时删除退避与重试统计，旧字段严格拒绝。`collector` 仅保留明确过滤。旧 `kodi`、`ffmpeg`、音乐视频目录和未生效的命名模式/过滤开关也已删除；旧字段严格拒绝。完整可编辑示例见 [example.config.json](example.config.json)。

## 数据源与事实缓存

`tmdb` 配置官方 API 地址、密钥、图片地址、语言、分级、代理和超时；`thetvdb` 配置站点、网页语言、代理、超时和请求间隔，无需用户 API 密钥。TheTVDB 名称定位使用网站公开搜索请求，详情使用 HTML，单集采用官方播出顺序。

`cache_hours` 是来源响应磁盘缓存的有效小时数，`0` 禁用跨次运行的磁盘复用。缓存包含来源配置、媒体类型、作品引用、语言、分组与季集坐标；搜索缓存与旧的忽略或放宽年份的结果隔离；详情与整剧缓存版本保持不变，整剧继续使用独立的 `series-*` 命名空间。来源在内存中的快照只在本次 CLI 运行内复用；缓存命中仍执行本轮提取和作品核验；模型编号命中同来源、同类型的真实搜索候选才跳过判断。

通用 LLM 提供的 `tmdb_id` 是 TMDb 电影/节目记录编号，`thetvdb_id` 是 TheTVDB 节目记录编号；可根据明确标记或模型已有知识给出未验证提示，未知留空；仍须先搜索，不接受演员、季或单集编号。未知季集号为 `null`，季号 `0` 表示特别篇。模型不生成最终事实。

## 人工约束

节目根目录 `.metadata/source.json`：

```json
{"provider":"thetvdb","kind":"show","id":"371065"}
```

`provider` 指数据源，`kind` 指对象类型，`id` 指该网站内的节目记录编号。也可使用 `slug` 指定节目页面路径片段，例如 `26882341-show`；它不是数字编号。只写 `{"provider":"thetvdb"}` 表示限定来源，仍在该来源搜索；LLM 的同源编号不能省略搜索。

电影使用 `.metadata/<完整视频文件名>.source.json`，对象类型为 `movie`。人工约束在 AI 分类后生效；明确作品引用直取基本作品记录并做单候选判断，不跳过 LLM 提取或所选判断器，模型提示不能覆盖人工引用。

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
- 整剧批量结果使用独立缓存键；旧缓存不迁移，不删除真实媒体目录中的旧文件。来源任务内已获取的事实在本次运行中复用。
- 全部适用网站来源未取得可用元数据后，通用模型的 `DescribeLocal` 只依据输入路径返回标题、简介及分类；未知事实留空。程序为电影、节目和单集生成稳定的 `local` 对象标识，不伪造网站编号、页面或图片。人工明确来源或作品约束不得被 local 绕过；网站故障与无记录在日志中分别说明。
- 确认网站匹配或本地整理通过校验后才写 NFO/图片；输出失败立即终止整个进程并返回非零退出码，已成功写入的其他文件不自动回滚。

`collector.skip_folders`、`collector.skip_keywords`、`collector.tmp_suffix` 只作明确过滤；`scraper.nfo_field.tag/genre` 保持输出开关职责。目录名称、用户验证样本名称与 `res.txt`、`res2.txt` 文件名都不是生产分类特例；NFO 输出不需要媒体服务器账号或控制接口。

## 开发与验证

```sh
go test ./...
go test -race ./...
go vet ./...
make linux-amd64
```

[设计与契约](docs/metadata-providers.md) · [整改与验收报告](docs/provider-audit.md)

一次性 CLI、批量分类、整剧取数和受限本地整理已有主分支 race、vet 与构建证据。三方式接续已合并推送，主分支完整复验通过。真实固定样本前三部共 60 个视频已通过，其中 Super.Science 在两个网站候选被拒绝后成功使用 AI 本地整理；第四部 104 个视频按用户要求暂时跳过；Friends 的 234 个视频因一文件多集无法用当前单集协议表达而停止，尚未进入网站请求。10 部节目及 res2.txt 的整体验收尚未完成，见 [复验记录](docs/validation-2026-09-24.md)。受控回归不代表真实模型准确率或四种媒体软件实际导入已验证。

数据来源：[TMDb](https://www.themoviedb.org/) · [TheTVDB](https://thetvdb.com/)。决策协议：[TypeSafe 官方 API](https://docs.typesafe.ai/api)。许可证：[GPL-3.0](LICENSE)。

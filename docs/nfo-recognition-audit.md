# NFO 四端识别核查与修复记录

核查日期：2026-09-24。对象是 `codex/metadata-providers` 工作区实际写入器。初次核查记录了基线缺口，下方修复状态说明当前实现。

按用户要求，以官方文档、官方源码及本项目实际生成样本核查，不部署或运行四种媒体软件。初次核查未修改业务代码，随后按已批准方案完成下列修复。

## 结论

**普通电影、节目和单集的核心 NFO 与四端已核查读取契约相符。已修复本项目的字段遗漏、无效标签、图片命名和原盘入口/路径；读取端忽略部分扩展字段等边界仍存在，不能声称所有版本、全部字段和原盘播放行为一致。**

前提是文件属于对应媒体库，服务端能从目录和文件名识别电影/节目/季/集，且使用本地 NFO 读取能力。NFO 描述文件不会替代媒体文件自身，也不自动修正所有归集错误。

XML 规定语法；媒体 NFO 沿用共同约定，但各应用有自己的字段映射和文件发现逻辑。共同根元素和常用字段相通，不代表扩展字段语义完全一致。

## 当前修复状态

- 电影与单集写入正数标称分钟时长，未知省略。
- 不输出无效的 languages 标签，也不将其误改为元数据 language；来源语言事实保留。
- 节目不再输出网站总季集数，单集坐标与特别篇零季保留。
- 普通电影 Logo 改为 -logo.png，NFO增加clearlogo来源URL；原盘保持目录级clearlogo.png。
- DVD/蓝光作为单个电影任务，内部条目不单独派发；同一事实分别写入根 movie.nfo 和原盘索引 NFO。
- 下面字段矩阵用于解释基线问题及读端限制；生成样本已更新为修复后的真实输出。

## 1. 样本验证

直接调用当前 `nfo.Write` 生成以下四个合成样本，随后使用 Go XML 解析器读取。四个样本均可解析，中文、`&` 和 `<` 正确转义。

- [电影样本](<nfo-samples/Sample Movie (2020).nfo>)：根元素 `movie`，输入来源标称时长 123 分钟，修复后输出 runtime=123；不再输出无效语言标签。
- [节目样本](nfo-samples/tvshow.nfo)：根元素 `tvshow`，包含网站作品引用和命名季。
- [单集样本](<nfo-samples/Sample Show S01E02.nfo>)：根元素 `episodedetails`，季 1、集 2，输出时长 45 分钟。
- [特别篇样本](<nfo-samples/Sample Show S00E01.nfo>)：零季号保留，未知时长省略。

这些记录和网站编号仅作为格式测试输入，不代表已核实的真实作品；图片使用不可用的示例地址。样本没有用于真实媒体库导入。

## 2. 官方源码依据

| 软件 | 本次核查依据 |
| --- | --- |
| Kodi | 官方 Wiki；Omega 分支 `f8815ee40f49a700c047982d752be4b2a61420e2` 的 `VideoInfoTag.cpp` |
| Jellyfin | 官方文档；源码树快照 `208c278b75abd897aefa1e1175126eac5e4dbfaa` 的 NFO Providers、Parsers、MovieNfoSaver |
| Emby | 官方 `MediaBrowser/NfoMetadata` 插件源码树快照 `965f602939810b845a08548fe96ff10667762633`，以及 Movie/TV Naming 文档 |
| Silo Server | 官方源码树快照 `d4e35ba9df416e747822c6c9f2193b89c6b7e9fb` 的 `internal/metadata/nfo/` 与本地 NFO 文档 |

源码树快照与发布版本不是同一概念。本报告只对这些可核实的解析规则作结论，不宣称所有历史版本或不同插件版本一致。

## 3. 文件发现与归集

| 当前输出 | Kodi | Jellyfin | Emby | Silo |
| --- | --- | --- | --- | --- |
| 普通电影：与视频同名的 `.nfo` | 官方推荐 | 代码接受 | 插件代码接受 | 文件级候选路径接受 |
| 节目目录 `tvshow.nfo` | 接受 | 接受 | 接受 | 接受，根元素须匹配节目类型 |
| 单集：与视频同名的 `.nfo` | 接受 | 接受 | 接受 | 按对应单集媒体路径读取 |
| 单集季集编号 | 文件名决定归集 | NFO 解析器读取季集字段，仍须先识别媒体结构 | 插件读取季集字段，仍受命名与媒体库结构约束 | 目录/文件名编号优先于 NFO |
| 蓝光目录 `BDMV/index.nfo` | 符合原盘命名约定 | 所核查发现代码未列此路径 | 插件明确检查此路径 | NFO 目录发现逻辑没有此特殊路径；不能据此确认原盘可识别 |

依据：[Kodi 单集命名](https://kodi.wiki/view/NFO_files/Episodes)、[Jellyfin 电影发现路径](https://github.com/jellyfin/jellyfin/blob/208c278b75abd897aefa1e1175126eac5e4dbfaa/MediaBrowser.XbmcMetadata/Savers/MovieNfoSaver.cs#L47)、[Emby 电影发现路径](https://github.com/MediaBrowser/NfoMetadata/blob/965f602939810b845a08548fe96ff10667762633/NfoMetadata/Savers/MovieNfoSaver.cs#L34)、[Silo 文件发现](https://github.com/Silo-Server/silo-server/blob/d4e35ba9df416e747822c6c9f2193b89c6b7e9fb/internal/metadata/nfo/provider.go#L198)。

因此，LLM 从非标准文件名提取出正确季集号，不保证 Kodi 或 Silo 会按这些编号归集。保留原文件名的 `join`、分组坐标换算也有同样边界。这是当前流程必须明确的输入约束，不能仅靠 NFO 字段修复。

基线还存在项目自身入口问题（现已修复）：[扫描代码](../collector/scan.go)只单独派发蓝光目录和普通视频；DVD 的 `VIDEO_TS` 目录及 VOB/IFO 文件被分类为光盘对象，却不走蓝光或普通视频分支。另 [电影路径代码](../movies/process.go)将 DVD 路径再拼接 `VIDEO_TS/VIDEO_TS.nfo`，若传入实际 `VIDEO_TS` 目录会重复一层。现已通过真实目录回归修复扫描和重复路径，提供根目录与索引 NFO；这不代表已经验证各服务器的原盘播放能力。

## 4. 字段对照

| 字段或基线输出 | 官方读取逻辑结论 |
| --- | --- |
| `movie`、`tvshow`、`episodedetails` 根元素 | 对应四端已实现的三类 NFO 对象 |
| `title`、`plot`、日期及常见类型字段 | 基础字段可读取；不同对象有继承或忽略字段，不能要求每种字段在每种对象上都有效 |
| `uniqueid` | 表示当前 NFO 对象在某网站的记录编号，`type` 表示网站，`default` 标明主引用；当前 `tmdb`、`tvdb` 输出名称正确 |
| 电影/节目网站编号 | 四端均有对应读取映射 |
| 单集网站编号 | Kodi、Jellyfin、Emby 有读取路径；Silo 虽在 XML 解析结构中读取，但其 `GetEpisodes` 没有把这些编号复制到返回结果，不能宣称保留 |
| `runtime` | 四端有分钟级读取或说明；基线只映射单集，现已补齐电影的来源标称时长 |
| `languages` | 在四份已核查解析实现中没有相应字段映射，不会产生预期语言信息 |
| `ratings/rating`，来源 `tmdb` | 四端能读取当前单条评分；未写 `default` 不会导致当前单评分完全丢失 |
| 多条评分 | Kodi、Emby、Jellyfin 的默认或覆盖处理不同，Silo 按支持的来源名分别存储；不能保证同一默认展示值 |
| 演员名称、角色与排序 | 电影和节目可读取；Silo 明确忽略演员 `thumb` 图片地址，单集接受的字段更少 |
| `namedseason` | 季号对应的季名称，Kodi 与 Jellyfin 有解析；核查的 Emby、Silo NFO 解析没有对应映射 |
| 节目 `season`、`episode` | Kodi 文档语义为库中季数、集数；基线写网站总数，现已停止输出；不能将其等同于实际入库数量。其他服务没有统一映射 |
| `fileinfo/streamdetails` | 描述实际文件媒体流，不是识别当前基础 NFO 的必要条件；继续保持不输出 |

评分不是本次已经确认的阻断问题：Kodi 在未设默认时可使用第一条评分；Emby 使用默认项或尚未填入时的第一条；Jellyfin 对普通社区评分按读取顺序赋值；Silo 映射到命名评分槽位。以后增加多评分来源时须定义输出取舍，不能仅添加 `default` 就宣称四端完全一致。

依据：[Kodi 字段读取](https://github.com/xbmc/xbmc/blob/f8815ee40f49a700c047982d752be4b2a61420e2/xbmc/video/VideoInfoTag.cpp#L996)、[Jellyfin 基础解析](https://github.com/jellyfin/jellyfin/blob/208c278b75abd897aefa1e1175126eac5e4dbfaa/MediaBrowser.XbmcMetadata/Parsers/BaseNfoParser.cs)、[Emby 基础解析](https://github.com/MediaBrowser/NfoMetadata/blob/965f602939810b845a08548fe96ff10667762633/NfoMetadata/Parsers/BaseNfoParser.cs)、[Silo 基础解析](https://github.com/Silo-Server/silo-server/blob/d4e35ba9df416e747822c6c9f2193b89c6b7e9fb/internal/metadata/nfo/nfo.go)、[Silo 单集结果映射](https://github.com/Silo-Server/silo-server/blob/d4e35ba9df416e747822c6c9f2193b89c6b7e9fb/internal/metadata/nfo/series_depth.go#L123)。

## 5. 图片读取不是统一契约

- Kodi、Jellyfin 有 NFO 海报和背景图引用读取；Jellyfin 通常只取同类第一张。
- 本次 Emby 插件基础及电影解析器未发现顶层 `thumb`、`fanart` 处理；其官方文档明确列出本地图片命名。因此不能只依靠 NFO 中的远程图片 URL。
- Silo 的图片 Provider 独立扫描本地图片，不读取 NFO 中的远程海报列表。当前电影 `-poster.jpg`、`-fanart.jpg`，节目 `poster.jpg`、`fanart.jpg` 和单集 `-thumb.jpg` 能对应其规则。
- **基线电影 Logo 不一致（已修复）**：本项目原先普通电影写 `<视频名>-clearlogo.png`，Silo 列出的匹配名为目录级 `logo`/`clearlogo` 或 `<视频名>-logo`，缺少当前名称；基线 NFO 也未输出 Logo 引用。当前已改为 -logo.png，并补充 NFO clearlogo 来源 URL，符合已核查的发现和引用语义。
- Emby 官方文档未明确列出本项目普通电影的 `<视频名>-fanart.jpg`，不能只从文档证明该路径有效，也不能仅凭未列出就断言一定不支持；本项保留为未确认，不能冒充已验证。

依据：[Jellyfin 本地 NFO 图片](https://jellyfin.org/docs/general/server/metadata/nfo/#image-paths-and-urls-in-nfo-files)、[Emby 图片命名](https://emby.media/support/articles/Movie-Naming.html#video-images)、[Silo 图片发现实现](https://github.com/Silo-Server/silo-server/blob/d4e35ba9df416e747822c6c9f2193b89c6b7e9fb/internal/metadata/nfo/images.go#L25)。

## 6. 对优化方案的直接影响

以下整改已按 [优化方案第 7 节](metadata-providers.md) 执行；Emby 电影背景图命名在官方文档中未明确列出的证据边界仍保留：

1. 补齐电影来源标称时长的 NFO 映射；继续省略未知时长，不恢复媒体文件探测。
2. 移除无效语言输出：`languages` 表示来源作品语言列表，但当前标签不被读取；优化方案已确定删除该 NFO 序列化，来源记录中的语言事实保留。不能直接改成 `language`，Jellyfin/Emby 将该标签解释为首选元数据语言，不是实际音轨或作品语言列表。
3. 统一配套图片的发现契约，修正 Silo 电影 Logo 命名缺口；确认 Emby 电影背景图路径。
4. 修正原盘扫描和 NFO 发现位置后再声明原盘适用；不以普通视频结果外推。
5. 停止将网站总季数/总集数写成节目 NFO 的媒体库数量；单集季集坐标保留。命名季等有效扩展字段保持原义；Silo 忽略的单集编号、演员图片不能通过伪造字段或复制节目编号来补偿。
6. 将季集命名规则作为输入契约。现阶段不新增批量重命名、不生成四套 NFO、不恢复 ffmpeg/ffprobe。

## 7. 核查边界

- 已验证：当前写入器的四类样本输出与 XML 解析；逐项追踪官方文件发现、标签解析及部分结果传递代码。
- 未执行：Kodi、Jellyfin、Emby、Silo 的服务器运行、数据库导入、界面展示和播放验证，符合用户限定的核查方式。
- 已修改：项目内写入映射、图片命名、原盘和媒体根处理；未修改真实媒体、服务器配置或系统软件。
- 当前统一设计见 [优化方案](metadata-providers.md)。本报告替代笼统的“四端字段尚未核查”描述，并区分已修复的生成端问题与仍存在的读取端能力差异。

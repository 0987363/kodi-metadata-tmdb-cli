# 初始审计证据（基线 9f184d1）

以下是整改前证据，最终结果见 docs/provider-audit.md。

## 已核实的结构

- `collector` 调度 `movies.Process`、`shows.Process` 和 `music_videos.Process`。
- 电影和剧集的解析、缓存、详情、NFO、图片下载直接引用 `tmdb`；仅增加 Provider 接口无法解除依赖。
- `common/ai` 已有文件名解析和 TMDb 搜索候选选择，并非需要从零新增 AI 识别。
- 剧集有用户手动控制：TMDb 作品标识、季号、TMDb 剧集分组标识、合并规则。缓存目录混合了可重建响应和不可随意删除的用户输入。
- 基线音乐视频使用本地媒体工具；该分支已按当前仅电影和剧集的范围移除。

## 已观察的问题

- `utils.NormalizePath` 依赖操作系统解释反斜杠，已有跨平台测试失败；根目录 `/` 还会被去尾斜杠变为空。
- `shows.Process` 丢弃两次 NFO 保存错误；图片下载方法不返回错误。
- `tmdb.DownloadFile` 对非 200 返回 nil，中断下载可留下非空半文件，下一次按文件大小直接跳过。
- 电影 NFO 的 genre 受 tag 开关控制；剧集 NFO 的 tag 受 genre 开关控制。
- 剧集 NFO 固定写 runtime=6；演员角色直接取 Roles[0]；剧集总集数被循环覆盖成最后一季集数。
- 路径/文件名使用 TrimRight 的字符集合语义或首次 Replace，可能误删标题或修改目录名。
- TMDb 搜索吞掉网络、认证和 JSON 错误，最终统一返回未找到，无法正确驱动多源调度。
- 内存缓存返回共享剧集对象，Process 仍直接修改分组字段，与隔离分组状态的注释矛盾。
- 输出层仅支持单个 uniqueid，图片地址由 TMDb 全局对象拼装。
- 现有缓存未包含语言、剧集排序/分组映射等完整请求身份；缓存命中可能绕过用户覆盖。

上述尚为审计结果，除基线失败外没有声称已增加回归测试或完成修复。

## 附件需修正的前提

- “找到就停止”与“字段级跨源补全”是不同的请求策略。
- TMDb 作品标识、TheTVDB 作品标识、各平台单集标识属于不同命名空间，不能用一个裸整数互换。
- aired/DVD/absolute/自定义分组排序不能仅凭相同季集编号自动对应。
- AI 自报 confidence 不构成剧情、演员、日期等事实的证据。
- TheStaticTurtle 的参考脚本是特定节目页面转 Sonarr SQL 的 2023 年脚本，不能当作通用 Provider 的完整实现。

## 参考来源

- https://thetvdb.com/api-information
- https://github.com/thetvdb/v4-api
- https://www.tvmaze.com/api
- https://kodi.wiki/view/NFO_files/TV_shows
- https://kodi.wiki/view/NFO_files/Episodes
- https://gist.github.com/TheStaticTurtle
- https://thetvdb.com/series/26882341-show/allseasons/official

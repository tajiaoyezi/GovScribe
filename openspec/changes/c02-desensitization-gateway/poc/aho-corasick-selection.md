# Aho-Corasick 选型 PoC

日期：2026-06-15

## 结论

默认字典识别后端选用 `github.com/petar-dambovaliev/aho-corasick`，锁定伪版本：

```text
github.com/petar-dambovaliev/aho-corasick v0.0.0-20250424160509-463d218d4745
```

实现仍通过 `Automaton` / `AutomatonBuilder` 接口隔离，后续如需替换为其他 AC 实现，不影响网关处理器与占位回填逻辑。

## 候选对比

`go list -m -versions` 对两个候选库均未返回 semver tag，因此只能锁伪版本。

| 候选库 | 复核版本 | 结论 |
|---|---|---|
| `petar-dambovaliev/aho-corasick` | `v0.0.0-20250424160509-463d218d4745` | 选用。MIT；提供 `Match.Start()` / `Match.End()` / `Match.Pattern()`，可直接得到 byte offset 与 pattern index。 |
| `cloudflare/ahocorasick` | `v0.0.0-20240916140611-054963ec9396` | 不选。MIT；`Match` / `MatchThreadSafe` 返回字典 index 集合，不返回每次命中的 start/end span，不满足占位替换需要的精确切片契约。 |

## Offset 验证

网关统一使用 Go 字符串 byte offset：正则 `FindAllStringIndex`、外置 NER span 校验、本次 AC 字典命中保持同一契约。

验证入口：

```powershell
$env:GOCACHE='H:\devlopment\code\GovScribe\.gocache'; $env:GOTELEMETRY='off'; go test -count=1 ./internal/desensitization/gateway -run TestDictionaryRecognizerUsesLockedPetarAhoCorasickWithChineseByteOffsets
```

该测试覆盖：

- 默认字典识别器使用锁定的 petar AC 后端；
- 中文单位名 / 人名返回可直接用于 `text[start:end]` 的 byte offset；
- 同一中文词条在同一文本中多次出现时，每次命中都返回独立 span。

实现使用 `IterOverlapping` 收集重叠命中，再交给既有 `MergeHits` 按优先级处理，避免短词先命中时吞掉更长的黑名单词条。

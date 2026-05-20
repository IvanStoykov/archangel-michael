# Grading: `docs/qwen_hypothesis.md`

**Overall: ~6.5/10** — "Right bug, wrong rank, missed the real one."

Qwen did a careful, mostly factually-accurate read of `ConvertFmp4ToProgressiveMp4`, and the one bug it fixates on is genuinely real. But it crowns a secondary cosmetic bug as "the primary root cause," gets the ffprobe mechanism wrong, and never looks at the function that actually causes the symptom.

Grading against the pre-fix repo.

## Claim-by-claim

| Qwen's claim | Verdict |
| --- | --- |
| `ConvertFmp4ToProgressiveMp4` copies `init.Ftyp` verbatim (`fmp4_to_mp4.go:134-139`) | ✓ Accurate — code does exactly this |
| This is the primary root cause | ✗ Wrong — it's secondary and cosmetic |
| "ffprobe… classifies the file based on the brands" | ✗ Wrong — ffprobe never does this |
| `moov` carries over `mvex` (code trace line 122) | ✗ Wrong / fabricated — and Qwen contradicts itself |
| Two paths: `GetVideo` (native muxers) vs `GetVideoOfType` (FFmpeg pipe) | ✓ Accurate (I verified `endpoints.go`) |
| `/video/mp4` explicit endpoint → FFmpeg path, not the native converter | ✓ Accurate (`endpoints.go:172,348,917`) |
| fMP4 muxer emits `[ftyp][moov][moof][mdat]…` | ✓ Accurate |
| `stbl` (`stts`/`stss`/`stsc`/`stsz`/`stco`/`ctts`) built correctly | ✓ Accurate |
| PTS rebasing to 0 is correct | ✓ Accurate |
| Fix 1 — always build a clean `isom` `ftyp` | ✓ Correct fix (matches expert recommendation) |
| Missed: `RTSPClient.Init` discards `VideoReturnType` | ✗✗ Critical omission |

## The critical miss

Qwen's whole document analyzes a function that, in the pre-fix repo, is unreachable via the config the user is complaining about.

Pre-fix, `RTSPClient.Init` took 9 loose primitives and rebuilt a fresh `vms.VmsServer{}`, throwing away `VideoReturnType`. So `rc.videoReturnType` is always `""` → `GetVideo` always defaults to `"fmp4"` → the `case "mp4"` branch that calls `ConvertFmp4ToProgressiveMp4` never runs. Qwen never opened `Init`. It deep-dove a dead branch.

This also means Qwen's hypothesis explains none of the user's actual symptoms. The bug report table has 6 wrong rows — ts requests returning fMP4, etc. Every one of those is the Init bug (format defaulted to fMP4). The ftyp-brand issue only affects the labeling of the mp4 case, and that case isn't even reached pre-fix. Net explanatory power of Qwen's "root cause": ~0 of 6 rows.

## The ffprobe error matters

Qwen's causal chain hinges on "ffprobe reads the ftyp box and classifies the file based on the brands." ffprobe does no such thing. Its `format_name` is always `mov,mp4,m4a,3gp,3g2,mj2` for both fragmented and progressive MP4 — there is no "fragmented" verdict in `-show_format` output. The "fragmented MP4 (mp42, hlsf)" label in the bug report came from the test script's `_classify_container()` brand heuristic, which Qwen never saw. So Qwen's stated fix-validation ("ffprobe should see: Major Brand: isom") is built on a false model of the tool.

Ironically, Fix 1 would make the current buggy script stop false-positiving (it keys on `mp42`/`hlsf`) — but that's feeding a broken heuristic cleaner input, not fixing detection. True fragmented-vs-progressive detection requires checking for `moof` boxes, not brands.

## Internal contradiction

Code trace line 122 asserts `moov.AddChild(init.Moov.Mvex)` — that line doesn't exist. Then Fix 2 walks it back: "buildProgressiveMoov does NOT explicitly copy mvex… this may already be fine." Correct conclusion, but the doc ships both the wrong claim and its retraction.

## Credit where due

- The ftyp brand copy is a real bug, correctly located and quoted — it's exactly Part B of the plan I gave you.
- Fix 1 is the correct fix.
- The two-path endpoint architecture is described accurately.
- Good epistemic hygiene: it self-corrects on mvex, hedges on the FFmpeg path, and says "verify which path is hit."
- Its line numbers run ~12–15 higher than the current repo — consistent with the pre-fix Init (the fix deleted ~14 lines), which confirms it genuinely read the old code rather than hallucinating.

## Bottom line

Treat it as a useful supporting note on a secondary bug, not a root-cause diagnosis. For a 27B model running offline at 3 tok/s with a conversion-scoped prompt, the local code reading is solid — but the prompt steered it at the converter, so it found a converter bug and over-promoted it. The title "Conversion Failure" also overstates: the converter produces a structurally valid progressive MP4; only the brand label is stale. The real failure was upstream in Init, which your prior session already fixed.

If you re-task it, point it at the config→struct→client plumbing (`config.go` → `vms.VmsServer` → every driver's `.Init()`), not just the codec layer.

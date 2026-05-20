Now I have a complete picture. Let me compile the detailed report.


---


# Deep-Dive Analysis: fMP4 → Progressive MP4 Conversion Failure


## Root Cause Hypothesis


**The primary root cause is that the `ftyp` box's `major_brand` and `compatible_brands` fields are being copied directly from the fMP4 init segment into the progressive MP4 output, without being updated to standard progressive MP4 brands.**


When the `Fmp4Muxer` creates the init segment via `fmp4.Init.Marshal()`, it embeds fragmented MP4 brands (such as `hlsf`, `dash`, or `iso6`). The conversion function `ConvertFmp4ToProgressiveMp4` in `codecs/fmp4_to_mp4.go` simply copies this `ftyp` box verbatim into the output file. The `moov`, `mdat`, and sample tables are correctly built for progressive MP4, but the `ftyp` box still declares the file as fragmented. Tools like `ffprobe` read the `ftyp` box first and classify the file based on the brands listed there.


A secondary contributing issue: the `moov` box may also carry over the `mvex` (movie extended) container from the init segment, which is a fragmented-MP4-only box and has no meaning in progressive MP4.


---


## Code Trace: Step-by-Step Data Flow


### 1. API Request Entry (endpoints/endpoints.go)


```
GET /v2/streams/{id}/video/mp4
 └─> getVideoEndpoints() (line 126)
      └─> case 3: pathSegments[3] == "mp4" (line 172)
           └─> getVideoOfType(streamId, starttime, endtime, "mp4") (line 173)
```


**Wait -- there's a critical branching path here.** `getVideoOfType` does NOT call `ConvertFmp4ToProgressiveMp4`. Let me re-examine.


### 2. Two Different Code Paths


**Path A: `getVideo()` (for default/`fmp4` format)**
```
getVideo() (endpoints.go:787)
 └─> getVideoInternal() (endpoints.go:849)
      └─> serviceptr.GetVideo() (endpoints.go:882)
           └─> RTSPClient.GetVideo() (rtsp_client.go:1638)
                └─> format="fmp4" (default, line 1642)
                     └─> getFmp4Stream() → returns fMP4 bytes directly
```


**Path B: `getVideoOfType()` (for explicit format like "mp4")**
```
getVideoOfType() (endpoints.go:902)
 └─> serviceptr.GetVideoOfType() (endpoints.go:917)
      └─> RTSPClient.GetVideoOfType() (rtsp_client.go:1615)
           └─> getMpegTsStream() → ConvertMpegTsToFormatPipe()
```


**Path B uses FFmpeg pipe conversion, NOT the native Go converter.** This is a separate code path that uses `utils/ConvertMpegTsToFormatPipe()` which invokes FFmpeg.


**Path A's "mp4" format case:**
```
RTSPClient.GetVideo() (rtsp_client.go:1638)
 └─> format = rc.videoReturnType (line 1640)
      └─> If format == "", defaults to "fmp4" (line 1642)
      └─> Switch format:
           case "mp4":  (line 1656)
                fmp4Data = getFmp4Stream()  (line 1658)
                data = codecs.ConvertFmp4ToProgressiveMp4(fmp4Data)  (line 1662) ← THE CONVERSION
```


So the native Go conversion IS invoked when `videoReturnType` is set to `"mp4"`. But the default is `"fmp4"`, which bypasses conversion entirely.


### 3. fMP4 Generation (rtsp_client.go)


```
getFmp4Stream() (rtsp_client.go:1865)
 └─> buf.GetItemsBetween() → collects AU frames
 └─> Fmp4Muxer.Initialize(codec, vps, sps, pps) (line 2016)
 └─> For each frame: muxer.WriteH264/H265(nalus, normalizedPTS) (lines 2069/2087)
 └─> muxer.Close() (line 2096)
 └─> return muxer.Bytes() (line 2098)  ← Returns init + all media segments
```


### 4. fMP4 Muxer Output Structure (fmp4_muxer.go)


The `Fmp4Muxer.Bytes()` method returns:
```
[ftyp][moov][moof1][mdat1][moof2][mdat2]...[moofN][mdatN]
```


The init segment contains:
- `ftyp` with fragmented MP4 brands (set by `fmp4.Init.Marshal()`)
- `moov` with `mvex`/`trex` (fragmented-only boxes)


### 5. Conversion Function (fmp4_to_mp4.go)


```
ConvertFmp4ToProgressiveMp4(fmp4Data) (line 47)
 └─> mp4.DecodeFile(fmp4Reader) (line 54)
      └─> parsedFile.Init.Ftyp ← fragmented MP4 brands!
      └─> parsedFile.Segments → parsedFile.Segments[0].Fragments[]
 └─> Check IsFragmented() (line 60)
 └─> Collect samples from all fragments (lines 101-112)
 └─> ftyp = init.Ftyp ← COPIED VERBATIM (line 136) ← ROOT CAUSE
 └─> buildProgressiveMoov(init, videoTrack, 0) (line 142)
      └─> moov.AddChild(init.Moov.Mvex) ← mvex copied too
 └─> Calculate stco offset (line 156)
 └─> Update stco.ChunkOffset[0] (line 159)
 └─> Write ftyp.Encode() → moov.Encode() → mdat header + data (lines 174-224)
```


---


## Specific Box/Atom Analysis


### ftyp Box (THE PRIMARY BUG)


**Location:** `fmp4_to_mp4.go` lines 134-139


```go
var ftyp *mp4.FtypBox
if init.Ftyp != nil {
   ftyp = init.Ftyp    // ← COPIED DIRECTLY from fMP4 init segment
} else {
   ftyp = mp4.NewFtyp("isom", 0x200, []string{"isom", "iso2", "avc1", "mp41"})
}
```


**Current behavior:** When the init segment has an `ftyp` box (which it always does), the code uses it verbatim. The `fmp4.Init.Marshal()` in `fmp4_muxer.go` generates an `ftyp` with brands specific to fragmented MP4 streaming (e.g., `hlsf` for HLS, `dash` for DASH, or `iso6` for CMAF).


**Expected behavior:** The output should have `major_brand="isom"` or `"mp42"` with `compatible_brands` like `["isom", "iso2", "avc1", "mp41"]` -- the standard progressive MP4 brand set.


**ffprobe sees:**
```
Major Brand: hlsf (or dash, iso6)
Compatible Brands: hlsf, iso6, mp41  ← identifies as fragmented
```


**ffprobe should see:**
```
Major Brand: isom
Compatible Brands: isom, iso2, avc1, mp41  ← identifies as progressive
```


### moov Box


**Location:** `fmp4_to_mp4.go` lines 243-268


The `moov` box is built with:
- `mvhd` (movie header) -- copied from init with updated duration
- `trak` (track) -- rebuilt with complete `stbl` (sample table)
- `mvex` (movie extended) -- **copied verbatim from init** -- this is a fragmented-MP4-only box


**Issue:** The `mvex` box is unnecessary and potentially confusing in a progressive MP4. It contains `trex` (track extend) boxes that reference fragment-level track properties. In a progressive MP4, all track info lives in the `stbl` (sample table) boxes.


### moof / mdat Boxes


**moof (fragment movie fragment):** The conversion correctly **eliminates** all `moof` boxes. The progressive MP4 output has no `moof` boxes -- all sample info is in the `stbl`. This is correct.


**mdat:** The conversion correctly concatenates all sample payloads into a single `mdat` box. The `stco` offset is calculated to point to the start of this `mdat` data. This is correct.


### Sample Table (stbl) -- Correct


The sample table is properly built:
- `stsd` -- copied from init (codec config)
- `stts` -- built from sample durations (lines 349-369)
- `stss` -- built from `IsSync()` flag (lines 372-383)
- `stsc` -- all samples in one chunk (lines 386-391)
- `stsz` -- sample sizes from `len(s.Data)` (lines 394-400)
- `stco` -- correctly calculated offset (line 159)
- `ctts` -- only if B-frames present (lines 409-431)


### Timestamp Rebasng -- Correct


In `rtsp_client.go` lines 2021-2035:
```go
var firstPTS int64
...
pts := int64(frame.PTS.Seconds() * 90000)
if count == 0 {
   firstPTS = pts
}
normalizedPTS := pts - firstPTS
```


PTS values are rebased to start at 0. This is correct.


---


## Proposed Fixes


### Fix 1: Replace ftyp Box with Progressive MP4 Brands (PRIMARY FIX)


In `fmp4_to_mp4.go`, replace lines 134-139:


```go
// OLD (buggy):
var ftyp *mp4.FtypBox
if init.Ftyp != nil {
   ftyp = init.Ftyp
} else {
   ftyp = mp4.NewFtyp("isom", 0x200, []string{"isom", "iso2", "avc1", "mp41"})
}


// NEW (fixed):
// Always create a new ftyp box with progressive MP4 brands.
// The init segment's ftyp may contain fragmented MP4 brands (hlsf, dash, iso6)
// which would cause ffprobe to misidentify the output as fragmented MP4.
ftyp = mp4.NewFtyp("isom", 0x200, []string{"isom", "iso2", "avc1", "mp41"})
```


This is a 3-line change. The `else` branch already has the correct logic; we just need to always use it.


### Fix 2: Remove mvex from moov Box (SECONDARY FIX)


In `fmp4_to_mp4.go` `buildProgressiveMoov()`, after copying `mvhd` and before adding `trak`, do NOT copy `mvex`:


```go
// DO NOT copy mvex -- it's a fragmented-MP4-only box
// moov.AddChild(init.Moov.Mvex)  ← REMOVE THIS LINE
```


The `mvex` box is already being copied implicitly through the `init` reference. Looking at the code more carefully, `buildProgressiveMoov` does:
```go
mvhd := *init.Moov.Mvhd
moov.AddChild(&mvhd)
```


It does NOT explicitly copy `mvex`. However, `mp4.NewMoovBox()` may or may not include `mvex`. The `trex` boxes are found in the init segment's `mvex` and used during sample collection, but they're not explicitly copied into the output `moov`. So this may already be fine.


### Fix 3: Verify the API Endpoint Path


The user mentioned requesting `mp4` via `/v2/streams/{id}/video/mp4`. Looking at the code:


- `getVideoOfType()` is called for explicit format requests (e.g., `/video/mp4`)
- `getVideoOfType()` calls `GetVideoOfType()` on the RTSPClient
- `GetVideoOfType()` uses `ConvertMpegTsToFormatPipe()` (FFmpeg pipe), NOT `ConvertFmp4ToProgressiveMp4()`


**This means the endpoint `/v2/streams/{id}/video/mp4` does NOT use the native Go converter at all.** It goes through FFmpeg's `ConvertMpegTsToFormatPipe`. If FFmpeg is producing fragmented MP4 output, that's a separate issue.


However, if `videoReturnType` is set to `"mp4"` in the config, then `GetVideo()` (the default path) would use the native Go converter, and **that** is where the ftyp brand bug would apply.


### Recommended Immediate Action


1. **Apply Fix 1** to `fmp4_to_mp4.go` (replace ftyp with progressive brands)
2. **Verify** which code path is actually being hit by the test script (check if `videoReturnType` is set to `"mp4"` in config, or if the request goes through `getVideoOfType`)
3. **If** the FFmpeg path is being used, that's a separate investigation (FFmpeg flags like `-movflags +faststart` may be needed)



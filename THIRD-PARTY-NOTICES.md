# Third-party notices

Tiny Cam Viewer itself is MIT licensed (see `LICENSE`).

## Runtime library: `libmpv-2.dll`

The program needs one DLL next to the exe. It is **not stored in this git
repository**. `scripts\fetch-libmpv.ps1` downloads it.

| Item | Value |
|---|---|
| File | `libmpv-2.dll` (64-bit) |
| Build | `mpv-dev-lgpl-x86_64` from <https://github.com/zhongfly/mpv-winbuild> |
| mpv license | **LGPL v2.1 or later** (this build is compiled without GPL parts) |
| FFmpeg inside | LGPL build (`libavcodec`, `libavformat`, `libavutil`, `libswscale`, `libswresample`, ...) |
| Other libraries inside | libplacebo (LGPL 2.1+), libass (ISC), FreeType (FTL), HarfBuzz (MIT), zlib, and others. Each has a permissive or LGPL license |

All FFmpeg decoders used here (H.264, H.265, MJPEG, and so on) are part of the
LGPL build. Hardware decoding uses the Windows D3D11VA API, so no extra
vendor DLL is needed.

### What the LGPL means for you

- You may use and give away the program, also in commercial use.
- `libmpv-2.dll` is a separate file (dynamic link). You can replace it with
  your own build of mpv. Do not remove this possibility.
- If you give the DLL to other people, give them the license text and a way to get its source:
  - mpv source: <https://github.com/mpv-player/mpv>
  - FFmpeg source: <https://github.com/FFmpeg/FFmpeg>
  - Build scripts: <https://github.com/zhongfly/mpv-winbuild>
- Do **not** swap in a `gpl` build of mpv/FFmpeg unless you accept the GPL for the whole package.
  The GPL builds also work with this program, but then you must follow the GPL.

## Go modules

| Module | License |
|---|---|
| `golang.org/x/sys` | BSD-3-Clause |
| Go standard library | BSD-3-Clause |

The libmpv DLL is loaded at run time with `LoadLibrary`. There is no cgo and no C compiler is needed.

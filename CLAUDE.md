# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```sh
go build -o tc ./cmd/tc            # the client
go test ./...                      # unit tests; integration ones skip
go vet ./... && gofmt -l .         # gofmt must print nothing
./tc -camera                       # camera and render modes, no network needed

cd worker
npm run dev                        # wrangler dev on 127.0.0.1:8787
npm run check                      # node --check every source file
npm run deploy                     # deploy to Cloudflare
npm run logs                       # wrangler tail
npx wrangler deploy --dry-run --outdir /tmp/build   # bundles without deploying
```

Running one test, or one package:

```sh
go test ./internal/video/ -run TestBrailleFindsAnEdge -v
go test ./internal/video/ -run 'TestRender|TestFit' -count=1   # -count=1 defeats the cache
go test ./internal/app/ -v
```

### Tests that need something

Integration tests are skipped unless `TC_HOST` is set. They are the only ones
that prove anything about the Durable Object or WebRTC, so run them before
believing a change to either:

```sh
cd worker && npx wrangler dev --port 8787 --local &
TC_HOST=http://127.0.0.1:8787 go test ./... -count=1
TC_HOST=https://call.dharun.dev go test ./... -count=1   # against production

TC_CAMERA=1 go test ./internal/video/ -run Camera -v      # needs a real webcam
```

`internal/call` spins up two real peers in one process and connects them over
host candidates, so it exercises offer, answer, trickled candidates, the data
channel and JPEG both ways without leaving the machine. What it cannot prove
is NAT traversal; only two machines on different networks can show that.

### Release builds

Release binaries carry ffmpeg. Development builds deliberately do not — it is
a 29 MB download and it turns `go build` into something you wait for:

```sh
tools/fetch-ffmpeg.sh windows amd64        # writes internal/video/ffmpeg.bin.gz
go build -tags embedffmpeg -ldflags "-s -w" -o tc.exe ./cmd/tc
go test -tags embedffmpeg ./internal/video/ -run Embedded -v   # unpacks it and runs it
```

That takes the binary from ~7 MB to ~40 MB. `.github/workflows/release.yml`
builds all five targets on one Linux runner — Go cross-compiles and the
embedded ffmpeg is just data, so no target needs its own machine.

`deploy.yml` is `workflow_dispatch` only, deliberately: a push to main must
never replace what is live.

### Verifying the interface

The TUI cannot be driven by piping stdin — it refuses to start without a
terminal, by design, and CI asserts that refusal. Verify screens by running
them. Verify logic through the pure functions in `internal/video` and
`internal/app`, which is why the rendering, wrapping, levels and layout maths
all live in functions that take values and return values.

## Architecture

One Worker over one Durable Object class, plus a Go client. No database, no
framework, no build step beyond Wrangler's bundler and `go build`.

**Chat is relayed; video is not.** This is the load-bearing decision and
everything follows from it. Cloudflare bills one request per *incoming*
WebSocket message, so relaying 10 fps from four people would spend a quarter
of the 100k/day free tier on a single ten-minute call. Chat messages are rare
and small, so they go through the Durable Object. Video frames go peer-to-peer
over WebRTC and the Durable Object sees only the handshake — about twenty
messages. **Do not add a server-side video path.**

**A JPEG crosses the wire, not characters.** Rendering happens on the machine
that displays it. That is what lets a viewer change mode mid-call and fit
every tile to their own terminal, and it is also smaller — a screen of ANSI
colour codes compresses no better than a photograph of the same scene. Frames
are ~1.5 KB at 192×144, so a full four-way call sends ~45 KB/s upstream.

**A room exists because `born` is in storage.** A Durable Object exists the
moment you name it, so "does this room exist" cannot be a fact about the
object; it has to be one written down. Create refuses a code that has `born`,
join refuses one that does not, and the last person out calls `deleteAll`.
Remove that and every code ever typed stays claimed for good.

**Newcomers offer.** You open a WebRTC connection to everyone who was already
in the room when you arrived, and wait for anyone who arrives after you. That
is the whole of the glare avoidance: for any two people, exactly one arrived
second. There is no tie-break rule and none is needed.

**Renderers are per person.** `internal/app` keeps one `video.Renderer` for
each peer because auto-levels is per picture. Sharing one would let somebody
in bright sunlight set the exposure for everyone in a dark room.

**Auto-levels is not a filter, it is required.** A webcam indoors puts its
whole picture into a band roughly fifteen luminance levels wide; a ten-step
ramp maps that onto one character. `internal/video/levels.go` stretches it,
smoothed across frames so the picture settles rather than pulsing.

## Things that will bite

**The Worker must complete the close handshake.** `webSocketClose` calls
`ws.close()`. Workers does not echo a close frame for you, and a client that
politely waits for one blocks until its own timeout — five seconds in the Go
client, which turned "leave the room" into a five-second hang and the test
suite from 3s into 63s.

**WebSocket dials must be HTTP/1.1.** Go's default transport negotiates
HTTP/2 over TLS, where `Upgrade` is not permitted, and the handshake then
hangs until the deadline rather than failing legibly. `client.dialHTTP` pins
this with a non-nil, empty `TLSNextProto`. Never hand `websocket.Dial` a nil
`HTTPClient`.

**Limits are mirrored, not shared.** `worker/src/limits.js` and
`internal/proto/proto.go` both carry the capacities, the code alphabet and the
text length. Change them together or rooms disagree about what fits in them.

**Handshake errors travel in a header.** A rejected WebSocket upgrade does not
reliably deliver its body to the client, but headers always arrive, so the
Durable Object sends `x-termcall-error: taken|notfound|full|mode` and
`internal/client` maps those to sentinel errors.

**`from` means two things.** In a `msg` event it is a display name; in a `sig`
event the sender is `peer` and holds an ID. Names are unique within a room and
shown to people; IDs address peers and are never displayed.

**`.gitignore` entries for the binary are anchored** (`/tc`, not `tc`).
Unanchored, `tc` also matches the `cmd/tc/` directory and silently drops
`main.go` from the repository.

**Shell scripts need their execute bit set in git explicitly.** The main
development machine is Windows, where git does not track file modes, so
`chmod +x` locally changes nothing that gets committed and the script lands
in CI unrunnable (`Permission denied`, exit 126). Use
`git update-index --chmod=+x <path>` when adding one. Workflows invoke them
as `sh tools/whatever.sh` so the bit is not load-bearing either way.

**The installers must keep `__HOST__`.** The Worker substitutes it per
request, so a fork deployed anywhere installs itself from where it is actually
running. CI checks the placeholder is still there, and that neither script has
CRLF endings.

## Layout

```
cmd/tc/            entry point and flags
internal/proto/    the wire contract; mirrors worker/src/limits.js
internal/client/   one WebSocket connection to one room
internal/call/     the WebRTC mesh: handshake, data channels, frames
internal/video/    capture (ffmpeg), auto-levels, three render modes, JPEG
internal/ui/       raw mode, alternate screen, key decoding, colour
internal/app/      the screens: menus, prompts, chat, call, camera check
worker/src/        the Worker, the Durable Object, landing page, installers
tools/             fetch-ffmpeg.sh
```

Everything drawn to the screen goes through `ui.Buf` and is flushed in one
write. A partial frame reaching a terminal is what tearing looks like.

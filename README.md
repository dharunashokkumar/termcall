# termcall

Chat rooms and video calls that live in your terminal. The video is characters.

```
$ tc

  termcall

  1  chat room       talk to up to 10 people
  2  video call      ASCII video, up to 4 people
  3  camera check    see yourself, try the render modes
```

One binary. Nothing to install alongside it — ffmpeg rides along inside.

## Install

```sh
# macOS / Linux
curl -fsSL https://call.dharun.dev/install.sh | sh

# Windows
irm https://call.dharun.dev/install.ps1 | iex
```

Or take a binary from [the releases page](https://github.com/dharunashokkumar/termcall/releases).

## Using it

Run `tc`. Pick a chat room or a video call, then create one or join one.
Creating gives you a six-character code; anyone with the code and `tc` can
join. Codes avoid `0`, `O`, `1`, `I` and `L`, so one read down a phone line
survives the trip.

In a room:

| key | |
|---|---|
| `enter` | send a message |
| `↑` `↓` | scroll back through the transcript |
| `m` | change the video render mode |
| `esc` | leave |

Rooms are not stored. When the last person leaves, the room is deleted and
its code is free again.

### The three render modes

`m` cycles them mid-call, and it applies to everyone on your screen.

**blocks** — the default. Each cell is `▀` carrying two colours, the
foreground painting the top half and the background the bottom, so one cell
shows two pixels in full 24-bit colour. The most picture per character.

**ascii** — one character per cell, chosen from ` .:-=+*#%@` by brightness and
tinted with the colour it stands for. The classic look, half the vertical
detail.

**braille** — 2×4 dots per cell, so four times the resolution, with colour per
cell rather than per dot. Thresholded against each cell's own average, which
makes it find edges. Good for outlines, weaker on flat colour.

All three run through auto-levels first. A webcam indoors puts almost
everything it sees into a narrow band — measuring one gave a picture living
entirely between luminance 125 and 140 — and a ten-step ramp maps that whole
range onto a single character. Stretching what is actually there across the
full range is the difference between a picture and a texture.

## How it works

One Cloudflare Worker, one Durable Object per room, and a Go client. No
database, no build step, no framework.

**Chat goes through the Durable Object.** Every message is relayed there and
fanned out to the room. Text is small and rare, so this costs almost nothing.

**Video does not.** After the handshake, frames go directly between terminals
over WebRTC, and Cloudflare carries nothing further.

That split is the whole design, and it is a billing decision as much as an
architectural one. Cloudflare charges one request per *incoming* WebSocket
message. Relaying ten frames a second from four people is 24,000 requests in a
ten-minute call — a quarter of the free tier's daily budget, gone on one call.
The same call peer-to-peer costs about twenty messages, all of them handshake.

**What crosses the wire is a JPEG, not characters.** Each terminal renders
locally, which is why you can change mode mid-call and why every tile fits
whatever size your window happens to be. It is also *smaller*: ANSI colour
codes are bulky enough that a screenful compresses no better than a photograph
of the same scene. A frame is about 1.5 KB, so a full four-way call sends
roughly 45 KB/s upstream.

**Everyone connects to everyone.** A mesh, not a server. At four people that
is three connections each, which is exactly why video rooms stop at four: the
cost of a mesh climbs as the square, and a fifth person makes it worse for
everyone already there.

```
  chat                          video
  ────                          ─────
  you ─┐                        you ─────────────┐
       ├─→ Durable Object            └─ handshake ─→ Worker
  them ┘      │                                        │
              └─→ everyone       them ←─────────────────┘
                                   ↕
                                 frames, direct
```

### Staying inside the free tier

Everything runs on Cloudflare's free plan, and it is not close:

| | free allowance | what termcall uses |
|---|---|---|
| Worker requests | 100,000/day | ~1,000 for ten people chatting a hundred messages each |
| Durable Objects | free plan, SQLite-backed | one per live room, hibernating when idle |
| Video relay | — | none; peer-to-peer after the handshake |

Idle rooms hibernate, so holding a room open bills no duration. Keepalives are
protocol-level WebSocket pings, which the runtime answers at the edge without
waking the Durable Object — an application-level ping would bill a request
every time.

The one thing that may eventually need something else is TURN, the relay for
networks too strict for peer-to-peer to cross. `/ice` serves the list, so
adding one is three secrets on the Worker and no client update:

```sh
npx wrangler secret put TURN_URL     # turn:host:3478
npx wrangler secret put TURN_USER
npx wrangler secret put TURN_PASS
```

## Building it

```sh
go build -o tc ./cmd/tc          # needs a system ffmpeg for video
go test ./...                    # unit tests; no server needed

cd worker && npm run dev         # wrangler dev on 127.0.0.1:8787
TC_HOST=http://127.0.0.1:8787 go test ./...   # integration tests too
```

The integration tests are skipped unless `TC_HOST` is set, because what they
check — that create refuses a live code, that a room fills up, that two peers
complete a WebRTC handshake — are decisions the real server makes. Testing
them against a mock would only prove the mock agrees with itself.

To build a release binary carrying ffmpeg:

```sh
tools/fetch-ffmpeg.sh linux amd64
GOOS=linux GOARCH=amd64 go build -tags embedffmpeg -ldflags "-s -w" ./cmd/tc
```

That takes the binary from about 7 MB to about 40 MB. It is worth it: a person
downloads one file and it works. A system ffmpeg on `PATH` is still preferred
at runtime when there is one, so the unpacking is skipped for anyone who
already has it.

| environment variable | |
|---|---|
| `TC_HOST` | the server to use, default `call.dharun.dev` |
| `TC_FFMPEG` | a specific ffmpeg to use instead of searching |

## Licence

MIT — see [LICENSE](LICENSE).

Release binaries bundle a GPLv3 build of FFmpeg, run as a separate program
rather than linked. See [NOTICE](NOTICE).

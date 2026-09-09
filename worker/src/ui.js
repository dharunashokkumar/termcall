// The one page this service serves to a browser. Its only job is to tell a
// person who found the URL how to get the client, because the client is where
// everything actually happens.

export const landing = (host) => `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>termcall</title>
<style>
  :root { color-scheme: dark; --bg:#0b0d10; --fg:#d7dae0; --dim:#6b7280; --ac:#7dd3a0; }
  * { box-sizing: border-box; }
  body { margin:0; background:var(--bg); color:var(--fg); font:14px/1.65 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace; }
  main { max-width:46rem; margin:0 auto; padding:5rem 1.25rem 6rem; }
  h1 { font-size:1.5rem; margin:0 0 .25rem; letter-spacing:.02em; }
  h1 span { color:var(--ac); }
  p.tag { color:var(--dim); margin:0 0 3rem; }
  h2 { font-size:.8rem; text-transform:uppercase; letter-spacing:.12em; color:var(--dim); margin:2.75rem 0 .75rem; font-weight:600; }
  pre { background:#12151a; border:1px solid #1e232b; border-radius:8px; padding:.85rem 1rem; overflow-x:auto; margin:0 0 .75rem; }
  code { color:var(--ac); }
  a { color:var(--ac); }
  ul { padding-left:1.1rem; color:var(--dim); }
  li { margin:.3rem 0; }
  footer { color:var(--dim); margin-top:3.5rem; font-size:.85rem; }
</style>
</head><body><main>

<h1>term<span>call</span></h1>
<p class="tag">Chat rooms and ASCII video calls that live in your terminal.</p>

<pre>$ tc

  <span style="color:#6b7280">termcall</span>

  1  chat room
  2  video call
  q  quit</pre>

<h2>Install</h2>
<pre>macOS / Linux   curl -fsSL https://${host}/install.sh | sh
Windows         irm https://${host}/install.ps1 | iex</pre>
<p style="color:var(--dim);margin:0">Or take a binary from
<a href="https://github.com/dharunashokkumar/termcall/releases">the releases page</a>.
One file, nothing else to install &mdash; ffmpeg rides along inside it.</p>

<h2>How it works</h2>
<ul>
  <li>Chat messages pass through a Cloudflare Durable Object, one per room.
      Text is small and rare, so a room costs close to nothing to run.</li>
  <li>Video does not. After the handshake, frames go straight between
      terminals over WebRTC &mdash; Cloudflare carries the introduction and
      then nothing at all.</li>
  <li>What crosses the wire is a small JPEG, not characters. Each terminal
      renders it locally, so everyone picks their own look and their own size,
      and can change either mid-call.</li>
  <li>Nothing is stored anywhere. When the last person leaves a room, it is
      deleted and its code is free again.</li>
</ul>

<footer>
  <a href="https://github.com/dharunashokkumar/termcall">source</a> &middot;
  MIT &middot; by Dharun Ashokkumar
</footer>

</main></body></html>`;

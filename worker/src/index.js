// termcall — terminal chat rooms and ASCII video calls.
//
// The Worker is a router and nothing else. All room state lives in the Room
// Durable Object (src/room.js); all rendering lives in the Go client. There
// is no database, no build step and no framework.

import { validCode, CODE_ALPHABET, CODE_LEN } from "./limits.js";
import { landing } from "./ui.js";
import installSh from "./install.sh";
import installPs1 from "./install.ps1";

export { Room } from "./room.js";

const json = (body, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });

export default {
  async fetch(req, env) {
    const url = new URL(req.url);
    const path = url.pathname;

    if (path === "/" || path === "/index.html") {
      return new Response(landing(url.host), {
        headers: { "content-type": "text/html; charset=utf-8" },
      });
    }

    // What the client should hand WebRTC so two terminals can find each other
    // through their routers. STUN is enough for most networks and costs
    // nothing; TURN is the paid-by-bandwidth fallback for the rest, and is
    // only included once its credentials are configured.
    if (path === "/ice") {
      const servers = [
        { urls: ["stun:stun.cloudflare.com:3478", "stun:stun.l.google.com:19302"] },
      ];
      if (env.TURN_URL && env.TURN_USER && env.TURN_PASS) {
        servers.push({
          urls: env.TURN_URL.split(",").map((s) => s.trim()).filter(Boolean),
          username: env.TURN_USER,
          credential: env.TURN_PASS,
        });
      }
      return json({ iceServers: servers });
    }

    // The client picks its own room code and asks to create it; the Durable
    // Object rejects the code if it is already live. Handing out codes from
    // here instead would cost an extra request per room for no benefit --
    // 887 million codes make a collision something the 409 handles, not
    // something worth a round trip to prevent.
    const room = path.match(/^\/r\/([^/]+)$/);
    if (room) {
      const code = decodeURIComponent(room[1]).toUpperCase();
      if (!validCode(code)) {
        return new Response(
          `a room code is ${CODE_LEN} characters from ${CODE_ALPHABET}`,
          { status: 400 }
        );
      }
      const id = env.ROOM.idFromName(code);
      // Forward with the code uppercased, so "abc123" and "ABC123" are one room.
      const fwd = new URL(url);
      fwd.pathname = `/r/${code}`;
      return env.ROOM.get(id).fetch(new Request(fwd, req));
    }

    // The scripts carry __HOST__ rather than a hard-coded domain, so a fork
    // deployed anywhere installs itself from where it is actually running.
    if (path === "/install.sh" || path === "/install.ps1") {
      const body = (path === "/install.sh" ? installSh : installPs1)
        .replaceAll("__HOST__", url.host);
      return new Response(body, {
        headers: { "content-type": "text/plain; charset=utf-8" },
      });
    }

    if (path === "/health") return json({ ok: true });

    return new Response("not found", { status: 404 });
  },
};

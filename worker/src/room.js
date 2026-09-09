// One Durable Object per room. It holds the WebSockets and relays between them.
//
// It does two jobs for two kinds of room, and the split is the whole design:
//
//   chat   every message is relayed here. Text is small and rare, so the
//          Durable Object carrying it costs almost nothing.
//   video  only the WebRTC handshake is relayed here. The ASCII frames never
//          touch this object; they go peer to peer. Cloudflare bills one
//          request per *incoming* WebSocket message, so relaying 10 frames a
//          second from 4 people would spend a quarter of the 100k/day free
//          tier on a single ten minute call. The handshake is ~20 messages.
//
// Sockets are accepted with the hibernation API, so an idle room is evicted
// from memory and bills no duration until someone speaks.

import { CHAT_MAX, VIDEO_MAX, TEXT_MAX, NAME_MAX } from "./limits.js";

// Refusals carry a machine-readable reason in a header as well as prose in the
// body. A failed WebSocket handshake does not reliably hand its body to the
// client, but the response headers always survive, so the header is what the
// Go client actually reads to decide what to tell the person.
const deny = (reason, msg, status) =>
  new Response(msg, { status, headers: { "x-termcall-error": reason } });

export class Room {
  constructor(ctx, env) {
    this.ctx = ctx;
    this.env = env;
  }

  // Everyone currently connected, as {ws, id, name}. Read back out of each
  // socket's attachment rather than kept in a field: after hibernation this
  // object is reconstructed with no memory of who was here.
  peers() {
    return this.ctx.getWebSockets().map((ws) => {
      const a = ws.deserializeAttachment() || {};
      return { ws, id: a.id, name: a.name };
    });
  }

  roster() {
    return this.peers().map((p) => ({ id: p.id, name: p.name }));
  }

  // Send to everyone except `exceptId`. Outgoing messages are not billed as
  // requests, so fanning out is free; only what arrives costs.
  broadcast(msg, exceptId) {
    const text = JSON.stringify(msg);
    for (const p of this.peers()) {
      if (p.id === exceptId) continue;
      try {
        p.ws.send(text);
      } catch {
        // Socket died between the roster read and the send. The close
        // handler will clean it up.
      }
    }
  }

  async fetch(req) {
    const url = new URL(req.url);
    const create = url.searchParams.get("create") === "1";
    const mode = url.searchParams.get("mode") === "video" ? "video" : "chat";
    const name = (url.searchParams.get("name") || "").trim().slice(0, NAME_MAX);

    if (!name) return deny("name", "a name is required", 400);
    if (req.headers.get("Upgrade") !== "websocket") {
      return deny("upgrade", "expected a websocket", 426);
    }

    // `born` is what makes create and join different. A Durable Object always
    // exists the moment you name it, so "does this room exist" has to be a
    // fact we wrote down, not a fact about the object.
    const born = await this.ctx.storage.get("born");
    if (create && born) {
      return deny("taken", "that room code is taken", 409);
    }
    if (!create && !born) {
      return deny("notfound", "no room with that code", 404);
    }

    if (create) {
      await this.ctx.storage.put({ born: Date.now(), mode });
    } else {
      const roomMode = await this.ctx.storage.get("mode");
      if (roomMode !== mode) {
        const is = roomMode === "video" ? "a video call" : "a chat room";
        return deny("mode", `that code is ${is}`, 409);
      }
    }

    const cap = mode === "video" ? VIDEO_MAX : CHAT_MAX;
    const here = this.peers();
    if (here.length >= cap) {
      return deny("full", `room is full (${cap} people)`, 409);
    }

    // Names are how people address each other, so they have to be unique
    // inside a room. Ids are how the code addresses them, and stay hidden.
    let final = name;
    for (let n = 2; here.some((p) => p.name === final); n++) {
      final = `${name}${n}`;
    }
    const id = crypto.randomUUID().slice(0, 8);

    const pair = new WebSocketPair();
    const [client, server] = [pair[0], pair[1]];
    this.ctx.acceptWebSocket(server);
    server.serializeAttachment({ id, name: final, mode });

    // Tell the newcomer who is already here, then tell everyone else about
    // the newcomer. Order matters: the new peer must not see itself in the
    // arrival broadcast.
    server.send(
      JSON.stringify({
        t: "welcome",
        you: { id, name: final },
        mode,
        cap,
        peers: here.map((p) => ({ id: p.id, name: p.name })),
      })
    );
    this.broadcast({ t: "join", who: { id, name: final } }, id);

    return new Response(null, { status: 101, webSocket: client });
  }

  async webSocketMessage(ws, raw) {
    const me = ws.deserializeAttachment() || {};
    if (typeof raw !== "string") return;
    if (raw.length > TEXT_MAX * 4) return; // JSON overhead on a full message

    let m;
    try {
      m = JSON.parse(raw);
    } catch {
      return;
    }

    switch (m.t) {
      // A chat line. Stamped with the sender and the clock here rather than
      // on the client, so nobody can forge either.
      case "say": {
        const text = String(m.text || "").slice(0, TEXT_MAX);
        if (!text) return;
        this.broadcast({ t: "msg", from: me.name, text, ts: Date.now() });
        return;
      }

      // A WebRTC offer, answer or ICE candidate on its way to one other peer.
      // The body is opaque here; this object never looks inside it.
      case "sig": {
        const to = this.peers().find((p) => p.id === m.to);
        if (!to) return;
        try {
          to.ws.send(JSON.stringify({ t: "sig", peer: me.id, data: m.data }));
        } catch {
          // Peer vanished mid-handshake; its close handler will tidy up.
        }
        return;
      }

      case "ping":
        try {
          ws.send(JSON.stringify({ t: "pong" }));
        } catch {}
        return;
    }
  }

  async webSocketClose(ws, code, reason) {
    this.depart(ws, code, reason);
    await this.reap(ws);
  }

  async webSocketError(ws) {
    this.depart(ws, 1011, "error");
    await this.reap(ws);
  }

  // depart completes the closing handshake and tells the room.
  //
  // Workers does not echo a close frame back for us. A client that waits for
  // one -- which most WebSocket libraries do, politely -- sits blocked until
  // its own timeout fires instead. In the Go client that timeout is five
  // seconds, which turns "leave the room" into a five second hang.
  depart(ws, code, reason) {
    const me = ws.deserializeAttachment() || {};
    try {
      // 1005 and 1006 describe how a connection ended; neither may be sent
      // back inside a close frame, so anything odd becomes a plain 1000.
      const ok = code >= 1000 && code < 5000 && code !== 1005 && code !== 1006;
      ws.close(ok ? code : 1000, reason || "bye");
    } catch {
      // Already gone. Nothing to hand the handshake to.
    }
    this.broadcast({ t: "part", who: { id: me.id, name: me.name } }, me.id);
  }

  // The last person out deletes the room, which frees the code for reuse and
  // leaves nothing behind to bill. The departing socket is excluded by
  // identity: it may still be listed while its own close handler runs, and
  // trusting the count alone would leave every code ever used claimed for
  // good.
  async reap(leaving) {
    const rest = this.ctx.getWebSockets().filter((w) => w !== leaving);
    if (rest.length === 0) {
      await this.ctx.storage.deleteAll();
    }
  }
}

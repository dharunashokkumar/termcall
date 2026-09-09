// Shared between the Worker and the Durable Object, and mirrored in the Go
// client (internal/proto/proto.go). Change them together.

// A chat room is a conversation, so it can hold a crowd. A video call is a
// peer-to-peer mesh: at N people every client encodes for and decodes from
// N-1 others, so the cost climbs as the square. Four is where a terminal
// still has the rows to show everyone.
export const CHAT_MAX = 10;
export const VIDEO_MAX = 4;

export const TEXT_MAX = 2000;
export const NAME_MAX = 24;

// Room codes avoid 0/O/1/I/L so a code read aloud or off a screen cannot be
// mistyped. 31 characters over 6 places is ~887 million rooms.
export const CODE_ALPHABET = "23456789ABCDEFGHJKMNPQRSTUVWXYZ";
export const CODE_LEN = 6;

export function validCode(s) {
  if (typeof s !== "string" || s.length !== CODE_LEN) return false;
  for (const c of s) if (!CODE_ALPHABET.includes(c)) return false;
  return true;
}

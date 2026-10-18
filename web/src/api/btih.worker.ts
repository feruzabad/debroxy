// Web Worker wrapper around the .torrent info-hash (docs/frontend/revision2.md
// §5.5.4, §12): hashing a large .torrent runs off the main thread.

import { torrentInfoHash } from "./btih";

interface HashRequest {
  id: number;
  data: ArrayBuffer;
}

type HashResponse = { id: number; fingerprint: string | null } | { id: number; error: string };

self.onmessage = async (event: MessageEvent<HashRequest>) => {
  const { id, data } = event.data;
  try {
    const fingerprint = await torrentInfoHash(new Uint8Array(data));
    const response: HashResponse = { id, fingerprint };
    self.postMessage(response);
  } catch (error) {
    const response: HashResponse = {
      id,
      error: error instanceof Error ? error.message : String(error),
    };
    self.postMessage(response);
  }
};

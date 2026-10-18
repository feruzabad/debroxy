import { useEffect, useState } from "react";

/** Seconds left until `until` (epoch ms), ticking once per second; 0 when passed or unset. */
export function useCountdown(until: number | null): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (until === null || until <= Date.now()) {
      return;
    }
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, [until]);
  if (until === null) {
    return 0;
  }
  return Math.max(0, Math.ceil((until - now) / 1000));
}

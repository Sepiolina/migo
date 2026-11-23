/** Retains completed JSON responses within a serialized byte budget for one request scope. */
export class RequestCache<T> {
  private entries = new Map<string, { data: T; bytes: number }>();
  private pending = new Map<string, Promise<T>>();
  private bytes = 0;

  constructor(private budget: number, private cacheable: (data: T) => boolean) {}

  get(key: string): T | undefined {
    const hit = this.entries.get(key);
    if (hit) {
      this.entries.delete(key);
      this.entries.set(key, hit); // Most recently used.
    }
    return hit?.data;
  }

  forget(key: string) {
    this.pending.delete(key);
    const entry = this.entries.get(key);
    if (entry) {
      this.bytes -= entry.bytes;
      this.entries.delete(key);
    }
  }

  load(key: string, fetch: () => Promise<T>): Promise<T> {
    const hit = this.get(key);
    if (hit !== undefined) return Promise.resolve(hit);
    const pending = this.pending.get(key);
    if (pending) return pending;

    const flight = fetch().then((data) => {
      if (this.pending.get(key) === flight && this.cacheable(data)) {
        const bytes = new TextEncoder().encode(JSON.stringify(data)).byteLength;
        if (bytes <= this.budget) {
          for (const [oldest, entry] of this.entries) {
            if (this.bytes + bytes <= this.budget) break;
            this.entries.delete(oldest);
            this.bytes -= entry.bytes;
          }
          this.entries.set(key, { data, bytes });
          this.bytes += bytes;
        }
      }
      return data;
    }).finally(() => {
      if (this.pending.get(key) === flight) this.pending.delete(key);
    });
    this.pending.set(key, flight);
    return flight;
  }
}

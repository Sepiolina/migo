/**
 * An in-memory file system with the callback API of Node's `fs` module, as
 * far as Go's js/wasm port uses it (syscall/fs_js.go). The browser build of
 * Studio installs it as globalThis.fs before the Go engine starts, so the
 * engine reads sample files, writes run outputs and keeps run history exactly
 * as it does on disk, without a folder on the computer.
 *
 * Paths are POSIX and absolute; relative paths resolve against "/". Symlinks
 * and hard links are not supported, which the engine treats as ordinary
 * "not permitted" errors.
 */

const S_IFDIR = 0o040000;
const S_IFREG = 0o100000;
const MAX_TOTAL_BYTES = 768 << 20;

export const constants = {
  O_RDONLY: 0,
  O_WRONLY: 1,
  O_RDWR: 2,
  O_CREAT: 0o100,
  O_EXCL: 0o200,
  O_TRUNC: 0o1000,
  O_APPEND: 0o2000,
  O_DIRECTORY: 0o200000,
};

type FileNode = { kind: "file"; ino: number; mode: number; data: Uint8Array; size: number; atimeMs: number; mtimeMs: number; ctimeMs: number };
type DirNode = { kind: "dir"; ino: number; mode: number; children: Map<string, Entry>; atimeMs: number; mtimeMs: number; ctimeMs: number };
type Entry = FileNode | DirNode;
type Handle = { node: Entry; flags: number; pos: number };
type Callback<T = unknown> = (error: Error | null, value?: T) => void;

export class FsError extends Error {
  readonly code: string;
  constructor(code: string, message: string) {
    super(`${code}: ${message}`);
    this.code = code;
  }
}

class Stats {
  dev = 1;
  nlink = 1;
  uid = 0;
  gid = 0;
  rdev = 0;
  blksize = 4096;
  readonly ino: number;
  readonly mode: number;
  readonly size: number;
  readonly blocks: number;
  readonly atimeMs: number;
  readonly mtimeMs: number;
  readonly ctimeMs: number;
  constructor(node: Entry) {
    this.ino = node.ino;
    this.mode = (node.kind === "dir" ? S_IFDIR : S_IFREG) | node.mode;
    this.size = node.kind === "file" ? node.size : 4096;
    this.blocks = Math.ceil(this.size / 512);
    this.atimeMs = Math.floor(node.atimeMs);
    this.mtimeMs = Math.floor(node.mtimeMs);
    this.ctimeMs = Math.floor(node.ctimeMs);
  }
  isDirectory() { return (this.mode & S_IFDIR) === S_IFDIR; }
  isFile() { return (this.mode & S_IFREG) === S_IFREG; }
  isSymbolicLink() { return false; }
}

/** Splits an absolute or "/"-relative path into its names, resolving "." and "..". */
export function parts(path: string): string[] {
  const out: string[] = [];
  for (const name of path.split("/")) {
    if (!name || name === ".") continue;
    if (name === "..") out.pop();
    else out.push(name);
  }
  return out;
}

export class MemFS {
  readonly constants = constants;
  private root: DirNode;
  private handles = new Map<number, Handle>();
  private nextFd = 10;
  private nextIno = 1;
  private total = 0;
  private lineBuffer = ["", "", ""];
  /** Called after any change to a file or directory, with its path. */
  onChange?: (path: string) => void;
  /** Where the engine's stdout and stderr go (it logs nothing in normal use). */
  log: (fd: number, line: string) => void = (fd, line) => (fd === 2 ? console.warn : console.log)(line);

  constructor() {
    this.root = this.dir(0o755);
    this.mkdirp("/tmp");
  }

  // ---- helpers for the host ---------------------------------------------------------

  /** Creates a directory and its parents. */
  mkdirp(path: string) {
    let node: DirNode = this.root;
    for (const name of parts(path)) {
      let next = node.children.get(name);
      if (!next) {
        next = this.dir(0o755);
        node.children.set(name, next);
        node.mtimeMs = Date.now();
      }
      if (next.kind !== "dir") throw new FsError("ENOTDIR", path);
      node = next;
    }
    return node;
  }
  /** Writes a whole file, creating its parent directories. */
  writeFile(path: string, data: Uint8Array | string) {
    const bytes = typeof data === "string" ? new TextEncoder().encode(data) : data;
    const names = parts(path);
    const name = names.pop();
    if (!name) throw new FsError("EISDIR", path);
    const parent = this.mkdirp("/" + names.join("/"));
    const existing = parent.children.get(name);
    if (existing?.kind === "dir") throw new FsError("EISDIR", path);
    this.reserve(bytes.length - (existing?.size ?? 0));
    const now = Date.now();
    if (existing) {
      existing.data = bytes.slice();
      existing.size = bytes.length;
      existing.mtimeMs = existing.ctimeMs = now;
    } else {
      parent.children.set(name, { kind: "file", ino: this.nextIno++, mode: 0o644, data: bytes.slice(), size: bytes.length, atimeMs: now, mtimeMs: now, ctimeMs: now });
      parent.mtimeMs = now;
    }
    this.changed(path);
  }
  /** The bytes of a file, or undefined when there is no such file. */
  readFile(path: string): Uint8Array | undefined {
    const node = this.find(path);
    return node?.kind === "file" ? node.data.slice(0, node.size) : undefined;
  }
  exists(path: string) { return !!this.find(path); }
  isDir(path: string) { return this.find(path)?.kind === "dir"; }
  /** The names in a directory, or [] when it does not exist. */
  list(path: string): string[] {
    const node = this.find(path);
    return node?.kind === "dir" ? [...node.children.keys()].sort() : [];
  }
  /** Every file below a directory, as paths relative to it. */
  walk(path: string): string[] {
    const out: string[] = [];
    const visit = (node: Entry, prefix: string) => {
      if (node.kind === "file") out.push(prefix);
      else for (const [name, child] of node.children) visit(child, prefix ? `${prefix}/${name}` : name);
    };
    const start = this.find(path);
    if (start?.kind === "dir") visit(start, "");
    return out.sort();
  }
  /** Removes a file or a whole directory; no error when it is missing. */
  remove(path: string) {
    const names = parts(path);
    const name = names.pop();
    const parent = name ? this.find("/" + names.join("/")) : undefined;
    if (parent?.kind !== "dir") return;
    const node = parent.children.get(name!);
    if (!node) return;
    this.total -= this.bytesIn(node);
    parent.children.delete(name!);
    this.changed(path);
  }

  // ---- the Node fs API Go calls -------------------------------------------------------

  writeSync(fd: number, buf: Uint8Array): number {
    if (fd === 1 || fd === 2) {
      this.lineBuffer[fd] += new TextDecoder().decode(buf);
      const lines = this.lineBuffer[fd].split("\n");
      this.lineBuffer[fd] = lines.pop()!;
      for (const line of lines) this.log(fd, line);
      return buf.length;
    }
    return this.writeAt(fd, buf, 0, buf.length, null);
  }
  open(path: string, flags: number, mode: number, cb: Callback<number>) {
    this.call(cb, () => {
      const names = parts(path);
      let node = this.find(path);
      if (node && flags & constants.O_CREAT && flags & constants.O_EXCL) throw new FsError("EEXIST", path);
      if (!node) {
        if (!(flags & constants.O_CREAT)) throw new FsError("ENOENT", path);
        const parent = this.parentOf(path);
        const now = Date.now();
        node = { kind: "file", ino: this.nextIno++, mode: mode & 0o777, data: new Uint8Array(0), size: 0, atimeMs: now, mtimeMs: now, ctimeMs: now };
        parent.children.set(names[names.length - 1], node);
        parent.mtimeMs = now;
        this.changed(path);
      }
      const writing = (flags & 3) !== constants.O_RDONLY;
      if (node.kind === "dir") {
        if (writing) throw new FsError("EISDIR", path);
      } else {
        if (flags & constants.O_DIRECTORY) throw new FsError("ENOTDIR", path);
        if (flags & constants.O_TRUNC && writing && node.size) {
          this.total -= node.size;
          node.data = new Uint8Array(0);
          node.size = 0;
          node.mtimeMs = Date.now();
          this.changed(path);
        }
      }
      const fd = this.nextFd++;
      this.handles.set(fd, { node, flags, pos: 0 });
      return fd;
    });
  }
  close(fd: number, cb: Callback) {
    this.call(cb, () => {
      if (!this.handles.delete(fd)) throw new FsError("EBADF", String(fd));
    });
  }
  read(fd: number, buf: Uint8Array, offset: number, length: number, position: number | null, cb: Callback<number>) {
    this.call(cb, () => {
      const handle = this.handle(fd);
      if (handle.node.kind !== "file") throw new FsError("EISDIR", String(fd));
      const node = handle.node;
      const at = position ?? handle.pos;
      const n = Math.max(0, Math.min(length, node.size - at));
      buf.set(node.data.subarray(at, at + n), offset);
      if (position == null) handle.pos += n;
      node.atimeMs = Date.now();
      return n;
    });
  }
  write(fd: number, buf: Uint8Array, offset: number, length: number, position: number | null, cb: Callback<number>) {
    this.call(cb, () => this.writeSync2(fd, buf, offset, length, position));
  }
  fstat(fd: number, cb: Callback<Stats>) { this.call(cb, () => new Stats(this.handle(fd).node)); }
  stat(path: string, cb: Callback<Stats>) { this.call(cb, () => new Stats(this.need(path))); }
  lstat(path: string, cb: Callback<Stats>) { this.call(cb, () => new Stats(this.need(path))); }
  readdir(path: string, cb: Callback<string[]>) {
    this.call(cb, () => {
      const node = this.need(path);
      if (node.kind !== "dir") throw new FsError("ENOTDIR", path);
      return [...node.children.keys()];
    });
  }
  mkdir(path: string, perm: number, cb: Callback) {
    this.call(cb, () => {
      if (this.find(path)) throw new FsError("EEXIST", path);
      const parent = this.parentOf(path);
      const names = parts(path);
      parent.children.set(names[names.length - 1], this.dir(perm & 0o777));
      parent.mtimeMs = Date.now();
      this.changed(path);
    });
  }
  rmdir(path: string, cb: Callback) {
    this.call(cb, () => {
      const node = this.need(path);
      if (node.kind !== "dir") throw new FsError("ENOTDIR", path);
      if (node.children.size) throw new FsError("ENOTEMPTY", path);
      if (!parts(path).length) throw new FsError("EBUSY", path);
      this.detach(path);
    });
  }
  unlink(path: string, cb: Callback) {
    this.call(cb, () => {
      const node = this.need(path);
      if (node.kind === "dir") throw new FsError("EISDIR", path);
      this.total -= node.size;
      this.detach(path);
    });
  }
  rename(from: string, to: string, cb: Callback) {
    this.call(cb, () => {
      const node = this.need(from);
      const src = parts(from);
      const dst = parts(to);
      if (dst.length > src.length && src.every((name, i) => dst[i] === name)) throw new FsError("EINVAL", to);
      const parent = this.parentOf(to);
      const existing = this.find(to);
      if (existing && existing !== node) {
        if (existing.kind === "dir" && node.kind !== "dir") throw new FsError("EISDIR", to);
        if (existing.kind !== "dir" && node.kind === "dir") throw new FsError("ENOTDIR", to);
        if (existing.kind === "dir" && existing.children.size) throw new FsError("ENOTEMPTY", to);
        this.total -= this.bytesIn(existing);
      }
      this.detach(from);
      parent.children.set(dst[dst.length - 1], node);
      node.ctimeMs = parent.mtimeMs = Date.now();
      this.changed(to);
    });
  }
  ftruncate(fd: number, length: number, cb: Callback) { this.call(cb, () => this.resize(this.handle(fd).node, length)); }
  truncate(path: string, length: number, cb: Callback) { this.call(cb, () => this.resize(this.need(path), length)); }
  fsync(_fd: number, cb: Callback) { this.call(cb, () => undefined); }
  utimes(path: string, atime: number, mtime: number, cb: Callback) {
    this.call(cb, () => {
      const node = this.need(path);
      node.atimeMs = atime * 1000;
      node.mtimeMs = mtime * 1000;
    });
  }
  chmod(path: string, mode: number, cb: Callback) { this.call(cb, () => { this.need(path).mode = mode & 0o777; }); }
  fchmod(fd: number, mode: number, cb: Callback) { this.call(cb, () => { this.handle(fd).node.mode = mode & 0o777; }); }
  chown(path: string, _u: number, _g: number, cb: Callback) { this.call(cb, () => void this.need(path)); }
  fchown(fd: number, _u: number, _g: number, cb: Callback) { this.call(cb, () => void this.handle(fd)); }
  lchown(path: string, _u: number, _g: number, cb: Callback) { this.call(cb, () => void this.need(path)); }
  readlink(path: string, cb: Callback) { this.call(cb, () => { this.need(path); throw new FsError("EINVAL", path); }); }
  symlink(_target: string, path: string, cb: Callback) { this.call(cb, () => { throw new FsError("EPERM", path); }); }
  link(_from: string, to: string, cb: Callback) { this.call(cb, () => { throw new FsError("EPERM", to); }); }

  // ---- internals --------------------------------------------------------------------

  private call<T>(cb: Callback<T>, work: () => T) {
    let value: T;
    try {
      value = work();
    } catch (error) {
      cb(error instanceof FsError ? error : new FsError("EIO", String(error)));
      return;
    }
    cb(null, value);
  }
  private writeSync2(fd: number, buf: Uint8Array, offset: number, length: number, position: number | null) {
    if (fd === 1 || fd === 2) return this.writeSync(fd, buf.subarray(offset, offset + length));
    return this.writeAt(fd, buf, offset, length, position);
  }
  private writeAt(fd: number, buf: Uint8Array, offset: number, length: number, position: number | null) {
    const handle = this.handle(fd);
    const node = handle.node;
    if (node.kind !== "file") throw new FsError("EISDIR", String(fd));
    if ((handle.flags & 3) === constants.O_RDONLY) throw new FsError("EBADF", String(fd));
    const at = handle.flags & constants.O_APPEND ? node.size : position ?? handle.pos;
    const end = at + length;
    if (end > node.size) this.resize(node, end);
    node.data.set(buf.subarray(offset, offset + length), at);
    if (position == null) handle.pos = end;
    node.mtimeMs = Date.now();
    return length;
  }
  private resize(node: Entry, length: number) {
    if (node.kind !== "file") throw new FsError("EISDIR", "");
    this.reserve(length - node.size);
    if (length > node.data.length) {
      const grown = new Uint8Array(Math.max(length, Math.min(node.data.length * 2, length + (16 << 20))));
      grown.set(node.data.subarray(0, node.size));
      node.data = grown;
    } else if (length < node.size) {
      node.data.fill(0, length, node.size);
    }
    node.size = length;
    node.mtimeMs = Date.now();
  }
  private reserve(bytes: number) {
    if (this.total + bytes > MAX_TOTAL_BYTES) throw new FsError("ENOSPC", "The in-browser workspace is full; remove old runs or files");
    this.total += bytes;
  }
  private bytesIn(node: Entry): number {
    if (node.kind === "file") return node.size;
    let sum = 0;
    for (const child of node.children.values()) sum += this.bytesIn(child);
    return sum;
  }
  private dir(mode: number): DirNode {
    const now = Date.now();
    return { kind: "dir", ino: this.nextIno++, mode, children: new Map(), atimeMs: now, mtimeMs: now, ctimeMs: now };
  }
  private find(path: string): Entry | undefined {
    let node: Entry = this.root;
    for (const name of parts(path)) {
      if (node.kind !== "dir") return undefined;
      const next: Entry | undefined = node.children.get(name);
      if (!next) return undefined;
      node = next;
    }
    return node;
  }
  private need(path: string): Entry {
    const names = parts(path);
    let node: Entry = this.root;
    for (const name of names) {
      if (node.kind !== "dir") throw new FsError("ENOTDIR", path);
      const next: Entry | undefined = node.children.get(name);
      if (!next) throw new FsError("ENOENT", path);
      node = next;
    }
    return node;
  }
  private parentOf(path: string): DirNode {
    const names = parts(path);
    if (!names.length) throw new FsError("EEXIST", path);
    const parent = this.need("/" + names.slice(0, -1).join("/"));
    if (parent.kind !== "dir") throw new FsError("ENOTDIR", path);
    return parent;
  }
  private detach(path: string) {
    const names = parts(path);
    const parent = this.parentOf(path);
    parent.children.delete(names[names.length - 1]);
    parent.mtimeMs = Date.now();
    this.changed(path);
  }
  private handle(fd: number): Handle {
    const handle = this.handles.get(fd);
    if (!handle) throw new FsError("EBADF", String(fd));
    return handle;
  }
  private changed(path: string) {
    this.onChange?.("/" + parts(path).join("/"));
  }
}

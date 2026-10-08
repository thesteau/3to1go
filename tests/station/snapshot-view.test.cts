const { loadFeature } = require("../helpers/scripts.cts");
const { test } = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");
const nodeCrypto = require("node:crypto");
const zlib = require("node:zlib");
const zipReader = require("../../app/node_modules/@zip.js/zip.js");

const key = Buffer.alloc(32, 7);
const keyB64 = key.toString("base64url");

// Independent tar writer for interoperability fixtures: USTAR headers and byte-counted PAX records.
function tarEntry(name, contents = Buffer.alloc(0), type = "0", prefix = "") {
  const data = Buffer.from(contents);
  const header = Buffer.alloc(512);
  const text = (value, offset, length) => header.write(value, offset, length, "utf8");
  text(name, 0, 100);
  text("0000644\0", 100, 8);
  text("0000000\0", 108, 8);
  text("0000000\0", 116, 8);
  text(`${data.length.toString(8).padStart(11, "0")}\0`, 124, 12);
  text("14524770400\0", 136, 12);
  header.fill(32, 148, 156);
  text(type, 156, 1);
  text("ustar\0", 257, 6);
  text("00", 263, 2);
  text(prefix, 345, 155);
  const checksum = header.reduce((sum, byte) => sum + byte, 0);
  text(`${checksum.toString(8).padStart(6, "0")}\0 `, 148, 8);
  return Buffer.concat([header, data, Buffer.alloc((512 - (data.length % 512)) % 512)]);
}

function paxRecord(name, value) {
  const record = `${name}=${value}\n`;
  let length = Buffer.byteLength(record) + 2;
  while (Buffer.byteLength(`${length} ${record}`) !== length) length = Buffer.byteLength(`${length} ${record}`);
  return Buffer.from(`${length} ${record}`);
}

function archive(...entries) {
  return new Blob([...entries, Buffer.alloc(1024)]);
}

function encryptedSnapshot(data) {
  const random = nodeCrypto.randomBytes(12);
  random[0] &= 0x7f;
  const parts = [];
  for (let offset = 0, sequence = 0; offset < data.length; offset += 65536, sequence++) {
    const payload = data.subarray(offset, offset + 65536);
    const header = Buffer.alloc(16);
    header[0] = 0x20;
    header.writeUInt16LE(payload.length - 1, 2);
    random.copy(header, 4);
    if (offset + payload.length === data.length) header[4] |= 0x80;
    const nonce = Buffer.from(header.subarray(4));
    nonce.writeUInt32LE((nonce.readUInt32LE(8) ^ sequence) >>> 0, 8);
    const cipher = nodeCrypto.createCipheriv("aes-256-gcm", key, nonce).setAAD(header.subarray(0, 4));
    parts.push(header, cipher.update(payload), cipher.final(), cipher.getAuthTag());
  }
  return Buffer.concat(parts);
}

function context() {
  const elements = new Map();
  const element = (id) => {
    if (!elements.has(id))
      elements.set(id, {
        value: "",
        textContent: "",
        innerHTML: "",
        hidden: false,
        disabled: false,
        children: [],
        open: false,
        style: {},
        attributes: {},
        setAttribute(name, value) {
          this.attributes[name] = value;
        },
        replaceChildren(...children) {
          this.children = children;
          this.textContent = "";
          this.innerHTML = "";
        },
        appendChild(child) {
          this.children.push(child);
        },
        scrollIntoView() {},
        classList: { add() {}, remove() {} },
      });
    return elements.get(id);
  };
  const downloads = [];
  const requests = [];
  const messages = [];
  const storage = new Map();
  const revoked = [];
  let nextURL = 0;
  const ctx = vm.createContext({
    Blob,
    Response,
    TextDecoder,
    TextEncoder,
    AbortController,
    Uint8Array,
    TransformStream,
    ReadableStream,
    WritableStream,
    CompressionStream,
    DecompressionStream,
    crypto: globalThis.crypto,
    atob,
    btoa,
    setTimeout,
    clearTimeout,
    structuredClone,
    document: {
      getElementById: element,
      querySelector: () => null,
      querySelectorAll: () => [],
      createElement: () => element(`element-${nextURL++}`),
    },
    URL: { createObjectURL: () => `blob:${nextURL++}`, revokeObjectURL: (url) => revoked.push(url) },
    sessionStorage: {
      getItem: (k) => storage.get(k) || null,
      setItem: (k, v) => storage.set(k, v),
      removeItem: (k) => storage.delete(k),
      key: (i) => [...storage.keys()][i],
      get length() {
        return storage.size;
      },
    },
    fetch: async (...args) => {
      requests.push(args);
      throw new Error("Unexpected request");
    },
    setActionStatus: (...args) => messages.push(args),
    loadOverview: async () => true,
    openDialog: (id) => {
      element(id).open = true;
    },
    closeDialog: (id) => {
      element(id).open = false;
    },
    resolveAppDialog() {},
    appDialog: async () => keyB64,
  });
  for (const feature of [
    "utils",
    "crypto",
    "keys",
    "snapshots",
    "fzstd",
    "zip",
    "snapshot-archive",
    "files",
    "snapshot-view",
  ])
    loadFeature(ctx, "station", feature);
  ctx.triggerBlobDownload = (blob, name) => downloads.push({ blob, name });
  return { ctx, element, downloads, requests, messages, revoked };
}

function renderedText(element) {
  return [element.textContent, ...element.children.map(renderedText)].join("\n");
}

async function openFixture(env, tar, encrypted = true) {
  const compressed = zlib.zstdCompressSync(Buffer.from(await tar.arrayBuffer()));
  const data = encrypted ? encryptedSnapshot(compressed) : compressed;
  env.ctx.fetch = async (...args) => {
    env.requests.push(args);
    return new Response(data);
  };
  await env.ctx.openSnapshotView("scout-a", "inst-1", "docs", "docs.tar.zst", null);
}

test("the viewer prompts for the Scout key before fetching, decrypts, and browses files and empty folders", async () => {
  const env = context();
  const events = [];
  env.ctx.appDialog = async () => {
    events.push("key");
    return keyB64;
  };
  const tar = archive(
    tarEntry("notes.txt", "hello"),
    tarEntry("folder/file.txt", "nested"),
    tarEntry("empty/", "", "5"),
  );
  const compressed = zlib.zstdCompressSync(Buffer.from(await tar.arrayBuffer()));
  env.ctx.fetch = async (...args) => {
    events.push("fetch");
    env.requests.push(args);
    return new Response(encryptedSnapshot(compressed));
  };
  await env.ctx.openSnapshotView("scout-a", "inst-1", "docs", "docs.tar.zst", null);
  assert.deepEqual(events, ["key", "fetch"]);
  assert.equal(env.requests.length, 1);
  assert.equal(env.requests[0][0], "/api/snapshots/scout-a/inst-1/docs/docs.tar.zst");
  assert.deepEqual(Object.keys(env.requests[0][1]), ["signal"], "the Scout key is never sent to Station");
  assert.equal(env.element("snapshot-view-content").hidden, false);
  assert.match(renderedText(env.element("snapshot-view-files")), /notes.txt/);
  assert.match(renderedText(env.element("snapshot-view-files")), /▸ empty/);
  assert.doesNotMatch(renderedText(env.element("snapshot-view-files")), /file.txt/);
  env.ctx.toggleSnapshotViewFolder("folder");
  assert.match(renderedText(env.element("snapshot-view-files")), /file.txt/);
  env.ctx.toggleSnapshotViewFolder("empty");
  assert.match(env.element("snapshot-view-status").textContent, /empty is empty/);
});

test("canceling or rejecting a Scout key never fetches or reveals snapshot files", async () => {
  for (const answer of [null, "not-a-key"]) {
    const env = context();
    env.ctx.appDialog = async () => answer;
    if (answer) vm.runInContext('_scoutKeyFingerprints["scout-a::inst-1"] = "expected"', env.ctx);
    await env.ctx.openSnapshotView("scout-a", "inst-1", "docs", "docs.tar.zst", null);
    assert.equal(env.requests.length, 0);
    assert.equal(env.element("snapshot-view-dialog").open, false);
    assert.equal(env.element("snapshot-view-content").hidden, true);
  }
});

test("PAX Unicode and long paths, prefix paths, and empty files extract their exact bytes", async () => {
  const env = context();
  const path = `folder/${"long".repeat(40)}/日本語.txt`;
  const tar = archive(
    tarEntry("PaxHeaders/file", Buffer.concat([paxRecord("path", path), paxRecord("mtime", "1700000000.125")]), "x"),
    tarEntry("placeholder", "PAX bytes"),
    tarEntry("empty.txt", ""),
    tarEntry("name.txt", "prefix bytes", "0", "prefix"),
    tarEntry("link", "", "2"),
  );
  const result = await env.ctx.indexSnapshotTar(tar);
  const file = result.entries.find((entry) => entry.path === path);
  assert.equal(file.modified, 1700000000125);
  assert.equal(await env.ctx.snapshotEntryBlob(result, file).text(), "PAX bytes");
  assert.equal(result.entries.find((entry) => entry.path === "empty.txt").size, 0);
  assert.equal(
    await env.ctx
      .snapshotEntryBlob(
        result,
        result.entries.find((entry) => entry.path === "prefix/name.txt"),
      )
      .text(),
    "prefix bytes",
  );
  assert.equal(
    result.entries.some((entry) => entry.path === "link"),
    false,
  );
  assert.equal(result.entries.find((entry) => entry.path === "folder").directory, true);
});

test("malformed, truncated, unsafe and conflicting tar entries cannot be exposed", async () => {
  const env = context();
  const damaged = tarEntry("good.txt", "hello");
  damaged[12] ^= 1;
  for (const tar of [
    archive(damaged),
    new Blob([tarEntry("good.txt", "hello").subarray(0, 515)]),
    archive(tarEntry("../outside.txt", "bad")),
    archive(tarEntry("/absolute.txt", "bad")),
    archive(tarEntry("C:/drive.txt", "bad")),
    archive(tarEntry("folder\\escape.txt", "bad")),
    archive(tarEntry("duplicate.txt", "one"), tarEntry("duplicate.txt", "two")),
    archive(tarEntry("folder", "file"), tarEntry("folder/child.txt", "conflict")),
    archive(tarEntry("PaxHeaders/file", "999 path=bad\n", "x")),
    archive(tarEntry("PaxHeaders/file", paxRecord("path", "../pax-escape"), "x"), tarEntry("name", "bad")),
  ])
    await assert.rejects(env.ctx.indexSnapshotTar(tar));
});

test("streaming Zstandard decoding handles multi-block archives and rejects truncation", async () => {
  const env = context();
  const data = nodeCrypto.randomBytes(800000);
  const tar = archive(tarEntry("random.bin", data));
  const compressed = zlib.zstdCompressSync(Buffer.from(await tar.arrayBuffer()));
  const decoded = await env.ctx.decompressSnapshotArchive(new Blob([compressed]));
  const indexed = await env.ctx.indexSnapshotTar(decoded);
  assert.deepEqual(Buffer.from(await env.ctx.snapshotEntryBlob(indexed, indexed.entries[0]).arrayBuffer()), data);
  await assert.rejects(env.ctx.decompressSnapshotArchive(new Blob([compressed.subarray(0, compressed.length - 8)])));
});

test("one and two selected files download individually; three become a readable ZIP with paths and bytes", async () => {
  for (const count of [1, 2, 3]) {
    const env = context();
    const tar = archive(tarEntry("a.txt", "one"), tarEntry("nested/b.txt", "two"), tarEntry("nested/c.txt", "three"));
    await openFixture(env, tar);
    const files = vm.runInContext(
      "snapshotView.archive.entries.map((entry, index) => ({entry,index})).filter(({entry}) => !entry.directory).map(({index}) => index)",
      env.ctx,
    );
    files.slice(0, count).forEach((index) => {
      env.ctx.toggleSnapshotFile(index, true);
    });
    await env.ctx.downloadSelectedSnapshotFiles(null);
    assert.equal(env.downloads.length, count > 2 ? 1 : count);
    if (count <= 2) {
      assert.equal(env.downloads[0].name, "a.txt");
      assert.equal(await env.downloads[0].blob.text(), "one");
      if (count === 2) assert.equal(env.downloads[1].name, "b.txt");
    } else {
      assert.equal(env.downloads[0].name, "docs-files.zip");
      const reader = new zipReader.ZipReader(new zipReader.BlobReader(env.downloads[0].blob), { useWebWorkers: false });
      const entries = await reader.getEntries();
      assert.deepEqual(
        entries.map((entry) => entry.filename),
        ["a.txt", "nested/b.txt", "nested/c.txt"],
      );
      assert.deepEqual(await Promise.all(entries.map((entry) => entry.getData(new zipReader.TextWriter()))), [
        "one",
        "two",
        "three",
      ]);
      await reader.close();
    }
  }
});

test("selections survive folder navigation and search, and only shown files are selected", async () => {
  const env = context();
  await openFixture(
    env,
    archive(tarEntry("root.txt", "one"), tarEntry("nested/next.txt", "two"), tarEntry("empty/", "", "5")),
  );
  env.ctx.selectVisibleSnapshotFiles();
  assert.equal(env.element("snapshot-view-count").textContent, "1 selected");
  env.ctx.toggleSnapshotViewFolder("nested");
  env.ctx.selectVisibleSnapshotFiles();
  assert.equal(env.element("snapshot-view-count").textContent, "2 selected");
  env.element("snapshot-view-search").value = "root";
  env.ctx.renderSnapshotView();
  assert.equal(env.element("snapshot-view-files").children[0].children[0].children[0].checked, true);
  env.ctx.clearSnapshotSelection();
  assert.equal(env.element("snapshot-view-download").disabled, true);
});

test("text and HTML previews are inert text; binary files are downloadable without a preview", async () => {
  const env = context();
  const html = '<script>alert("never")</script>';
  await openFixture(env, archive(tarEntry("page.html", html), tarEntry("binary.bin", Buffer.from([0, 255, 13]))));
  await env.ctx.previewSnapshotFile(0);
  assert.equal(env.element("snapshot-view-preview-body").children[0].textContent, html);
  await env.ctx.previewSnapshotFile(1);
  assert.match(env.element("snapshot-view-preview-body").textContent, /Preview is unavailable/);
  await env.ctx.downloadSnapshotViewFiles([1], null);
  assert.deepEqual(Buffer.from(await env.downloads[0].blob.arrayBuffer()), Buffer.from([0, 255, 13]));
});

test("closing, clearing the Scout key, or session expiry drops decrypted contents and revokes previews", async () => {
  for (const action of ["closeSnapshotView()", 'clearEncKey("scout-a", "inst-1")', "clearSessionEncKeys()"]) {
    const env = context();
    await openFixture(env, archive(tarEntry("image.png", "image")));
    await env.ctx.previewSnapshotFile(0);
    assert.equal(env.element("snapshot-view-preview-body").children.length, 1);
    const retainedView = vm.runInContext("snapshotView", env.ctx);
    vm.runInContext(action, env.ctx);
    assert.equal(vm.runInContext("snapshotView", env.ctx), null);
    assert.equal(retainedView.archive, null, "even retained callbacks cannot keep the decrypted archive");
    assert.equal(retainedView.folders.size, 0);
    assert.equal(retainedView.controller.signal.aborted, true);
    assert.equal(env.element("snapshot-view-dialog").onclose, null);
    assert.equal(env.revoked.length, 1);
    assert.equal(env.element("snapshot-view-preview-body").children.length, 0);
    assert.equal(env.element("snapshot-view-dialog").open, false);
    assert.equal(env.element("snapshot-view-files").innerHTML, "");
  }
});

test("folders expand in place, collapsing clears descendant expansions, and hidden selections stay selected", async () => {
  const env = context();
  await openFixture(
    env,
    archive(tarEntry("docs/drafts/a.txt", "a"), tarEntry("docs/z.txt", "z"), tarEntry("root.txt", "root")),
  );
  const names = () => env.element("snapshot-view-files").children.map((row) => row.children[1].children[0].textContent);
  assert.deepEqual(names(), ["▸ docs", "root.txt"]);
  env.ctx.toggleSnapshotViewFolder("docs");
  assert.deepEqual(names(), ["▾ docs", "▸ drafts", "z.txt", "root.txt"]);
  env.ctx.toggleSnapshotViewFolder("docs/drafts");
  assert.deepEqual(names(), ["▾ docs", "▾ drafts", "a.txt", "z.txt", "root.txt"]);
  const index = vm.runInContext(
    'snapshotView.archive.entries.findIndex((entry) => entry.path === "docs/drafts/a.txt")',
    env.ctx,
  );
  env.ctx.toggleSnapshotFile(index, true);
  env.ctx.toggleSnapshotViewFolder("docs");
  assert.deepEqual(names(), ["▸ docs", "root.txt"]);
  assert.equal(env.element("snapshot-view-count").textContent, "1 selected");
  env.ctx.toggleSnapshotViewFolder("docs");
  assert.deepEqual(names(), ["▾ docs", "▸ drafts", "z.txt", "root.txt"]);
});

test("large lists page at 100 rows and Select shown files applies only to the current page", async () => {
  const env = context();
  await openFixture(
    env,
    archive(...Array.from({ length: 105 }, (_, index) => tarEntry(`${String(index).padStart(3, "0")}.txt`, "x"))),
  );
  assert.equal(env.element("snapshot-view-files").children.length, 100);
  env.ctx.selectVisibleSnapshotFiles();
  assert.equal(env.element("snapshot-view-count").textContent, "100 selected");
  env.ctx.changeSnapshotViewPage(1);
  assert.equal(env.element("snapshot-view-files").children.length, 5);
  env.ctx.selectVisibleSnapshotFiles();
  assert.equal(env.element("snapshot-view-count").textContent, "105 selected");
  env.element("snapshot-view-search").value = "104.txt";
  env.ctx.renderSnapshotView();
  assert.equal(env.element("snapshot-view-files").children.length, 1);
  assert.equal(env.element("snapshot-view-next").disabled, true);
});

test("closing during a fetch aborts it and late results cannot reopen the viewer", async () => {
  const env = context();
  env.ctx.setEncKey("scout-a", "inst-1", keyB64);
  let finish;
  let signal;
  env.ctx.fetch = (_url, options) => {
    signal = options.signal;
    return new Promise((resolve) => {
      finish = resolve;
    });
  };
  const pending = env.ctx.openSnapshotView("scout-a", "inst-1", "docs", "docs.tar.zst", null);
  await new Promise((resolve) => setImmediate(resolve));
  env.ctx.closeSnapshotView();
  assert.equal(signal.aborted, true);
  const compressed = zlib.zstdCompressSync(Buffer.from(await archive(tarEntry("secret.txt", "secret")).arrayBuffer()));
  finish(new Response(encryptedSnapshot(compressed)));
  await pending;
  assert.equal(env.element("snapshot-view-dialog").open, false);
  assert.equal(env.element("snapshot-view-files").innerHTML, "");
});

test("archive errors leave the verified Scout key saved, while wrong decryption keys reveal nothing", async () => {
  const env = context();
  await openFixture(env, archive(tarEntry("../bad.txt", "bad")));
  assert.match(env.element("snapshot-view-status").textContent, /unsafe file path/);
  assert.equal(env.ctx.getEncKey("scout-a", "inst-1"), keyB64);
  env.ctx.closeSnapshotView();
  env.ctx.setEncKey("scout-a", "inst-1", Buffer.alloc(32, 8).toString("base64url"));
  await openFixture(env, archive(tarEntry("secret.txt", "secret")));
  assert.equal(env.ctx.getEncKey("scout-a", "inst-1"), null);
  assert.equal(env.element("snapshot-view-content").hidden, true);
  assert.equal(env.element("snapshot-view-files").innerHTML, "");
  assert.match(env.messages.at(-1)[0], /Decryption failed/);
});

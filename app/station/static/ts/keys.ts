type KeyStatusKind = "info" | "ok" | "warn" | "error";

const _encKeys: Record<string, string> = {};
let _scoutKeyFingerprints: Record<string, string> = {};
let _keySessionGeneration = 0;

function clearSessionEncKeys(): void {
  // Invalidate key validation and prompts already in flight as well as stored keys.
  _keySessionGeneration += 1;
  if (typeof closeSnapshotView === "function") closeSnapshotView();
  for (const key of Object.keys(_encKeys)) delete _encKeys[key];
  for (let index = sessionStorage.length - 1; index >= 0; index -= 1) {
    const key = sessionStorage.key(index);
    if (key?.startsWith("3to1go_enc_")) sessionStorage.removeItem(key);
  }
  document.querySelectorAll<HTMLInputElement>("[data-scout-key-input], #app-dialog-input").forEach((input) => {
    input.value = "";
  });
  document.querySelectorAll("[data-scout-key-status]").forEach((element) => {
    element.textContent = "";
  });
}

function buildScoutKeyId(scoutId: string, scoutInstanceId: string | null | undefined): string {
  return `${scoutId}::${scoutInstanceId || "_legacy"}`;
}

function getExpectedKeyFingerprint(scoutId: string, scoutInstanceId: string | null | undefined): string | null {
  return _scoutKeyFingerprints[buildScoutKeyId(scoutId, scoutInstanceId)] || null;
}

function getEncKey(scoutId: string, scoutInstanceId: string | null | undefined): string | null {
  const keyId = buildScoutKeyId(scoutId, scoutInstanceId);
  if (_encKeys[keyId]) return _encKeys[keyId];
  const stored = sessionStorage.getItem(`3to1go_enc_${keyId}`);
  if (stored) {
    _encKeys[keyId] = stored;
    return stored;
  }
  return null;
}

function setEncKey(scoutId: string, scoutInstanceId: string | null | undefined, key: string): void {
  const keyId = buildScoutKeyId(scoutId, scoutInstanceId);
  _encKeys[keyId] = key;
  sessionStorage.setItem(`3to1go_enc_${keyId}`, key);
}

function clearStoredEncKey(scoutId: string, scoutInstanceId: string | null | undefined): void {
  const keyId = buildScoutKeyId(scoutId, scoutInstanceId);
  delete _encKeys[keyId];
  sessionStorage.removeItem(`3to1go_enc_${keyId}`);
  if (typeof closeSnapshotViewForKey === "function") closeSnapshotViewForKey(keyId);
}

function keyInputElement(scoutId: string, scoutInstanceId: string | null | undefined): HTMLInputElement | null {
  const selector = `[data-scout-key-input="${escapeSelectorValue(buildScoutKeyId(scoutId, scoutInstanceId))}"]`;
  return document.querySelector<HTMLInputElement>(selector);
}

function keyStatusElement(scoutId: string, scoutInstanceId: string | null | undefined): Element | null {
  const selector = `[data-scout-key-status="${escapeSelectorValue(buildScoutKeyId(scoutId, scoutInstanceId))}"]`;
  return document.querySelector(selector);
}

function setKeyStatus(
  scoutId: string,
  scoutInstanceId: string | null | undefined,
  message: string,
  kind: KeyStatusKind = "info",
): void {
  const element = keyStatusElement(scoutId, scoutInstanceId);
  if (!element) return;
  element.textContent = message;
  element.className = `key-status ${kind}`;
}

async function storeEncKey(
  scoutId: string,
  scoutInstanceId: string | null | undefined,
  key: string,
  { alertOnError = false } = {},
): Promise<string | null> {
  const generation = _keySessionGeneration;
  try {
    const actualFingerprint = await fingerprintKey(key);
    if (generation !== _keySessionGeneration) return null;
    const expectedFingerprint = getExpectedKeyFingerprint(scoutId, scoutInstanceId);
    if (expectedFingerprint && actualFingerprint !== expectedFingerprint) {
      clearStoredEncKey(scoutId, scoutInstanceId);
      const message = `That key belongs to a different Scout. Expected ${shortFingerprint(expectedFingerprint)}, got ${shortFingerprint(actualFingerprint)}.`;
      setKeyStatus(scoutId, scoutInstanceId, message, "error");
      if (alertOnError) setActionStatus(message, "error");
      return null;
    }

    setEncKey(scoutId, scoutInstanceId, key);
    setKeyStatus(
      scoutId,
      scoutInstanceId,
      expectedFingerprint
        ? `Key saved and verified for this browser session. Fingerprint ${shortFingerprint(actualFingerprint)}.`
        : `Key saved for this browser session. Fingerprint ${shortFingerprint(actualFingerprint)}.`,
      "ok",
    );
    return key;
  } catch {
    if (generation !== _keySessionGeneration) return null;
    clearStoredEncKey(scoutId, scoutInstanceId);
    const message = "Scout key was not valid base64url text.";
    setKeyStatus(scoutId, scoutInstanceId, message, "error");
    if (alertOnError) setActionStatus(message, "error");
    return null;
  }
}

async function rememberEncKey(scoutId: string, scoutInstanceId: string | null | undefined): Promise<void> {
  const input = keyInputElement(scoutId, scoutInstanceId);
  const key = input?.value.trim() || "";
  if (!key) {
    const expectedFingerprint = getExpectedKeyFingerprint(scoutId, scoutInstanceId);
    setKeyStatus(
      scoutId,
      scoutInstanceId,
      expectedFingerprint
        ? `Paste the Scout key first. Expected fingerprint ${shortFingerprint(expectedFingerprint)}.`
        : "Paste the Scout key first.",
      "warn",
    );
    return;
  }
  const stored = await storeEncKey(scoutId, scoutInstanceId, key);
  if (stored && input) input.value = "";
}

function clearEncKey(scoutId: string, scoutInstanceId: string | null | undefined): void {
  clearStoredEncKey(scoutId, scoutInstanceId);
  const input = keyInputElement(scoutId, scoutInstanceId);
  if (input) input.value = "";
  const expectedFingerprint = getExpectedKeyFingerprint(scoutId, scoutInstanceId);
  setKeyStatus(
    scoutId,
    scoutInstanceId,
    expectedFingerprint
      ? `Cleared. Expected fingerprint ${shortFingerprint(expectedFingerprint)}.`
      : "Cleared saved key for this browser session.",
    "info",
  );
}

async function refreshKeyPanel(scoutId: string, scoutInstanceId: string | null | undefined): Promise<void> {
  const generation = _keySessionGeneration;
  const expectedFingerprint = getExpectedKeyFingerprint(scoutId, scoutInstanceId);
  const key = getEncKey(scoutId, scoutInstanceId);

  if (!key) {
    setKeyStatus(
      scoutId,
      scoutInstanceId,
      expectedFingerprint
        ? `No key saved yet. Expected fingerprint ${shortFingerprint(expectedFingerprint)}.`
        : "No key saved yet. Station has not seen a key fingerprint for this Scout yet.",
      expectedFingerprint ? "info" : "warn",
    );
    return;
  }

  try {
    const actualFingerprint = await fingerprintKey(key);
    if (generation !== _keySessionGeneration) return;
    if (expectedFingerprint && actualFingerprint !== expectedFingerprint) {
      clearStoredEncKey(scoutId, scoutInstanceId);
      setKeyStatus(
        scoutId,
        scoutInstanceId,
        `Saved key fingerprint ${shortFingerprint(actualFingerprint)} did not match expected ${shortFingerprint(expectedFingerprint)} and was cleared.`,
        "error",
      );
      return;
    }

    setKeyStatus(
      scoutId,
      scoutInstanceId,
      expectedFingerprint
        ? `Saved key verified for this browser session. Expected fingerprint ${shortFingerprint(expectedFingerprint)}.`
        : `Saved key present for this browser session. Fingerprint ${shortFingerprint(actualFingerprint)}.`,
      "ok",
    );
  } catch {
    if (generation !== _keySessionGeneration) return;
    clearStoredEncKey(scoutId, scoutInstanceId);
    setKeyStatus(scoutId, scoutInstanceId, "Saved key was invalid and has been cleared.", "error");
  }
}

async function resolveEncKey(scoutId: string, scoutInstanceId: string | null | undefined): Promise<string | null> {
  const generation = _keySessionGeneration;
  const saved = getEncKey(scoutId, scoutInstanceId);
  if (saved) return saved;

  const typed = keyInputElement(scoutId, scoutInstanceId)?.value.trim() || "";
  if (typed) {
    return storeEncKey(scoutId, scoutInstanceId, typed, { alertOnError: true });
  }

  const expectedFingerprint = getExpectedKeyFingerprint(scoutId, scoutInstanceId);
  const instanceLabel = scoutInstanceId || "legacy";
  const promptMessage = expectedFingerprint
    ? `Enter the Scout key for Scout "${scoutId}" (${instanceLabel}). Expected fingerprint: ${shortFingerprint(expectedFingerprint)}.`
    : `Enter the Scout key for Scout "${scoutId}" (${instanceLabel}).`;
  const prompted = await appDialog({
    title: "Scout Key Required",
    message: promptMessage,
    input: true,
    inputLabel: "Scout key",
    inputType: "secret",
    confirmLabel: "Use Key",
  });
  if (!prompted || generation !== _keySessionGeneration) return null;
  return storeEncKey(scoutId, scoutInstanceId, prompted, { alertOnError: true });
}

const _encKeys = {};
let _edgeKeyFingerprints = {};

function buildEdgeKeyId(edgeId, edgeInstanceId) {
  return `${edgeId}::${edgeInstanceId || "_legacy"}`;
}

function getExpectedKeyFingerprint(edgeId, edgeInstanceId) {
  return _edgeKeyFingerprints[buildEdgeKeyId(edgeId, edgeInstanceId)] || null;
}

function getEncKey(edgeId, edgeInstanceId) {
  const keyId = buildEdgeKeyId(edgeId, edgeInstanceId);
  if (_encKeys[keyId]) return _encKeys[keyId];
  const stored = sessionStorage.getItem(`3to1go_enc_${keyId}`);
  if (stored) {
    _encKeys[keyId] = stored;
    return stored;
  }
  return null;
}

function setEncKey(edgeId, edgeInstanceId, key) {
  const keyId = buildEdgeKeyId(edgeId, edgeInstanceId);
  _encKeys[keyId] = key;
  sessionStorage.setItem(`3to1go_enc_${keyId}`, key);
}

function clearStoredEncKey(edgeId, edgeInstanceId) {
  const keyId = buildEdgeKeyId(edgeId, edgeInstanceId);
  delete _encKeys[keyId];
  sessionStorage.removeItem(`3to1go_enc_${keyId}`);
}

function keyInputElement(edgeId, edgeInstanceId) {
  const selector = `[data-edge-key-input="${escapeSelectorValue(buildEdgeKeyId(edgeId, edgeInstanceId))}"]`;
  return document.querySelector(selector);
}

function keyStatusElement(edgeId, edgeInstanceId) {
  const selector = `[data-edge-key-status="${escapeSelectorValue(buildEdgeKeyId(edgeId, edgeInstanceId))}"]`;
  return document.querySelector(selector);
}

function setKeyStatus(edgeId, edgeInstanceId, message, kind = "info") {
  const element = keyStatusElement(edgeId, edgeInstanceId);
  if (!element) return;
  element.textContent = message;
  element.className = `key-status ${kind}`;
}

async function storeEncKey(edgeId, edgeInstanceId, key, { alertOnError = false } = {}) {
  try {
    const actualFingerprint = await fingerprintKey(key);
    const expectedFingerprint = getExpectedKeyFingerprint(edgeId, edgeInstanceId);
    if (expectedFingerprint && actualFingerprint !== expectedFingerprint) {
      clearStoredEncKey(edgeId, edgeInstanceId);
      const message = `That key belongs to a different Edge. Expected ${shortFingerprint(expectedFingerprint)}, got ${shortFingerprint(actualFingerprint)}.`;
      setKeyStatus(edgeId, edgeInstanceId, message, "error");
      if (alertOnError) setActionStatus(message, "error");
      return null;
    }

    setEncKey(edgeId, edgeInstanceId, key);
    setKeyStatus(
      edgeId,
      edgeInstanceId,
      expectedFingerprint
        ? `Key saved and verified for this browser session. Fingerprint ${shortFingerprint(actualFingerprint)}.`
        : `Key saved for this browser session. Fingerprint ${shortFingerprint(actualFingerprint)}.`,
      "ok",
    );
    return key;
  } catch {
    clearStoredEncKey(edgeId, edgeInstanceId);
    const message = "Encryption key was not valid base64url text.";
    setKeyStatus(edgeId, edgeInstanceId, message, "error");
    if (alertOnError) setActionStatus(message, "error");
    return null;
  }
}

async function rememberEncKey(edgeId, edgeInstanceId) {
  const input = keyInputElement(edgeId, edgeInstanceId);
  const key = input?.value.trim() || "";
  if (!key) {
    const expectedFingerprint = getExpectedKeyFingerprint(edgeId, edgeInstanceId);
    setKeyStatus(
      edgeId,
      edgeInstanceId,
      expectedFingerprint
        ? `Paste the Edge key first. Expected fingerprint ${shortFingerprint(expectedFingerprint)}.`
        : "Paste the Edge key first.",
      "warn",
    );
    return;
  }
  const stored = await storeEncKey(edgeId, edgeInstanceId, key);
  if (stored && input) input.value = "";
}

function clearEncKey(edgeId, edgeInstanceId) {
  clearStoredEncKey(edgeId, edgeInstanceId);
  const input = keyInputElement(edgeId, edgeInstanceId);
  if (input) input.value = "";
  const expectedFingerprint = getExpectedKeyFingerprint(edgeId, edgeInstanceId);
  setKeyStatus(
    edgeId,
    edgeInstanceId,
    expectedFingerprint
      ? `Cleared. Expected fingerprint ${shortFingerprint(expectedFingerprint)}.`
      : "Cleared saved key for this browser session.",
    "info",
  );
}

async function refreshKeyPanel(edgeId, edgeInstanceId) {
  const expectedFingerprint = getExpectedKeyFingerprint(edgeId, edgeInstanceId);
  const key = getEncKey(edgeId, edgeInstanceId);

  if (!key) {
    setKeyStatus(
      edgeId,
      edgeInstanceId,
      expectedFingerprint
        ? `No key saved yet. Expected fingerprint ${shortFingerprint(expectedFingerprint)}.`
        : "No key saved yet. Central has not seen a key fingerprint for this Edge yet.",
      expectedFingerprint ? "info" : "warn",
    );
    return;
  }

  try {
    const actualFingerprint = await fingerprintKey(key);
    if (expectedFingerprint && actualFingerprint !== expectedFingerprint) {
      clearStoredEncKey(edgeId, edgeInstanceId);
      setKeyStatus(
        edgeId,
        edgeInstanceId,
        `Saved key fingerprint ${shortFingerprint(actualFingerprint)} did not match expected ${shortFingerprint(expectedFingerprint)} and was cleared.`,
        "error",
      );
      return;
    }

    setKeyStatus(
      edgeId,
      edgeInstanceId,
      expectedFingerprint
        ? `Saved key verified for this browser session. Expected fingerprint ${shortFingerprint(expectedFingerprint)}.`
        : `Saved key present for this browser session. Fingerprint ${shortFingerprint(actualFingerprint)}.`,
      "ok",
    );
  } catch {
    clearStoredEncKey(edgeId, edgeInstanceId);
    setKeyStatus(edgeId, edgeInstanceId, "Saved key was invalid and has been cleared.", "error");
  }
}

async function resolveEncKey(edgeId, edgeInstanceId) {
  const saved = getEncKey(edgeId, edgeInstanceId);
  if (saved) return saved;

  const typed = keyInputElement(edgeId, edgeInstanceId)?.value.trim() || "";
  if (typed) {
    return storeEncKey(edgeId, edgeInstanceId, typed, { alertOnError: true });
  }

  const expectedFingerprint = getExpectedKeyFingerprint(edgeId, edgeInstanceId);
  const instanceLabel = edgeInstanceId || "legacy";
  const promptMessage = expectedFingerprint
    ? `Snapshot is encrypted. Enter the encryption key for edge "${edgeId}" instance "${instanceLabel}". Expected fingerprint: ${shortFingerprint(expectedFingerprint)}.`
    : `Snapshot is encrypted. Enter the encryption key for edge "${edgeId}" instance "${instanceLabel}".`;
  const prompted = await appDialog({
    title: "Encryption Key Required",
    message: promptMessage,
    input: true,
    inputLabel: "Encryption key",
    inputType: "password",
    confirmLabel: "Use Key",
  });
  if (!prompted) return null;
  return storeEncKey(edgeId, edgeInstanceId, prompted, { alertOnError: true });
}

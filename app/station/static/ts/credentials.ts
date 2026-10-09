// --- Credentials ---

function openCredentialDialog(): void {
  (document.getElementById("credential_ttl_days") as HTMLInputElement).value = "365";
  (document.getElementById("credential_shared") as HTMLInputElement).checked = false;
  (document.getElementById("credential_max_registrations") as HTMLInputElement).value = "1";
  (document.getElementById("credential_max_registrations") as HTMLInputElement).disabled = true;
  (document.getElementById("credential_output") as HTMLTextAreaElement).value = "";
  clearStatus("credential-status");
  openDialog("credential-dialog");
  void loadCredentials();
}

async function handleCredentialSharedToggle(): Promise<void> {
  const sharedInput = document.getElementById("credential_shared") as HTMLInputElement;
  const limitInput = document.getElementById("credential_max_registrations") as HTMLInputElement;
  if (!sharedInput.checked) {
    limitInput.disabled = true;
    return;
  }
  const confirmed = await confirmApp({
    title: "Shared Token",
    message: "Lets several Scouts use one Station token. Revoking it stops all of them.",
    confirmLabel: "Use Shared",
    danger: true,
  });
  if (!confirmed) {
    sharedInput.checked = false;
    limitInput.disabled = true;
    return;
  }
  limitInput.disabled = false;
  limitInput.focus();
}

async function mintCredential(): Promise<void> {
  const ttlDays = Number((document.getElementById("credential_ttl_days") as HTMLInputElement).value || 365);
  const shared = (document.getElementById("credential_shared") as HTMLInputElement).checked;
  const maxRegistrations = shared
    ? Number((document.getElementById("credential_max_registrations") as HTMLInputElement).value || 1)
    : 1;
  if (shared && (maxRegistrations < 2 || maxRegistrations > 10000)) {
    setStatus("credential-status", "Shared instance limit must be between 2 and 10000.", "error");
    return;
  }
  setStatus("credential-status", "Minting...", "info");
  const response = await fetch("/api/credentials/mint", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      ttl_days: ttlDays,
      shared,
      max_registrations: maxRegistrations,
    }),
  });
  const body = await readJson<MintCredentialResponse>(response);
  if (!response.ok) {
    setStatus("credential-status", body.detail || "Mint failed.", "error");
    setActionStatus(body.detail || "Mint failed.", "error");
    return;
  }
  (document.getElementById("credential_output") as HTMLTextAreaElement).value = body.credential || "";
  setStatus("credential-status", body.message || "Station token minted. Copy it before closing.", "success");
  setActionStatus("Station token minted.", "success");
  await loadCredentials();
}

async function copyMintedCredential(): Promise<void> {
  const value = (document.getElementById("credential_output") as HTMLTextAreaElement).value.trim();
  if (!value) {
    setStatus("credential-status", "Mint a token first.", "error");
    return;
  }
  try {
    await navigator.clipboard.writeText(value);
  } catch {
    const output = document.getElementById("credential_output") as HTMLTextAreaElement;
    output.focus();
    output.select();
    document.execCommand("copy");
  }
  setStatus("credential-status", "Copied.", "success");
}

interface IssuedCredential {
  token_hash: string;
  expires_at: string;
  created_at: string;
  shared: boolean;
  max_registrations: number;
}

async function loadCredentials(): Promise<void> {
  try {
    const response = await fetch("/api/credentials", { signal: globalThis.AbortSignal?.timeout?.(30000) });
    const body = await readJson<{ credentials?: IssuedCredential[]; detail?: string }>(response);
    if (!response.ok) throw new Error(body.detail || "Could not load Station tokens.");
    document.getElementById("credential-list")!.innerHTML =
      (body.credentials || [])
        .map(
          (token) => `<div class="user-row"><div>
      <strong>${escapeHtml(token.token_hash.slice(0, 12))}</strong>
      <p class="hint">Minted ${escapeHtml(token.created_at)} · expires ${escapeHtml(token.expires_at)} · ${token.shared ? `Shared, limit ${token.max_registrations}` : "Single instance"}</p></div>
      <button type="button" class="danger" onclick="revokeIssuedCredential(${escapeHtml(JSON.stringify(token.token_hash))})">Revoke</button></div>`,
        )
        .join("") || '<p class="hint">No Station tokens.</p>';
  } catch (error) {
    setStatus("credential-status", (error as Error).message || "Could not load Station tokens.", "error");
  }
}

async function revokeIssuedCredential(hash: string): Promise<void> {
  if (
    !(await confirmApp({
      title: "Revoke Station Token",
      message: "Uploads and recovery will stop for every Scout using this token.",
      confirmLabel: "Revoke",
      danger: true,
    }))
  )
    return;
  try {
    const response = await fetch(`/api/credentials/${encodeURIComponent(hash)}`, { method: "DELETE" });
    const body = await readJson(response);
    if (!response.ok) throw new Error(body.detail || "Could not revoke Station token.");
    (document.getElementById("credential_output") as HTMLTextAreaElement).value = "";
    setStatus("credential-status", "Station token revoked.", "success");
    await loadCredentials();
    await loadOverview();
  } catch (error) {
    setStatus("credential-status", (error as Error).message || "Could not revoke Station token.", "error");
  }
}

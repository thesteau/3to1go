interface UserUpdatePayload {
  password?: string;
  username?: string | null;
  is_admin?: boolean;
}

async function openUserManagementDialog(): Promise<void> {
  clearStatus("users-status");
  openDialog("users-dialog");
  for (const panel of [automationTokenPanel, monitoringTokenPanel]) {
    const output = document.getElementById(`${panel.prefix}_token_output`) as HTMLTextAreaElement | null;
    if (output) output.value = "";
    clearStatus(`${panel.prefix}-token-status`);
  }
  await loadUsers();
  await loadAutomationTokens();
  await loadMonitoringTokens();
}

async function loadUsers(): Promise<void> {
  try {
    const response = await fetch("/api/users", { signal: globalThis.AbortSignal?.timeout?.(30000) });
    const body = await readJson<UsersResponse>(response);
    if (!response.ok) {
      setStatus("users-status", body.detail || "Could not load users.", "error");
      return;
    }
    renderUsers(body.users || []);
    renderBuildInfo(body.build);
  } catch {
    setStatus("users-status", "Could not load users. Close and reopen to retry.", "error");
  }
}

// Prod builds show the tag, main builds the commit hash.
function renderBuildInfo(build: BuildInfo | undefined): void {
  const element = document.getElementById("build-info");
  if (!element || !build?.summary) return;
  element.textContent = `Version ${build.summary}`;
  element.title = build.commit ? `Commit ${build.commit}` : "";
}

function renderUsers(users: CurrentUser[]): void {
  const canAdmin = Boolean(currentUser?.is_admin);
  document.getElementById("add-user-section")!.hidden = !canAdmin;
  document.getElementById("users-list")!.innerHTML = users
    .map((user) => {
      const isSelf = currentUser?.id === user.id;
      const isBootstrapAdmin = Boolean(user.is_bootstrap_admin);
      const canEditUsername = canAdmin || isSelf;
      const canResetPassword = canAdmin && !isSelf;
      const canToggleAdmin = canAdmin && !isSelf && !isBootstrapAdmin;
      const canRemove = canAdmin && !isSelf && !isBootstrapAdmin;
      return `
      <div class="user-row">
        <div>
          <strong>${escapeHtml(user.username)}</strong>
          ${user.is_admin ? '<span class="admin-pill">Admin</span>' : ""}
          ${isSelf ? '<span class="hint">You</span>' : ""}
          ${user.must_change_password ? '<span class="hint">Password change pending</span>' : ""}
        </div>
        <div>
          ${canEditUsername ? `<input id="user_username_${user.id}" type="text" value="${escapeHtml(user.username)}" autocomplete="off" spellcheck="false" autocapitalize="none">` : ""}
          ${canResetPassword ? `<input id="user_password_${user.id}" type="text" class="secret-value" placeholder="reset password" minlength="5" autocomplete="off" spellcheck="false" autocapitalize="none">` : ""}
          ${canToggleAdmin ? `<label class="checkbox"><input id="user_admin_${user.id}" type="checkbox" ${user.is_admin ? "checked" : ""}><span>Admin</span></label>` : ""}
        </div>
        <div class="user-actions">
          ${canEditUsername || canResetPassword || canToggleAdmin ? `<button type="button" class="secondary" onclick="saveUser(${user.id})">Save</button>` : ""}
          ${canRemove ? `<button type="button" class="danger" onclick="deleteUser(${user.id})">Remove</button>` : ""}
        </div>
      </div>
    `;
    })
    .join("");
}

async function saveUser(userId: number): Promise<void> {
  const payload: UserUpdatePayload = {};
  const passwordInput = document.getElementById(`user_password_${userId}`) as HTMLInputElement | null;
  if (passwordInput?.value) {
    if (passwordInput.value.length < 5) {
      setStatus("users-status", "Password must be at least 5 characters.", "error");
      return;
    }
    if (!passwordInput.value.trim()) {
      setStatus("users-status", "Password cannot be only spaces.", "error");
      return;
    }
    payload.password = passwordInput.value;
  }
  const usernameInput = document.getElementById(`user_username_${userId}`) as HTMLInputElement | null;
  if (usernameInput) {
    payload.username = usernameInput.value.trim() || null;
  }
  const adminInput = document.getElementById(`user_admin_${userId}`) as HTMLInputElement | null;
  if (adminInput) {
    payload.is_admin = Boolean(adminInput.checked);
  }
  if (currentUser?.is_admin) {
    payload.username = usernameInput?.value.trim() || payload.username || null;
  }
  const response = await fetch(`/api/users/${userId}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  const body = await readJson<UserResponse>(response);
  setStatus("users-status", response.ok ? "Saved." : body.detail || "Save failed.", response.ok ? "success" : "error");
  if (response.ok) {
    if (currentUser?.id === userId) {
      currentUser = body.user;
      updateIntegrationAccess();
    }
    await loadUsers();
  }
}

async function createUser(): Promise<void> {
  const usernameInput = document.getElementById("new_user_username") as HTMLInputElement;
  const passwordInput = document.getElementById("new_user_password") as HTMLInputElement;
  const adminInput = document.getElementById("new_user_admin") as HTMLInputElement;
  const password = passwordInput.value;
  if (password.length < 5) {
    setStatus("users-status", "Password must be at least 5 characters.", "error");
    return;
  }
  if (!password.trim()) {
    setStatus("users-status", "Password cannot be only spaces.", "error");
    return;
  }
  const response = await fetch("/api/users", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      username: usernameInput.value.trim(),
      password,
      is_admin: adminInput.checked,
    }),
  });
  const body = await readJson(response);
  setStatus(
    "users-status",
    response.ok ? "User added." : body.detail || "Add failed.",
    response.ok ? "success" : "error",
  );
  if (response.ok) {
    usernameInput.value = "";
    passwordInput.value = "";
    adminInput.checked = false;
    await loadUsers();
  }
}

async function deleteUser(userId: number): Promise<void> {
  if (
    !(await confirmApp({ title: "Remove User", message: "Remove this user?", confirmLabel: "Remove", danger: true }))
  ) {
    return;
  }
  const response = await fetch(`/api/users/${userId}`, { method: "DELETE" });
  const body = await readJson(response);
  setStatus(
    "users-status",
    response.ok ? "User removed." : body.detail || "Remove failed.",
    response.ok ? "success" : "error",
  );
  if (response.ok) await loadUsers();
}

interface IssuedTokenInfo {
  id: string;
  name: string;
  scopes?: string[];
  routes?: string[];
  created_at: string;
  expires_at: string;
}

// Automation and monitoring tokens share one flow. An app without a panel's section skips it.
interface TokenPanel {
  prefix: string;
  label: string;
  title: string;
  endpoint: string;
  field: "scopes" | "routes";
  options: string[];
  revokeHandler: string;
}

const automationTokenPanel: TokenPanel = {
  prefix: "automation",
  label: "automation token",
  title: "Automation Token",
  endpoint: "/api/automation-tokens",
  field: "scopes",
  options: ["read", "backup", "restore", "manage", "monitoring"],
  revokeHandler: "revokeAutomationToken",
};

const monitoringTokenPanel: TokenPanel = {
  prefix: "monitoring",
  label: "monitoring token",
  title: "Monitoring Token",
  endpoint: "/api/monitoring-tokens",
  field: "routes",
  options: ["metrics", "report"],
  revokeHandler: "revokeMonitoringToken",
};

function renderTokens(panel: TokenPanel, tokens: IssuedTokenInfo[]): string {
  return (
    tokens
      .map(
        (token) => `<div class="user-row"><div><strong>${escapeHtml(token.name)}</strong>
    <p class="hint">${escapeHtml((token[panel.field] || []).join(", "))} · expires ${escapeHtml(token.expires_at)}</p></div>
    <button type="button" class="danger" onclick="${panel.revokeHandler}(${escapeHtml(JSON.stringify(token.id))})">Revoke</button></div>`,
      )
      .join("") || `<p class="hint">No ${panel.label}s.</p>`
  );
}

async function loadTokens(panel: TokenPanel): Promise<void> {
  const section = document.getElementById(`${panel.prefix}-token-section`);
  if (!section) return;
  section.hidden = !currentUser?.is_admin;
  if (section.hidden) return;
  try {
    const response = await fetch(panel.endpoint, { signal: globalThis.AbortSignal?.timeout?.(30000) });
    const body = await readJson<{ tokens?: IssuedTokenInfo[]; detail?: string }>(response);
    if (!response.ok) throw new Error(body.detail || `Could not load ${panel.label}s.`);
    document.getElementById(`${panel.prefix}-token-list`)!.innerHTML = renderTokens(panel, body.tokens || []);
  } catch (error) {
    setStatus(`${panel.prefix}-token-status`, (error as Error).message || `Could not load ${panel.label}s.`, "error");
  }
}

async function createToken(panel: TokenPanel): Promise<void> {
  const status = `${panel.prefix}-token-status`;
  const output = document.getElementById(`${panel.prefix}_token_output`) as HTMLTextAreaElement;
  output.value = "";
  const name = (document.getElementById(`${panel.prefix}_token_name`) as HTMLInputElement).value.trim();
  const days = Number((document.getElementById(`${panel.prefix}_token_days`) as HTMLInputElement).value);
  // Not every app offers every option.
  const selected = panel.options.filter(
    (option) => (document.getElementById(`${panel.prefix}_scope_${option}`) as HTMLInputElement | null)?.checked,
  );
  if (!name || !Number.isInteger(days) || days < 1 || days > 3650 || selected.length === 0) {
    setStatus(status, "Enter a name, expiry from 1 to 3650 days, and at least one permission.", "error");
    return;
  }
  try {
    const response = await fetch(panel.endpoint, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name, ttl_days: days, [panel.field]: selected }),
    });
    const body = await readJson<{ token?: string; detail?: string }>(response);
    if (!response.ok || !body.token) throw new Error(body.detail || `Could not create ${panel.label}.`);
    output.value = body.token;
    setStatus(status, "Copy the token now. It will not be shown again.", "success");
    await loadTokens(panel);
  } catch (error) {
    setStatus(status, (error as Error).message || `Could not create ${panel.label}.`, "error");
  }
}

async function revokeToken(panel: TokenPanel, id: string): Promise<void> {
  if (
    !(await confirmApp({
      title: `Revoke ${panel.title}`,
      message: "Clients using this token will lose access immediately.",
      confirmLabel: "Revoke",
      danger: true,
    }))
  )
    return;
  const status = `${panel.prefix}-token-status`;
  try {
    const response = await fetch(`${panel.endpoint}/${encodeURIComponent(id)}`, { method: "DELETE" });
    const body = await readJson(response);
    if (!response.ok) throw new Error(body.detail || `Could not revoke ${panel.label}.`);
    setStatus(status, "Token revoked.", "success");
    await loadTokens(panel);
  } catch (error) {
    setStatus(status, (error as Error).message || `Could not revoke ${panel.label}.`, "error");
  }
}

function renderAutomationTokens(tokens: IssuedTokenInfo[]): string {
  return renderTokens(automationTokenPanel, tokens);
}

async function loadAutomationTokens(): Promise<void> {
  await loadTokens(automationTokenPanel);
}

async function createAutomationToken(): Promise<void> {
  await createToken(automationTokenPanel);
}

async function revokeAutomationToken(id: string): Promise<void> {
  await revokeToken(automationTokenPanel, id);
}

function renderMonitoringTokens(tokens: IssuedTokenInfo[]): string {
  return renderTokens(monitoringTokenPanel, tokens);
}

async function loadMonitoringTokens(): Promise<void> {
  await loadTokens(monitoringTokenPanel);
}

async function createMonitoringToken(): Promise<void> {
  await createToken(monitoringTokenPanel);
}

async function revokeMonitoringToken(id: string): Promise<void> {
  await revokeToken(monitoringTokenPanel, id);
}

interface UserUpdatePayload {
  password?: string;
  username?: string | null;
  is_admin?: boolean;
}

async function openUserManagementDialog(): Promise<void> {
  clearStatus("users-status");
  openDialog("users-dialog");
  await loadUsers();
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
  } catch {
    setStatus("users-status", "Could not load users. Close and reopen to retry.", "error");
  }
}

function renderUsers(users: CurrentUser[]): void {
  const canAdmin = Boolean(currentUser?.is_admin);
  document.getElementById("add-user-section")!.hidden = !canAdmin;
  document.getElementById("users-list")!.innerHTML = users.map((user) => {
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
          ${canEditUsername ? `<input id="user_username_${user.id}" value="${escapeHtml(user.username)}">` : ""}
          ${canResetPassword ? `<input id="user_password_${user.id}" type="password" placeholder="reset password" minlength="5">` : ""}
          ${canToggleAdmin ? `<label class="checkbox"><input id="user_admin_${user.id}" type="checkbox" ${user.is_admin ? "checked" : ""}><span>Admin</span></label>` : ""}
        </div>
        <div class="user-actions">
          ${canEditUsername || canResetPassword || canToggleAdmin ? `<button type="button" class="secondary" onclick="saveUser(${user.id})">Save</button>` : ""}
          ${canRemove ? `<button type="button" class="danger" onclick="deleteUser(${user.id})">Remove</button>` : ""}
        </div>
      </div>
    `;
  }).join("");
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
  setStatus("users-status", response.ok ? "Saved." : (body.detail || "Save failed."), response.ok ? "success" : "error");
  if (response.ok) {
    if (currentUser?.id === userId) currentUser = body.user;
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
  setStatus("users-status", response.ok ? "User added." : (body.detail || "Add failed."), response.ok ? "success" : "error");
  if (response.ok) {
    usernameInput.value = "";
    passwordInput.value = "";
    adminInput.checked = false;
    await loadUsers();
  }
}

async function deleteUser(userId: number): Promise<void> {
  if (!await confirmApp({ title: "Remove User", message: "Remove this user?", confirmLabel: "Remove", danger: true })) {
    return;
  }
  const response = await fetch(`/api/users/${userId}`, { method: "DELETE" });
  const body = await readJson(response);
  setStatus("users-status", response.ok ? "User removed." : (body.detail || "Remove failed."), response.ok ? "success" : "error");
  if (response.ok) await loadUsers();
}

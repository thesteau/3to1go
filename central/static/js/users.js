async function openUserManagementDialog() {
  clearStatus("users-status");
  openDialog("users-dialog");
  await loadUsers();
}

async function loadUsers() {
  const response = await fetch("/api/users");
  const body = await readJson(response);
  if (!response.ok) {
    setStatus("users-status", body.detail || "Could not load users.", "error");
    return;
  }
  renderUsers(body.users || []);
}

function renderUsers(users) {
  const canAdmin = Boolean(currentUser?.is_admin);
  document.getElementById("add-user-section").hidden = !canAdmin;
  document.getElementById("migration-section").hidden = !canAdmin;
  document.getElementById("users-list").innerHTML = users.map((user) => {
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

async function migrateUploadSessions() {
  if (!currentUser?.is_admin) return;
  const button = document.getElementById("migrate-upload-sessions-btn");
  if (button) button.disabled = true;
  setStatus("users-status", "Migrating upload sessions...", "info");
  try {
    const response = await fetch("/api/migrations/upload-sessions", { method: "POST" });
    const body = await readJson(response);
    if (!response.ok) {
      setStatus("users-status", body.detail || "Migration failed.", "error");
      return;
    }
    const migrated = Number(body.migrated || 0);
    if (migrated > 0) {
      const message = `Migrated ${migrated} upload session${migrated === 1 ? "" : "s"}.`;
      setStatus("users-status", message, "success");
      showToast(message, "success", { title: "Migration complete" });
      return;
    }
    clearStatus("users-status");
  } catch (error) {
    setStatus("users-status", error.message || "Migration failed.", "error");
  } finally {
    if (button) button.disabled = false;
  }
}

async function saveUser(userId) {
  const payload = {};
  const passwordInput = document.getElementById(`user_password_${userId}`);
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
  const usernameInput = document.getElementById(`user_username_${userId}`);
  if (usernameInput) {
    payload.username = usernameInput.value.trim() || null;
  }
  const adminInput = document.getElementById(`user_admin_${userId}`);
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
  const body = await readJson(response);
  setStatus("users-status", response.ok ? "Saved." : (body.detail || "Save failed."), response.ok ? "success" : "error");
  if (response.ok) {
    if (currentUser?.id === userId) currentUser = body.user;
    await loadUsers();
  }
}

async function createUser() {
  const password = document.getElementById("new_user_password").value;
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
      username: document.getElementById("new_user_username").value.trim(),
      password,
      is_admin: document.getElementById("new_user_admin").checked,
    }),
  });
  const body = await readJson(response);
  setStatus("users-status", response.ok ? "User added." : (body.detail || "Add failed."), response.ok ? "success" : "error");
  if (response.ok) {
    document.getElementById("new_user_username").value = "";
    document.getElementById("new_user_password").value = "";
    document.getElementById("new_user_admin").checked = false;
    await loadUsers();
  }
}

async function deleteUser(userId) {
  if (!await confirmApp({ title: "Remove User", message: "Remove this user?", confirmLabel: "Remove", danger: true })) {
    return;
  }
  const response = await fetch(`/api/users/${userId}`, { method: "DELETE" });
  const body = await readJson(response);
  setStatus("users-status", response.ok ? "User removed." : (body.detail || "Remove failed."), response.ok ? "success" : "error");
  if (response.ok) await loadUsers();
}

let currentUser: CurrentUser | null = null;
const rawFetch = window.fetch.bind(window);

window.fetch = async (...args: Parameters<typeof fetch>) => {
  const response = await rawFetch(...args);
  const url = typeof args[0] === "string" ? args[0] : (args[0] as Request | undefined)?.url || "";
  if (!url.includes("/api/session/") && response.status === 401) {
    openLoginDialog();
  }
  if (!url.includes("/api/session/") && response.status === 403) {
    response.clone().json().then((body: ApiBody) => {
      if (body.detail === "password change required") {
        openPasswordDialog(true);
      }
    }).catch(() => {});
  }
  return response;
};

async function refreshSession(): Promise<CurrentUser | null> {
  const response = await rawFetch("/api/session/me", { signal: globalThis.AbortSignal?.timeout?.(30000) });
  const body = await readJson<SessionResponse>(response);
  currentUser = body.user || null;
  return body.authenticated ? currentUser : null;
}

function openLoginDialog(): void {
  clearSessionEncKeys();
  resolveAppDialog(false);
  clearStatus("login-status");
  openDialog("login-dialog");
  window.setTimeout(() => document.getElementById("login_password")?.focus(), 0);
}

async function loginUser(): Promise<void> {
  setStatus("login-status", "Signing in...", "info");
  const response = await rawFetch("/api/session/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      username: (document.getElementById("login_username") as HTMLInputElement).value.trim(),
      password: (document.getElementById("login_password") as HTMLInputElement).value,
    }),
  });
  const body = await readJson<UserResponse>(response);
  if (!response.ok) {
    setStatus("login-status", body.detail || "Sign in failed.", "error");
    return;
  }
  currentUser = body.user;
  closeDialog("login-dialog");
  (document.getElementById("login_password") as HTMLInputElement).value = "";
  if (currentUser.must_change_password) {
    openPasswordDialog(true);
    return;
  }
  startCentralApp();
}

function openPasswordDialog(force = false): void {
  clearStatus("password-status");
  (document.getElementById("current_password") as HTMLInputElement).value = "";
  (document.getElementById("new_password") as HTMLInputElement).value = "";
  (document.getElementById("confirm_new_password") as HTMLInputElement).value = "";
  document.getElementById("password-dialog-message")!.textContent = force
    ? "The default admin password must be changed before continuing."
    : "Update your password.";
  document.getElementById("password-cancel")!.hidden = force;
  openDialog("password-dialog");
}

async function changeOwnPassword(): Promise<void> {
  const currentPassword = (document.getElementById("current_password") as HTMLInputElement).value;
  const newPassword = (document.getElementById("new_password") as HTMLInputElement).value;
  const confirmNewPassword = (document.getElementById("confirm_new_password") as HTMLInputElement).value;
  if (!currentPassword) {
    setStatus("password-status", "Current password is required.", "error");
    return;
  }
  if (newPassword.length < 5) {
    setStatus("password-status", "New password must be at least 5 characters.", "error");
    return;
  }
  if (!newPassword.trim()) {
    setStatus("password-status", "New password cannot be only spaces.", "error");
    return;
  }
  if (newPassword !== confirmNewPassword) {
    setStatus("password-status", "New passwords do not match.", "error");
    return;
  }
  setStatus("password-status", "Saving...", "info");
  const response = await rawFetch("/api/session/change-password", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      current_password: currentPassword,
      new_password: newPassword,
      confirm_new_password: confirmNewPassword,
    }),
  });
  const body = await readJson<UserResponse>(response);
  if (!response.ok) {
    setStatus("password-status", body.detail || "Password change failed.", "error");
    return;
  }
  currentUser = body.user;
  closeDialog("password-dialog");
  setActionStatus("Password updated.", "success");
  startCentralApp();
}

async function logoutUser(): Promise<void> {
  clearSessionEncKeys();
  resolveAppDialog(false);
  try {
    const response = await rawFetch("/api/session/logout", { method: "POST" });
    if (!response.ok) throw new Error("Sign out failed. Please try again.");
  } catch (error) {
    setActionStatus((error as Error).message || "Sign out failed. Please try again.", "error");
    return;
  }
  currentUser = null;
  closeDialog("users-dialog");
  openLoginDialog();
}

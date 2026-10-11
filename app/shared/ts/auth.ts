async function refreshSession(): Promise<CurrentUser | null> {
  const response = await rawFetch("/api/session/me", { signal: globalThis.AbortSignal?.timeout?.(30000) });
  const body = await readJson<SessionResponse>(response);
  currentUser = body.user || null;
  updateIntegrationAccess();
  return body.authenticated ? currentUser : null;
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

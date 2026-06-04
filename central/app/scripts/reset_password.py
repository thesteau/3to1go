from __future__ import annotations

import argparse
import getpass
import os
import sys

from app.core.config import app_database_path, load_settings
from app.services.user_store import UserStore


def main() -> int:
    parser = argparse.ArgumentParser(description="Reset a Central web user's password from the server.")
    parser.add_argument("username", help="Existing username to reset")
    parser.add_argument("--password-env", help="Read the new password from this environment variable")
    parser.add_argument("--no-force-change", action="store_true", help="Do not require the user to change this password on next sign-in")
    args = parser.parse_args()

    password = _read_password(args.password_env)
    settings = load_settings()
    store = UserStore(database_url=settings.index_database_url, sqlite_path=app_database_path())
    user = store.get_user_by_username(args.username)
    if user is None:
        print(f"User not found: {args.username}", file=sys.stderr)
        return 1

    updated = store.update_user(user["id"], password=password, must_change_password=not args.no_force_change)
    store.delete_sessions_for_user(user["id"])
    suffix = "" if args.no_force_change else " and must change it on next sign-in"
    print(f"Password reset for {updated['username']}{suffix}.")
    return 0


def _read_password(password_env: str | None) -> str:
    if password_env:
        password = os.getenv(password_env, "")
        if not password:
            raise SystemExit(f"{password_env} is empty or not set")
        return password

    password = getpass.getpass("New password: ")
    confirm = getpass.getpass("Confirm new password: ")
    if password != confirm:
        raise SystemExit("Passwords do not match")
    return password


if __name__ == "__main__":
    raise SystemExit(main())

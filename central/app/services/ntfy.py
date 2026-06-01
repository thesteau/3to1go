from __future__ import annotations

import json
import re
from typing import Any
from urllib import error, parse, request


DEFAULT_CENTRAL_NTFY_MESSAGE_TEMPLATE = (
    "Central received {{ edge_id }}/{{ edge_instance_id }} job {{ job_name }} from {{ source_address }} as {{ stored_as }}."
)

_TEMPLATE_PATTERN = re.compile(r"{{\s*([a-zA-Z0-9_]+)\s*}}")


class NtfyPublisher:
    def __init__(self, logger) -> None:
        self.logger = logger

    def snapshot(self, settings) -> dict[str, Any]:
        return {
            "ntfy_url": settings.ntfy_url,
            "ntfy_topic": settings.ntfy_topic,
            "ntfy_message_template": settings.ntfy_message_template,
            "ntfy_match_edge_id": settings.ntfy_match_edge_id,
            "ntfy_match_edge_instance_id": settings.ntfy_match_edge_instance_id,
            "ntfy_match_source": settings.ntfy_match_source,
            "default_message_template": DEFAULT_CENTRAL_NTFY_MESSAGE_TEMPLATE,
        }

    def publish_test(self, config: dict[str, Any]) -> None:
        template = str(config.get("ntfy_message_template") or "").strip() or DEFAULT_CENTRAL_NTFY_MESSAGE_TEMPLATE
        message = self.render_message(
            template,
            {
                "edge_id": config.get("ntfy_match_edge_id") or "edge-01",
                "edge_instance_id": config.get("ntfy_match_edge_instance_id") or "edgeinstance0001",
                "job_name": "test-job",
                "source_address": config.get("ntfy_match_source") or "127.0.0.1",
                "stored_as": "test-upload.tar.zst",
            },
        )
        self._publish(
            ntfy_url=str(config.get("ntfy_url") or "").strip(),
            ntfy_topic=str(config.get("ntfy_topic") or "").strip(),
            message=message,
        )

    def publish_best_effort(self, settings, context: dict[str, Any]) -> None:
        if not self._matches(settings, context):
            return

        template = settings.ntfy_message_template or DEFAULT_CENTRAL_NTFY_MESSAGE_TEMPLATE
        message = self.render_message(template, context)
        try:
            self._publish(settings.ntfy_url, settings.ntfy_topic, message)
        except Exception as exc:
            self.logger.warning(
                "ntfy_publish_failed edge_id=%s edge_instance_id=%s job_name=%s detail=%s",
                context.get("edge_id"),
                context.get("edge_instance_id"),
                context.get("job_name"),
                exc,
            )

    def render_message(self, template: str, context: dict[str, Any]) -> str:
        normalized = template.strip() or DEFAULT_CENTRAL_NTFY_MESSAGE_TEMPLATE

        def replace(match: re.Match[str]) -> str:
            key = match.group(1)
            value = context.get(key)
            return "" if value is None else str(value)

        return _TEMPLATE_PATTERN.sub(replace, normalized)

    def _matches(self, settings, context: dict[str, Any]) -> bool:
        if not settings.ntfy_url or not settings.ntfy_topic:
            return False
        if settings.ntfy_match_edge_id and settings.ntfy_match_edge_id != str(context.get("edge_id") or "").strip():
            return False
        if settings.ntfy_match_edge_instance_id and settings.ntfy_match_edge_instance_id != str(
            context.get("edge_instance_id") or ""
        ).strip():
            return False
        if settings.ntfy_match_source and settings.ntfy_match_source != str(context.get("source_address") or "").strip():
            return False
        return True

    def _publish(self, ntfy_url: str, ntfy_topic: str, message: str) -> None:
        base_url = str(ntfy_url or "").strip().rstrip("/")
        topic = str(ntfy_topic or "").strip()
        if not base_url or not topic:
            raise ValueError("ntfy url and topic are required")

        publish_url = f"{base_url}/{parse.quote(topic, safe='')}"
        payload = message.encode("utf-8")
        req = request.Request(
            publish_url,
            data=payload,
            method="POST",
            headers={
                "Content-Type": "text/plain; charset=utf-8",
                "Content-Length": str(len(payload)),
                "X-Relay-Event": "upload-received",
            },
        )
        try:
            with request.urlopen(req, timeout=5) as response:
                if response.status >= 400:
                    detail = response.read().decode("utf-8", errors="replace")
                    raise RuntimeError(detail or f"ntfy returned {response.status}")
        except error.HTTPError as exc:
            detail = exc.read().decode("utf-8", errors="replace")
            try:
                parsed = json.loads(detail)
                detail = parsed.get("error") or parsed.get("message") or detail
            except json.JSONDecodeError:
                pass
            raise RuntimeError(detail or f"ntfy returned {exc.code}") from exc
        except error.URLError as exc:
            raise RuntimeError(str(exc.reason) or "unable to reach ntfy server") from exc

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timedelta, timezone


MINIMUM_SCHEDULE_MINUTES = 5
_MINIMUM_SCHEDULE_GAP = timedelta(minutes=MINIMUM_SCHEDULE_MINUTES)
_FIELD_SPECS = (
    ("minute", 0, 59),
    ("hour", 0, 23),
    ("day_of_month", 1, 31),
    ("month", 1, 12),
    ("day_of_week", 0, 6),
)
_MAX_SEARCH_MINUTES = 5 * 366 * 24 * 60


@dataclass(frozen=True, slots=True)
class CronSchedule:
    expression: str
    minutes: frozenset[int]
    hours: frozenset[int]
    days_of_month: frozenset[int]
    months: frozenset[int]
    days_of_week: frozenset[int]
    day_of_month_wildcard: bool
    day_of_week_wildcard: bool

    @classmethod
    def from_expression(cls, expression: str) -> CronSchedule:
        normalized = " ".join(expression.split())
        parts = normalized.split(" ")
        if len(parts) != 5:
            raise ValueError("CRON_SCHEDULE must use 5 fields like '0 2 * * *'")

        parsed_fields: list[frozenset[int]] = []
        wildcard_flags: list[bool] = []
        for part, (name, minimum, maximum) in zip(parts, _FIELD_SPECS, strict=True):
            values, is_wildcard = _parse_field(part, minimum, maximum, normalize_day_of_week=name == "day_of_week")
            parsed_fields.append(values)
            wildcard_flags.append(is_wildcard)

        schedule = cls(
            expression=normalized,
            minutes=parsed_fields[0],
            hours=parsed_fields[1],
            days_of_month=parsed_fields[2],
            months=parsed_fields[3],
            days_of_week=parsed_fields[4],
            day_of_month_wildcard=wildcard_flags[2],
            day_of_week_wildcard=wildcard_flags[4],
        )
        schedule._validate_minimum_spacing()
        return schedule

    def next_after(self, after: datetime) -> datetime:
        if after.tzinfo is None:
            raise ValueError("cron calculations require timezone-aware datetimes")

        candidate = after.replace(second=0, microsecond=0) + timedelta(minutes=1)
        for _ in range(_MAX_SEARCH_MINUTES):
            if self.matches(candidate):
                return candidate
            candidate += timedelta(minutes=1)
        raise ValueError(f"unable to find next cron occurrence for '{self.expression}' within one year")

    def matches(self, value: datetime) -> bool:
        if value.minute not in self.minutes or value.hour not in self.hours or value.month not in self.months:
            return False

        day_of_month_match = value.day in self.days_of_month
        day_of_week_match = _cron_day_of_week(value) in self.days_of_week

        if self.day_of_month_wildcard and self.day_of_week_wildcard:
            day_match = True
        elif self.day_of_month_wildcard:
            day_match = day_of_week_match
        elif self.day_of_week_wildcard:
            day_match = day_of_month_match
        else:
            day_match = day_of_month_match or day_of_week_match

        return day_match

    def _validate_minimum_spacing(self) -> None:
        reference = datetime(2026, 1, 1, tzinfo=timezone.utc) - timedelta(minutes=1)
        previous = self.next_after(reference)
        for _ in range(128):
            current = self.next_after(previous)
            if current - previous < _MINIMUM_SCHEDULE_GAP:
                raise ValueError(
                    f"CRON_SCHEDULE must not run more often than every {MINIMUM_SCHEDULE_MINUTES} minutes"
                )
            if current - previous > timedelta(days=7):
                return
            previous = current


def _parse_field(
    field: str,
    minimum: int,
    maximum: int,
    *,
    normalize_day_of_week: bool = False,
) -> tuple[frozenset[int], bool]:
    if field == "*":
        return frozenset(range(minimum, maximum + 1)), True

    values: set[int] = set()
    for chunk in field.split(","):
        values.update(_expand_chunk(chunk.strip(), minimum, maximum, normalize_day_of_week=normalize_day_of_week))

    if not values:
        raise ValueError(f"invalid cron field '{field}'")
    return frozenset(sorted(values)), False


def _expand_chunk(chunk: str, minimum: int, maximum: int, *, normalize_day_of_week: bool) -> set[int]:
    if not chunk:
        raise ValueError("cron field contains an empty segment")

    base, step = _split_step(chunk)
    if step <= 0:
        raise ValueError("cron step values must be positive")

    start, end = _parse_base_range(base, minimum, maximum, normalize_day_of_week=normalize_day_of_week)
    values: set[int] = set()
    for value in range(start, end + 1, step):
        normalized_value = _normalize_value(value, minimum, maximum, normalize_day_of_week=normalize_day_of_week)
        values.add(normalized_value)
    return values


def _split_step(chunk: str) -> tuple[str, int]:
    if "/" not in chunk:
        return chunk, 1

    base, step_text = chunk.split("/", 1)
    try:
        return base, int(step_text)
    except ValueError as exc:
        raise ValueError(f"invalid cron step '{step_text}'") from exc


def _parse_base_range(
    base: str,
    minimum: int,
    maximum: int,
    *,
    normalize_day_of_week: bool,
) -> tuple[int, int]:
    if base == "*":
        return minimum, maximum

    if "-" in base:
        start_text, end_text = base.split("-", 1)
        start = _parse_value(start_text, minimum, maximum, normalize_day_of_week=normalize_day_of_week)
        end = _parse_value(end_text, minimum, maximum, normalize_day_of_week=normalize_day_of_week)
        if start > end:
            raise ValueError(f"invalid cron range '{base}'")
        return start, end

    value = _parse_value(base, minimum, maximum, normalize_day_of_week=normalize_day_of_week)
    return value, value


def _parse_value(value_text: str, minimum: int, maximum: int, *, normalize_day_of_week: bool) -> int:
    try:
        value = int(value_text)
    except ValueError as exc:
        raise ValueError(f"invalid cron value '{value_text}'") from exc
    return _normalize_value(value, minimum, maximum, normalize_day_of_week=normalize_day_of_week)


def _normalize_value(value: int, minimum: int, maximum: int, *, normalize_day_of_week: bool) -> int:
    if normalize_day_of_week and value == 7:
        value = 0
    if value < minimum or value > maximum:
        raise ValueError(f"cron value '{value}' must be between {minimum} and {maximum}")
    return value


def _cron_day_of_week(value: datetime) -> int:
    return (value.weekday() + 1) % 7

#!/usr/bin/env python3

import json
import re
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
PUBLIC_GROUPS = [
    "shared",
    "api-explorer",
    "project",
    "topic",
    "index",
    "log",
    "host-group",
    "collector",
    "metric-topic",
]
ACTION_GROUPS = [
    "project",
    "topic",
    "index",
    "log",
    "host-group",
    "collector",
    "metric-topic",
]
REQUIRED_SECTIONS = [
    "## 适用场景",
    "## 必填输入",
    "## 可选参数触发词",
    "## 字段联动/限制",
    "## 常见误用",
    "## 下一步命令",
]


def load_generated_capabilities() -> dict:
    path = ROOT / "internal/cli/generated_capabilities.go"
    text = path.read_text()
    match = re.search(r"const generatedCapabilitiesJSON = `(.+)`$", text, re.S)
    if not match:
        raise RuntimeError("failed to locate generatedCapabilitiesJSON")
    return json.loads(match.group(1))


def list_reference_files() -> list[Path]:
    files: list[Path] = []
    for group in PUBLIC_GROUPS:
        files.extend(sorted((ROOT / f"skills/volclog-{group}/references").glob("*.md")))
    return files


def audit_sections(files: list[Path]) -> list[tuple[Path, list[str]]]:
    failures: list[tuple[Path, list[str]]] = []
    for path in files:
        content = path.read_text()
        missing = [section for section in REQUIRED_SECTIONS if section not in content]
        if missing:
            failures.append((path, missing))
    return failures


def audit_action_coverage(capabilities: dict) -> list[tuple[str, str]]:
    failures: list[tuple[str, str]] = []
    ref_blobs: dict[str, list[tuple[str, str]]] = {}
    for group in ACTION_GROUPS:
        ref_dir = ROOT / f"skills/volclog-{group}/references"
        ref_blobs[group] = [
            (path.name, path.read_text(errors="ignore"))
            for path in sorted(ref_dir.glob("*.md"))
        ]
    for command in capabilities["commands"]:
        group = command["group"]
        action = command["action"]
        if group not in ACTION_GROUPS:
            continue
        if not any(action in blob for _, blob in ref_blobs[group]):
            failures.append((group, action))
    return failures


def main() -> int:
    files = list_reference_files()
    section_failures = audit_sections(files)
    action_failures = audit_action_coverage(load_generated_capabilities())

    if not section_failures and not action_failures:
        print("public skill references audit: PASS")
        print(f"checked references: {len(files)}")
        return 0

    print("public skill references audit: FAIL")
    if section_failures:
        print("\n[missing required sections]")
        for path, missing in section_failures:
            rel = path.relative_to(ROOT)
            print(f"- {rel}: {', '.join(missing)}")
    if action_failures:
        print("\n[action missing from references]")
        for group, action in action_failures:
            print(f"- {group}: {action}")
    return 1


if __name__ == "__main__":
    sys.exit(main())

"""Audit local candidate banks; this inventory NEVER grants publication approval."""
from __future__ import annotations

import argparse
import hashlib
import json
from collections import Counter
from pathlib import Path

PRODUCT_ROOT = Path(__file__).resolve().parents[1]
CANDIDATES = (
    'computer_fundamentals.json',
    'java_bank_analyzed_reviewed.json',
    'web_bank_analyzed.json',
    'software_engineering_process_tests.json',
)
OUTPUT = PRODUCT_ROOT / 'generated' / 'learning_feedback_inventory.json'


def inspect_bank(path: Path) -> dict:
    raw = path.read_bytes()
    data = json.loads(raw)
    if not isinstance(data, dict) or not isinstance(data.get('questions'), list):
        raise ValueError(f'{path.name}: expected an object with a questions list')
    questions = data['questions']
    forms, ids, missing = Counter(), Counter(), Counter()
    chapters, fields = set(), set()
    for question in questions:
        if not isinstance(question, dict):
            raise ValueError(f'{path.name}: question must be an object')
        form = question.get('type')
        if form not in ('single', 'multi', 'judge', 'blank'):
            raise ValueError(f'{path.name}: unsupported answer form {form!r}')
        forms[form] += 1
        fields.update(question)
        for field in ('id', 'type', 'chapter_id', 'answer'):
            value = question.get(field)
            if value is None or value == '' or value == []:
                missing[field] += 1
        if question.get('id') is not None:
            ids[str(question['id'])] += 1
        if question.get('chapter_id') is not None:
            chapters.add(str(question['chapter_id']))
    duplicates = sorted(key for key, count in ids.items() if count > 1)
    meta = data.get('meta', {})
    if not isinstance(meta, dict):
        raise ValueError(f'{path.name}: meta must be an object')
    blockers = [
        'human_taxonomy_review_pending',
        'approved_lessons_missing',
        'source_usage_review_pending',
        'production_mapping_unverified',
    ]
    if not questions:
        blockers.append('empty_bank')
    if duplicates:
        blockers.append('duplicate_question_ids')
    if missing:
        blockers.append('missing_required_fields')
    if meta.get('total') != len(questions):
        blockers.append('metadata_count_mismatch')
    return {
        'source_file': path.name,
        'source_sha256': hashlib.sha256(raw).hexdigest(),
        'question_count': len(questions),
        'declared_question_count': meta.get('total'),
        'answer_forms': dict(sorted(forms.items())),
        'chapter_count': len(chapters),
        'question_fields': sorted(fields),
        'duplicate_question_ids': duplicates,
        'missing_fields': dict(sorted(missing.items())),
        'ready_for_learning_feedback': False,
        'blockers': blockers,
    }


def build_inventory(root: Path, names=CANDIDATES) -> dict:
    return {
        'schema_version': 1,
        'scope': 'Local candidates only; not production or human approval evidence.',
        'candidates': [inspect_bank(root / name) for name in names],
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument('--check', action='store_true', help='fail if committed inventory has drifted')
    mode.add_argument('--write', action='store_true', help='write the deterministic candidate inventory')
    args = parser.parse_args()
    text = json.dumps(build_inventory(PRODUCT_ROOT / 'generated'), ensure_ascii=False, indent=2) + chr(10)
    if args.check:
        if not OUTPUT.is_file() or OUTPUT.read_text() != text:
            parser.exit(1, 'Inventory drift: run learning_feedback_inventory.py --write and review.' + chr(10))
        print('Learning feedback candidate inventory is current; all publication gates remain closed.')
    elif args.write:
        OUTPUT.write_text(text)
        print(f'Wrote {OUTPUT.relative_to(PRODUCT_ROOT)}')
    else:
        print(text, end='')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())

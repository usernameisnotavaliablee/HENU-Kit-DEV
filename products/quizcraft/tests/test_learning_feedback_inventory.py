import hashlib
import json
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
from learning_feedback_inventory import inspect_bank, build_inventory


class LearningFeedbackInventoryTests(unittest.TestCase):
    def write_bank(self, root, name='bank.json', questions=None, total=None):
        questions = questions if questions is not None else [
            {'id': 'q1', 'type': 'judge', 'chapter_id': 'c1', 'answer': False},
            {'id': 'q2', 'type': 'blank', 'chapter_id': 'c1', 'answer': ['NA']},
        ]
        path = root / name
        path.write_text(json.dumps({'meta': {'total': len(questions) if total is None else total}, 'questions': questions}))
        return path

    def test_inventory_is_deterministic_and_never_implies_approval(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            path = self.write_bank(root, 'already_reviewed.json')
            result = inspect_bank(path)
            self.assertEqual(result, inspect_bank(path))
            self.assertEqual(result['source_sha256'], hashlib.sha256(path.read_bytes()).hexdigest())
            self.assertEqual(result['question_count'], 2)
            self.assertEqual(result['answer_forms'], {'blank': 1, 'judge': 1})
            self.assertEqual(result['missing_fields'], {})
            self.assertFalse(result['ready_for_learning_feedback'])
            self.assertIn('human_taxonomy_review_pending', result['blockers'])
            self.assertIn('approved_lessons_missing', result['blockers'])
            self.assertIn('production_mapping_unverified', result['blockers'])

    def test_duplicates_missing_answers_and_metadata_mismatch_are_visible(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = self.write_bank(Path(tmp), questions=[
                {'id': 'q1', 'type': 'single', 'chapter_id': 'c1', 'answer': 'A'},
                {'id': 'q1', 'type': 'single', 'chapter_id': 'c1'},
            ], total=5)
            result = inspect_bank(path)
            self.assertEqual(result['duplicate_question_ids'], ['q1'])
            self.assertEqual(result['missing_fields'], {'answer': 1})
            for reason in ['duplicate_question_ids', 'missing_required_fields', 'metadata_count_mismatch']:
                self.assertIn(reason, result['blockers'])

    def test_invalid_shapes_and_unknown_answer_forms_fail_explicitly(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / 'broken.json'
            for value in [[], {'questions': 'bad'}, {'questions': [None]}, {'questions': [{'type': 'code'}]}]:
                path.write_text(json.dumps(value))
                with self.assertRaises(ValueError):
                    inspect_bank(path)

    def test_build_checks_exact_candidate_files_and_does_not_change_sources(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            path = self.write_bank(root)
            before = path.read_bytes()
            result = build_inventory(root, ['bank.json'])
            self.assertEqual(result['schema_version'], 1)
            self.assertEqual(len(result['candidates']), 1)
            self.assertEqual(path.read_bytes(), before)
            with self.assertRaises(FileNotFoundError):
                build_inventory(root, ['missing.json'])


if __name__ == '__main__':
    unittest.main()

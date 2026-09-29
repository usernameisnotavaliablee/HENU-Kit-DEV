import json
import subprocess
import shutil
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
CONTRACT = ROOT / 'packages/api-contracts/openapi/quizcraft.yaml'
PREFIX = '/api/v1/portal/practice/banks/{bank_id}/learning-reports'


@unittest.skipUnless(shutil.which("ruby"), "Offline check needs Ruby/Psych; canonical OpenAPI validation remains required")
class LearningReportContractTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        # System Ruby/Psych is an offline syntax reader, not the production OpenAPI validator.
        result = subprocess.run(['ruby', '-rjson', '-ryaml', '-e',
            'puts JSON.generate(YAML.safe_load(File.read(ARGV[0]), [], [], true))',
            str(CONTRACT)], check=True, capture_output=True, text=True)
        cls.spec = json.loads(result.stdout)

    def test_actor_is_required_and_only_service_signatures_authorize_reports(self):
        for suffix, methods in {
            '/preferences': ['get', 'put'], '': ['post', 'delete'],
            '/latest': ['get'], '/tasks/{task_id}': ['get'],
            '/{report_id}/practice-sessions': ['post'],
        }.items():
            for method in methods:
                op = self.spec['paths'][PREFIX + suffix][method]
                self.assertTrue(op['x-internal'])
                self.assertIn('portalPracticeSignature', op['security'][0])
                self.assertNotIn({}, op['security'])
                self.assertTrue(any(p.get('name') == 'X-Actor-User-Id' and p.get('required') for p in op['parameters']))

    def test_defaults_are_weekly_and_bounds_match_plan(self):
        schema = self.spec['components']['schemas']['LearningReportPreferencesUpdate']
        days = schema['properties']['interval_days']
        self.assertEqual((days['default'], days['minimum'], days['maximum']), (7, 1, 30))
        self.assertFalse(schema['properties']['enabled']['default'])
        self.assertIn('external_analysis_consent', schema['required'])
        self.assertNotIn('user_id', schema['properties'])

    def test_manual_requests_support_dedupe_async_and_abuse_limit_not_credits(self):
        op = self.spec['paths'][PREFIX]['post']
        self.assertTrue({'200', '202', '429', '503'}.issubset(op['responses']))
        self.assertTrue(any(p.get('$ref', '').endswith('/IdempotencyKey') for p in op['parameters']))
        self.assertIn('no daily quota', op['description'])

    def test_all_local_refs_and_operation_ids_are_valid(self):
        ids = []
        for path in self.spec['paths'].values():
            for op in path.values():
                if isinstance(op, dict) and 'operationId' in op:
                    ids.append(op['operationId'])
        self.assertEqual(len(ids), len(set(ids)))

        def walk(value):
            if isinstance(value, dict):
                if value.get('$ref', '').startswith('#/'):
                    target = self.spec
                    for part in value['$ref'][2:].split('/'):
                        target = target[part]
                for item in value.values():
                    walk(item)
            elif isinstance(value, list):
                for item in value:
                    walk(item)
        walk(self.spec)


if __name__ == '__main__':
    unittest.main()

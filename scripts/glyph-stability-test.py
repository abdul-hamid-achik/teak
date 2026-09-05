import copy
import json
import os
import runpy
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

gate = runpy.run_path(str(Path(__file__).with_name('glyph-stability.py')))
validate = gate['validate']


class StabilityGateTest(unittest.TestCase):
    def test_rejects_missing_screen_even_when_probe_reports_stable(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            runs = [root / str(i) for i in range(3)]
            for run in runs:
                run.mkdir()
                (run / 'run.json').write_text(json.dumps({
                    'artifacts': {'finalScreenText': 'screens/final.txt'}}))
            report = {'schemaVersion': 1, 'runsPerSpec': 3, 'results': [{
                'spec': 'fixture.yml', 'runs': 3, 'passed': 3, 'failed': 0,
                'stable': True, 'flaky': False, 'runDirs': list(map(str, runs))}]}

            def probe(*args, stdout, **kwargs):
                json.dump(report, stdout)
                return SimpleNamespace(returncode=0)

            previous = Path.cwd()
            try:
                os.chdir(root)
                with patch('subprocess.run', side_effect=probe):
                    with self.assertRaisesRegex(ValueError, '1 spec probes failed'):
                        gate['main'](['fixture.yml'])
            finally:
                os.chdir(previous)

    def test_requires_complete_stable_success(self):
        good = {'schemaVersion': 1, 'runsPerSpec': 3, 'results': [
            {'spec': 'fixture.yml', 'runs': 3, 'passed': 3, 'failed': 0,
             'stable': True, 'flaky': False, 'runDirs': ['one', 'two', 'three']}]}
        validate(good, 'fixture.yml')
        for field, value in [('stable', False), ('flaky', True), ('failed', 1),
                             ('passed', 2), ('runs', 2), ('spec', 'other.yml'),
                             ('runDirs', ['one', 'two'])]:
            with self.subTest(field=field):
                report = copy.deepcopy(good)
                report['results'][0][field] = value
                with self.assertRaises(ValueError):
                    validate(report, 'fixture.yml')
        for report in [{}, dict(good, results=[]), dict(good, runsPerSpec=1)]:
            with self.subTest(report=report):
                with self.assertRaises(ValueError):
                    validate(report, 'fixture.yml')


if __name__ == '__main__':
    unittest.main()

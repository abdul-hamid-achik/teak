#!/usr/bin/env python3
"""Probe each spec with retained evidence and reject unstable screen reports."""
import json
import subprocess
import sys
from pathlib import Path


def validate(report, spec_name):
    if report.get('schemaVersion') != 1 or report.get('runsPerSpec') != 3:
        raise ValueError('expected a version 1, three-run probe')
    results = report.get('results', [])
    if len(results) != 1:
        raise ValueError('expected exactly one spec result')
    result = results[0]
    if (result.get('spec') != spec_name or result.get('runs') != 3
            or result.get('passed') != 3 or result.get('failed') != 0
            or result.get('stable') is not True or result.get('flaky') is not False):
        raise ValueError(f'unstable or unsuccessful probe: {result}')
    if len(set(result.get('runDirs', []))) != 3:
        raise ValueError('probe must identify three distinct evidence directories')


def main(specs):
    if not specs:
        raise ValueError('pass one or more Glyphrun spec paths')
    failures = []
    for spec in specs:
        try:
            # Retention defaults to three runs per root. Sharing a root across
            # specs can delete the screens before Glyphrun compares iterations.
            root = Path('.glyphrun/runs/stability') / Path(spec).stem
            root.mkdir(parents=True, exist_ok=True)
            report_path = root / 'probe.json'
            with report_path.open('w') as output:
                result = subprocess.run(['glyph', 'run', spec, '--parallel', '1',
                                         '--repeat', '3', '--format', 'json',
                                         '--artifact-root', str(root)], stdout=output)
            if result.returncode:
                raise ValueError(f'{spec}: Glyphrun exited {result.returncode}; see {report_path}')
            report = json.loads(report_path.read_text())
            validate(report, Path(spec).name)
            for directory in report['results'][0]['runDirs']:
                run = json.loads((Path(directory) / 'run.json').read_text())
                screen = run.get('artifacts', {}).get('finalScreenText')
                # Specs with finalScreen: never compare outcomes only. When a
                # screen is advertised, a missing file must not look stable.
                if screen and not (Path(directory) / screen).is_file():
                    raise ValueError(f'{spec}: missing final screen in {directory}')
            print(f'PASS {spec}: 3/3 stable; {report_path}', flush=True)
        except (OSError, ValueError) as error:
            failures.append(str(error))
            print(f'FAIL {spec}: {error}', file=sys.stderr, flush=True)
    if failures:
        raise ValueError(f'{len(failures)} spec probes failed; see reports above')



if __name__ == '__main__':
    try:
        main(sys.argv[1:])
    except (OSError, ValueError) as error:
        print(f'FAIL stability gate: {error}', file=sys.stderr)
        sys.exit(1)

#!/usr/bin/env python3
"""A failed workflow command must expose its JSON response and exit status."""
import pathlib
import subprocess
import tempfile

repo = pathlib.Path(__file__).resolve().parents[1]
with tempfile.TemporaryDirectory() as temporary:
    root = pathlib.Path(temporary)
    fixture = root / 'testdata/glyphrun/headless-workflow-fixture.sh'
    fixture.parent.mkdir(parents=True)
    fixture.write_bytes((repo / fixture.relative_to(root)).read_bytes())
    binary = root / 'bin/teak'
    binary.parent.mkdir()
    binary.write_text('#!/bin/sh\nprintf \'%s\\n\' \'{"state":"timed_out","detail":"fixture failure"}\'\nexit 7\n')
    binary.chmod(0o755)
    result = subprocess.run(['sh', str(fixture)], capture_output=True, text=True)
    assert result.returncode == 7, result
    assert 'buffer read' in result.stderr, result.stderr
    assert '"state":"timed_out"' in result.stderr, result.stderr
    assert 'fixture failure' in result.stderr, result.stderr
    assert 'HEADLESS_WORKFLOW_JSON_OK' not in result.stdout, result.stdout
print('PASS workflow failure diagnostics')

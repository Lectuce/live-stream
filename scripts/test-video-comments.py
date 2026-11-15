#!/usr/bin/env python3
"""Compatibility entry point; see test-content-comments.py for video and post options."""
import runpy
from pathlib import Path

if __name__ == '__main__':
    runpy.run_path(str(Path(__file__).with_name('test-content-comments.py')), run_name='__main__')

"""Verify all Python plugin handlers register themselves in sys.modules.

When Python runs handler.py as __main__, deferred imports like
``from handler import config`` in tools.py create a second module
instance with its own empty config dict. Each handler must register
itself so that ``import handler`` resolves to the running instance.
"""

from pathlib import Path

_plugins_dir = Path(__file__).resolve().parent
_PATTERNS = [
    'sys.modules.setdefault("handler"',
    'sys.modules["handler"]',
    "sys.modules.setdefault('handler'",
    "sys.modules['handler']",
    '_sys.modules["handler"]',
    "_sys.modules['handler']",
]


def test_all_python_handlers_register_in_sys_modules():
    """Every Python handler.py must self-register."""
    missing = []
    for plugin_dir in sorted(_plugins_dir.iterdir()):
        handler = plugin_dir / "handler.py"
        if not handler.is_file():
            continue
        content = handler.read_text()
        if not any(p in content for p in _PATTERNS):
            missing.append(plugin_dir.name)
    assert not missing, (
        f"These plugin handlers are missing sys.modules self-registration: {missing}. "
        f'Add: if __name__ == "__main__": {_PATTERNS[0]}...)'
    )

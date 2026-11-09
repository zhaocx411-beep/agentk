import tomllib
from pathlib import Path

from setuptools import find_namespace_packages, setup

PROJECT_DIR = Path(__file__).parent
PROJECT = tomllib.loads((PROJECT_DIR / "pyproject.toml").read_text())["project"]


setup(
    name=PROJECT["name"],
    version=PROJECT["version"],
    description=PROJECT["description"],
    python_requires=PROJECT["requires-python"],
    install_requires=PROJECT["dependencies"],
    package_dir={"": "src"},
    packages=find_namespace_packages(where="src"),
    setup_requires=["wheel"],
)

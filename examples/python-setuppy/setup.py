from setuptools import setup, find_packages

setup(
    name='myapp',
    version='1.0.0',
    packages=find_packages(),
    install_requires=[
        'flask>=3.0',
        'gunicorn>=21.0',
    ],
    python_requires='>=3.9',
    entry_points={
        'console_scripts': [
            'myapp=myapp.cli:main',
        ],
    },
)

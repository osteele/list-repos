# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed
- Handle non-git jj repos gracefully in status check (2025-11-10)

### Added
- Improve jujutsu status checking and error reporting (2025-11-04)
- Expression-based filtering and sorting capabilities (2025-09-15)
- Parallel directory status collection for improved performance (2025-09-15)
- CI workflow for automated testing (2025-09-14)

### Documentation
- Introduce expression-based filtering and sorting in documentation (2025-09-15)

## [0.1.0] - 2025-09-14

### Added
- Initial release
- Scan subdirectories for Git and Jujutsu repositories
- Report repository status (dirty, remote, ahead)
- Display results in formatted table
- Support for both Git and Jujutsu version control systems

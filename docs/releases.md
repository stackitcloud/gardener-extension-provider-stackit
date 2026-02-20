# Release Procedure

## Overview

This document outlines the standard procedure for creating new releases of the gardener-extension-provider-stackit.

## General Information

- **Versioning:** Versioning follows official [SemVer 2.0](https://semver.org/)
- **CI/CD System:** All release and image builds are managed by our **Prow CI** infrastructure.

## Automated Release Process (Primary Method)

The primary release method is automated using a tool called `release-tool`. This process is designed to be straightforward and require minimal manual intervention.

1. **Draft Creation:** On every successful merge (post-submit) to the `main` branch, a Prow job automatically runs the `release-tool`. This tool creates a new draft release on GitHub or updates the existing one with a changelog generated from recent commits.
2. **Publishing the Release:** When the draft is ready, navigate to the repository's "Releases" page on GitHub. Locate the draft, review the changelog, replace the placeholder with your GitHub handle and publish it by clicking the "Publish release" button.

Publishing the release automatically creates the corresponding Git tag (e.g., `v1.3.1`), which triggers a separate Prow job to build the final container images and attach them to the GitHub release.

## Manual Release Process (Fallback Method)

If the `release-tool` or its associated Prow job fails, use the GitHub web UI to create and publish a release:

1. Go to the repository on GitHub and click **Releases** on the right side, then click **Draft new release**.

2. Open the **Select tag** dropdown and choose **Create new tag** at the bottom. Enter the new tag name (for example `v2.1.0`) and pick the target branch/commit, then confirm.

3. Click **Generate release notes** to let GitHub populate the changelog.

4. In the release description, add a line `Released by @<your github handle>` to indicate the publisher.

5. Click **Publish release** to create the release.

Publishing a new release triggers the same Prow release job that builds and publishes the final container images.

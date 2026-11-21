# Releasing

Pushing a version tag publishes the image to GHCR: [`.github/workflows/publish-image.yml`](../.github/workflows/publish-image.yml) builds `linux/amd64` and `linux/arm64` and pushes both, as one multi-arch tag, to `ghcr.io/crimsoncowlabs/vinyl-underground`.

## Cutting a release

1. On an up-to-date `main`, tag the commit with a semver tag and push the tag:

   ```sh
   git tag -a v0.1.0 -m "v0.1.0"
   git push origin v0.1.0
   ```

2. Watch the **Publish image** run under the repo's Actions tab. It takes a while, as arm64's final stage runs under QEMU.

The tag sets the image tags and the version the binary reports (`v0.1.0` reports `0.1.0 (<sha>)`). Once both platforms are pushed, the workflow pulls each one and runs `--version`.

| Git tag       | Image tags                                       |
| ------------- | ------------------------------------------------ |
| `v0.1.0`      | `0.1.0`, `0.1`, `latest`                         |
| `v1.2.3`      | `1.2.3`, `1.2`, `1`, `latest`                    |
| `v1.3.0-rc.1` | `1.3.0-rc.1` (a prerelease never moves `latest`) |

Only tags shaped like `vX.Y.Z` (with an optional suffix) start the workflow.

## Dry run

Run **Publish image** by hand (Actions tab, **Run workflow**) to build both platforms from any branch without pushing anything. The binary then reports the last tag plus `-SNAPSHOT`.

## One-time: make the package public

GHCR creates the package on the first push, and a package created by an organization starts **private**. A workflow's `GITHUB_TOKEN` can't change that, so after the first release, once:

1. Open the org's **Packages** tab and pick `vinyl-underground` (it is linked to this repo by the image's `org.opencontainers.image.source` label).
2. **Package settings** → **Danger Zone** → **Change visibility** → **Public**, and confirm with the package's name.

If the org forbids public packages, an org owner first allows them under the org's **Settings** → **Packages** → **Package creation**.

Then check that a pull works with no login:

```sh
docker logout ghcr.io
docker pull ghcr.io/crimsoncowlabs/vinyl-underground:latest
```

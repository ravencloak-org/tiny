# Installing TinyRaven

TinyRaven ships a single binary, `tr`, packaged as `tinyraven` across as many
package managers as possible. Every artifact for a release is built by
[GoReleaser](../.goreleaser.yaml) on each `v*` tag and published to the
[GitHub Releases](https://github.com/ravencloak-org/tiny/releases) page; the APT
and RPM repositories are GPG-signed and hosted on GitHub Pages.

> Package name is always `tinyraven`; the binary is always `tr`. We never ship a
> `tb` (that's the Tinybird CLI) to avoid collisions.

- [Homebrew (macOS / Linux)](#homebrew-macos--linux)
- [APT (Debian / Ubuntu)](#apt-debian--ubuntu)
- [DNF / YUM (RHEL / Fedora)](#dnf--yum-rhel--fedora)
- [Scoop (Windows)](#scoop-windows)
- [WinGet (Windows)](#winget-windows)
- [AUR (Arch Linux)](#aur-arch-linux)
- [Nix](#nix)
- [Docker](#docker)
- [Raw binary download](#raw-binary-download)
- [Verifying release checksums (GPG)](#verifying-release-checksums-gpg)

---

## Homebrew (macOS / Linux)

GoReleaser pushes a formula to the tap repo
[`ravencloak-org/homebrew-tinyraven`](https://github.com/ravencloak-org/homebrew-tinyraven)
on every release.

```bash
brew tap ravencloak-org/tinyraven   # add the tap once
brew install tinyraven              # installs the `tr` binary
brew upgrade tinyraven              # later, to update
```

### Understanding the Homebrew naming

There are three ways the name can appear, and only the last two work for us today:

| Command | Works for TinyRaven? | Why |
|---------|----------------------|-----|
| `brew install tinyraven` (bare) | ❌ not yet | Bare names resolve against **homebrew-core**. TinyRaven isn't in core. |
| `brew install ravencloak-org/tinyraven/tinyraven` | ✅ | Fully-qualified `owner/tap/formula` — always works, no tap step. |
| `brew tap ravencloak-org/tinyraven` then `brew install tinyraven` | ✅ **recommended** | Tapping registers the repo so the short name resolves locally. |

The tap repo `ravencloak-org/homebrew-tinyraven` is referenced as
`ravencloak-org/tinyraven` (Homebrew drops the `homebrew-` prefix). Submitting the
formula to **homebrew-core** — which would make the bare `brew install tinyraven`
work for everyone — is a future option once the project is past pre-alpha.

---

## APT (Debian / Ubuntu)

We host a signed APT repository on GitHub Pages (suite `stable`, component `main`,
architectures `amd64` + `arm64`). Add the key and repo once:

```bash
# 1. Trust the signing key (dearmored into a dedicated keyring).
curl -fsSL https://ravencloak-org.github.io/tiny/apt/KEY.gpg \
  | sudo gpg --dearmor -o /usr/share/keyrings/tinyraven.gpg

# 2. Add the repo, pinned to that keyring (signed-by).
echo "deb [signed-by=/usr/share/keyrings/tinyraven.gpg] https://ravencloak-org.github.io/tiny/apt stable main" \
  | sudo tee /etc/apt/sources.list.d/tinyraven.list

# 3. Install.
sudo apt-get update
sudo apt-get install tinyraven
```

Updates then arrive through normal `apt-get update && apt-get upgrade`.

> The repo's `Release` file is signed both as `Release.gpg` (detached) and
> `InRelease` (inline), so old and modern apt clients both verify it. `signed-by=`
> scopes trust to this repo only — the key can't vouch for any other source.

**One-off `.deb` (no repo):**

```bash
curl -fsSL -O https://github.com/ravencloak-org/tiny/releases/latest/download/tinyraven_<ver>_linux_amd64.deb
sudo apt-get install ./tinyraven_<ver>_linux_amd64.deb
```

---

## DNF / YUM (RHEL / Fedora)

A signed yum/dnf repository is published alongside the APT repo:

```bash
sudo curl -fsSL https://ravencloak-org.github.io/tiny/rpm/tinyraven.repo \
  -o /etc/yum.repos.d/tinyraven.repo
sudo dnf install tinyraven        # or: sudo yum install tinyraven
```

The `.repo` enables both `gpgcheck` (package signatures) and `repo_gpgcheck`
(repository metadata signature), pointing at
`https://ravencloak-org.github.io/tiny/rpm/KEY.gpg`.

**One-off `.rpm` (no repo):**

```bash
sudo rpm -i https://github.com/ravencloak-org/tiny/releases/latest/download/tinyraven_<ver>_linux_amd64.rpm
```

---

## Scoop (Windows)

GoReleaser publishes a manifest to the bucket
[`ravencloak-org/scoop-bucket`](https://github.com/ravencloak-org/scoop-bucket).

```powershell
scoop bucket add tinyraven https://github.com/ravencloak-org/scoop-bucket
scoop install tinyraven
scoop update tinyraven
```

---

## WinGet (Windows)

Each release opens/updates a PR to `microsoft/winget-pkgs` for
`Ravencloak.TinyRaven`. Once merged:

```powershell
winget install Ravencloak.TinyRaven
```

---

## AUR (Arch Linux)

The prebuilt `-bin` package is published to the AUR as
[`tinyraven-bin`](https://aur.archlinux.org/packages/tinyraven-bin):

```bash
yay -S tinyraven-bin      # or: paru -S tinyraven-bin
```

It installs the release binary to `/usr/bin/tr` and `provides`/`conflicts`
`tinyraven`.

---

## Nix

A package definition is published to the NUR-style repo
[`ravencloak-org/nur`](https://github.com/ravencloak-org/nur):

```bash
nix profile install github:ravencloak-org/nur#tinyraven
# or, ad-hoc:
nix run github:ravencloak-org/nur#tinyraven -- --version
```

---

## Docker

Images are pushed to GHCR on every tag (`latest` + the version tag):

```bash
docker run -p 8000:8000 ghcr.io/ravencloak-org/tiny:latest serve
docker run -p 8000:8000 ghcr.io/ravencloak-org/tiny:v0.1.0 serve
```

TinyRaven is stateless — it expects an external **ClickHouse 26.3** and **Redis**.
See [docs/deploy/docker.md](deploy/docker.md) for a full Compose setup.

---

## Raw binary download

Archives are built for Linux/macOS/Windows × amd64/arm64.

```bash
# Linux amd64 example — substitute your OS/arch and <ver>.
curl -fsSL -o tinyraven.tar.gz \
  https://github.com/ravencloak-org/tiny/releases/latest/download/tinyraven_<ver>_linux_amd64.tar.gz
tar -xzf tinyraven.tar.gz
sudo install tr /usr/local/bin/tr
tr --version
```

On Windows, download the `..._windows_amd64.zip`, unzip, and put `tr.exe` on your `PATH`.

---

## Verifying release checksums (GPG)

Every release includes `checksums.txt` and a detached signature
`checksums.txt.sig`, signed with the TinyRaven release key (the same key that
signs the APT/RPM repos).

```bash
# 1. Import the public key (from the APT repo mirror of the same key).
curl -fsSL https://ravencloak-org.github.io/tiny/apt/KEY.gpg | gpg --import

# 2. Download the checksums file, its signature, and your artifact.
base=https://github.com/ravencloak-org/tiny/releases/latest/download
curl -fsSL -O "$base/checksums.txt"
curl -fsSL -O "$base/checksums.txt.sig"
curl -fsSL -O "$base/tinyraven_<ver>_linux_amd64.tar.gz"

# 3. Verify the signature on the checksums file…
gpg --verify checksums.txt.sig checksums.txt

# 4. …then verify your artifact against the (now-trusted) checksums.
sha256sum --ignore-missing -c checksums.txt
```

A `Good signature` line in step 3 plus an `OK` in step 4 means the artifact is
authentic and intact.

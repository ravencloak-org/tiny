# Homebrew formula TEMPLATE for TinyRaven.
#
# This is a hand-written reference. In normal operation you do NOT edit this by
# hand: the `brews:` block in .goreleaser.yaml generates the real formula and
# pushes it to the tap repo (ravencloak-org/homebrew-tinyraven) on every
# release. Users then run:
#
#   brew tap ravencloak-org/tinyraven
#   brew install tinyraven          # installs the `tr` binary
#
# The VERSION / URL / SHA256 placeholders below are filled in automatically by
# GoReleaser. Keep this file only as documentation / a manual-publish fallback.
class Tinyraven < Formula
  desc "Open-source, self-hosted, drop-in alternative to Tinybird (binary: tr)"
  homepage "https://github.com/ravencloak-org/tiny"
  version "0.0.0" # replaced by GoReleaser with the release tag
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/ravencloak-org/tiny/releases/download/v#{version}/tinyraven_#{version}_darwin_arm64.tar.gz"
      sha256 "REPLACED_BY_GORELEASER_DARWIN_ARM64"
    end
    on_intel do
      url "https://github.com/ravencloak-org/tiny/releases/download/v#{version}/tinyraven_#{version}_darwin_amd64.tar.gz"
      sha256 "REPLACED_BY_GORELEASER_DARWIN_AMD64"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/ravencloak-org/tiny/releases/download/v#{version}/tinyraven_#{version}_linux_arm64.tar.gz"
      sha256 "REPLACED_BY_GORELEASER_LINUX_ARM64"
    end
    on_intel do
      url "https://github.com/ravencloak-org/tiny/releases/download/v#{version}/tinyraven_#{version}_linux_amd64.tar.gz"
      sha256 "REPLACED_BY_GORELEASER_LINUX_AMD64"
    end
  end

  def install
    bin.install "tr"
  end

  test do
    assert_match "tr", shell_output("#{bin}/tr --version")
  end
end
